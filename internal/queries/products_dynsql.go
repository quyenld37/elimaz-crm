package queries

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// ErrInvalidInput báo bộ lọc, chuỗi tìm, giới hạn hay con trỏ không hợp lệ.
var ErrInvalidInput = errors.New("queries: dữ liệu vào không hợp lệ")

// Giới hạn của danh sách và tìm.
const (
	MaxListLimit   = 100  // ProductFilter.Limit tối đa
	MaxSearchLimit = 100  // SearchVariants limit tối đa
	maxQueryRunes  = 100  // chuỗi tìm (sau khi bỏ khoảng trắng đầu cuối) tối đa
	maxQMatches    = 1000 // số mẫu khớp ProductFilter.Q tối đa (catalog.search_products)
)

// Cursor là vị trí sau dòng cuối của một trang danh sách mẫu (thứ tự updated_at giảm, rồi id giảm).
type Cursor struct {
	UpdatedAt time.Time
	ID        uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ cho API (base64url của "<unix micro>.<id>").
func (c Cursor) String() string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.UpdatedAt.UnixMicro(), 10) + "." + c.ID.String()))
}

// ParseCursor đọc chuỗi do Cursor.String tạo. Hỏng → ErrInvalidInput.
func ParseCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("đọc con trỏ: %w", ErrInvalidInput)
	}
	ts, id, ok := strings.Cut(string(raw), ".")
	if !ok {
		return Cursor{}, fmt.Errorf("đọc con trỏ: %w", ErrInvalidInput)
	}
	micros, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("đọc con trỏ: %w", ErrInvalidInput)
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return Cursor{}, fmt.Errorf("đọc con trỏ: %w", ErrInvalidInput)
	}
	return Cursor{UpdatedAt: time.UnixMicro(micros).UTC(), ID: u}, nil
}

// ProductFilter là bộ lọc của ListProducts. SellerID bắt buộc (ADR-29). Q: tìm theo mã mẫu, SKU (tiền tố), barcode
// (đúng), tên không dấu (khớp hơn 1.000 mẫu: chỉ 1.000 mẫu sửa gần nhất). Status rỗng: mọi trạng thái. CategoryID:
// danh mục đó và mọi danh mục con, cháu. WarehouseID khác uuid.Nil: thêm cột Available ở kho đó (bên gọi đã kiểm
// quyền xem kho). Cursor: trang sau. Limit 1–100.
type ProductFilter struct {
	SellerID    uuid.UUID
	Q           string
	Status      []string
	CategoryID  *uuid.UUID
	TagID       *uuid.UUID
	WarehouseID uuid.UUID
	Cursor      *Cursor
	Limit       int
}

// ProductRow là một mẫu của danh sách. CategoryPath: tên danh mục từ gốc tới lá (rỗng khi chưa gán). MinPrice,
// MaxPrice: 0 khi mẫu chưa có biến thể. Available: tổng có thể bán của các biến thể ở kho của bộ lọc (có thể âm:
// thiếu so với phần giữ; 0 khi chưa có dòng số dư nào ở kho đó); nil khi bộ lọc không có kho. Ảnh bìa: ảnh có vị trí
// nhỏ nhất.
type ProductRow struct {
	ProductID    uuid.UUID
	Code         string
	Name         string
	Status       string
	CategoryID   *uuid.UUID
	CategoryPath []string
	VariantCount int
	MinPrice     int64
	MaxPrice     int64
	Available    *int64
	UpdatedAt    time.Time
	// CoverImageID, CoverFileID: ảnh đang hiện đứng đầu của mẫu (catalog.product_images_v); nil khi chưa có ảnh.
	// Bên gọi lấy đường dẫn bằng catalog.ResolveImages.
	CoverImageID *uuid.UUID
	CoverFileID  *uuid.UUID
}

