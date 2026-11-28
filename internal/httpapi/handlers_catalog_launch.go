package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Sản phẩm: điều kiện lên kệ, hiện và ẩn hàng loạt (MO01, Task M7-16 = 261).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetCatalogLaunchReadiness (GET /api/v1/catalog/launch/readiness).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetCatalogLaunchReadiness(_ context.Context, _ gen.GetCatalogLaunchReadinessParams) (gen.GetCatalogLaunchReadinessRes, error) {
	return nil, ht.ErrNotImplemented
}

// PublishProducts (POST /api/v1/catalog/launch/publish).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PublishProducts(_ context.Context, _ *gen.PublishProductsRequest, _ gen.PublishProductsParams) (gen.PublishProductsRes, error) {
	return nil, ht.ErrNotImplemented
}

// UnpublishProducts (POST /api/v1/catalog/launch/unpublish).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) UnpublishProducts(_ context.Context, _ *gen.UnpublishProductsRequest, _ gen.UnpublishProductsParams) (gen.UnpublishProductsRes, error) {
	return nil, ht.ErrNotImplemented
}
