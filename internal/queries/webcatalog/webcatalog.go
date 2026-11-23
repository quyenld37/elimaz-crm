// Package webcatalog ghép view chỉ đọc của catalog và inventory thành danh mục cho website (kế hoạch M5, Task 190 /
// M5-07): mẫu theo trang hay theo mã, luồng thay đổi, phân loại.
//
// Gói không ghi gì, không nhập module nào (luật f của tests/arch/boundaries_test) và chỉ nhắc view _v đã công bố
// (tests/arch/sqlusage_test). Mọi hàm nhận tx của tenancy.WithTenant (crm_app). Không có giá nhập. Bài viết trả nguyên
// cột article; bên gọi dựng HTML bằng catalog.ArticleHTML.
//
// Khi nào không dùng: giá, tồn khi đặt hàng (đọc bằng module trong transaction của đơn); màn hình nhân viên
// (internal/queries).
package webcatalog

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/queries/webcatalog/internal/store"
)

// ErrBadLimit báo limit ngoài [1, giới hạn] hoặc quá MaxIDs mã.
var ErrBadLimit = errors.New("webcatalog: limit ngoài khoảng")

// Products trả tối đa limit (1–MaxPage) mẫu có mã lớn hơn after (uuid.Nil: từ đầu) theo mã tăng dần, chưa xóa, của
// người bán đang hoạt động, kèm biến thể, ảnh (một lời gọi files.PublicVariants cho cả trang), nhãn, thuộc tính. next
// là mã của mẫu cuối khi trang đủ limit (còn có thể có trang sau), uuid.Nil khi hết.
func Products(ctx context.Context, tx pgx.Tx, after uuid.UUID, limit int, view ImageView) ([]WebProduct, uuid.UUID, error) {
	if limit < 1 || limit > MaxPage {
		return nil, uuid.Nil, ErrBadLimit
	}
	q := store.New(tx)
	sellers, err := q.ActiveSellers(ctx)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("đọc người bán đang hoạt động: %w", err)
	}
	rows, err := q.ProductPage(ctx, store.ProductPageParams{After: after, Sellers: sellers, Lim: int32(limit)})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("đọc trang mẫu cho website: %w", err)
	}
	out, err := assemble(ctx, tx, rows, view)
	if err != nil {
		return nil, uuid.Nil, err
	}
	next := uuid.Nil
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// ProductsByID trả các mẫu trong ids (tối đa MaxIDs, mã trùng tính một lần) theo mã tăng dần, cùng điều kiện với
// Products: mã không có, đã xóa, của người bán ngừng hoạt động thì không có trong kết quả.
func ProductsByID(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, view ImageView) ([]WebProduct, error) {
	if len(ids) > MaxIDs {
		return nil, ErrBadLimit
	}
	if len(ids) == 0 {
		return []WebProduct{}, nil
	}
	q := store.New(tx)
	sellers, err := q.ActiveSellers(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc người bán đang hoạt động: %w", err)
	}
	rows, err := q.ProductsByIDs(ctx, store.ProductsByIDsParams{Ids: ids, Sellers: sellers})
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu theo mã cho website: %w", err)
	}
	page := make([]store.ProductPageRow, len(rows))
	for i, r := range rows {
		page[i] = store.ProductPageRow(r)
	}
	return assemble(ctx, tx, page, view)
}

// Changes trả mã các mẫu có mẫu, biến thể hay số dư tồn đổi từ since (gồm cả mốc since), tối đa limit (1–MaxChanges),
// xếp theo mốc đổi rồi mã. Gồm cả mẫu đã xóa, của người bán ngừng (website gỡ khi ProductsByID không trả). until: khi
// trang đủ limit là mốc đổi lớn nhất đã trả (more = true); khi chưa đủ là now() của transaction đọc (đã xem hết tới
// đó). Bên gọi gửi lần sau since = until − 30 s và bỏ mã trùng (bù transaction ghi commit trễ hơn mốc của nó).
func Changes(ctx context.Context, tx pgx.Tx, since time.Time, limit int) (ids []uuid.UUID, until time.Time, more bool, err error) {
	if limit < 1 || limit > MaxChanges {
		return nil, time.Time{}, false, ErrBadLimit
	}
	q := store.New(tx)
	rows, err := q.Changes(ctx, store.ChangesParams{Since: since, Lim: int32(limit)})
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("đọc thay đổi danh mục cho website: %w", err)
	}
	ids = make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ProductID
	}
	if len(rows) == limit {
		return ids, rows[len(rows)-1].ChangedAt.UTC(), true, nil
	}
	now, err := q.Now(ctx)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("đọc mốc thay đổi: %w", err)
	}
	return ids, now.UTC(), false, nil
}

