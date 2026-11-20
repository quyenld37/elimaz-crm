package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng payments (kế hoạch M3, Task M3-30): phiếu thu, cọc, nghĩa vụ hoàn.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// RecordPayment (POST /api/v1/orders/{id}/payments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) RecordPayment(_ context.Context, _ *gen.RecordPaymentRequest, _ gen.RecordPaymentParams) (gen.RecordPaymentRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListOrderPayments (GET /api/v1/orders/{id}/payments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListOrderPayments(_ context.Context, _ gen.ListOrderPaymentsParams) (gen.ListOrderPaymentsRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListRefundObligations (GET /api/v1/refund-obligations).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListRefundObligations(_ context.Context, _ gen.ListRefundObligationsParams) (gen.ListRefundObligationsRes, error) {
	return nil, ht.ErrNotImplemented
}

// PayRefundObligation (POST /api/v1/refund-obligations/{id}/payments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PayRefundObligation(_ context.Context, _ *gen.PayRefundObligationRequest, _ gen.PayRefundObligationParams) (gen.PayRefundObligationRes, error) {
	return nil, ht.ErrNotImplemented
}

// WaiveRefundObligation (POST /api/v1/refund-obligations/{id}/waive).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) WaiveRefundObligation(_ context.Context, _ *gen.WaiveRefundObligationRequest, _ gen.WaiveRefundObligationParams) (gen.WaiveRefundObligationRes, error) {
	return nil, ht.ErrNotImplemented
}
