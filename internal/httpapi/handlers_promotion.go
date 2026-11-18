package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Mã giảm giá và đợt mã (kế hoạch M2, Task M2-14). Đọc cần promotion.read, ghi promotion.manage, kiểm mã order.create
// (bảng route). Audit ghi ở đây, cùng tx (promotion không nhập identity). Không trả, không ghi audit SĐT gắn mã.

// ListCoupons (GET /api/v1/coupons, promotion.read): mới nhất trước; q theo mã hoặc tên.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCoupons(ctx context.Context, params gen.ListCouponsParams) (gen.ListCouponsRes, error) {
	f := promotion.CouponFilter{
		Q: params.Q.Value, Status: string(params.Status.Value), BatchID: params.BatchId.Value,
		Limit: int(params.Limit.Or(defaultDocLimit)),
	}
	after, err := couponCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	f.After = after
	var cs []promotion.Coupon
	var next *uuid.UUID
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		cs, next, err = promotion.ListCoupons(ctx, tx, f)
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách mã giảm giá: %w", err)
	}
	page := &gen.CouponPage{Items: make([]gen.Coupon, 0, len(cs))}
	for _, c := range cs {
		page.Items = append(page.Items, *couponOut(c))
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}

// CreateCoupon (POST /api/v1/coupons, promotion.manage) → 201; trùng → 409 code_taken.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCoupon(ctx context.Context, req *gen.CreateCouponRequest, _ gen.CreateCouponParams) (gen.CreateCouponRes, error) {
	in := promotion.CouponInput{
		Code: req.Code.Value, Name: req.Name, DiscountKind: string(req.DiscountKind), Value: req.Value,
		MaxDiscount: optMoney(req.MaxDiscount), MinSubtotal: int64(req.MinSubtotal.Value),
		Channels: channelsIn(req.Channels), MaxUses: optPtr[int32](req.MaxUses),
		PerCustomerLimit: optPtr[int32](req.PerCustomerLimit), ReleaseOnCancel: req.ReleaseOnCancel.Or(true),
		ValidFrom: req.ValidFrom, ValidTo: req.ValidTo, BoundCustomerID: req.BoundCustomerId.Value,
		BoundPhone: string(req.BoundPhone.Value),
	}
	out, err := h.promotionWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser) (any, error) {
		c, err := w.CreateCoupon(ctx, tx, promotionActor(u), in)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return couponOut(c), h.audit(ctx, tx, u, "promotion.coupon.create", "promotion.coupon", c.ID, nil, c.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Coupon), nil //nolint:forcetypeassert // fn trên trả *gen.Coupon
}

// GetCoupon (GET /api/v1/coupons/{couponId}, promotion.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCoupon(ctx context.Context, params gen.GetCouponParams) (gen.GetCouponRes, error) {
	var c promotion.Coupon
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		c, err = promotion.GetCoupon(ctx, tx, params.CouponId)
		return err //nolint:wrapcheck // sentinel của promotion
	}); err != nil {
		return nil, fmt.Errorf("đọc mã giảm giá: %w", err)
	}
	return couponOut(c), nil
}

// UpdateCoupon (PATCH /api/v1/coupons/{couponId}, promotion.manage): sửa khi version khớp; mã đã dùng chỉ đổi tên, hạn
// cuối, số lượt tối đa, trạng thái.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateCoupon(ctx context.Context, req *gen.UpdateCouponRequest, params gen.UpdateCouponParams) (gen.UpdateCouponRes, error) {
	p := promotion.CouponPatch{
		Name: optString(req.Name.Set, req.Name.Value), Value: optPtr[int64](req.Value), MaxDiscount: optMoney(req.MaxDiscount),
		MinSubtotal: optMoney(req.MinSubtotal), Channels: channelsIn(req.Channels), MaxUses: optPtr[int32](req.MaxUses),
		PerCustomerLimit: optPtr[int32](req.PerCustomerLimit),
		DiscountKind:     optString(req.DiscountKind.Set, string(req.DiscountKind.Value)),
		Status:           optString(req.Status.Set, string(req.Status.Value)),
	}
	if v, ok := req.ReleaseOnCancel.Get(); ok {
		p.ReleaseOnCancel = &v
	}
	if v, ok := req.ValidFrom.Get(); ok {
		p.ValidFrom = &v
	}
	if v, ok := req.ValidTo.Get(); ok {
		p.ValidTo = &v
	}
	out, err := h.promotionWrite(ctx, http.StatusOK, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser) (any, error) {
		ch, err := w.UpdateCoupon(ctx, tx, promotionActor(u), params.CouponId, req.Version, p)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return couponOut(ch.After), h.audit(ctx, tx, u, "promotion.coupon.update", "promotion.coupon", ch.After.ID,
			ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Coupon), nil //nolint:forcetypeassert // fn trên trả *gen.Coupon
}