// Taxonomy trả danh mục đang dùng, thuộc tính kèm giá trị, nhãn (theo vị trí rồi tên).
func Taxonomy(ctx context.Context, tx pgx.Tx) (WebTaxonomy, error) {
	q := store.New(tx)
	cats, err := q.Categories(ctx)
	if err != nil {
		return WebTaxonomy{}, fmt.Errorf("đọc danh mục cho website: %w", err)
	}
	attrs, err := q.Attributes(ctx)
	if err != nil {
		return WebTaxonomy{}, fmt.Errorf("đọc thuộc tính cho website: %w", err)
	}
	vals, err := q.AttributeValues(ctx)
	if err != nil {
		return WebTaxonomy{}, fmt.Errorf("đọc giá trị thuộc tính cho website: %w", err)
	}
	tags, err := q.Tags(ctx)
	if err != nil {
		return WebTaxonomy{}, fmt.Errorf("đọc nhãn cho website: %w", err)
	}
	out := WebTaxonomy{
		Categories: make([]WebCategory, 0, len(cats)), Attributes: make([]WebAttribute, 0, len(attrs)),
		Tags: make([]WebTag, 0, len(tags)),
	}
	for _, c := range cats {
		wc := WebCategory{ID: c.CategoryID, Name: c.Name, Slug: c.Slug, Depth: int(c.Depth), Position: int(c.Position)}
		if c.ParentID.Valid {
			p := uuid.UUID(c.ParentID.Bytes)
			wc.ParentID = &p
		}
		out.Categories = append(out.Categories, wc)
	}
	byAttr := make(map[uuid.UUID][]WebAttributeValue, len(attrs))
	for _, v := range vals {
		byAttr[v.AttributeID] = append(byAttr[v.AttributeID], WebAttributeValue{
			ID: v.AttributeValueID, Value: v.Value, Position: int(v.Position),
		})
	}
	for _, a := range attrs {
		vs := byAttr[a.AttributeID]
		if vs == nil {
			vs = []WebAttributeValue{}
		}
		out.Attributes = append(out.Attributes, WebAttribute{
			ID: a.AttributeID, Code: a.Code, Name: a.Name, Position: int(a.Position), Values: vs,
		})
	}
	for _, t := range tags {
		out.Tags = append(out.Tags, WebTag{Code: t.Code, Name: t.Name})
	}
	return out, nil
}

// assemble đọc biến thể, ảnh, nhãn, thuộc tính của các mẫu rows trong bốn câu (theo danh sách mã) và một lời gọi
// files.PublicVariants cho mọi ảnh, rồi ghép theo thứ tự của rows.
func assemble(ctx context.Context, tx pgx.Tx, rows []store.ProductPageRow, view ImageView) ([]WebProduct, error) {
	out := make([]WebProduct, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	at := make(map[uuid.UUID]int, len(rows))
	for i, r := range rows {
		ids[i], at[r.ProductID] = r.ProductID, i
		out[i] = WebProduct{
			ID: r.ProductID, SellerID: r.SellerID, Code: r.Code, Name: r.Name, ShortDescription: r.Description,
			Article: r.Article, Status: r.Status, CategoryIDs: []uuid.UUID{}, Tags: []string{},
			Attributes: []WebAttr{}, Images: []WebImage{}, Variants: []WebVariant{}, UpdatedAt: r.UpdatedAt.UTC(),
			Version: r.Version,
		}
		if r.CategoryID.Valid {
			out[i].CategoryIDs = append(out[i].CategoryIDs, uuid.UUID(r.CategoryID.Bytes))
		}
	}
	q := store.New(tx)
	if err := attachVariants(ctx, q, ids, at, out); err != nil {
		return nil, err
	}
	if err := attachClassification(ctx, q, ids, at, out); err != nil {
		return nil, err
	}
	if err := attachImages(ctx, tx, q, ids, at, out, view); err != nil {
		return nil, err
	}
	return out, nil
}

// attachVariants gắn biến thể kèm còn hàng ở kho DefaultWarehouseCode (tenant chưa có kho đó: không còn hàng).
func attachVariants(ctx context.Context, q *store.Queries, ids []uuid.UUID, at map[uuid.UUID]int, out []WebProduct) error {
	wh, err := defaultWarehouse(ctx, q)
	if err != nil {
		return err
	}
	vs, err := q.VariantsOf(ctx, store.VariantsOfParams{WarehouseID: wh, ProductIds: ids})
	if err != nil {
		return fmt.Errorf("đọc biến thể cho website: %w", err)
	}
	for _, v := range vs {
		wv := WebVariant{
			ID: v.VariantID, SKU: v.Sku, Barcode: v.Barcode, Color: v.Color, Size: v.Size, Price: v.Price,
			Status: v.VariantStatus, Sellable: v.Sellable.Valid && v.Sellable.Bool, Available: v.Available > 0,
		}
		if v.WeightGrams.Valid {
			w := int(v.WeightGrams.Int32)
			wv.WeightGrams = &w
		}
		p := &out[at[v.ProductID]]
		p.Variants = append(p.Variants, wv)
	}
	return nil
}

// attachClassification gắn mã nhãn (theo mã) và thuộc tính của các mẫu; tên nhãn, thuộc tính đọc từ bảng nhỏ chỉ khi
// có mẫu mang chúng.
func attachClassification(ctx context.Context, q *store.Queries, ids []uuid.UUID, at map[uuid.UUID]int, out []WebProduct) error {
	if err := attachTags(ctx, q, ids, at, out); err != nil {
		return err
	}
	return attachAttributes(ctx, q, ids, at, out)
}

func attachTags(ctx context.Context, q *store.Queries, ids []uuid.UUID, at map[uuid.UUID]int, out []WebProduct) error {
	tagIDs, err := q.TagIDsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc nhãn của mẫu cho website: %w", err)
	}
	if len(tagIDs) == 0 {
		return nil
	}
	tags, err := q.Tags(ctx)
	if err != nil {
		return fmt.Errorf("đọc nhãn cho website: %w", err)
	}
	code := make(map[uuid.UUID]string, len(tags))
	for _, t := range tags {
		code[t.TagID] = t.Code
	}
	for _, t := range tagIDs {
		if c, ok := code[t.TagID]; ok {
			p := &out[at[t.ProductID]]
			p.Tags = append(p.Tags, c)
		}
	}
	for i := range out {
		slices.Sort(out[i].Tags)
	}
	return nil
}

