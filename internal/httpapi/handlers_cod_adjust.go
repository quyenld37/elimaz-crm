package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng cod_adjust (kế hoạch M3, Task M3-33): điều chỉnh COD, kết quả đối chiếu sổ.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// PreviewCodAdjustment (POST /api/v1/cod/adjustments/preview).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PreviewCodAdjustment(_ context.Context, _ *gen.CodAdjustmentRequest, _ gen.PreviewCodAdjustmentParams) (gen.PreviewCodAdjustmentRes, error) {
	return nil, ht.ErrNotImplemented
}

// CreateCodAdjustment (POST /api/v1/cod/adjustments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateCodAdjustment(_ context.Context, _ *gen.CreateCodAdjustmentRequest, _ gen.CreateCodAdjustmentParams) (gen.CreateCodAdjustmentRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListReconcileFindings (GET /api/v1/cod/reconcile-findings).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListReconcileFindings(_ context.Context, _ gen.ListReconcileFindingsParams) (gen.ListReconcileFindingsRes, error) {
	return nil, ht.ErrNotImplemented
}
