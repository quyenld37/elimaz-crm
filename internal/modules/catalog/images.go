package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/article"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/files"
)

// Ảnh sản phẩm (Task M1-44).
const (
	// MaxImages là số ảnh đang hiện tối đa của một mẫu.
	MaxImages = 50
	// RestoreDays là hạn khôi phục ảnh đã xóa (TP06).
	RestoreDays = 30

	// ImageReady: ảnh hiện được (có đủ biến thể của bộ kích thước chọn được). ImageLost: tệp mất trên kho hay bị kho
	// tệp xóa; màn hình hiện ô trống có ghi chú (TP12).
	ImageReady = "ready"
	ImageLost  = "lost"

	productImagePurpose = "product_image"
)

// ImageVariant là một biến thể ảnh: chiều rộng, định dạng (avif, webp, jpeg), đường dẫn công khai.
type ImageVariant = article.ImageVariant

// Image là một ảnh đang hiện của mẫu. Color nil: ảnh chung mọi màu. Variants rỗng khi Status là ImageLost.
type Image struct {
	ID, FileID uuid.UUID
	Color      *string
	Position   int
	Status     string
	Variants   []ImageVariant
}

// ImageView là cách dựng đường dẫn ảnh: BaseURL là FILES_PUBLIC_BASE_URL, Preset là IMAGE_PRESET_VERSION (0 → 1).
type ImageView struct {
	BaseURL string
	Preset  int
}

