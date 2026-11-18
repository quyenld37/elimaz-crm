package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/files"
)

// AttachProductImage (POST /api/v1/catalog/products/{id}/images, catalog.manage) gắn tệp ảnh vào cuối danh sách ảnh của
// mẫu khi version còn khớp → 201, mẫu kèm ảnh.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AttachProductImage(ctx context.Context, req *gen.AttachProductImageRequest,
	params gen.AttachProductImageParams,
) (gen.AttachProductImageRes, error) {
	color := optString(req.Color.Set, req.Color.Value)
	return h.imageWrite(ctx, http.StatusCreated, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) error {
		_, err := w.AttachImage(ctx, tx, seller, params.ID, req.ProductVersion, req.FileId, color, u)
		return err //nolint:wrapcheck // sentinel của catalog
	})
}

// ReorderProductImages (PUT /api/v1/catalog/products/{id}/images/order, catalog.manage) đặt thứ tự ảnh đang hiện khi
// version còn khớp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReorderProductImages(ctx context.Context, req *gen.ReorderProductImagesRequest,
	params gen.ReorderProductImagesParams,
) (gen.ReorderProductImagesRes, error) {
	return h.imageWrite(ctx, http.StatusOK, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) error {
		return w.ReorderImages(ctx, tx, seller, params.ID, req.ProductVersion, req.ImageIds, u)
	})
}

// DeleteProductImage (DELETE /api/v1/catalog/products/{id}/images/{imageId}, catalog.manage) xóa mềm một ảnh.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DeleteProductImage(ctx context.Context, params gen.DeleteProductImageParams) (gen.DeleteProductImageRes, error) {
	return h.imageWrite(ctx, http.StatusOK, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) error {
		_, err := w.DeleteImage(ctx, tx, seller, params.ID, params.ImageId, u)
		return err //nolint:wrapcheck // sentinel của catalog
	})
}

// RestoreProductImage (POST /api/v1/catalog/products/{id}/images/{imageId}/restore, catalog.manage) khôi phục ảnh đã
// xóa trong 30 ngày (quá hạn → 410 restore_expired).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RestoreProductImage(ctx context.Context, params gen.RestoreProductImageParams) (gen.RestoreProductImageRes, error) {
	return h.imageWrite(ctx, http.StatusOK, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) error {
		_, err := w.RestoreImage(ctx, tx, seller, params.ID, params.ImageId, u)
		return err //nolint:wrapcheck // sentinel của catalog
	})
}

// imageWrite chạy change trong catalogWriteAsDefaultSeller rồi đọc lại mẫu productID kèm ảnh trong cùng transaction
// (câu trả lời của bốn operation ảnh là mẫu sau khi đổi). Mẫu của người bán khác người bán mặc định → ErrNotFound
// (catalog lọc người bán, ADR-29).
func (h *handler) imageWrite(ctx context.Context, status int, productID uuid.UUID,
	change func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) error,
) (*gen.Product, error) {
	out, err := h.catalogWriteAsDefaultSeller(ctx, status, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		if err := change(tx, w, u, seller); err != nil {
			return nil, err
		}
		return h.productWithImages(ctx, tx, seller, productID)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Product), nil //nolint:forcetypeassert // fn trên trả *gen.Product
}

// productWithImages đọc mẫu id của người bán sellerID kèm biến thể, ảnh đang hiện và ảnh trong bài viết (đường dẫn
// theo FILES_PUBLIC_BASE_URL, IMAGE_PRESET_VERSION) trong tx.
func (h *handler) productWithImages(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (*gen.Product, error) {
	p, err := catalog.GetProduct(ctx, tx, sellerID, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của catalog
	}
	imgs, err := catalog.ProductImages(ctx, tx, sellerID, id, h.imageView())
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của catalog
	}
	if err := catalog.ResolveArticleImages(ctx, tx, p.Article, h.imageView()); err != nil {
		return nil, err //nolint:wrapcheck // lỗi của catalog
	}
	return productOut(p, imgs)
}

func (h *handler) imageView() catalog.ImageView {
	return catalog.ImageView{BaseURL: h.d.Config.FilesPublicBaseURL, Preset: h.d.Config.ImagePresetVersion}
}

func productImagesOut(imgs []catalog.Image) []gen.ProductImage {
	out := make([]gen.ProductImage, 0, len(imgs))
	for _, i := range imgs {
		img := gen.ProductImage{
			ID: i.ID, FileId: i.FileID, Position: int32(i.Position), //nolint:gosec // ≤ catalog.MaxImages
			Status: gen.ProductImageStatus(i.Status), Variants: imageVariantsOut(i.Variants),
		}
		if i.Color != nil {
			img.Color = gen.NewOptString(*i.Color)
		}
		out = append(out, img)
	}
	return out
}

// imageVariantsOut: nil khi ảnh không có biến thể (lost: trường variants bị bỏ).
func imageVariantsOut(vs []catalog.ImageVariant) []gen.FileVariant {
	if len(vs) == 0 {
		return nil
	}
	out := make([]gen.FileVariant, 0, len(vs))
	for _, v := range vs {
		out = append(out, gen.FileVariant{Width: int32(v.Width), Format: gen.FileVariantFormat(v.Format), URL: v.URL}) //nolint:gosec // cỡ ảnh nhỏ
	}
	return out
}

// GetOriginalImageURL (GET /api/v1/catalog/products/{id}/images/{imageId}/original, catalog.manage) trả đường dẫn tải
// ảnh gốc hạn 5 phút (TP05); ảnh đã xóa, của mẫu khác hay tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOriginalImageURL(ctx context.Context, params gen.GetOriginalImageURLParams) (gen.GetOriginalImageURLRes, error) {
	u, ok := authUser(ctx)
	switch {
	case !ok:
		return nil, errNoAuthUser
	case h.d.Files == nil:
		return nil, errNoFileStore
	}
	var su files.SignedURL
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		su, err = catalog.OriginalImageURL(ctx, tx, h.d.Files, seller, params.ID, params.ImageId)
		return err //nolint:wrapcheck // sentinel của catalog
	})
	if err != nil {
		return nil, fmt.Errorf("lấy đường dẫn ảnh gốc: %w", err)
	}
	return &gen.SignedUrl{URL: su.URL, ExpiresAt: su.ExpiresAt}, nil
}
