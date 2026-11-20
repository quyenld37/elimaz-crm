package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Áp kết quả tra lên vận đơn, đơn, tồn (kế hoạch M3, Task M3-08): ApplyTracking (apply_run.go, tác động ở
// apply_effects.go), AttachTrackingCode (apply_attach.go), lịch sử ShipmentEvents. Gọi từ lệnh tra (Task M3-11), tạo
// vận đơn (M3-12), nhân viên ghi tay (M3-14…), trong transaction của bên gọi; không gọi mạng.

// Source là nguồn của sự kiện đem ra quyết định (bí danh của shipstate.Source để gói ngoài dùng được).
type Source = shipstate.Source

// Nguồn sự kiện. Webhook không bao giờ là nguồn: tin webhook chỉ là tín hiệu để tra lại.
const (
	SrcCarrierAPI = shipstate.SrcCarrierAPI
	SrcStaff      = shipstate.SrcStaff
	SrcSystem     = shipstate.SrcSystem
)

// Sự kiện, báo hiệu M3-08 phát.
const (
	// TopicShipmentCreated phát khi hãng nhận vận đơn (AttachTrackingCode: creating → awaiting_pickup).
	TopicShipmentCreated = "shipment.created"
	// signalShipmentChanged: báo hiệu vận đơn đổi (fulfillment.read, phạm vi kho của tài khoản hãng).
	signalShipmentChanged = "shipment.changed"
	// signalCallbackChanged: báo hiệu việc gọi lại đổi (fulfillment.callbacks.work, không phạm vi).
	signalCallbackChanged = "callback_task.changed"
	permShipmentRead      = "fulfillment.read"           // identity.Permission; fulfillment không nhập identity
	permCallbacksWork     = "fulfillment.callbacks.work" // identity.Permission
)

var (
	// ErrStockShort báo tác động lấy hàng, giao xong gặp tồn thực không đủ (bọc cả order.ErrStockShort).
	ErrStockShort = errors.New("fulfillment: tồn thực không đủ khi áp vận đơn")
	// ErrInvalidTracking báo kết quả tra sai dạng (khóa sự kiện rỗng hay dài quá 200, mã hãng rỗng hay dài quá 64).
	ErrInvalidTracking = errors.New("fulfillment: kết quả tra không hợp lệ")
	// ErrInvalidPage báo con trỏ hay cỡ trang của lịch sử vận đơn sai.
	ErrInvalidPage = errors.New("fulfillment: trang lịch sử không hợp lệ")
	// errNotWired: Service thiếu Events hay Queue (đường ghi phát sự kiện).
	errNotWired = errors.New("fulfillment: Service thiếu Events hoặc Queue")
)

// ApplyInput là một kết quả tra của vận đơn ShipmentID: hành trình Tracking, nguồn Source, người làm Actor (Type rỗng
// → theo nguồn: carrier_api → carrier, system → system), giờ tra SyncedAt.
type ApplyInput struct {
	ShipmentID uuid.UUID
	Tracking   Tracking
	Source     shipstate.Source
	Actor      order.Actor
	SyncedAt   time.Time
}

// ApplyReport là kết quả một lần áp: số sự kiện mới chèn (Inserted; sự kiện đã có không tính), áp (Applied), bỏ qua
// (Ignored); trạng thái trước, sau; tác động đã chạy theo thứ tự; loại anomaly mới mở.
type ApplyReport struct {
	Inserted, Applied, Ignored int
	From, To                   shipstate.Status
	Effects                    []shipstate.Effect
	Anomalies                  []string
}

// ShipmentCreated là payload của shipment.created: chỉ mã và số.
type ShipmentCreated struct {
	ShipmentID   uuid.UUID `json:"shipmentId"`
	OrderID      uuid.UUID `json:"orderId"`
	TrackingCode string    `json:"trackingCode"`
	Fee          *int64    `json:"fee,omitempty"`
}

// EventPage là trang của lịch sử vận đơn: After là mã dòng cuối trang trước (nil: từ đầu), Limit 1–100.
type EventPage struct {
	After *uuid.UUID
	Limit int
}

// EventView là một dòng lịch sử vận đơn (không có thân thô). Status rỗng khi sự kiện không chỉ tới trạng thái nào;
// Applied nil với dòng của M1 (chưa ghi quyết định).
type EventView struct {
	ID                     uuid.UUID
	OccurredAt, RecordedAt time.Time
	Source, RawStatus      string
	Status                 Status
	Label                  string
	Applied                *bool
	IgnoredReason          string
}

const maxEventPage = 100

// ShipmentEvents trả trang lịch sử của vận đơn shipmentID, mới nhất trước (occurred_at, id giảm dần), và con trỏ trang
// sau (nil: hết). Không có vận đơn (hay tenant khác) → ErrNotFound; Limit ngoài 1–100 hay After không thuộc vận đơn →
// ErrInvalidPage. Kiểm quyền theo kho do bên gọi làm (Get trả kho).
func ShipmentEvents(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID, p EventPage) ([]EventView, *uuid.UUID, error) {
	if p.Limit < 1 || p.Limit > maxEventPage {
		return nil, nil, fmt.Errorf("%w: cỡ trang %d", ErrInvalidPage, p.Limit)
	}
	q := store.New(tx)
	if _, err := q.ShipmentOrderRef(ctx, shipmentID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("đọc vận đơn: %w", err)
	}
	arg := store.ListShipmentEventsParams{ShipmentID: shipmentID, Lim: int32(p.Limit) + 1}
	if p.After != nil {
		arg.AfterID = pgtype.UUID{Bytes: *p.After, Valid: true}
	}
	rows, err := q.ListShipmentEvents(ctx, arg)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc lịch sử vận đơn: %w", err)
	}
	if p.After != nil && len(rows) == 0 {
		if ok, err := eventOf(ctx, tx, shipmentID, *p.After); err != nil {
			return nil, nil, err
		} else if !ok {
			return nil, nil, fmt.Errorf("%w: con trỏ không thuộc vận đơn", ErrInvalidPage)
		}
	}
	var next *uuid.UUID
	if len(rows) > p.Limit {
		rows = rows[:p.Limit]
		id := rows[len(rows)-1].ID
		next = &id
	}
	out := make([]EventView, 0, len(rows))
	for _, r := range rows {
		v := EventView{
			ID: r.ID, OccurredAt: r.OccurredAt, RecordedAt: r.RecordedAt, Source: r.Source, RawStatus: r.RawStatus,
			Status: Status(r.Status), IgnoredReason: r.IgnoredReason.String,
		}
		if r.Status != "" {
			v.Label = shipstate.Display(shipstate.Status(r.Status), "", "")
		}
		if r.Applied.Valid {
			a := r.Applied.Bool
			v.Applied = &a
		}
		out = append(out, v)
	}
	return out, next, nil
}

// eventOf: dòng id có phải sự kiện của vận đơn shipmentID không (con trỏ trang hợp lệ dù trang sau rỗng).
func eventOf(ctx context.Context, tx pgx.Tx, shipmentID, id uuid.UUID) (bool, error) {
	ok, err := store.New(tx).ShipmentEventExists(ctx, store.ShipmentEventExistsParams{ShipmentID: shipmentID, ID: id})
	if err != nil {
		return false, fmt.Errorf("kiểm con trỏ lịch sử: %w", err)
	}
	return ok, nil
}
