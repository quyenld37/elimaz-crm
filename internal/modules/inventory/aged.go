package inventory

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Danh sách "Giữ hàng quá hạn" (kế hoạch M3, Task M3-22; KH09, AT53): chỉ đọc, không nhả hàng, không tạo việc.

const (
	defaultAgedLimit = 50
	maxAgedLimit     = 100
	day              = 24 * time.Hour
)

// farPast là mốc trước mọi state_changed_at: ngưỡng của trạng thái bị lọc bỏ, và con trỏ của trang đầu.
//
//nolint:gochecknoglobals // hằng thời gian, chỉ đọc.
var farPast = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

// AgedRow là một dòng giữ hàng quá hạn. OrderItemID là uuid.Nil khi dòng đơn đã bị xóa (liên kết ON DELETE SET NULL
// của M2; giữ hàng held, in_transit thực tế luôn còn dòng đơn). AgeDays: số ngày tròn từ Since tới now.
type AgedRow struct {
	ReservationID uuid.UUID
	OrderItemID   uuid.UUID
	VariantID     uuid.UUID
	WarehouseID   uuid.UUID
	State         string
	Quantity      int32
	Since         time.Time
	AgeDays       int32
}

// AgedCursor là vị trí sau dòng cuối của một trang (xếp theo Since rồi ReservationID tăng dần: cũ nhất trước).
type AgedCursor struct {
	Since time.Time
	ID    uuid.UUID
}

const agedCursorLen = 8 + len(uuid.UUID{})

// String mã hóa con trỏ thành chuỗi mờ cho API (base64url của micro giây Unix và 16 byte mã giữ hàng).
func (c AgedCursor) String() string {
	b := make([]byte, agedCursorLen)
	binary.BigEndian.PutUint64(b, uint64(c.Since.UnixMicro()))
	copy(b[8:], c.ID[:])
	return base64.RawURLEncoding.EncodeToString(b)
}

// ParseAgedCursor đọc chuỗi do AgedCursor.String tạo; hỏng → ErrInvalidInput.
func ParseAgedCursor(s string) (AgedCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != agedCursorLen {
		return AgedCursor{}, fmt.Errorf("%w: con trỏ giữ hàng quá hạn", ErrInvalidInput)
	}
	c := AgedCursor{Since: time.UnixMicro(int64(binary.BigEndian.Uint64(raw))).UTC()} //nolint:gosec // đảo của String
	copy(c.ID[:], raw[8:])
	return c, nil
}

// AgedPage là trang cần đọc: State rỗng là cả held và in_transit, hoặc đúng StateHeld, StateInTransit; After: trang
// sau (nil: trang đầu); Limit ≤ 0 → 50, tối đa 100.
type AgedPage struct {
	State string
	After *AgedCursor
	Limit int
}

// AgedReservations trả một trang giữ hàng quá hạn tại now, trong tx của bên gọi (tenancy.WithTenant): dòng held có
// state_changed_at ≤ now − heldDays ngày, dòng in_transit có state_changed_at ≤ now − transitDays ngày (đúng ngưỡng là
// quá hạn), ở các kho warehouseIDs (nil: mọi kho; rỗng không nil: không kho nào), cũ nhất trước; kèm con trỏ trang sau
// (nil khi hết). Chỉ đọc: không khóa, không đổi giữ hàng, sổ hay việc nào (KH09: hệ thống không tự trả hàng).
// Ngưỡng ≤ 0, State lạ, Limit > 100 → ErrInvalidInput.
//
// Khi nào không dùng: đọc giữ hàng của một đơn (ReservationStatesByItems); mã đơn, SKU ghép ở internal/queries.
func AgedReservations(ctx context.Context, tx pgx.Tx, now time.Time, heldDays, transitDays int,
	warehouseIDs []uuid.UUID, page AgedPage,
) ([]AgedRow, *AgedCursor, error) {
	if heldDays <= 0 || transitDays <= 0 {
		return nil, nil, fmt.Errorf("%w: ngưỡng giữ hàng quá hạn %d/%d ngày", ErrInvalidInput, heldDays, transitDays)
	}
	limit := page.Limit
	if limit <= 0 {
		limit = defaultAgedLimit
	}
	if limit > maxAgedLimit {
		return nil, nil, fmt.Errorf("%w: limit %d", ErrInvalidInput, limit)
	}
	p := store.AgedReservationsParams{
		HeldBefore: now.Add(-time.Duration(heldDays) * day), TransitBefore: now.Add(-time.Duration(transitDays) * day),
		AllWarehouses: warehouseIDs == nil, WarehouseIds: warehouseIDs, AfterAt: farPast, Lim: int32(limit + 1),
	}
	if p.WarehouseIds == nil {
		p.WarehouseIds = []uuid.UUID{}
	}
	switch page.State {
	case "":
	case StateHeld:
		p.TransitBefore = farPast
	case StateInTransit:
		p.HeldBefore = farPast
	default:
		return nil, nil, fmt.Errorf("%w: trạng thái giữ hàng %q", ErrInvalidInput, page.State)
	}
	if page.After != nil {
		p.AfterAt, p.AfterID = page.After.Since, page.After.ID
	}
	rows, err := store.New(tx).AgedReservations(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc giữ hàng quá hạn: %w", err)
	}
	var next *AgedCursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = &AgedCursor{Since: last.StateChangedAt.UTC(), ID: last.ID}
	}
	out := make([]AgedRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgedRow{
			ReservationID: r.ID, OrderItemID: uuid.UUID(r.OrderItemID.Bytes), VariantID: r.VariantID,
			WarehouseID: r.WarehouseID, State: r.State, Quantity: r.Quantity, Since: r.StateChangedAt,
			AgeDays: int32(now.Sub(r.StateChangedAt) / day), //nolint:gosec // số ngày nhỏ
		})
	}
	return out, next, nil
}
