package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng payments (kế hoạch M3, Task M3-30): phiếu thu, cọc, nghĩa vụ hoàn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.
// Task M3-30: recordPayment, listOrderPayments, listRefundObligations, payRefundObligation, waiveRefundObligation.

// defaultPaymentLimit: số dòng mỗi trang khi không gửi limit (mặc định của hợp đồng).
const defaultPaymentLimit = 50

// cashActor là tác nhân nhân viên u của module tiền.
func cashActor(u identity.AuthUser) cash.Actor {
	return cash.Actor{Type: cash.ActorStaff, ID: u.UserID}
}

// RecordPayment (POST /api/v1/orders/{id}/payments, cash.payments.record): trong WriteTx khóa đơn, ghi phiếu thu, đẩy
// tổng về đơn (COD còn thu tính lại), audit cash.payment.record → 201. Đơn đã gửi hãng → 409 cod_locked_by_shipment;
// đơn đã hủy, đã giao… → 409 invalid_transition; vượt tổng đơn → 409 payment_exceeds_total; đơn không có → 404.
// Từ chối không lưu theo khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RecordPayment(ctx context.Context, req *gen.RecordPaymentRequest, params gen.RecordPaymentParams) (gen.RecordPaymentRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	in := cash.PaymentInput{
		OrderID: params.ID, Method: string(req.Method), Amount: int64(req.Amount), Reference: req.Reference.Or(""),
		ReceivedAt: req.ReceivedAt.Or(time.Time{}),
	}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isCashRefusal, "ghi phiếu thu",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			if err := sellerOrder(ctx, tx, params.ID, cash.ErrOrderNotFound); err != nil {
				return nil, err
			}
			p, err := h.d.Cash.RecordPayment(ctx, tx, u.TenantID, in, cashActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			body, err := paymentsOut(ctx, tx, []cash.PaymentView{p})
			if err != nil {
				return nil, err
			}
			return &body[0], h.audit(ctx, tx, u, "cash.payment.record", "cash.payment", p.ID, nil, map[string]any{
				"order_id": p.OrderID.String(), "method": p.Method, "amount": p.Amount,
			})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Payment), nil //nolint:forcetypeassert // fn trên trả *gen.Payment
}