// ProductImages trả ảnh đang hiện của mẫu productID của người bán sellerID theo thứ tự hiện, kèm biến thể của bộ kích
// thước chọn theo files.SelectPreset (một lời gọi files.PublicVariants cho cả mẫu). Mẫu không có, đã xóa, của người
// bán hay tenant khác → ErrNotFound.
func ProductImages(ctx context.Context, tx pgx.Tx, sellerID, productID uuid.UUID, view ImageView) ([]Image, error) {
	q := store.New(tx)
	if _, err := q.ProductVersionOf(ctx, store.ProductVersionOfParams{SellerID: sellerID, ID: productID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("đọc mẫu: %w", err)
	}
	rows, err := q.ListProductImages(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("đọc ảnh của mẫu: %w", err)
	}
	out := make([]Image, 0, len(rows))
	for _, r := range rows {
		out = append(out, Image{ID: r.ID, FileID: r.FileID, Color: textPtr(r.Color), Position: int(r.Position)})
	}
	if err := ResolveImages(ctx, tx, out, view); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveImages điền Status và Variants cho các ảnh imgs (FileID đã có) bằng một lời gọi files.PublicVariants. Ảnh bìa
// của danh sách sản phẩm dùng cùng hàm này.
func ResolveImages(ctx context.Context, tx pgx.Tx, imgs []Image, view ImageView) error {
	if len(imgs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(imgs))
	for _, i := range imgs {
		ids = append(ids, i.FileID)
	}
	vs, err := files.PublicVariants(ctx, tx, ids, view.BaseURL)
	if err != nil {
		return fmt.Errorf("đọc biến thể ảnh: %w", err)
	}
	preset := max(view.Preset, 1)
	for i := range imgs {
		picked := files.SelectPreset(vs[imgs[i].FileID], preset)
		imgs[i].Status, imgs[i].Variants = ImageLost, nil
		if len(picked) == 0 {
			continue
		}
		imgs[i].Status = ImageReady
		imgs[i].Variants = make([]ImageVariant, 0, len(picked))
		for _, v := range picked {
			imgs[i].Variants = append(imgs[i].Variants, ImageVariant{Width: v.Width, Format: v.Format, URL: v.URL})
		}
	}
	return nil
}

// AttachImage gắn tệp fileID (ready, mục đích product_image, cùng tenant) vào cuối danh sách ảnh của mẫu productID khi
// version của mẫu còn khớp; color (không bắt buộc) phải là một màu của biến thể trong mẫu. Tệp đã gắn và đang hiện →
// ErrAlreadyExists; tệp đã gắn rồi xóa mềm → hồi lại dòng đó. Version mẫu tăng 1, phát product.updated, audit. Lỗi:
// ErrNotFound (mẫu hay tệp không có, tệp đã xóa), ErrFileNotReady, ErrVersionConflict, ErrInvalidInput (màu, quá
// MaxImages), ErrAlreadyExists. Status, Variants của ảnh trả về để trống (đọc bằng ProductImages).
func (w Writer) AttachImage(ctx context.Context, tx pgx.Tx, productID uuid.UUID, productVersion int64, fileID uuid.UUID,
	color *string, actor identity.AuthUser,
) (Image, error) {
	if err := w.check(); err != nil {
		return Image{}, err
	}
	if err := checkImageFile(ctx, tx, fileID); err != nil {
		return Image{}, err
	}
	p, err := lockProduct(ctx, tx, productID, productVersion)
	if err != nil {
		return Image{}, err
	}
	c, err := imageColor(ctx, tx, p, color)
	if err != nil {
		return Image{}, err
	}
	q := store.New(tx)
	next, err := nextPosition(ctx, q, productID)
	if err != nil {
		return Image{}, err
	}
	img := Image{FileID: fileID, Color: c, Position: int(next)}
	old, err := q.LockProductImageByFile(ctx, store.LockProductImageByFileParams{ProductID: productID, FileID: fileID})
	switch {
	case err == nil && !old.DeletedAt.Valid:
		return Image{}, ErrAlreadyExists
	case err == nil:
		img.ID = old.ID
		err = q.ReviveProductImage(ctx, store.ReviveProductImageParams{ID: old.ID, Color: optText(deref(c)), Position: next})
	case errors.Is(err, pgx.ErrNoRows):
		img.ID, err = q.InsertProductImage(ctx, store.InsertProductImageParams{
			TenantID: actor.TenantID, ProductID: productID, FileID: fileID, Color: optText(deref(c)), Position: next,
			CreatedBy: &actor.UserID,
		})
	}
	if err != nil {
		return Image{}, fmt.Errorf("gắn ảnh: %w", err)
	}
	if err := w.imagesChanged(ctx, tx, actor, p, "catalog.image.attach", nil, imageAudit(img)); err != nil {
		return Image{}, err
	}
	return img, nil
}

// ReorderImages đặt thứ tự ảnh đang hiện của mẫu productID theo imageIDs khi version của mẫu còn khớp. imageIDs phải
// là đúng tập ảnh đang hiện (không thiếu, thừa, trùng), ngược lại ErrInvalidInput. Version mẫu tăng 1, phát
// product.updated, audit. Lỗi: ErrNotFound, ErrVersionConflict, ErrInvalidInput.
func (w Writer) ReorderImages(ctx context.Context, tx pgx.Tx, productID uuid.UUID, version int64, imageIDs []uuid.UUID,
	actor identity.AuthUser,
) error {
	if err := w.check(); err != nil {
		return err
	}
	p, err := lockProduct(ctx, tx, productID, version)
	if err != nil {
		return err
	}
	q := store.New(tx)
	rows, err := q.ListProductImages(ctx, productID)
	if err != nil {
		return fmt.Errorf("đọc ảnh của mẫu: %w", err)
	}
	before := make([]string, 0, len(rows))
	current := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		before = append(before, r.ID.String())
		current[r.ID] = true
	}
	if len(imageIDs) != len(rows) || len(rows) == 0 {
		return ErrInvalidInput
	}
	after := make([]string, 0, len(imageIDs))
	for i, id := range imageIDs {
		if !current[id] {
			return ErrInvalidInput // mã lạ, ảnh đã xóa, hay trùng (lần thứ hai không còn trong current)
		}
		delete(current, id)
		after = append(after, id.String())
		if err := q.SetProductImagePosition(ctx, store.SetProductImagePositionParams{ID: id, Position: int32(i + 1)}); err != nil {
			return fmt.Errorf("đặt vị trí ảnh: %w", err)
		}
	}
	return w.imagesChanged(ctx, tx, actor, p, "catalog.image.reorder", map[string]any{"order": before},
		map[string]any{"order": after})
}

// DeleteImage xóa mềm ảnh imageID của mẫu productID (đặt deleted_at; khôi phục được RestoreDays ngày). Ảnh đã xóa →
// không đổi gì. Version mẫu tăng 1, phát product.updated, audit. Lỗi: ErrNotFound.
func (w Writer) DeleteImage(ctx context.Context, tx pgx.Tx, productID, imageID uuid.UUID, actor identity.AuthUser) (Image, error) {
	p, img, deleted, err := w.lockImage(ctx, tx, productID, imageID)
	if err != nil || deleted {
		return img, err
	}
	if err := store.New(tx).SoftDeleteProductImage(ctx, store.SoftDeleteProductImageParams{ID: imageID, DeletedBy: &actor.UserID}); err != nil {
		return Image{}, fmt.Errorf("xóa ảnh: %w", err)
	}
	if err := w.imagesChanged(ctx, tx, actor, p, "catalog.image.delete", imageAudit(img), nil); err != nil {
		return Image{}, err
	}
	return img, nil
}

// RestoreImage khôi phục ảnh đã xóa imageID của mẫu productID về vị trí cũ. Ảnh đang hiện → không đổi gì; xóa quá
// RestoreDays ngày → ErrRestoreExpired; đã có MaxImages ảnh đang hiện → ErrInvalidInput. Version mẫu tăng 1, phát
// product.updated, audit. Lỗi: ErrNotFound, ErrRestoreExpired, ErrInvalidInput.
func (w Writer) RestoreImage(ctx context.Context, tx pgx.Tx, productID, imageID uuid.UUID, actor identity.AuthUser) (Image, error) {
	p, img, deleted, err := w.lockImage(ctx, tx, productID, imageID)
	if err != nil || !deleted {
		return img, err
	}
	q := store.New(tx)
	if _, err := nextPosition(ctx, q, productID); err != nil { // chỉ kiểm MaxImages: khôi phục về vị trí cũ
		return Image{}, err
	}
	if _, err := q.RestoreProductImage(ctx, store.RestoreProductImageParams{ID: imageID, KeepDays: RestoreDays}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Image{}, ErrRestoreExpired
		}
		return Image{}, fmt.Errorf("khôi phục ảnh: %w", err)
	}
	if err := w.imagesChanged(ctx, tx, actor, p, "catalog.image.restore", nil, imageAudit(img)); err != nil {
		return Image{}, err
	}
	return img, nil
}

