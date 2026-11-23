package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Orders là phần đơn hàng của website (kế hoạch M5: báo giá Task 192; tạo đơn Task 193, 194): đọc, ghi thẳng
// PostgreSQL, không qua bộ đệm.
type Orders struct {
	db  *db.DB
	now func() time.Time
}

// NewOrders trả Orders trên d; now nil → time.Now.
func NewOrders(d *db.DB, now func() time.Time) *Orders {
	return &Orders{db: d, now: now}
}

func (o *Orders) clock() time.Time {
	if o.now != nil {
		return o.now().UTC()
	}
	return time.Now().UTC()
}

// Quote báo giá req trong một transaction chỉ đọc của tenant (order.QuoteWeb); lỗi nghiệp vụ của order giữ nguyên để
// errors_order.go đổi mã.
func (o *Orders) Quote(ctx context.Context, tenant uuid.UUID, req *gen.WebQuoteRequest) (*gen.WebQuote, error) {
	in := order.WebQuoteInput{Lines: make([]order.WebLine, len(req.Lines)), ShippingFee: req.ShippingFee}
	for i, l := range req.Lines {
		in.Lines[i] = order.WebLine{VariantID: l.VariantId, Qty: int(l.Qty), QuotedUnitPrice: l.QuotedUnitPrice}
	}
	if c, ok := req.CouponCode.Get(); ok {
		in.CouponCode = c
	}
	if p, ok := req.CustomerPhone.Get(); ok && p != "" {
		ph := logging.Phone(p)
		in.CustomerPhone = &ph
	}
	now := o.clock()
	var q order.WebQuote
	err := tenancy.WithTenant(ctx, o.db, tenant, func(tx pgx.Tx) error {
		var err error
		q, err = order.QuoteWeb(ctx, tx, in, now)
		return err //nolint:wrapcheck // order đã bọc; sentinel giữ nguyên để đổi mã HTTP
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("báo giá cho website: %w", err)
	}
	return quoteOut(q), nil
}

func quoteOut(q order.WebQuote) *gen.WebQuote {
	out := &gen.WebQuote{
		Lines: make([]gen.WebQuotedLine, len(q.Lines)), Merchandise: q.Merchandise, CampaignDiscount: q.CampaignDiscount,
		CouponDiscount: q.CouponDiscount, ShippingFee: q.ShippingFee, GrandTotal: q.GrandTotal,
		Fingerprint: q.Fingerprint, Changed: q.Changed,
	}
	for i, l := range q.Lines {
		g := gen.WebQuotedLine{
			VariantId: l.VariantID, Qty: int32(l.Qty), UnitPrice: l.UnitPrice, //nolint:gosec // G115: Qty ≤ order.MaxWebQty
			CampaignDiscount: l.CampaignDiscount, CouponDiscount: l.CouponDiscount, Net: l.Net,
		}
		if c := l.Campaign; c != nil {
			g.Campaign.SetTo(gen.WebQuoteCampaign{
				ID: c.ID, Revision: int32(c.Revision), Percent: int32(c.Percent), AllowCoupon: c.AllowCoupon, //nolint:gosec // G115: revision, % nhỏ
			})
		}
		if l.Problem != "" {
			g.Problem.SetTo(gen.WebQuotedLineProblem(l.Problem))
		}
		out.Lines[i] = g
	}
	if c := q.Coupon; c != nil {
		gc := gen.WebQuoteCoupon{Code: c.Code, Applied: c.Applied}
		if c.Reason != "" {
			gc.Reason.SetTo(gen.WebQuoteCouponReason(c.Reason))
		}
		out.Coupon.SetTo(gc)
	}
	return out
}
