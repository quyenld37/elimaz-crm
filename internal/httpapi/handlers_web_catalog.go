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

// WebAvailability (GET /api/v1/web/availability, Task 191) → 200.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebAvailability(ctx context.Context, params gen.WebAvailabilityParams) (gen.WebAvailabilityRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Availability(ctx, u.TenantID, params) //nolint:wrapcheck // như trên
}

// WebSold30d (GET /api/v1/web/catalog/sold-30d, Task 191) → 200.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebSold30d(ctx context.Context) (gen.WebSold30dRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Sold30(ctx, u.TenantID) //nolint:wrapcheck // như trên
}

// WebLocations (GET /api/v1/web/locations, Task 191) → 200 có ETag hoặc 304.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebLocations(ctx context.Context, params gen.WebLocationsParams) (gen.WebLocationsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Locations(ctx, u.TenantID, params) //nolint:wrapcheck // như trên
}

// WebCampaigns (GET /api/v1/web/campaigns, Task 452) → 200 có ETag hoặc 304.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCampaigns(ctx context.Context, params gen.WebCampaignsParams) (gen.WebCampaignsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Campaigns(ctx, u.TenantID, params) //nolint:wrapcheck // như trên
}

// WebCampaign (GET /api/v1/web/campaigns/{linkKey}, Task 452) → 200; nháp, đã xóa → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebCampaign(ctx context.Context, params gen.WebCampaignParams) (gen.WebCampaignRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Campaign(ctx, u.TenantID, params) //nolint:wrapcheck // như trên
}
