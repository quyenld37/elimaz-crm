package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Trạng thái của bản nhập Excel (Task M1-47; applying, applied ở Task M1-47b).
const (
	ImportValidating = "validating"
	ImportInvalid    = "invalid"
	ImportReady      = "ready"
	ImportApplying   = "applying"
	ImportApplied    = "applied"
	ImportDuplicate  = "duplicate"
	ImportFailed     = "failed"
)

// importPurpose là mục đích tệp của tệp nhập (files.M1Purposes).
const importPurpose = "catalog_import"

// ImportError là một lỗi của tệp nhập: dòng Excel (0: cả tệp), tiêu đề cột (rỗng: cả dòng hay cả tệp), mã, câu tiếng
// Việt không chép ô dài quá 50 ký tự.
type ImportError = importer.RowError

// ImportPlan là kế hoạch ghi của tệp không lỗi (chỉ dữ liệu sản phẩm); Task M1-47b áp nó.
type ImportPlan = importer.Plan

// ImportSummary là phần tóm tắt của bản nhập: số mẫu, số biến thể sẽ tạo, sửa, giữ nguyên (ready); Errors là tổng số
// lỗi (invalid; danh sách lưu tối đa MaxImportErrors lỗi đầu).
type ImportSummary struct {
	Models    int `json:"models"`
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Errors    int `json:"errors,omitempty"`
}

// MaxImportErrors là số lỗi lưu tối đa của một bản nhập (tệp sai hàng loạt không làm dòng bản nhập phình to).
const MaxImportErrors = 1000

// Import là một bản nhập. Plan khác nil chỉ khi Status là ready (và các bước áp sau đó); DuplicateOf khác nil chỉ khi
// duplicate.
type Import struct {
	ID, FileID, SellerID uuid.UUID
	Status               string
	Summary              ImportSummary
	Errors               []ImportError
	Plan                 *ImportPlan
	DuplicateOf          *uuid.UUID
	CreatedBy            uuid.UUID
	Version              int64
	CreatedAt, UpdatedAt time.Time
}

// CreateImport tạo bản nhập validating của người bán sellerID cho tệp fileID (mục đích catalog_import, đã uploaded)
// và xếp lệnh catalog.import.validate (DedupeKey = mã bản nhập), ghi audit, trong tx của bên gọi. Lỗi: ErrNotFound
// (tệp không có, của tenant khác, đã xóa; người bán không có), ErrFileNotReady (sai mục đích, chưa tải xong).
func (w Writer) CreateImport(ctx context.Context, tx pgx.Tx, sellerID, fileID uuid.UUID, actor identity.AuthUser) (Import, error) {
	if err := w.check(); err != nil {
		return Import{}, err
	}
	st, err := files.Status(ctx, tx, fileID)
	switch {
	case errors.Is(err, files.ErrNotFound):
		return Import{}, ErrNotFound
	case err != nil:
		return Import{}, fmt.Errorf("đọc tệp nhập: %w", err)
	case st.Deleted:
		return Import{}, ErrNotFound
	case st.Purpose != importPurpose || st.Status != files.StatusUploaded:
		return Import{}, ErrFileNotReady
	}
	row, err := store.New(tx).InsertImport(ctx, store.InsertImportParams{
		TenantID: actor.TenantID, FileID: fileID, SellerID: sellerID, CreatedBy: actor.UserID,
	})
	if err != nil {
		return Import{}, fmt.Errorf("ghi bản nhập: %w", mapWriteError(err))
	}
	tenant := actor.TenantID
	if _, _, err := w.Queue.Enqueue(ctx, tx, ImportValidateKind(), queue.NewJob{
		TenantID: &tenant, Payload: importPayload{ImportID: row.ID}, DedupeKey: row.ID.String(),
	}); err != nil {
		return Import{}, fmt.Errorf("xếp lệnh kiểm bản nhập: %w", err)
	}
	if err := w.audit(ctx, tx, actor, "catalog.import.create", "catalog.import", row.ID, nil,
		map[string]any{"fileId": fileID.String(), "status": row.Status}); err != nil {
		return Import{}, err
	}
	return importFromRow(store.GetImportRow(row))
}