// ListProducts trả một trang mẫu (chưa xóa) của người bán f.SellerID, sửa gần nhất trước, và con trỏ trang sau (nil
// khi hết). Chạy trong tenancy.WithTenant. Lỗi: ErrInvalidInput.
func ListProducts(ctx context.Context, tx pgx.Tx, f ProductFilter) ([]ProductRow, *Cursor, error) {
	var tree categoryTree
	var inCategory []uuid.UUID
	if f.CategoryID != nil {
		var err error
		if tree, err = readCategoryTree(ctx, tx); err != nil {
			return nil, nil, err
		}
		inCategory = tree.subtree(*f.CategoryID)
	}
	sql, args, err := productsSQL(f, inCategory)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách mẫu: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ProductRow, error) {
		var p ProductRow
		var n int64
		err := r.Scan(&p.ProductID, &p.Code, &p.Name, &p.Status, &p.CategoryID, &n, &p.MinPrice, &p.MaxPrice,
			&p.Available, &p.UpdatedAt, &p.CoverImageID, &p.CoverFileID)
		p.VariantCount = int(n)
		return p, err //nolint:wrapcheck // bọc ở dưới
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách mẫu: %w", err)
	}
	var next *Cursor
	if len(out) > f.Limit {
		out = out[:f.Limit]
		last := out[len(out)-1]
		next = &Cursor{UpdatedAt: last.UpdatedAt, ID: last.ProductID}
	}
	if tree == nil && slices.ContainsFunc(out, func(p ProductRow) bool { return p.CategoryID != nil }) {
		if tree, err = readCategoryTree(ctx, tx); err != nil {
			return nil, nil, err
		}
	}
	for i := range out {
		if out[i].CategoryID != nil {
			out[i].CategoryPath = tree.path(*out[i].CategoryID)
		}
	}
	return out, next, nil
}

// categoryTree là cây danh mục của shop (catalog.categories_v, mọi trạng thái), theo mã. Cây nhỏ và có giới hạn (tối
// đa 3 cấp, danh mục do nhân viên tạo tay) nên đọc cả cây một lần mỗi trang thay vì ghép vào câu danh sách.
type categoryTree map[uuid.UUID]categoryNode

type categoryNode struct {
	parent *uuid.UUID
	name   string
}

