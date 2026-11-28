package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Kho: phiên kiểm kho mở sổ (MO02, Task M7-17 = 262).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// CreateOpeningStocktake (POST /api/v1/inventory/opening-stocktakes).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateOpeningStocktake(_ context.Context, _ *gen.CreateOpeningStocktakeRequest, _ gen.CreateOpeningStocktakeParams) (gen.CreateOpeningStocktakeRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListOpeningStocktakes (GET /api/v1/inventory/opening-stocktakes).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListOpeningStocktakes(_ context.Context, _ gen.ListOpeningStocktakesParams) (gen.ListOpeningStocktakesRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetOpeningStocktake (GET /api/v1/inventory/opening-stocktakes/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetOpeningStocktake(_ context.Context, _ gen.GetOpeningStocktakeParams) (gen.GetOpeningStocktakeRes, error) {
	return nil, ht.ErrNotImplemented
}

// PostOpeningStocktake (POST /api/v1/inventory/opening-stocktakes/{id}/post).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PostOpeningStocktake(_ context.Context, _ gen.PostOpeningStocktakeParams) (gen.PostOpeningStocktakeRes, error) {
	return nil, ht.ErrNotImplemented
}

// CheckOpeningStocktake (GET /api/v1/inventory/opening-stocktakes/{id}/check).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CheckOpeningStocktake(_ context.Context, _ gen.CheckOpeningStocktakeParams) (gen.CheckOpeningStocktakeRes, error) {
	return nil, ht.ErrNotImplemented
}
