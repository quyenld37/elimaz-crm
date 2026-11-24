// Package weborders ghép view chỉ đọc của ord và fulfillment cho đơn web của website (kế hoạch M5, Task 196 / M5-13):
// phần vận đơn khách được thấy, luồng thay đổi đơn web (đơn hay trạng thái vận đơn hiện hành).
//
// Gói không ghi gì, không nhập module nào (luật f của tests/arch/boundaries_test) và chỉ nhắc view _v đã công bố
// (tests/arch/sqlusage_test). Mọi hàm nhận tx của tenancy.WithTenant (crm_app). Không có địa chỉ, SĐT, tên.
//
// Khi nào không dùng: phần của đơn (dòng, tiền, số còn đổi được, hủy) đọc bằng module order (order.GetWebOrder);
// danh sách đơn của nhân viên (internal/queries).
package weborders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries/orderview"
	"elimaz.com.vn/crm/internal/queries/weborders/internal/store"
)

// MaxChanges là số dòng tối đa một trang của Changes.
const MaxChanges = 500

// ErrBadLimit báo limit ngoài [1, MaxChanges].
var ErrBadLimit = errors.New("weborders: limit ngoài khoảng")

// ErrNotFound báo không có vận đơn (của người bán, tenant).
var ErrNotFound = errors.New("weborders: không có vận đơn")

// Shipment là phần khách được thấy của vận đơn: trạng thái vận đơn, hãng (khóa và tên tài khoản hãng), mã tra cứu (""
// khi hãng chưa cấp), lúc lấy hàng, lúc giao.
type Shipment struct {
	Status, CarrierKey, CarrierName, TrackingCode string
	PickedUpAt, DeliveredAt                       *time.Time
}

// ShipmentOf trả phần khách được thấy của vận đơn id của người bán sellerID; không có → ErrNotFound.
func ShipmentOf(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Shipment, error) {
	r, err := store.New(tx).WebShipment(ctx, store.WebShipmentParams{ID: id, SellerID: sellerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, ErrNotFound
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("đọc vận đơn của đơn web: %w", err)
	}
	return Shipment{
		Status: r.Status, CarrierKey: r.CarrierKey, CarrierName: r.CarrierName, TrackingCode: r.TrackingCode,
		PickedUpAt: utcPtr(r.PickedUpAt), DeliveredAt: utcPtr(r.DeliveredAt),
	}, nil
}

// Change là một đơn web vừa đổi (đơn hay trạng thái vận đơn hiện hành); ChangedAt là lúc muộn hơn của hai.
// DisplayStatus là orderview.CustomerLabel; ShipmentStatus "" khi chưa có vận đơn hiện hành; DeliveredAt là lúc vận
// đơn hiện hành giao xong.
type Change struct {
	ID                                                 uuid.UUID
	ExternalRef, Status, DisplayStatus, ShipmentStatus string
	DeliveredAt                                        *time.Time
	ChangedAt                                          time.Time
}

// Cursor là con trỏ keyset (ChangedAt, ID) của Changes: trả đơn có (changed_at, id) > (Since, AfterID); AfterID rỗng
// lấy cả đơn đúng mốc Since.
type Cursor struct {
	Since   time.Time
	AfterID uuid.UUID
}

// Page là một trang của Changes: Next là con trỏ của dòng cuối (bằng con trỏ vào khi trang rỗng); HasMore còn dòng sau
// Next.
type Page struct {
	Items   []Change
	Next    Cursor
	HasMore bool
}

// Changes trả tối đa limit (1–MaxChanges, khác → ErrBadLimit) đơn web của người bán sellerID đổi sau con trỏ c, xếp
// (ChangedAt, ID) tăng dần.
func Changes(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, c Cursor, limit int) (Page, error) {
	if limit < 1 || limit > MaxChanges {
		return Page{}, ErrBadLimit
	}
	rows, err := store.New(tx).WebOrderChanges(ctx, store.WebOrderChangesParams{
		Since: c.Since.UTC(), AfterID: c.AfterID, Lim: int32(limit + 1), SellerID: sellerID,
	})
	if err != nil {
		return Page{}, fmt.Errorf("đọc đơn web đã đổi: %w", err)
	}
	n := min(len(rows), limit)
	p := Page{Next: c, HasMore: len(rows) > limit, Items: make([]Change, 0, n)}
	for _, r := range rows[:n] {
		p.Items = append(p.Items, Change{
			ID: r.ID, ExternalRef: r.ExternalRef, Status: r.Status, ShipmentStatus: r.ShipmentStatus,
			DisplayStatus: string(orderview.CustomerLabel(orderview.Axes{
				Status: r.Status, PendingReason: r.PendingReason, PackingStatus: r.PackingStatus,
				ShipmentStatus: r.ShipmentStatus,
			})),
			DeliveredAt: utcPtr(r.DeliveredAt), ChangedAt: r.ChangedAt.UTC(),
		})
		p.Next = Cursor{Since: r.ChangedAt.UTC(), AfterID: r.ID}
	}
	return p, nil
}

// utcPtr trả bản UTC của t (nil giữ nil).
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
