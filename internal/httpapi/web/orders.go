package web

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
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
