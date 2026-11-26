package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Sinh nhật khách — phía website (kế hoạch M5, Task 468 / M5-56; QĐ40; x-auth: service_key). Đăng ký và dừng nhận tin
// cần web.birthdays.write; outbox (quà website gửi tự động) cần web.coupons.issue. Người làm là khóa dịch vụ; audit do
// customer ghi (chỉ tên trường). Từ chối nghiệp vụ không lưu theo khóa.

// WebRegisterBirthday (POST /api/v1/web/birthdays): form ngày sinh nhật; lần đầu thắng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebRegisterBirthday(ctx context.Context, req *gen.WebBirthdayRequest, _ gen.WebRegisterBirthdayParams,
) (gen.WebRegisterBirthdayRes, error) {
	in := customer.BirthdayInput{
		Phone: string(req.Phone), FullName: req.FullName, Day: int(req.Day), Month: int(req.Month), Zalo: req.Zalo,
		Mail: req.Mail, Email: req.Email.Or(""), Source: customer.SourceWeb,
	}
	out, err := h.webBirthdayWrite(ctx, "lưu sinh nhật của khách", func(tx pgx.Tx, w customer.Writer, a customer.Actor) (any, error) {
		p, created, err := w.RegisterBirthday(ctx, tx, in, a, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của customer
		}
		return &gen.WebBirthdayResult{Created: created, Day: int32(p.Day), Month: int32(p.Month)}, nil //nolint:gosec // G115: 1–31
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebBirthdayResult), nil //nolint:forcetypeassert // fn trên trả *gen.WebBirthdayResult
}

// WebBirthdayStop (POST /api/v1/web/birthdays/stop): dừng nhận tin theo mã dừng; mã lạ → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebBirthdayStop(ctx context.Context, req *gen.WebBirthdayStopRequest, _ gen.WebBirthdayStopParams,
) (gen.WebBirthdayStopRes, error) {
	out, err := h.webBirthdayWrite(ctx, "dừng nhận tin sinh nhật", func(tx pgx.Tx, w customer.Writer, a customer.Actor) (any, error) {
		s, err := w.StopBirthday(ctx, tx, req.Token, a, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của customer
		}
		return &gen.WebBirthdayStopResult{StoppedAt: s.StoppedAt}, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebBirthdayStopResult), nil //nolint:forcetypeassert // fn trên trả *gen.WebBirthdayStopResult
}

// WebBirthdayOutbox (GET /api/v1/web/birthdays/outbox): quà website cần gửi tự động.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebBirthdayOutbox(ctx context.Context, params gen.WebBirthdayOutboxParams) (gen.WebBirthdayOutboxRes, error) {
	out := &gen.WebBirthdayOutbox{Items: []gen.WebBirthdayOutboxItem{}}
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		page, err := promotion.BirthdayOutbox(ctx, tx, h.now(), promotion.OutboxQuery{
			Limit: int(params.Limit.Or(0)), Cursor: params.Cursor.Or(""),
		})
		out.Items, out.NextCursor = out.Items[:0], gen.OptString{}
		if page.Next != "" {
			out.NextCursor = gen.NewOptString(page.Next)
		}
		for _, g := range page.Items {
			b := birthdayGiftOut(g)
			out.Items = append(out.Items, gen.WebBirthdayOutboxItem{
				CustomerId: b.CustomerId, Code: b.Code, FullName: b.FullName, Phone: b.Phone, Email: b.Email, Day: b.Day,
				Month: b.Month, Zalo: b.Zalo, Mail: b.Mail, Amount: b.Amount, MinOrder: b.MinOrder, ValidFrom: b.ValidFrom,
				ValidTo: b.ValidTo, BirthdayIn: b.BirthdayIn, StopToken: string(g.StopToken),
			})
		}
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc quà sinh nhật cần gửi: %w", err)
	}
	return out, nil
}

