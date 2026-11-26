package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// Mã quà sinh nhật và mã nhập — phía website (kế hoạch M5, Task 197; x-auth: service_key, web.coupons.issue). Người
// làm là khóa dịch vụ (ServiceKeyID ghi vào created_by của lô, mã); audit ghi ở đây với ActorType service, cùng tx,
// chỉ khi dữ liệu thật sự đổi. SĐT gắn mã không vào audit (chỉ số lượng). Từ chối nghiệp vụ không lưu theo khóa.

// WebCreateBirthdayBatch (POST /api/v1/web/birthday-batches): lô quà sinh nhật theo tên (promotion.EnsureWebBatch).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCreateBirthdayBatch(ctx context.Context, req *gen.WebBirthdayBatchRequest,
	_ gen.WebCreateBirthdayBatchParams,
) (gen.WebCreateBirthdayBatchRes, error) {
	in := promotion.WebBatchInput{
		Name: req.Name, Purpose: promotion.PurposeBirthday, Quantity: int(req.Quantity),
		Template: templateIn(req.Template), BoundPhones: phonesIn(req.BoundPhones),
	}
	out, err := h.webCouponWrite(ctx, "phát lô quà sinh nhật", func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser) (any, error) {
		b, codes, created, err := w.EnsureWebBatch(ctx, tx, in, webPromotionActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		body := &gen.WebCouponBatchCodes{BatchId: b.ID, Created: created, Codes: codes}
		if !created {
			return body, nil
		}
		return body, auditService(ctx, tx, u, "promotion.coupon_batch.web_create", "promotion.coupon_batch", b.ID, nil,
			batchAuditFields(b, len(in.BoundPhones)))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCouponBatchCodes), nil //nolint:forcetypeassert // fn trên trả *gen.WebCouponBatchCodes
}

// WebMarkCouponGiven (POST /api/v1/web/coupons/{code}/given): ghi lúc trao mã lần đầu (giờ máy chủ); gọi lại trả lúc
// trao đầu tiên, không audit lại.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebMarkCouponGiven(ctx context.Context, params gen.WebMarkCouponGivenParams) (gen.WebMarkCouponGivenRes, error) {
	out, err := h.webCouponWrite(ctx, "ghi lúc trao mã", func(tx pgx.Tx, _ promotion.Writer, u identity.AuthUser) (any, error) {
		m, err := promotion.MarkGiven(ctx, tx, params.Code, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		body := &gen.WebCouponGiven{Code: params.Code, GivenAt: m.At}
		if !m.First {
			return body, nil
		}
		return body, auditService(ctx, tx, u, "promotion.coupon.given", "promotion.coupon", m.CouponID, nil,
			map[string]any{"code": params.Code, "givenAt": m.At})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCouponGiven), nil //nolint:forcetypeassert // fn trên trả *gen.WebCouponGiven
}

// WebImportCoupons (POST /api/v1/web/coupons/import): nhập tối đa 500 mã còn hạn của website (promotion.ImportCoupons);
// audit một dòng cho lần nhập có mã mới (số mã, tên lô; không nêu từng mã).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebImportCoupons(ctx context.Context, req *gen.WebCouponImportRequest, _ gen.WebImportCouponsParams,
) (gen.WebImportCouponsRes, error) {
	items := make([]promotion.ImportItem, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, promotion.ImportItem{
			Code: it.Code, DiscountKind: string(it.DiscountKind), Value: it.Value,
			MaxDiscount: int64(it.MaxDiscount.Value), MinSubtotal: int64(it.MinSubtotal.Value),
			ValidFrom: it.ValidFrom, ValidTo: it.ValidTo, GivenAt: optPtr[time.Time](it.GivenAt), Used: it.Used.Or(false),
			BatchName: it.BatchName.Value, BoundPhone: string(it.BoundPhone.Or("")),
		})
	}
	out, err := h.webCouponWrite(ctx, "nhập mã của website", func(tx pgx.Tx, _ promotion.Writer, u identity.AuthUser) (any, error) {
		r, err := promotion.ImportCoupons(ctx, tx, items, webPromotionActor(u))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		body := &gen.WebCouponImportReport{
			Inserted: int32(r.Inserted), Skipped: int32(r.Skipped), Conflicts: r.Conflicts, //nolint:gosec // G115: ≤ 500
		}
		if r.Inserted == 0 {
			return body, nil
		}
		return body, auditService(ctx, tx, u, "promotion.coupon.web_import", "promotion.coupon_import",
			uuid.Must(uuid.NewV7()), nil, map[string]any{
				"items": len(items), "inserted": r.Inserted, "skipped": r.Skipped, "conflicts": len(r.Conflicts),
			})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCouponImportReport), nil //nolint:forcetypeassert // fn trên trả *gen.WebCouponImportReport
}

// webCouponWrite chạy change trong refusableWrite: lỗi nghiệp vụ của promotion (và SĐT sai của customer) trả lời,
// không lưu theo khóa; Writer có báo hiệu realtime như đường nhân viên.
func (h *handler) webCouponWrite(ctx context.Context, what string,
	change func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	return h.refusableWrite(ctx, http.StatusOK, isPromotionRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, promotion.Writer{Realtime: h.d.Realtime}, u)
	})
}

// webPromotionActor: người làm của website là khóa dịch vụ.
func webPromotionActor(u identity.AuthUser) promotion.Actor {
	return promotion.Actor{TenantID: u.TenantID, UserID: u.ServiceKeyID}
}