// CreateCouponBatch (POST /api/v1/coupon-batches, promotion.manage) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCouponBatch(ctx context.Context, req *gen.CreateCouponBatchRequest, _ gen.CreateCouponBatchParams) (gen.CreateCouponBatchRes, error) {
	t := req.Template
	in := promotion.BatchInput{
		Name: req.Name, Prefix: req.Prefix.Value, Quantity: int(req.Quantity),
		Template: promotion.CouponTemplate{
			Name: t.Name, DiscountKind: string(t.DiscountKind), Value: t.Value, MaxDiscount: optMoney(t.MaxDiscount),
			MinSubtotal: int64(t.MinSubtotal.Value), Channels: channelsIn(t.Channels),
			PerCustomerLimit: optPtr[int32](t.PerCustomerLimit), ValidFrom: t.ValidFrom, ValidTo: t.ValidTo,
		},
	}
	for _, p := range req.BoundPhones {
		in.BoundPhones = append(in.BoundPhones, string(p))
	}
	out, err := h.promotionWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser) (any, error) {
		b, err := w.CreateBatch(ctx, tx, promotionActor(u), in)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return batchOut(b), h.audit(ctx, tx, u, "promotion.coupon_batch.create", "promotion.coupon_batch", b.ID, nil,
			map[string]any{
				"name": b.Name, "prefix": b.Prefix, "quantity": b.Quantity, "boundPhones": len(in.BoundPhones),
				"discountKind": b.Template.DiscountKind, "value": b.Template.Value,
			})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CouponBatch), nil //nolint:forcetypeassert // fn trên trả *gen.CouponBatch
}

// GetCouponBatch (GET /api/v1/coupon-batches/{batchId}, promotion.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCouponBatch(ctx context.Context, params gen.GetCouponBatchParams) (gen.GetCouponBatchRes, error) {
	var b promotion.Batch
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		b, err = promotion.GetBatch(ctx, tx, params.BatchId)
		return err //nolint:wrapcheck // sentinel của promotion
	}); err != nil {
		return nil, fmt.Errorf("đọc đợt mã: %w", err)
	}
	return batchOut(b), nil
}

// ListCouponRedemptions (GET /api/v1/coupons/{couponId}/redemptions, promotion.read): mã đơn (order.Codes với người
// bán mặc định như mọi đường đọc đơn, ADR-29; đơn của người bán khác có orderCode rỗng, Q-T71-8), số giảm, trạng thái.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCouponRedemptions(ctx context.Context, params gen.ListCouponRedemptionsParams) (gen.ListCouponRedemptionsRes, error) {
	after, err := couponCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	var rs []promotion.RedemptionRow
	var next *uuid.UUID
	var codes map[uuid.UUID]string
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		rs, next, err = promotion.ListRedemptions(ctx, tx, params.CouponId, after, int(params.Limit.Or(defaultDocLimit)))
		if err != nil {
			return err //nolint:wrapcheck // sentinel của promotion
		}
		if len(rs) == 0 {
			return nil
		}
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của catalog
		}
		ids := make([]uuid.UUID, 0, len(rs))
		for _, r := range rs {
			ids = append(ids, r.OrderID)
		}
		codes, err = order.Codes(ctx, tx, seller, ids)
		return err //nolint:wrapcheck // order đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc lượt dùng mã: %w", err)
	}
	page := &gen.CouponRedemptionPage{Items: make([]gen.CouponRedemption, 0, len(rs))}
	for _, r := range rs {
		it := gen.CouponRedemption{
			ID: r.ID, OrderId: r.OrderID, OrderCode: codes[r.OrderID], Discount: gen.Money(r.Discount),
			Status: gen.CouponRedemptionStatus(r.Status), CreatedAt: r.CreatedAt,
		}
		if r.ReleasedAt != nil {
			it.ReleasedAt = gen.NewOptDateTime(*r.ReleasedAt)
		}
		page.Items = append(page.Items, it)
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}

// CheckCoupon (GET /api/v1/coupon-check, order.create): kiểm mã cho form lên đơn, không đổi lượt dùng. customerId đổi
// về hồ sơ gốc (không có → 400); không có người bán của đơn: chỉ mã không giới hạn người bán đạt (Q-T71-7).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CheckCoupon(ctx context.Context, params gen.CheckCouponParams) (gen.CheckCouponRes, error) {
	in := promotion.EvalInput{
		Base: int64(params.Base), Channel: string(params.Channel.Value), Phone: logging.Phone(params.Phone.Value),
		Now: h.now(),
	}
	var res promotion.CheckResult
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		if id, ok := params.CustomerId.Get(); ok {
			if in.CustomerRootID, err = promotion.CustomerRoot(ctx, tx, id); err != nil {
				return err //nolint:wrapcheck // sentinel của promotion
			}
		}
		res, err = promotion.Check(ctx, tx, params.Code, in)
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("kiểm mã giảm giá: %w", err)
	}
	out := &gen.CouponCheck{Ok: res.OK, Discount: gen.Money(res.Discount)}
	if res.Reason != "" {
		out.Reason = gen.NewOptCouponRejectReason(gen.CouponRejectReason(res.Reason))
	}
	if res.RemainingUses != nil {
		out.RemainingUses = gen.NewOptInt32(*res.RemainingUses)
	}
	return out, nil
}