// nextPosition trả vị trí cuối + 1 của ảnh đang hiện của mẫu (dòng mẫu đã khóa); đã có MaxImages ảnh → ErrInvalidInput.
func nextPosition(ctx context.Context, q *store.Queries, productID uuid.UUID) (int32, error) {
	pos, err := q.NextImagePosition(ctx, productID)
	if err != nil {
		return 0, fmt.Errorf("đọc vị trí ảnh: %w", err)
	}
	if pos.Active >= MaxImages {
		return 0, ErrInvalidInput
	}
	return pos.Next, nil
}

// lockImage kiểm Writer, khóa mẫu productID rồi ảnh imageID của mẫu đó (ADR-46 nhóm 4); trả ảnh và cờ đã xóa mềm.
func (w Writer) lockImage(ctx context.Context, tx pgx.Tx, productID, imageID uuid.UUID) (Product, Image, bool, error) {
	if err := w.check(); err != nil {
		return Product{}, Image{}, false, err
	}
	p, err := lockProductAnyVersion(ctx, tx, productID)
	if err != nil {
		return Product{}, Image{}, false, err
	}
	row, err := store.New(tx).LockProductImage(ctx, store.LockProductImageParams{ProductID: productID, ID: imageID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, Image{}, false, ErrNotFound
	}
	if err != nil {
		return Product{}, Image{}, false, fmt.Errorf("khóa ảnh: %w", err)
	}
	img := Image{ID: row.ID, FileID: row.FileID, Color: textPtr(row.Color), Position: int(row.Position)}
	return p, img, row.DeletedAt.Valid, nil
}

// checkImageFile: tệp có trong tenant (không thì ErrNotFound), chưa xóa, mục đích product_image và ready (không thì
// ErrFileNotReady).
func checkImageFile(ctx context.Context, tx pgx.Tx, fileID uuid.UUID) error {
	st, err := files.Status(ctx, tx, fileID)
	switch {
	case errors.Is(err, files.ErrNotFound):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("đọc tệp ảnh: %w", err)
	case st.Deleted:
		return ErrNotFound
	case st.Purpose != productImagePurpose || st.Status != files.StatusReady:
		return ErrFileNotReady
	}
	return nil
}

// imageColor chuẩn hóa màu của ảnh: nil giữ nil; khác nil phải trùng (sau khi bỏ khoảng trắng đầu cuối) màu của một
// biến thể trong mẫu p.
func imageColor(ctx context.Context, tx pgx.Tx, p Product, color *string) (*string, error) {
	if color == nil {
		return nil, nil //nolint:nilnil // không màu là hợp lệ
	}
	c := strings.TrimSpace(*color)
	vs, err := listVariants(ctx, store.New(tx), p.SellerID, p.ID)
	if err != nil {
		return nil, err
	}
	for _, v := range vs {
		if c != "" && v.Color == c {
			return &c, nil
		}
	}
	return nil, ErrInvalidInput
}

// imagesChanged tăng version mẫu (phát product.updated, báo hiệu) và ghi audit của lần đổi ảnh (thực thể là mẫu).
func (w Writer) imagesChanged(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, p Product, action string,
	before, after map[string]any,
) error {
	if err := w.bumpProduct(ctx, tx, actor.TenantID, p); err != nil {
		return err
	}
	return w.audit(ctx, tx, actor, action, "catalog.product", p.ID, before, after)
}

// imageAudit: chỉ mã ảnh, mã tệp, màu, vị trí (không tên tệp gốc).
func imageAudit(i Image) map[string]any {
	return map[string]any{
		"imageId": i.ID.String(), "fileId": i.FileID.String(), "color": deref(i.Color), "position": i.Position,
	}
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
