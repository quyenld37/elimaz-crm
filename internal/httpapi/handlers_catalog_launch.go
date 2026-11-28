package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Sản phẩm: điều kiện lên kệ, hiện và ẩn hàng loạt (MO01, Task M7-16 = 261). Handler mỏng: mọi việc ở catalog
// (Readiness, ReadinessPage, Writer.Publish, Writer.Unpublish); lỗi đổi mã ở errors_catalog.go.

// GetCatalogLaunchReadiness (GET /api/v1/catalog/launch/readiness, catalog.read) → 200 một trang mẫu nháp kèm điều
// kiện; cursor không phải mã nextCursor → 400 invalid_input.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCatalogLaunchReadiness(ctx context.Context, params gen.GetCatalogLaunchReadinessParams) (gen.GetCatalogLaunchReadinessRes, error) {
	after := uuid.Nil
	if s, ok := params.Cursor.Get(); ok {
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			return nil, catalog.ErrInvalidInput
		}
		after = id
	}
	f := catalog.ReadinessFilter(params.Status.Or(""))
	limit := int(params.Limit.Or(50))
	var page catalog.ReadinessPageResult
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		page, err = catalog.ReadinessPage(ctx, tx, f, after, limit)
		return err //nolint:wrapcheck // sentinel của catalog
	}); err != nil {
		return nil, fmt.Errorf("điều kiện lên kệ: %w", err)
	}
	out := &gen.ProductReadinessPage{
		Items:      readinessOut(page.Items),
		ReadyCount: int32(page.ReadyCount), NotReadyCount: int32(page.NotReadyCount), //nolint:gosec // G115: số mẫu của shop
	}
	if page.Next != uuid.Nil {
		out.NextCursor = gen.NewOptString(page.Next.String())
	}
	return out, nil
}

// PublishProducts (POST /api/v1/catalog/launch/publish, catalog.launch.publish, Idempotency-Key) → 200 published,
// skipped; thiếu cả productIds lẫn allReady (hay có cả hai) → 400 invalid_input.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PublishProducts(ctx context.Context, req *gen.PublishProductsRequest, _ gen.PublishProductsParams) (gen.PublishProductsRes, error) {
	in := catalog.PublishInput{IDs: req.ProductIds, AllReady: req.AllReady.Or(false)}
	out, err := h.catalogWrite(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		r, err := w.Publish(ctx, tx, in, u, time.Now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return &gen.PublishProductsResult{Published: r.Published, Skipped: readinessOut(r.Skipped)}, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.PublishProductsResult), nil //nolint:forcetypeassert // fn trên trả *gen.PublishProductsResult
}

// UnpublishProducts (POST /api/v1/catalog/launch/unpublish, catalog.launch.publish, Idempotency-Key) → 200 mã các mẫu
// đã ẩn lại (mẫu không đang bán bị bỏ qua).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UnpublishProducts(ctx context.Context, req *gen.UnpublishProductsRequest, _ gen.UnpublishProductsParams) (gen.UnpublishProductsRes, error) {
	out, err := h.catalogWrite(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		ids, err := w.Unpublish(ctx, tx, req.ProductIds, u, time.Now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return &gen.UnpublishProductsResult{Unpublished: ids}, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.UnpublishProductsResult), nil //nolint:forcetypeassert // fn trên trả *gen.UnpublishProductsResult
}

func readinessOut(rs []catalog.ProductReadiness) []gen.ProductReadiness {
	out := make([]gen.ProductReadiness, len(rs))
	for i, r := range rs {
		out[i] = gen.ProductReadiness{
			ProductId: r.ProductID, Code: r.Code, Name: r.Name, Status: gen.ProductStatus(r.Status), Ready: r.Ready,
			Reasons: r.Reasons,
		}
	}
	return out
}
