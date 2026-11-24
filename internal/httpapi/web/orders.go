package web

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/queries/orderview"
	"elimaz.com.vn/crm/internal/queries/weborders"
)

// OrderInput dịch thân POST /api/v1/web/orders sang order.WebOrderInput; SĐT người đặt chuẩn hóa về +84… (sai →
// customer.ErrInvalidPhone). Giá, giảm của website chỉ để so: CreateFromWeb tính lại.
func OrderInput(req *gen.WebOrderRequest) (order.WebOrderInput, error) {
	ph, err := customer.NormalizePhone(string(req.Customer.Phone))
	if err != nil {
		return order.WebOrderInput{}, fmt.Errorf("SĐT người đặt: %w", err)
	}
	a := req.ShipAddress
	in := order.WebOrderInput{
		ExternalRef: req.ExternalRef,
		Customer:    order.WebCustomer{Phone: ph, Name: req.Customer.Name, WebAccountID: req.Customer.WebAccountId.Or("")},
		ShipAddress: order.ShipAddress{
			Name: a.Name, Phone: logging.Phone(a.Phone), Line: logging.Address(a.Line),
			ProvinceCode: a.ProvinceCode.Or(""), WardCode: a.WardCode.Or(""),
		},
		Lines:        make([]order.WebLine, len(req.Lines)),
		CouponCode:   req.CouponCode.Or(""),
		ShippingFee:  req.ShippingFee,
		Quoted:       order.WebQuoted{Fingerprint: req.Quoted.Fingerprint, GrandTotal: req.Quoted.GrandTotal},
		SubmittedVia: string(req.SubmittedVia),
		Note:         req.Note.Or(""),
	}
	for i, l := range req.Lines {
		in.Lines[i] = order.WebLine{VariantID: l.VariantId, Qty: int(l.Qty), QuotedUnitPrice: l.QuotedUnitPrice}
	}
	if r, ok := req.ReferralRef.Get(); ok {
		raw, err := json.Marshal(r)
		if err != nil {
			return order.WebOrderInput{}, fmt.Errorf("đọc referralRef: %w", err)
		}
		in.ReferralRef = raw
	}
	return in, nil
}

// Create tạo đơn web in (order.Writer.CreateFromWeb, actor a là khóa dịch vụ) trong một savepoint của tx: lỗi nào cũng
// lùi mọi thứ lần tạo đã ghi (khách mới, giữ hàng, lượt mã) mà tx vẫn dùng được để lưu câu trả lời từ chối theo khóa
// chống trùng. Lỗi trả nguyên (kiểu lỗi của order) để lớp HTTP phân loại.
func (o *Orders) Create(ctx context.Context, tx pgx.Tx, in order.WebOrderInput, a order.Actor) (order.WebOrderResult, error) {
	now := o.clock()
	var res order.WebOrderResult
	err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var err error
		res, err = o.w.CreateFromWeb(ctx, sp, in, a, now)
		return err //nolint:wrapcheck // kiểu lỗi của order giữ nguyên để errors_web.go đổi mã
	})
	if err != nil {
		return order.WebOrderResult{}, err //nolint:wrapcheck // như trên
	}
	return res, nil
}

// QuoteJSON là báo giá q theo hình dạng WebQuote của hợp đồng (thân quote của lỗi quote_changed).
func QuoteJSON(q order.WebQuote) (json.RawMessage, error) {
	b, err := quoteOut(q).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("mã hóa báo giá: %w", err)
	}
	return b, nil
}

// OrderViewOut ghép phần đơn (order.GetWebOrder) với vận đơn hiện hành sh (nil: chưa có; weborders.ShipmentOf) thành
// WebOrderView của hợp đồng; displayStatus = orderview.CustomerLabel của ba trục cùng trạng thái vận đơn.
func OrderViewOut(v order.WebOrderView, sh *weborders.Shipment) *gen.WebOrderView {
	axes := orderview.Axes{Status: v.Status, PendingReason: v.PendingReason, PackingStatus: v.PackingStatus}
	out := &gen.WebOrderView{
		ExternalRef: v.ExternalRef, Code: v.Code, Status: gen.OrderStatus(v.Status),
		Lines:       make([]gen.WebOrderLine, 0, len(v.Lines)),
		Merchandise: gen.Money(v.Merchandise), Discount: gen.Money(v.Discount), ShippingFee: gen.Money(v.ShippingFee),
		GrandTotal: gen.Money(v.GrandTotal), CreatedAt: v.CreatedAt, DeliveredAt: optTime(v.DeliveredAt),
		ExchangeUntil: optTime(v.ExchangeUntil), Cancellable: v.Cancellable, PriceReview: v.PriceReview,
	}
	if sh != nil {
		axes.ShipmentStatus = sh.Status
		s := gen.WebOrderShipment{
			Status: sh.Status, CarrierKey: sh.CarrierKey, CarrierName: sh.CarrierName,
			PickedUpAt: optTime(sh.PickedUpAt), DeliveredAt: optTime(sh.DeliveredAt),
		}
		if sh.TrackingCode != "" {
			s.TrackingCode = gen.NewOptString(sh.TrackingCode)
		}
		out.Shipment = gen.NewOptWebOrderShipment(s)
	}
	out.DisplayStatus = gen.OrderLabel(orderview.CustomerLabel(axes))
	for _, l := range v.Lines {
		out.Lines = append(out.Lines, gen.WebOrderLine{
			VariantId: l.VariantID, Sku: l.SKU, Name: l.Name, Qty: int32(l.Qty), //nolint:gosec // số lượng dòng đơn nhỏ
			UnitPrice: gen.Money(l.UnitPrice), Net: gen.Money(l.Net),
			ExchangeableQty: int32(l.ExchangeableQty), //nolint:gosec // ≤ Qty
		})
	}
	return out
}

// ChangesOut đổi một trang của weborders.Changes sang WebOrderChanges của hợp đồng.
func ChangesOut(p weborders.Page) *gen.WebOrderChanges {
	out := &gen.WebOrderChanges{
		Items: make([]gen.WebOrderChange, 0, len(p.Items)), NextSince: p.Next.Since, HasMore: p.HasMore,
	}
	if p.Next.AfterID != uuid.Nil {
		out.NextAfterId = gen.NewOptUUID(p.Next.AfterID)
	}
	for _, c := range p.Items {
		it := gen.WebOrderChange{
			ID: c.ID, ExternalRef: c.ExternalRef, Status: gen.OrderStatus(c.Status),
			DisplayStatus: gen.OrderLabel(c.DisplayStatus), DeliveredAt: optTime(c.DeliveredAt), ChangedAt: c.ChangedAt,
		}
		if c.ShipmentStatus != "" {
			it.ShipmentStatus = gen.NewOptString(c.ShipmentStatus)
		}
		out.Items = append(out.Items, it)
	}
	return out
}

func optTime(t *time.Time) gen.OptDateTime {
	if t == nil {
		return gen.OptDateTime{}
	}
	return gen.NewOptDateTime(*t)
}
