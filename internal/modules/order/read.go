package order

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// WarningPossibleDuplicate là cảnh báo của chi tiết đơn: đơn nằm trong một cặp nghi trùng chưa xử lý (RefID là đơn kia).
const WarningPossibleDuplicate = "possible_duplicate"

// EditScope là các nhóm trường của đơn được sửa ở trạng thái hiện tại (Q25; luật ở order/internal/state.Editable).
// Ghi chú nội bộ luôn thêm được, không nằm ở đây.
type EditScope struct {
	Lines, Discounts, Coupon, Address, ShippingFee, CarrierNote, CareBy, TrafficSource bool
}

// CustomerSummary là khách của đơn: ID là hồ sơ ghi trên đơn; RootID, Code, FullName, Stats là của khách gốc (nhóm sau
// gộp).
type CustomerSummary struct {
	ID, RootID uuid.UUID
	Code       string
	FullName   customer.Name
	Stats      customer.Stats
}

// ReturnSummary là một phiếu trả của đơn.
type ReturnSummary struct {
	ID                 uuid.UUID
	Code, Kind, Status string
}

// OrderDetail là chi tiết một đơn: Order (như Get, kèm Version), nhãn gộp, phạm vi được sửa, khách, mã giảm giá đang
// dùng ("" khi không), cảnh báo đơn nghi trùng chưa xử lý, phiếu trả (cũ trước).
type OrderDetail struct { //nolint:revive // tên theo Interfaces của kế hoạch M2 (Task M2-24), task sau dùng
	Order
	Label      orderview.LabelKey
	EditScope  EditScope
	Customer   CustomerSummary
	CouponCode string
	Warnings   []Warning
	Returns    []ReturnSummary
}

// GetDetail trả chi tiết đơn id của người bán sellerID (ADR-29); không có (hay của người bán, tenant khác) →
// ErrNotFound. Nhãn và phạm vi sửa tính trên ba trục M2 (chưa có trục vận đơn: đơn có vận đơn hiện hành coi như
// trục vận đơn chưa biết, phạm vi sửa vẫn khóa theo Q25). Get (M1) giữ nguyên cho người gọi cũ.
func GetDetail(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (OrderDetail, error) {
	o, err := Get(ctx, tx, sellerID, id)
	if err != nil {
		return OrderDetail{}, err
	}
	axes := orderview.Axes{Status: o.Status, PendingReason: o.PendingReason, PackingStatus: o.PackingStatus}
	d := OrderDetail{Order: o, Label: orderview.Label(axes)}
	scopeAxes := axes
	if o.CurrentShipmentID != nil {
		scopeAxes.ShipmentStatus = "attached"
	}
	d.EditScope = EditScope(state.Editable(scopeAxes))

	p, err := customer.Get(ctx, tx, o.CustomerID)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("đọc khách của đơn: %w", err)
	}
	d.Customer = CustomerSummary{
		ID: o.CustomerID, RootID: p.ID, Code: p.Code, FullName: p.FullName, Stats: p.Stats,
	}
	if d.CouponCode, err = promotion.ActiveCouponCode(ctx, tx, o.ID); err != nil {
		return OrderDetail{}, fmt.Errorf("đọc mã giảm giá của đơn: %w", err)
	}
	q := store.New(tx)
	links, err := q.ListOrderDuplicateLinks(ctx, o.ID)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("đọc đơn nghi trùng: %w", err)
	}
	d.Warnings = make([]Warning, 0, len(links))
	for _, l := range links {
		d.Warnings = append(d.Warnings, Warning{Code: WarningPossibleDuplicate, RefID: l.OtherID})
	}
	rets, err := q.ListOrderReturns(ctx, o.ID)
	if err != nil {
		return OrderDetail{}, fmt.Errorf("đọc phiếu trả của đơn: %w", err)
	}
	d.Returns = make([]ReturnSummary, 0, len(rets))
	for _, r := range rets {
		d.Returns = append(d.Returns, ReturnSummary{ID: r.ID, Code: r.Code, Kind: r.Kind, Status: r.Status})
	}
	return d, nil
}

// orderOfSeller trả ErrNotFound khi đơn id không thuộc người bán sellerID (hay không có, của tenant khác).
func orderOfSeller(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) error {
	ok, err := store.New(tx).OrderExists(ctx, store.OrderExistsParams{SellerID: sellerID, ID: id})
	if err != nil {
		return fmt.Errorf("kiểm đơn: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