// ListOrderPayments (GET /api/v1/orders/{id}/payments, cash.payments.record): phiếu thu, cọc, chi hoàn của đơn mới
// nhất trước; con trỏ là mã phiếu cuối trang trước (sai dạng → 400). Đơn không có → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListOrderPayments(ctx context.Context, params gen.ListOrderPaymentsParams) (gen.ListOrderPaymentsRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	before, err := uuidCursor(params.Cursor, "phiếu thu")
	if err != nil {
		return nil, err
	}
	page := &gen.PaymentPage{Items: []gen.Payment{}}
	err = h.catalogRead(ctx, func(tx pgx.Tx) error {
		if err := sellerOrder(ctx, tx, params.ID, cash.ErrOrderNotFound); err != nil {
			return err
		}
		items, next, err := h.d.Cash.ListOrderPayments(ctx, tx, params.ID, before, int(params.Limit.Or(defaultPaymentLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		if page.Items, err = paymentsOut(ctx, tx, items); err != nil {
			return err
		}
		page.NextCursor = gen.OptString{}
		if next != nil {
			page.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc phiếu của đơn: %w", err)
	}
	return page, nil
}

// ListRefundObligations (GET /api/v1/refund-obligations, cash.cod.read): nghĩa vụ hoàn mới nhất trước, lọc theo
// trạng thái; con trỏ là mã nghĩa vụ cuối trang trước (sai dạng → 400).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListRefundObligations(ctx context.Context, params gen.ListRefundObligationsParams) (gen.ListRefundObligationsRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	before, err := uuidCursor(params.Cursor, "nghĩa vụ hoàn")
	if err != nil {
		return nil, err
	}
	page := &gen.RefundObligationPage{Items: []gen.RefundObligation{}}
	err = h.sellerRead(ctx, func(tx pgx.Tx, seller uuid.UUID) error {
		items, next, err := h.d.Cash.ListRefundObligations(ctx, tx, seller, string(params.Status.Or("")), before,
			int(params.Limit.Or(defaultPaymentLimit)))
		if err != nil {
			return err //nolint:wrapcheck // cash đã bọc
		}
		page.Items, page.NextCursor = make([]gen.RefundObligation, 0, len(items)), gen.OptString{}
		for _, r := range items {
			page.Items = append(page.Items, *obligationOut(r))
		}
		if next != nil {
			page.NextCursor = gen.NewOptString(next.String())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc nghĩa vụ hoàn: %w", err)
	}
	return page, nil
}

// PayRefundObligation (POST /api/v1/refund-obligations/{id}/payments, cash.refunds.record): trong WriteTx khóa đơn rồi
// nghĩa vụ, cộng số đã chi (đủ → paid), ghi phiếu chi âm, audit cash.refund.pay → 200 nghĩa vụ. Vượt số còn phải hoàn
// → 409 refund_exceeds_remaining; nghĩa vụ đã đóng → 409 invalid_transition; không có → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PayRefundObligation(ctx context.Context, req *gen.PayRefundObligationRequest, params gen.PayRefundObligationParams) (gen.PayRefundObligationRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	out, err := h.cashSellerWrite(ctx, http.StatusOK, "chi hoàn tiền", cash.ScopeRefundObligation, params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			r, err := h.d.Cash.PayRefund(ctx, tx, u.TenantID, params.ID, int64(req.Amount), req.Reference.Or(""), cashActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			return obligationOut(r), h.audit(ctx, tx, u, "cash.refund.pay", "cash.refund_obligation", r.ID, nil,
				map[string]any{
					"order_id": r.OrderID.String(), "amount": int64(req.Amount), "paid_amount": r.PaidAmount,
					"status": r.Status,
				})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.RefundObligation), nil //nolint:forcetypeassert // fn trên trả *gen.RefundObligation
}

// WaiveRefundObligation (POST /api/v1/refund-obligations/{id}/waive, cash.refunds.waive): trong WriteTx đóng nghĩa vụ
// "không hoàn" kèm lý do, audit cash.refund.waive có lý do (Q-2) → 200. Lý do chỉ khoảng trắng → 422 invalid_input;
// nghĩa vụ đã đóng → 409 invalid_transition; không có → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WaiveRefundObligation(ctx context.Context, req *gen.WaiveRefundObligationRequest, params gen.WaiveRefundObligationParams) (gen.WaiveRefundObligationRes, error) {
	if h.d.Cash == nil {
		return nil, errNoCash
	}
	out, err := h.cashSellerWrite(ctx, http.StatusOK, "miễn hoàn tiền", cash.ScopeRefundObligation, params.ID,
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			r, err := h.d.Cash.WaiveRefund(ctx, tx, u.TenantID, params.ID, req.Reason, cashActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của cash
			}
			return obligationOut(r), h.audit(ctx, tx, u, "cash.refund.waive", "cash.refund_obligation", r.ID,
				map[string]any{"status": cash.RefundOpen},
				map[string]any{"order_id": r.OrderID.String(), "status": r.Status, "reason": r.WaiveReason})
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.RefundObligation), nil //nolint:forcetypeassert // fn trên trả *gen.RefundObligation
}

// paymentsOut dựng thân Payment của từng phiếu ps, đọc tên mỗi người ghi một lần (staffNames) cùng tx.
func paymentsOut(ctx context.Context, tx pgx.Tx, ps []cash.PaymentView) ([]gen.Payment, error) {
	ids := make([]uuid.UUID, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ActorID)
	}
	names, err := staffNames(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Payment, 0, len(ps))
	for _, p := range ps {
		out = append(out, paymentOut(p, names))
	}
	return out, nil
}

// paymentOut dựng thân Payment của p, tên người ghi lấy từ names.
func paymentOut(p cash.PaymentView, names map[uuid.UUID]string) gen.Payment {
	out := gen.Payment{
		ID: p.ID, OrderId: p.OrderID, Method: gen.PaymentMethod(p.Method), Amount: gen.SignedMoney(p.Amount),
		Reference: gen.NewOptString(p.Reference), ReceivedAt: p.ReceivedAt, CreatedAt: p.CreatedAt,
		Actor: gen.NewOptActor(gen.Actor{ID: p.ActorID, Name: names[p.ActorID]}),
	}
	if p.RefundObligationID != nil {
		out.RefundObligationId = gen.NewOptUUID(*p.RefundObligationID)
	}
	return out
}

func obligationOut(r cash.RefundObligation) *gen.RefundObligation {
	out := &gen.RefundObligation{
		ID: r.ID, OrderId: r.OrderID, Source: gen.RefundObligationSource(r.Source), Amount: gen.Money(r.Amount),
		PaidAmount: gen.Money(r.PaidAmount), Status: gen.RefundObligationStatus(r.Status), CreatedAt: r.CreatedAt,
		Version: r.Version,
	}
	if r.OrderCode != "" {
		out.OrderCode = gen.NewOptString(r.OrderCode)
	}
	if r.ReturnID != nil {
		out.ReturnId = gen.NewOptUUID(*r.ReturnID)
	}
	if r.WaiveReason != "" {
		out.WaiveReason = gen.NewOptString(r.WaiveReason)
	}
	return out
}
