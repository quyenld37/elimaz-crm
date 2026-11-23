package httpapi

import (
	"context"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Danh mục cho website (Task 190, M5-07; x-auth: service_key, web.catalog.read): handler mỏng, mọi việc ở web.Catalog.

// WebCatalogProducts (GET /api/v1/web/catalog/products) → 200 có ETag hoặc 304.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCatalogProducts(ctx context.Context, params gen.WebCatalogProductsParams) (gen.WebCatalogProductsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Products(ctx, u.TenantID, params) //nolint:wrapcheck // lỗi của web, đổi mã ở errors*.go
}

// WebCatalogChanges (GET /api/v1/web/catalog/changes) → 200.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCatalogChanges(ctx context.Context, params gen.WebCatalogChangesParams) (gen.WebCatalogChangesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Changes(ctx, u.TenantID, params) //nolint:wrapcheck // như trên
}

// WebCatalogTaxonomy (GET /api/v1/web/catalog/taxonomy) → 200 có ETag hoặc 304.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCatalogTaxonomy(ctx context.Context, params gen.WebCatalogTaxonomyParams) (gen.WebCatalogTaxonomyRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Taxonomy(ctx, u.TenantID, params) //nolint:wrapcheck // như trên
}
