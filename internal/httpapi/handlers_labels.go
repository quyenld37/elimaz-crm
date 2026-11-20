package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng labels (kế hoạch M3, Task M3-16): in tem theo lô, phiếu nhặt hàng, đóng gói.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// CreateLabelBatch (POST /api/v1/label-batches).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateLabelBatch(_ context.Context, _ *gen.CreateLabelBatchRequest, _ gen.CreateLabelBatchParams) (gen.CreateLabelBatchRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetLabelBatch (GET /api/v1/label-batches/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetLabelBatch(_ context.Context, _ gen.GetLabelBatchParams) (gen.GetLabelBatchRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetPickingList (GET /api/v1/picking-list).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetPickingList(_ context.Context, _ gen.GetPickingListParams) (gen.GetPickingListRes, error) {
	return nil, ht.ErrNotImplemented
}

// SetPackingStatus (POST /api/v1/orders/packing).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) SetPackingStatus(_ context.Context, _ *gen.SetPackingStatusRequest, _ gen.SetPackingStatusParams) (gen.SetPackingStatusRes, error) {
	return nil, ht.ErrNotImplemented
}