// attrKey là thứ tự của một giá trị thuộc tính trên mẫu: vị trí thuộc tính, tên, vị trí giá trị, giá trị.
type attrKey struct {
	attrPos  int32
	attrName string
	valPos   int32
	value    string
}

func compareAttr(x, y attrKey) int {
	return cmp.Or(cmp.Compare(x.attrPos, y.attrPos), strings.Compare(x.attrName, y.attrName),
		cmp.Compare(x.valPos, y.valPos), strings.Compare(x.value, y.value))
}

func attachAttributes(ctx context.Context, q *store.Queries, ids []uuid.UUID, at map[uuid.UUID]int, out []WebProduct) error {
	valIDs, err := q.AttributeValueIDsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc thuộc tính của mẫu cho website: %w", err)
	}
	if len(valIDs) == 0 {
		return nil
	}
	attrs, err := q.Attributes(ctx)
	if err != nil {
		return fmt.Errorf("đọc thuộc tính cho website: %w", err)
	}
	vals, err := q.AttributeValues(ctx)
	if err != nil {
		return fmt.Errorf("đọc giá trị thuộc tính cho website: %w", err)
	}
	attrOf := make(map[uuid.UUID]store.CatalogAttributesV, len(attrs))
	for _, a := range attrs {
		attrOf[a.AttributeID] = a
	}
	keyOf := make(map[uuid.UUID]attrKey, len(vals))
	for _, v := range vals {
		if a, ok := attrOf[v.AttributeID]; ok {
			keyOf[v.AttributeValueID] = attrKey{a.Position, a.Name, v.Position, v.Value}
		}
	}
	keys := make([][]attrKey, len(out))
	for _, pv := range valIDs {
		if k, ok := keyOf[pv.AttributeValueID]; ok {
			keys[at[pv.ProductID]] = append(keys[at[pv.ProductID]], k)
		}
	}
	for i, ks := range keys {
		slices.SortFunc(ks, compareAttr)
		for _, k := range ks {
			out[i].Attributes = append(out[i].Attributes, WebAttr{Name: k.attrName, Value: k.value, Position: int(k.attrPos)})
		}
	}
	return nil
}

// attachImages gắn ảnh của các mẫu: một câu đọc ảnh, một lời gọi files.PublicVariants cho mọi tệp; chọn bộ kích thước
// như catalog.ProductImages (files.SelectPreset theo view.Preset); tệp chưa có biến thể (đang xử lý, mất) bị bỏ.
func attachImages(ctx context.Context, tx pgx.Tx, q *store.Queries, ids []uuid.UUID, at map[uuid.UUID]int,
	out []WebProduct, view ImageView,
) error {
	imgs, err := q.ImagesOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc ảnh của mẫu cho website: %w", err)
	}
	if len(imgs) == 0 {
		return nil
	}
	fileIDs := make([]uuid.UUID, 0, len(imgs))
	for _, im := range imgs {
		fileIDs = append(fileIDs, im.FileID)
	}
	vars, err := files.PublicVariants(ctx, tx, fileIDs, view.BaseURL)
	if err != nil {
		return fmt.Errorf("đọc biến thể ảnh cho website: %w", err)
	}
	preset := max(view.Preset, 1)
	for _, im := range imgs {
		picked := files.SelectPreset(vars[im.FileID], preset)
		if len(picked) == 0 {
			continue
		}
		wi := WebImage{Position: int(im.Position), Variants: make([]WebImageVariant, 0, len(picked))}
		if im.Color.Valid {
			c := im.Color.String
			wi.Color = &c
		}
		for _, v := range picked {
			wi.Variants = append(wi.Variants, WebImageVariant{Width: v.Width, Format: v.Format, URL: v.URL})
		}
		p := &out[at[im.ProductID]]
		p.Images = append(p.Images, wi)
	}
	return nil
}
