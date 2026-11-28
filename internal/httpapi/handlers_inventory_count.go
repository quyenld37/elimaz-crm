package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Kho: đếm cộng dồn nhiều người, nhập số đếm bằng Excel (Task M7-18 = 263).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// IncrementStocktakeLine (POST /api/v1/inventory/stocktakes/{id}/increments).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) IncrementStocktakeLine(_ context.Context, _ *gen.IncrementStocktakeLineRequest, _ gen.IncrementStocktakeLineParams) (gen.IncrementStocktakeLineRes, error) {
	return nil, ht.ErrNotImplemented
}

// CreateStocktakeCountImport (POST /api/v1/inventory/stocktakes/{id}/count-imports).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateStocktakeCountImport(_ context.Context, _ *gen.CreateStocktakeCountImportRequest, _ gen.CreateStocktakeCountImportParams) (gen.CreateStocktakeCountImportRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetStocktakeCountImport (GET /api/v1/inventory/count-imports/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetStocktakeCountImport(_ context.Context, _ gen.GetStocktakeCountImportParams) (gen.GetStocktakeCountImportRes, error) {
	return nil, ht.ErrNotImplemented
}