// promotionWrite chạy change trong refusableWrite: lỗi nghiệp vụ của promotion (và SĐT sai của customer) là câu trả
// lời không đổi gì (không lưu vào khóa chống trùng).
func (h *handler) promotionWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	return h.refusableWrite(ctx, status, isPromotionRefusal, "ghi mã giảm giá", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, promotion.Writer{Realtime: h.d.Realtime}, u)
	})
}

func isPromotionRefusal(err error) bool {
	if _, _, ok := promotionError(err); ok {
		return true
	}
	_, _, ok := customerError(err)
	return ok
}

func promotionActor(u identity.AuthUser) promotion.Actor {
	return promotion.Actor{TenantID: u.TenantID, UserID: u.UserID}
}

// now là giờ hiện tại của máy chủ (Deps.Now nil → time.Now).
func (h *handler) now() time.Time {
	if h.d.Now != nil {
		return h.d.Now()
	}
	return time.Now()
}

// couponCursor đọc con trỏ trang (id của dòng cuối trang trước); sai dạng → 400 invalid_input.
func couponCursor(c gen.OptString) (*uuid.UUID, error) {
	v, ok := c.Get()
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, fmt.Errorf("con trỏ trang: %w", promotion.ErrInvalidInput)
	}
	return &id, nil
}

func channelsIn(cs []gen.OrderChannel) []string {
	if cs == nil {
		return nil
	}
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, string(c))
	}
	return out
}

// optPtr trả con trỏ tới giá trị của một trường tùy chọn của ogen (chưa gửi → nil).
func optPtr[T any](v interface{ Get() (T, bool) }) *T {
	if x, ok := v.Get(); ok {
		return &x
	}
	return nil
}

func couponOut(c promotion.Coupon) *gen.Coupon {
	out := &gen.Coupon{
		ID: c.ID, Code: c.Code, Name: c.Name, DiscountKind: gen.DiscountKind(c.DiscountKind), Value: c.Value,
		MinSubtotal: gen.Money(c.MinSubtotal), Channels: c.Channels, UsedCount: c.UsedCount,
		ReleaseOnCancel: c.ReleaseOnCancel, ValidFrom: c.ValidFrom, ValidTo: c.ValidTo,
		Status: gen.CouponStatus(c.Status), Bound: c.Bound(), CreatedAt: c.CreatedAt, Version: c.Version,
	}
	if c.BatchID != uuid.Nil {
		out.BatchId = gen.NewOptUUID(c.BatchID)
	}
	if c.MaxDiscount != nil {
		out.MaxDiscount = gen.NewOptMoney(gen.Money(*c.MaxDiscount))
	}
	if c.MaxUses != nil {
		out.MaxUses = gen.NewOptInt32(*c.MaxUses)
	}
	if rem := c.RemainingUses(); rem != nil {
		out.RemainingUses = gen.NewOptInt32(*rem)
	}
	if c.PerCustomerLimit != nil {
		out.PerCustomerLimit = gen.NewOptInt32(*c.PerCustomerLimit)
	}
	return out
}

func batchOut(b promotion.Batch) *gen.CouponBatch {
	t := b.Template
	tpl := gen.CouponTemplate{
		Name: t.Name, DiscountKind: gen.DiscountKind(t.DiscountKind), Value: t.Value,
		MinSubtotal: gen.NewOptMoney(gen.Money(t.MinSubtotal)), ValidFrom: t.ValidFrom, ValidTo: t.ValidTo,
		Channels: make([]gen.OrderChannel, 0, len(t.Channels)),
	}
	for _, c := range t.Channels {
		tpl.Channels = append(tpl.Channels, gen.OrderChannel(c))
	}
	if t.MaxDiscount != nil {
		tpl.MaxDiscount = gen.NewOptMoney(gen.Money(*t.MaxDiscount))
	}
	if t.PerCustomerLimit != nil {
		tpl.PerCustomerLimit = gen.NewOptInt32(*t.PerCustomerLimit)
	}
	return &gen.CouponBatch{
		ID: b.ID, Name: b.Name, Prefix: b.Prefix, Quantity: int32(b.Quantity), //nolint:gosec // ≤ 100.000
		Template: tpl, CreatedAt: b.CreatedAt,
	}
}