// GetImport đọc bản nhập id của người bán sellerID; không có, của người bán hay tenant khác → ErrNotFound.
func GetImport(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Import, error) {
	row, err := store.New(tx).GetImport(ctx, store.GetImportParams{SellerID: sellerID, ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Import{}, ErrNotFound
	case err != nil:
		return Import{}, fmt.Errorf("đọc bản nhập: %w", err)
	}
	return importFromRow(row)
}

func importFromRow(r store.GetImportRow) (Import, error) {
	out := Import{
		ID: r.ID, FileID: r.FileID, SellerID: r.SellerID, Status: r.Status, DuplicateOf: r.DuplicateOf,
		CreatedBy: r.CreatedBy, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Errors: []ImportError{},
	}
	if err := json.Unmarshal(r.Summary, &out.Summary); err != nil {
		return Import{}, fmt.Errorf("đọc tóm tắt bản nhập %s: %w", r.ID, err)
	}
	if err := json.Unmarshal(r.Errors, &out.Errors); err != nil {
		return Import{}, fmt.Errorf("đọc lỗi bản nhập %s: %w", r.ID, err)
	}
	if r.Plan != nil {
		out.Plan = &ImportPlan{}
		if err := json.Unmarshal(r.Plan, out.Plan); err != nil {
			return Import{}, fmt.Errorf("đọc kế hoạch bản nhập %s: %w", r.ID, err)
		}
	}
	return out, nil
}

// ImportTemplate sinh tệp mẫu Excel sản phẩm (importer.Template) với danh mục đang dùng và thuộc tính hiện có của shop.
func ImportTemplate(ctx context.Context, tx pgx.Tx) ([]byte, error) {
	refs, err := classificationRefs(ctx, tx)
	if err != nil {
		return nil, err
	}
	b, err := importer.Template(refs)
	if err != nil {
		return nil, fmt.Errorf("sinh mẫu nhập: %w", err)
	}
	return b, nil
}

// classificationRefs đọc phần Refs dùng chung cả shop: danh mục active theo đường dẫn tên từ gốc (đường dẫn của hơn
// một danh mục bị bỏ: tệp chọn nó thì báo danh mục không có, không gán bừa), thuộc tính theo tên
// (hai thuộc tính trùng tên: lấy cái đứng trước theo vị trí), nhãn theo mã.
func classificationRefs(ctx context.Context, tx pgx.Tx) (importer.Refs, error) {
	refs := importer.Refs{
		Categories: map[string]uuid.UUID{}, Attributes: map[string]map[string]uuid.UUID{}, Tags: map[string]uuid.UUID{},
	}
	cats, err := ListCategories(ctx, tx)
	if err != nil {
		return refs, err
	}
	byID := make(map[uuid.UUID]Category, len(cats))
	for _, c := range cats {
		byID[c.ID] = c
	}
	ambiguous := map[string]bool{}
	for _, c := range cats {
		if c.Status != CategoryActive {
			continue
		}
		names := []string{c.Name}
		for p := c.ParentID; p != nil && len(names) <= MaxCategoryDepth; p = byID[*p].ParentID {
			names = append([]string{byID[*p].Name}, names...)
		}
		path := importer.CategoryPath(names...)
		if _, dup := refs.Categories[path]; dup || ambiguous[path] {
			// Hai danh mục cùng đường dẫn (tên có ">" ghi trước khi categoryText cấm): tệp không chọn được cái nào.
			delete(refs.Categories, path)
			ambiguous[path] = true
			continue
		}
		refs.Categories[path] = c.ID
	}
	attrs, err := ListAttributes(ctx, tx)
	if err != nil {
		return refs, err
	}
	for _, a := range attrs {
		if _, dup := refs.Attributes[a.Name]; dup {
			continue
		}
		values := make(map[string]uuid.UUID, len(a.Values))
		for _, v := range a.Values {
			values[v.Value] = v.ID
		}
		refs.Attributes[a.Name] = values
	}
	tags, err := ListTags(ctx, tx)
	if err != nil {
		return refs, err
	}
	for _, t := range tags {
		refs.Tags[t.Code] = t.ID
	}
	return refs, nil
}

// existingRefs điền refs.ExistingSKUs, refs.ExistingProducts của người bán sellerID, chỉ cho các SKU và mã mẫu skus,
// codes (đã viết hoa) có trong tệp.
func existingRefs(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, skus, codes []string, refs *importer.Refs) error {
	q := store.New(tx)
	vs, err := q.ListVariantsBySKUs(ctx, store.ListVariantsBySKUsParams{SellerID: sellerID, Skus: skus})
	if err != nil {
		return fmt.Errorf("đọc biến thể có sẵn: %w", err)
	}
	refs.ExistingSKUs = make(map[string]importer.ExistingVariant, len(vs))
	for _, v := range vs {
		refs.ExistingSKUs[v.Sku] = importer.ExistingVariant{
			ID: v.ID, ProductID: v.ProductID, ProductCode: v.ProductCode, Version: v.Version, Barcode: v.Barcode,
			Color: v.Color, ColorHex: v.ColorHex.String, Size: v.Size, Price: v.Price, CostPrice: v.CostPrice,
		}
	}
	ps, err := q.ListVariantsByProductCodes(ctx, store.ListVariantsByProductCodesParams{SellerID: sellerID, Codes: codes})
	if err != nil {
		return fmt.Errorf("đọc mẫu có sẵn: %w", err)
	}
	refs.ExistingProducts = map[string]importer.ExistingProduct{}
	for _, r := range ps {
		p, ok := refs.ExistingProducts[r.Code]
		if !ok {
			p = importer.ExistingProduct{ID: r.ProductID, Options: map[importer.Option]string{}}
			refs.ExistingProducts[r.Code] = p
		}
		if r.Sku.Valid {
			p.Options[importer.Option{Color: r.Color.String, Size: r.Size.String}] = r.Sku.String
		}
	}
	return nil
}