// WebBirthdayGiftFailed (POST /api/v1/web/birthdays/gifts/{code}/failed): website báo quà trong outbox gửi lỗi (Task
// 491); lần ghi được audit promotion.birthday_gift.send_failed (mã, số lần; không dữ liệu khách).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebBirthdayGiftFailed(ctx context.Context, params gen.WebBirthdayGiftFailedParams,
) (gen.WebBirthdayGiftFailedRes, error) {
	out, err := h.webCouponWrite(ctx, "ghi quà sinh nhật gửi lỗi", func(tx pgx.Tx, _ promotion.Writer, u identity.AuthUser) (any, error) {
		f, err := promotion.MarkBirthdaySendFailed(ctx, tx, params.Code, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		body := &gen.WebBirthdayGiftFailure{Code: f.Code, Failures: int32(f.Failures), Given: f.Given} //nolint:gosec // G115: ≤ 3
		if f.RetryAt != nil {
			body.RetryAt = gen.NewOptDateTime(*f.RetryAt)
		}
		if f.Given {
			return body, nil
		}
		return body, auditService(ctx, tx, u, "promotion.birthday_gift.send_failed", "promotion.coupon", f.CouponID, nil,
			map[string]any{"code": f.Code, "failures": f.Failures, "retry": f.RetryAt != nil})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebBirthdayGiftFailure), nil //nolint:forcetypeassert // fn trên trả *gen.WebBirthdayGiftFailure
}

// WebImportBirthdays (POST /api/v1/web/birthdays/import): nhập hồ sơ sinh nhật cũ của website giữ mã dừng (Task 491);
// audit do customer ghi (chỉ tên trường).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebImportBirthdays(ctx context.Context, req *gen.WebBirthdayImportRequest, _ gen.WebImportBirthdaysParams,
) (gen.WebImportBirthdaysRes, error) {
	items := make([]customer.BirthdayImport, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, customer.BirthdayImport{
			Phone: string(it.Phone), FullName: it.FullName.Or(""), Day: int(it.Day), Month: int(it.Month), Zalo: it.Zalo,
			Mail: it.Mail, Email: it.Email.Or(""), StopToken: it.StopToken, SavedAt: it.SavedAt,
			StoppedAt: optPtr[time.Time](it.StoppedAt), LegacyDelivered: it.LegacyDelivered.Or(false),
		})
	}
	out, err := h.webBirthdayWrite(ctx, "nhập hồ sơ sinh nhật của website", func(tx pgx.Tx, w customer.Writer, a customer.Actor) (any, error) {
		r, err := w.ImportBirthdays(ctx, tx, items, a, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của customer
		}
		body := &gen.WebBirthdayImportReport{
			Inserted: int32(r.Inserted), Skipped: int32(r.Skipped), Stopped: int32(r.Stopped), //nolint:gosec // G115: ≤ 500
			Conflicts: make([]int32, 0, len(r.Conflicts)),
		}
		for _, c := range r.Conflicts {
			body.Conflicts = append(body.Conflicts, int32(c)) //nolint:gosec // G115: < 500
		}
		return body, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebBirthdayImportReport), nil //nolint:forcetypeassert // fn trên trả *gen.WebBirthdayImportReport
}

// webBirthdayWrite chạy change trong refusableWrite với Writer của customer và người làm là khóa dịch vụ.
func (h *handler) webBirthdayWrite(ctx context.Context, what string,
	change func(tx pgx.Tx, w customer.Writer, a customer.Actor) (any, error),
) (any, error) {
	w := customer.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	return h.refusableWrite(ctx, http.StatusOK, isBirthdayRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, w, customer.Actor{Type: customer.ActorService, ID: u.ServiceKeyID, RequestID: RequestID(ctx)})
	})
}

// isBirthdayRefusal: lỗi nghiệp vụ của đăng ký, dừng nhận tin (SĐT sai, ngày sai, mã dừng lạ).
func isBirthdayRefusal(err error) bool {
	return errors.Is(err, customer.ErrInvalidInput) || isPromotionRefusal(err)
}
