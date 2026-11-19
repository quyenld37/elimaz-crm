package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Phân loại sản phẩm (kế hoạch M1, Task M1-34; Q27): danh mục dạng cây tối đa MaxCategoryDepth cấp, thuộc tính và
// giá trị, nhãn kệ hàng. Dùng chung cả cửa hàng (không có người bán, spec 05). Module khác đọc qua view
// catalog.categories_v, attributes_v, attribute_values_v, tags_v, product_tags_v, product_attribute_values_v.

// Trạng thái danh mục.
const (
	CategoryActive   = "active"
	CategoryArchived = "archived"
)

// MaxCategoryDepth là số cấp tối đa của cây danh mục (Váy > Váy dài > Váy maxi).
const MaxCategoryDepth = 3

// Giới hạn dữ liệu vào (CHECK tương ứng ở migrations/0120_catalog_classification.sql).
const (
	maxClassName     = 100
	maxSlug          = 80
	maxClassifyItems = 100 // số giá trị thuộc tính, số nhãn của một mẫu
)

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9-]{1,80}$`)
	classCodeRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	slugJunk        = regexp.MustCompile(`[^a-z0-9]+`)
)

// Category là một danh mục. ParentID nil là danh mục gốc (Depth 1).
type Category struct {
	ID         uuid.UUID
	ParentID   *uuid.UUID
	Name, Slug string
	Depth      int
	Position   int32
	Status     string
	Version    int64
}

// CategoryInput là danh mục khi tạo. Slug rỗng → sinh từ tên (bỏ dấu, chữ thường, gạch nối).
type CategoryInput struct {
	ParentID   *uuid.UUID
	Name, Slug string
	Position   int32
}

// CategoryPatch là phần sửa của danh mục; trường nil giữ nguyên. Move: đổi cha sang ParentID (nil → lên gốc).
type CategoryPatch struct {
	Name, Slug *string
	Position   *int32
	Move       bool
	ParentID   *uuid.UUID
}

// Attribute là một thuộc tính kèm giá trị (theo vị trí).
type Attribute struct {
	ID         uuid.UUID
	Code, Name string
	Position   int32
	Values     []AttributeValue
}

// AttributeValue là một giá trị của thuộc tính.
type AttributeValue struct {
	ID, AttributeID uuid.UUID
	Value           string
	Position        int32
}

// AttributeInput là thuộc tính khi tạo; Code chuẩn hóa chữ thường.
type AttributeInput struct {
	Code, Name string
	Position   int32
}

// AttributeValueInput là giá trị khi thêm vào thuộc tính.
type AttributeValueInput struct {
	Value    string
	Position int32
}

// Tag là một nhãn kệ hàng (điều khiển kệ hàng trên website, 01 mục 3).
type Tag struct {
	ID         uuid.UUID
	Code, Name string
}

// TagInput là nhãn khi tạo; Code chuẩn hóa chữ thường.
type TagInput struct {
	Code, Name string
}

// Classification là phân loại của một mẫu: danh mục (nil là không có), giá trị thuộc tính, nhãn.
type Classification struct {
	CategoryID        *uuid.UUID
	AttributeValueIDs []uuid.UUID
	TagIDs            []uuid.UUID
}

// ProductClassification là phân loại đã lưu của mẫu ProductID ở Version; danh sách mã không trùng, tăng dần.
type ProductClassification struct {
	ProductID uuid.UUID
	Version   int64
	Classification
}

// CreateCategory tạo danh mục (active) trong tx; cha phải có và active, khóa chia sẻ dòng cha để đổi cha hay lưu trữ
// cùng lúc phải chờ. Lỗi: ErrInvalidInput, ErrNotFound (cha không có hay đã lưu trữ), ErrCategoryTooDeep,
// ErrAlreadyExists (trùng slug hay trùng tên cùng cha).
func (w Writer) CreateCategory(ctx context.Context, tx pgx.Tx, in CategoryInput, actor identity.AuthUser) (Category, error) {
	if err := w.check(); err != nil {
		return Category{}, err
	}
	name, slug, err := categoryText(in.Name, in.Slug)
	if err != nil {
		return Category{}, err
	}
	q := store.New(tx)
	depth := 1
	if in.ParentID != nil {
		parent, err := shareLockActiveCategory(ctx, q, *in.ParentID)
		if err != nil {
			return Category{}, err
		}
		depth = int(parent.Depth) + 1
	}
	if depth > MaxCategoryDepth {
		return Category{}, ErrCategoryTooDeep
	}
	row, err := q.InsertCategory(ctx, store.InsertCategoryParams{
		TenantID: actor.TenantID, ParentID: in.ParentID, Name: name, Slug: slug, Depth: int16(depth),
		Position: in.Position,
	})
	if err != nil {
		return Category{}, fmt.Errorf("ghi danh mục: %w", mapWriteError(err))
	}
	c := categoryFromRow(store.LockCategoryRow(row))
	if err := w.audit(ctx, tx, actor, "catalog.category.create", "catalog.category", c.ID, nil, categoryAudit(c)); err != nil {
		return Category{}, err
	}
	return c, nil
}

// UpdateCategory sửa tên, slug, vị trí, cha của danh mục id khi version còn khớp; version tăng 1. Đổi cha (Move) khóa
// mọi danh mục của shop theo id tăng dần trước, rồi kiểm bằng truy vấn đệ quy: cha mới không nằm trong cây con của id
// (ErrCategoryCycle), cả cây con sau khi chuyển không quá MaxCategoryDepth cấp (ErrCategoryTooDeep); depth của cây
// con đổi theo. Lỗi khác: ErrNotFound, ErrVersionConflict, ErrInvalidInput, ErrAlreadyExists.
func (w Writer) UpdateCategory(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, patch CategoryPatch,
	actor identity.AuthUser,
) (Category, error) {
	if err := w.check(); err != nil {
		return Category{}, err
	}
	q := store.New(tx)
	if patch.Move {
		if _, err := q.LockAllCategories(ctx); err != nil {
			return Category{}, fmt.Errorf("khóa cây danh mục: %w", err)
		}
	}
	before, err := lockCategory(ctx, q, id, version)
	if err != nil {
		return Category{}, err
	}
	after := before
	if patch.Name != nil {
		after.Name = *patch.Name
	}
	if patch.Slug != nil {
		after.Slug = *patch.Slug
	}
	if patch.Position != nil {
		after.Position = *patch.Position
	}
	if after.Name, after.Slug, err = categoryText(after.Name, after.Slug); err != nil {
		return Category{}, err
	}
	if patch.Move {
		if after.ParentID, after.Depth, err = moveTarget(ctx, q, before, patch.ParentID); err != nil {
			return Category{}, err
		}
	}
	row, err := q.UpdateCategory(ctx, store.UpdateCategoryParams{
		ID: id, Version: version, ParentID: after.ParentID, Name: after.Name, Slug: after.Slug,
		Depth: int16(after.Depth), Position: after.Position, //nolint:gosec // depth ≤ 3
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Category{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng; giữ để an toàn
	}
	if err != nil {
		return Category{}, fmt.Errorf("sửa danh mục: %w", mapWriteError(err))
	}
	if delta := after.Depth - before.Depth; delta != 0 {
		if err := q.ShiftDescendantsDepth(ctx, store.ShiftDescendantsDepthParams{ID: id, Delta: int16(delta)}); err != nil { //nolint:gosec // |delta| < 3
			return Category{}, fmt.Errorf("đổi độ sâu cây con: %w", err)
		}
	}
	after = categoryFromRow(store.LockCategoryRow(row))
	if err := w.audit(ctx, tx, actor, "catalog.category.update", "catalog.category", id, categoryAudit(before), categoryAudit(after)); err != nil {
		return Category{}, err
	}
	return after, nil
}

// moveTarget kiểm việc đổi cha của c sang parentID (nil → gốc) dưới khóa cây và trả cha, depth mới của c.
func moveTarget(ctx context.Context, q *store.Queries, c Category, parentID *uuid.UUID) (*uuid.UUID, int, error) {
	depth, candidate := 1, uuid.Nil
	if parentID != nil {
		if *parentID == c.ID {
			return nil, 0, ErrCategoryCycle
		}
		parent, err := shareLockActiveCategory(ctx, q, *parentID)
		if err != nil {
			return nil, 0, err
		}
		depth, candidate = int(parent.Depth)+1, *parentID
	}
	sub, err := q.CategorySubtree(ctx, store.CategorySubtreeParams{ID: c.ID, Candidate: candidate})
	if err != nil {
		return nil, 0, fmt.Errorf("đọc cây con danh mục: %w", err)
	}
	if sub.ContainsCandidate {
		return nil, 0, ErrCategoryCycle
	}
	if depth+int(sub.MaxDepth)-c.Depth > MaxCategoryDepth {
		return nil, 0, ErrCategoryTooDeep
	}
	return parentID, depth, nil
}

// ArchiveCategory lưu trữ danh mục id khi version còn khớp (đã lưu trữ → trả nguyên, không đổi). Còn danh mục con
// active hay mẫu active chưa xóa → ErrCategoryInUse (mẫu nháp, ngừng bán không chặn). Danh mục đã lưu trữ không gán
// được cho mẫu, không thêm con được. Lỗi khác: ErrNotFound, ErrVersionConflict.
func (w Writer) ArchiveCategory(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, actor identity.AuthUser) (Category, error) {
	if err := w.check(); err != nil {
		return Category{}, err
	}
	q := store.New(tx)
	before, err := lockCategory(ctx, q, id, version)
	if err != nil || before.Status == CategoryArchived {
		return before, err
	}
	inUse, err := q.CategoryInUse(ctx, id)
	if err != nil {
		return Category{}, fmt.Errorf("kiểm danh mục đang dùng: %w", err)
	}
	if inUse {
		return Category{}, ErrCategoryInUse
	}
	row, err := q.ArchiveCategory(ctx, store.ArchiveCategoryParams{ID: id, Version: version})
	if err != nil {
		return Category{}, fmt.Errorf("lưu trữ danh mục: %w", err)
	}
	after := categoryFromRow(store.LockCategoryRow(row))
	if err := w.audit(ctx, tx, actor, "catalog.category.archive", "catalog.category", id, categoryAudit(before), categoryAudit(after)); err != nil {
		return Category{}, err
	}
	return after, nil
}

// ListCategories trả mọi danh mục của shop (cả đã lưu trữ) theo cấp, vị trí, tên.
func ListCategories(ctx context.Context, tx pgx.Tx) ([]Category, error) {
	rows, err := store.New(tx).ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh mục: %w", err)
	}
	out := make([]Category, 0, len(rows))
	for _, r := range rows {
		out = append(out, categoryFromRow(store.LockCategoryRow(r)))
	}
	return out, nil
}

// CreateAttribute tạo thuộc tính. Lỗi: ErrInvalidInput, ErrAlreadyExists (mã đã có).
func (w Writer) CreateAttribute(ctx context.Context, tx pgx.Tx, in AttributeInput, actor identity.AuthUser) (Attribute, error) {
	if err := w.check(); err != nil {
		return Attribute{}, err
	}
	code, ok := classCode(in.Code)
	name := strings.TrimSpace(in.Name)
	if !ok || !validLine(name, maxClassName) {
		return Attribute{}, ErrInvalidInput
	}
	row, err := store.New(tx).InsertAttribute(ctx, store.InsertAttributeParams{
		TenantID: actor.TenantID, Code: code, Name: name, Position: in.Position,
	})
	if err != nil {
		return Attribute{}, fmt.Errorf("ghi thuộc tính: %w", mapWriteError(err))
	}
	a := Attribute{ID: row.ID, Code: row.Code, Name: row.Name, Position: row.Position, Values: []AttributeValue{}}
	if err := w.audit(ctx, tx, actor, "catalog.attribute.create", "catalog.attribute", a.ID, nil,
		map[string]any{"code": a.Code, "name": a.Name}); err != nil {
		return Attribute{}, err
	}
	return a, nil
}

// AddAttributeValue thêm giá trị vào thuộc tính attributeID. Lỗi: ErrInvalidInput, ErrNotFound (thuộc tính không có
// trong shop), ErrAlreadyExists (thuộc tính đã có giá trị này).
func (w Writer) AddAttributeValue(ctx context.Context, tx pgx.Tx, attributeID uuid.UUID, in AttributeValueInput,
	actor identity.AuthUser,
) (AttributeValue, error) {
	if err := w.check(); err != nil {
		return AttributeValue{}, err
	}
	value := strings.TrimSpace(in.Value)
	if !validLine(value, maxClassName) || strings.Contains(value, ";") { // ";" tách nhiều giá trị trong ô Excel
		return AttributeValue{}, ErrInvalidInput
	}
	row, err := store.New(tx).InsertAttributeValue(ctx, store.InsertAttributeValueParams{
		TenantID: actor.TenantID, AttributeID: attributeID, Value: value, Position: in.Position,
	})
	if err != nil {
		return AttributeValue{}, fmt.Errorf("ghi giá trị thuộc tính: %w", mapWriteError(err))
	}
	v := AttributeValue(row)
	if err := w.audit(ctx, tx, actor, "catalog.attribute.add_value", "catalog.attribute", attributeID, nil,
		map[string]any{"valueId": v.ID.String(), "value": v.Value}); err != nil {
		return AttributeValue{}, err
	}
	return v, nil
}

// ListAttributes trả mọi thuộc tính của shop kèm giá trị, theo vị trí.
func ListAttributes(ctx context.Context, tx pgx.Tx) ([]Attribute, error) {
	q := store.New(tx)
	attrs, err := q.ListAttributes(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc thuộc tính: %w", err)
	}
	values, err := q.ListAttributeValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc giá trị thuộc tính: %w", err)
	}
	byAttr := map[uuid.UUID][]AttributeValue{}
	for _, v := range values {
		byAttr[v.AttributeID] = append(byAttr[v.AttributeID], AttributeValue(v))
	}
	out := make([]Attribute, 0, len(attrs))
	for _, a := range attrs {
		vs := byAttr[a.ID]
		if vs == nil {
			vs = []AttributeValue{}
		}
		out = append(out, Attribute{ID: a.ID, Code: a.Code, Name: a.Name, Position: a.Position, Values: vs})
	}
	return out, nil
}

// CreateTag tạo nhãn. Lỗi: ErrInvalidInput, ErrAlreadyExists (mã đã có).
func (w Writer) CreateTag(ctx context.Context, tx pgx.Tx, in TagInput, actor identity.AuthUser) (Tag, error) {
	if err := w.check(); err != nil {
		return Tag{}, err
	}
	code, ok := classCode(in.Code)
	name := strings.TrimSpace(in.Name)
	if !ok || !validLine(name, maxClassName) {
		return Tag{}, ErrInvalidInput
	}
	row, err := store.New(tx).InsertTag(ctx, store.InsertTagParams{TenantID: actor.TenantID, Code: code, Name: name})
	if err != nil {
		return Tag{}, fmt.Errorf("ghi nhãn: %w", mapWriteError(err))
	}
	tg := Tag(row)
	if err := w.audit(ctx, tx, actor, "catalog.tag.create", "catalog.tag", tg.ID, nil,
		map[string]any{"code": tg.Code, "name": tg.Name}); err != nil {
		return Tag{}, err
	}
	return tg, nil
}

// ListTags trả mọi nhãn của shop theo mã.
func ListTags(ctx context.Context, tx pgx.Tx) ([]Tag, error) {
	rows, err := store.New(tx).ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc nhãn: %w", err)
	}
	out := make([]Tag, 0, len(rows))
	for _, r := range rows {
		out = append(out, Tag(r))
	}
	return out, nil
}

// SetClassification thay toàn bộ phân loại của mẫu productID của người bán sellerID khi version còn khớp: danh mục (phải có và active, khóa
// chia sẻ để lưu trữ cùng lúc phải chờ), giá trị thuộc tính, nhãn (mã trùng gộp lại; tối đa 100 mỗi loại). Version
// mẫu tăng 1; ghi product.updated, báo hiệu, audit cùng tx. Lỗi: ErrNotFound (mẫu, danh mục, giá trị hay nhãn không có
// trong shop; danh mục đã lưu trữ), ErrVersionConflict, ErrInvalidInput. Lỗi trả về thì tx phải lùi.
func (w Writer) SetClassification(ctx context.Context, tx pgx.Tx, sellerID, productID uuid.UUID, version int64, c Classification,
	actor identity.AuthUser,
) (ProductClassification, error) {
	if err := w.check(); err != nil {
		return ProductClassification{}, err
	}
	out := ProductClassification{ProductID: productID, Classification: Classification{
		CategoryID: c.CategoryID, AttributeValueIDs: sortedUnique(c.AttributeValueIDs), TagIDs: sortedUnique(c.TagIDs),
	}}
	if len(out.AttributeValueIDs) > maxClassifyItems || len(out.TagIDs) > maxClassifyItems {
		return ProductClassification{}, ErrInvalidInput
	}
	q := store.New(tx)
	if c.CategoryID != nil { // ADR-46 nhóm 4: danh mục trước mẫu
		if _, err := shareLockActiveCategory(ctx, q, *c.CategoryID); err != nil {
			return ProductClassification{}, err
		}
	}
	p, err := lockProduct(ctx, tx, sellerID, productID, version)
	if err != nil {
		return ProductClassification{}, err
	}
	before, err := readClassification(ctx, q, productID)
	if err != nil {
		return ProductClassification{}, err
	}
	old, err := q.GetProductCategory(ctx, store.GetProductCategoryParams{SellerID: p.SellerID, ID: productID})
	if err != nil {
		return ProductClassification{}, fmt.Errorf("đọc danh mục cũ của mẫu: %w", err)
	}
	before.CategoryID = old.CategoryID
	if out.Version, err = q.SetProductCategory(ctx, store.SetProductCategoryParams{
		SellerID: p.SellerID, ID: productID, Version: version, CategoryID: c.CategoryID,
	}); err != nil {
		return ProductClassification{}, fmt.Errorf("gán danh mục cho mẫu: %w", mapWriteError(err))
	}
	if err := replaceLinks(ctx, q, actor.TenantID, out); err != nil {
		return ProductClassification{}, err
	}
	if err := w.productChanged(ctx, tx, actor.TenantID, p.SellerID, productID, out.Version); err != nil {
		return ProductClassification{}, err
	}
	if err := w.audit(ctx, tx, actor, "catalog.product.classify", "catalog.product", productID,
		classificationAudit(before, p.Version), classificationAudit(out.Classification, out.Version)); err != nil {
		return ProductClassification{}, err
	}
	return out, nil
}

// GetClassification trả phân loại của mẫu productID của người bán sellerID; không có, đã xóa, của người bán hay
// tenant khác → ErrNotFound.
func GetClassification(ctx context.Context, tx pgx.Tx, sellerID, productID uuid.UUID) (ProductClassification, error) {
	q := store.New(tx)
	row, err := q.GetProductCategory(ctx, store.GetProductCategoryParams{SellerID: sellerID, ID: productID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductClassification{}, ErrNotFound
	}
	if err != nil {
		return ProductClassification{}, fmt.Errorf("đọc danh mục của mẫu: %w", err)
	}
	c, err := readClassification(ctx, q, productID)
	if err != nil {
		return ProductClassification{}, err
	}
	c.CategoryID = row.CategoryID
	return ProductClassification{ProductID: productID, Version: row.Version, Classification: c}, nil
}

// readClassification đọc giá trị thuộc tính và nhãn của mẫu (CategoryID để trống cho bên gọi).
func readClassification(ctx context.Context, q *store.Queries, productID uuid.UUID) (Classification, error) {
	vals, err := q.ListProductAttributeValueIDs(ctx, productID)
	if err != nil {
		return Classification{}, fmt.Errorf("đọc giá trị thuộc tính của mẫu: %w", err)
	}
	tags, err := q.ListProductTagIDs(ctx, productID)
	if err != nil {
		return Classification{}, fmt.Errorf("đọc nhãn của mẫu: %w", err)
	}
	return Classification{AttributeValueIDs: nonNil(vals), TagIDs: nonNil(tags)}, nil
}

// replaceLinks thay hai bảng nối của mẫu (ADR-46 nhóm 4: product_attribute_values trước product_tags). Mã không có
// trong shop → khóa ngoại ghép (tenant_id, …) báo lỗi → ErrNotFound.
func replaceLinks(ctx context.Context, q *store.Queries, tenantID uuid.UUID, c ProductClassification) error {
	if err := q.DeleteProductAttributeValues(ctx, c.ProductID); err != nil {
		return fmt.Errorf("xóa giá trị thuộc tính cũ của mẫu: %w", err)
	}
	if len(c.AttributeValueIDs) > 0 {
		if err := q.InsertProductAttributeValues(ctx, store.InsertProductAttributeValuesParams{
			TenantID: tenantID, ProductID: c.ProductID, AttributeValueIds: c.AttributeValueIDs,
		}); err != nil {
			return fmt.Errorf("gán giá trị thuộc tính: %w", mapWriteError(err))
		}
	}
	if err := q.DeleteProductTags(ctx, c.ProductID); err != nil {
		return fmt.Errorf("xóa nhãn cũ của mẫu: %w", err)
	}
	if len(c.TagIDs) > 0 {
		if err := q.InsertProductTags(ctx, store.InsertProductTagsParams{
			TenantID: tenantID, ProductID: c.ProductID, TagIds: c.TagIDs,
		}); err != nil {
			return fmt.Errorf("gán nhãn: %w", mapWriteError(err))
		}
	}
	return nil
}

// lockCategory khóa dòng danh mục id và so version.
func lockCategory(ctx context.Context, q *store.Queries, id uuid.UUID, version int64) (Category, error) {
	row, err := q.LockCategory(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, fmt.Errorf("khóa danh mục: %w", err)
	}
	c := categoryFromRow(row)
	if c.Version != version {
		return Category{}, ErrVersionConflict
	}
	return c, nil
}

// shareLockActiveCategory khóa chia sẻ danh mục id; không có trong shop hay đã lưu trữ → ErrNotFound.
func shareLockActiveCategory(ctx context.Context, q *store.Queries, id uuid.UUID) (store.ShareLockCategoryRow, error) {
	row, err := q.ShareLockCategory(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.Status != CategoryActive) {
		return store.ShareLockCategoryRow{}, ErrNotFound
	}
	if err != nil {
		return store.ShareLockCategoryRow{}, fmt.Errorf("khóa danh mục: %w", err)
	}
	return row, nil
}

// categoryText kiểm tên và slug (slug rỗng → sinh từ tên). Tên không chứa ">" (dấu tách cấp của cột Danh mục khi
// nhập Excel: "Váy > Váy dài").
func categoryText(name, slug string) (string, string, error) {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		slug = slugFrom(name)
	}
	if !validLine(name, maxClassName) || strings.Contains(name, ">") || !slugPattern.MatchString(slug) {
		return "", "", ErrInvalidInput
	}
	return name, slug, nil
}

// slugFrom đổi tên thành slug: bỏ dấu, chữ thường (textnorm.Fold), mỗi đoạn không phải chữ số hay chữ cái thành một
// gạch nối, tối đa maxSlug ký tự. Tên không có chữ hay số → "" (bên gọi báo ErrInvalidInput).
func slugFrom(name string) string {
	s := strings.Trim(slugJunk.ReplaceAllString(textnorm.Fold(name), "-"), "-")
	if len(s) > maxSlug {
		s = strings.TrimRight(s[:maxSlug], "-")
	}
	return s
}

// classCode chuẩn hóa chữ thường và kiểm mã thuộc tính, mã nhãn.
func classCode(s string) (string, bool) {
	c := strings.ToLower(strings.TrimSpace(s))
	return c, classCodeRegexp.MatchString(c)
}

func categoryFromRow(r store.LockCategoryRow) Category {
	return Category{
		ID: r.ID, ParentID: r.ParentID, Name: r.Name, Slug: r.Slug, Depth: int(r.Depth), Position: r.Position,
		Status: r.Status, Version: r.Version,
	}
}

func categoryAudit(c Category) map[string]any {
	m := map[string]any{
		"name": c.Name, "slug": c.Slug, "depth": c.Depth, "position": c.Position, "status": c.Status,
		"version": c.Version, "parentId": nil,
	}
	if c.ParentID != nil {
		m["parentId"] = c.ParentID.String()
	}
	return m
}

func classificationAudit(c Classification, version int64) map[string]any {
	m := map[string]any{
		"version": version, "attributeValues": len(c.AttributeValueIDs), "tags": len(c.TagIDs), "categoryId": nil,
	}
	if c.CategoryID != nil {
		m["categoryId"] = c.CategoryID.String()
	}
	return m
}

func sortedUnique(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return nonNil(slices.Compact(out))
}

func nonNil(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}