func readCategoryTree(ctx context.Context, tx pgx.Tx) (categoryTree, error) {
	rows, err := tx.Query(ctx, "SELECT category_id, parent_id, name FROM catalog.categories_v")
	if err != nil {
		return nil, fmt.Errorf("đọc cây danh mục: %w", err)
	}
	tree := categoryTree{}
	var id uuid.UUID
	var n categoryNode
	_, err = pgx.ForEachRow(rows, []any{&id, &n.parent, &n.name}, func() error {
		tree[id] = n
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc cây danh mục: %w", err)
	}
	return tree, nil
}

// subtree trả id cùng mọi danh mục con, cháu của nó (rỗng khi không có danh mục id).
func (t categoryTree) subtree(id uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for c := range t {
		for cur, depth := c, 0; depth < 3; depth++ {
			if cur == id {
				out = append(out, c)
				break
			}
			n := t[cur]
			if n.parent == nil {
				break
			}
			cur = *n.parent
		}
	}
	return out
}

// path trả tên từ gốc tới id (tối đa 3 cấp).
func (t categoryTree) path(id uuid.UUID) []string {
	var out []string
	for cur, depth := &id, 0; cur != nil && depth < 3; depth++ {
		n, ok := t[*cur]
		if !ok {
			break
		}
		out = append([]string{n.name}, out...)
		cur = n.parent
	}
	return out
}

// productsSQL dựng câu của ListProducts (lấy Limit + 1 dòng để biết còn trang sau). inCategory: danh mục của bộ lọc
// cùng con, cháu (chỉ dùng khi f.CategoryID khác nil).
func productsSQL(f ProductFilter, inCategory []uuid.UUID) (string, []any, error) {
	if f.Limit < 1 || f.Limit > MaxListLimit {
		return "", nil, fmt.Errorf("giới hạn %d: %w", f.Limit, ErrInvalidInput)
	}
	for _, s := range f.Status {
		if !slices.Contains([]string{"draft", "active", "discontinued"}, s) {
			return "", nil, fmt.Errorf("trạng thái %q: %w", s, ErrInvalidInput)
		}
	}
	args := []any{f.SellerID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	var b strings.Builder
	available := "NULL::bigint"
	if f.WarehouseID != uuid.Nil {
		available = `(SELECT coalesce(sum(a.available), 0)
		FROM catalog.variants_v AS v
		JOIN inventory.stock_available_v AS a ON a.variant_id = v.variant_id
		WHERE v.seller_id = p.seller_id AND v.product_id = p.product_id AND a.warehouse_id = ` + arg(f.WarehouseID) + `)`
	}
	b.WriteString(`SELECT p.product_id, p.code, p.name, p.status, p.category_id, vs.n, coalesce(vs.min_price, 0), coalesce(vs.max_price, 0), ` + available + `, p.updated_at, cov.image_id, cov.file_id
FROM catalog.products_v AS p
CROSS JOIN LATERAL (
	SELECT count(*) AS n, min(v.price) AS min_price, max(v.price) AS max_price
	FROM catalog.variants_v AS v
	WHERE v.seller_id = p.seller_id AND v.product_id = p.product_id
) AS vs
LEFT JOIN LATERAL (
	SELECT i.image_id, i.file_id
	FROM catalog.product_images_v AS i
	WHERE i.product_id = p.product_id
	ORDER BY i.position, i.image_id
	LIMIT 1
) AS cov ON true
WHERE p.seller_id = $1 AND p.deleted_at IS NULL`)
	if len(f.Status) > 0 {
		b.WriteString("\n\tAND p.status = ANY(" + arg(f.Status) + "::text[])")
	}
	if f.CategoryID != nil {
		b.WriteString("\n\tAND p.category_id = ANY(" + arg(inCategory) + "::uuid[])")
	}
	if f.TagID != nil {
		b.WriteString("\n\tAND EXISTS (SELECT 1 FROM catalog.product_tags_v AS t WHERE t.product_id = p.product_id AND t.tag_id = " +
			arg(*f.TagID) + ")")
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		folded, err := foldQuery(q)
		if err != nil {
			return "", nil, err
		}
		b.WriteString("\n\tAND p.product_id IN (SELECT catalog.search_products($1, " + arg(folded) + ", " + arg(q) + ", " +
			strconv.Itoa(maxQMatches) + "))")
	}
	if f.Cursor != nil {
		b.WriteString("\n\tAND (p.updated_at, p.product_id) < (" + arg(f.Cursor.UpdatedAt) + "::timestamptz, " +
			arg(f.Cursor.ID) + "::uuid)")
	}
	b.WriteString("\nORDER BY p.updated_at DESC, p.product_id DESC\nLIMIT " + strconv.Itoa(f.Limit+1))
	return b.String(), args, nil
}

// foldQuery kiểm độ dài chuỗi tìm (1–100 ký tự sau khi bỏ khoảng trắng đầu cuối) và trả dạng không dấu.
func foldQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" || utf8.RuneCountInString(q) > maxQueryRunes {
		return "", fmt.Errorf("chuỗi tìm: %w", ErrInvalidInput)
	}
	return textnorm.Fold(q), nil
}

// VariantHit là một biến thể tìm được. Sellable: mẫu và biến thể đều đang bán.
type VariantHit struct {
	VariantID   uuid.UUID
	ProductID   uuid.UUID
	SKU         string
	Barcode     string
	ProductCode string
	ProductName string
	Color       string
	Size        string
	Price       int64
	Sellable    bool
}

// SearchVariants tìm biến thể của người bán sellerID theo barcode (đúng cả chuỗi, phân biệt hoa thường), SKU, mã mẫu (tiền tố, không phân biệt hoa
// thường), tên không dấu (các từ theo thứ tự) qua catalog.search_variants; tối đa limit (1–100) dòng, barcode khớp
// trước. Chạy trong tenancy.WithTenant. Lỗi: ErrInvalidInput.
func SearchVariants(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, q string, limit int) ([]VariantHit, error) {
	if limit < 1 || limit > MaxSearchLimit {
		return nil, fmt.Errorf("giới hạn %d: %w", limit, ErrInvalidInput)
	}
	folded, err := foldQuery(q)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT variant_id, product_id, sku, barcode, product_code, product_name, color, size,
	price, sellable FROM catalog.search_variants($1, $2, $3, $4)`, sellerID, folded, strings.TrimSpace(q), limit)
	if err != nil {
		return nil, fmt.Errorf("tìm biến thể: %w", err)
	}
	hits, err := pgx.CollectRows(rows, pgx.RowToStructByPos[VariantHit])
	if err != nil {
		return nil, fmt.Errorf("tìm biến thể: %w", err)
	}
	return hits, nil
}
