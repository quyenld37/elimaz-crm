package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// AttachProductImage (POST /api/v1/catalog/products/{id}/images, catalog.manage) gắn tệp ảnh vào cuối danh sách ảnh của
// mẫu khi version còn khớp → 201, mẫu kèm ảnh.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AttachProductImage(ctx context.Context, req *gen.AttachProductImageRequest,
	params gen.AttachProductImageParams,
) (gen.AttachProductImageRes, error) {
	color := optString(req.Color.Set, req.Color.Value)
	return h.imageWrite(ctx, http.StatusCreated, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) error {
		_, err := w.AttachImage(ctx, tx, params.ID, req.ProductVersion, req.FileId, color, u)
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
	return h.imageWrite(ctx, http.StatusOK, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) error {
		return w.ReorderImages(ctx, tx, params.ID, req.ProductVersion, req.ImageIds, u)
	})
}

// DeleteProductImage (DELETE /api/v1/catalog/products/{id}/images/{imageId}, catalog.manage) xóa mềm một ảnh.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DeleteProductImage(ctx context.Context, params gen.DeleteProductImageParams) (gen.DeleteProductImageRes, error) {
	return h.imageWrite(ctx, http.StatusOK, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) error {
		_, err := w.DeleteImage(ctx, tx, params.ID, params.ImageId, u)
		return err //nolint:wrapcheck // sentinel của catalog
	})
}

// RestoreProductImage (POST /api/v1/catalog/products/{id}/images/{imageId}/restore, catalog.manage) khôi phục ảnh đã
// xóa trong 30 ngày (quá hạn → 410 restore_expired).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RestoreProductImage(ctx context.Context, params gen.RestoreProductImageParams) (gen.RestoreProductImageRes, error) {
	return h.imageWrite(ctx, http.StatusOK, params.ID, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) error {
		_, err := w.RestoreImage(ctx, tx, params.ID, params.ImageId, u)
		return err //nolint:wrapcheck // sentinel của catalog
	})
}

// imageWrite chạy change trong catalogWrite rồi đọc lại mẫu productID kèm ảnh trong cùng transaction (câu trả lời
// của bốn operation ảnh là mẫu sau khi đổi). Mẫu của người bán khác người bán mặc định → ErrNotFound (ADR-29).
func (h *handler) imageWrite(ctx context.Context, status int, productID uuid.UUID,
	change func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) error,
) (*gen.Product, error) {
	out, err := h.catalogWrite(ctx, status, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		if _, err := catalog.GetProduct(ctx, tx, seller, productID); err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		if err := change(tx, w, u); err != nil {
			return nil, err
		}
		return h.productWithImages(ctx, tx, seller, productID)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Product), nil //nolint:forcetypeassert // fn trên trả *gen.Product
}

// productWithImages đọc mẫu id của người bán sellerID kèm biến thể và ảnh đang hiện (đường dẫn theo
// FILES_PUBLIC_BASE_URL, IMAGE_PRESET_VERSION) trong tx.
func (h *handler) productWithImages(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (*gen.Product, error) {
	p, err := catalog.GetProduct(ctx, tx, sellerID, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của catalog
	}
	imgs, err := catalog.ProductImages(ctx, tx, sellerID, id, h.imageView())
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của catalog
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
