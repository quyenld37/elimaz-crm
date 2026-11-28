package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Kho: hàng hoàn của đơn cũ (MO08, Task M7-19 = 264).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// CreateLegacyReturn (POST /api/v1/inventory/legacy-returns).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateLegacyReturn(_ context.Context, _ *gen.CreateLegacyReturnRequest, _ gen.CreateLegacyReturnParams) (gen.CreateLegacyReturnRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListLegacyReturns (GET /api/v1/inventory/legacy-returns).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListLegacyReturns(_ context.Context, _ gen.ListLegacyReturnsParams) (gen.ListLegacyReturnsRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetLegacyReturn (GET /api/v1/inventory/legacy-returns/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetLegacyReturn(_ context.Context, _ gen.GetLegacyReturnParams) (gen.GetLegacyReturnRes, error) {
	return nil, ht.ErrNotImplemented
}
