package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
		gs, err := promotion.BirthdayOutbox(ctx, tx, h.now(), int(params.Limit.Or(0)))
		out.Items = out.Items[:0]
		for _, g := range gs {
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
