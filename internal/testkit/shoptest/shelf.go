package shoptest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

// ShelfAttrCode là mã thuộc tính "bắt buộc cho kệ" của SeedShelf (kế hoạch M7 Task 261).
const ShelfAttrCode = "chat_lieu"

// Shelf là phân loại dùng chung của dữ liệu thử mở bán (Task 261): một danh mục, một nhãn kệ, thuộc tính ShelfAttrCode
// có required_for_shelf = true và một giá trị của nó.
type Shelf struct {
	Category, Tag, Attribute, AttributeValue uuid.UUID
}

// SeedShelf tạo phân loại Shelf của tenantID. tx là chủ bảng trong tenancy.WithTenant của tenantID.
func SeedShelf(t testing.TB, tx pgx.Tx, tenantID uuid.UUID) Shelf {
	t.Helper()
	var s Shelf
	ctx := t.Context()
	steps := []struct {
		sql  string
		args []any
		dst  *uuid.UUID
	}{
		{
			`INSERT INTO catalog.categories (tenant_id, name, slug, depth) VALUES ($1, 'Đầm', 'dam-ke', 1) RETURNING id`,
			[]any{tenantID},
			&s.Category,
		},
		{
			`INSERT INTO catalog.tags (tenant_id, code, name) VALUES ($1, 'moi-ve', 'Mới về') RETURNING id`,
			[]any{tenantID},
			&s.Tag,
		},
		{`INSERT INTO catalog.attributes (tenant_id, code, name, required_for_shelf) VALUES ($1, $2, 'Chất liệu', true)
		  RETURNING id`, []any{tenantID, ShelfAttrCode}, &s.Attribute},
	}
	for _, st := range steps {
		if err := tx.QueryRow(ctx, st.sql, st.args...).Scan(st.dst); err != nil {
			t.Fatalf("shoptest: dựng kệ: %v", err)
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO catalog.attribute_values (tenant_id, attribute_id, value) VALUES ($1, $2, 'Lụa')
		RETURNING id`, tenantID, s.Attribute).Scan(&s.AttributeValue); err != nil {
		t.Fatalf("shoptest: dựng giá trị thuộc tính: %v", err)
	}
	return s
}

// Lack là phần thiếu của một mẫu nháp DraftProduct; giá trị rỗng là mẫu đủ điều kiện lên kệ.
type Lack struct {
	NoVariant       bool // biến thể duy nhất ngừng bán
	ZeroPrice       bool // biến thể giá 0
	NoImage         bool // không có ảnh
	ProcessingImage bool // chỉ có ảnh đang xử lý (tệp uploaded); thắng NoImage
	NoCategory      bool
	NoAttr          bool // thiếu giá trị thuộc tính ShelfAttrCode
	NoTag           bool
}

// DraftProduct tạo mẫu nháp code (tên "Đầm <code>") của người bán sellerID với một biến thể SKU "<code>-S" giá 350.000
// (hoặc 0), một ảnh ready (hoặc đang xử lý, hoặc không), danh mục, nhãn, giá trị thuộc tính của s, trừ phần lack; trả
// mã mẫu và mã biến thể. code viết hoa, ngắn (SKU, barcode của nó phải hợp lệ). tx là chủ bảng trong
// tenancy.WithTenant của tenantID.
func DraftProduct(t testing.TB, tx pgx.Tx, tenantID, sellerID uuid.UUID, s Shelf, code string, lack Lack) (product, variant uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	if err := tx.QueryRow(ctx, `INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status)
		VALUES ($1, $2, $3, 'Đầm ' || $3, 'dam', 'draft') RETURNING id`, tenantID, sellerID, code).Scan(&product); err != nil {
		t.Fatalf("shoptest: ghi mẫu nháp: %v", err)
	}
	price := int64(350000)
	if lack.ZeroPrice {
		price = 0
	}
	variant = catalogtest.VariantWithSKU(t, tx, tenantID, sellerID, product, code+"-S", price)
	if lack.NoVariant {
		catalogtest.Discontinue(t, tx, sellerID, variant)
	}
	exec := func(sql string, args ...any) {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("shoptest: dựng mẫu nháp %s: %v", code, err)
		}
	}
	switch {
	case lack.ProcessingImage:
		exec(`INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position) VALUES ($1, $2, $3, 1)`,
			tenantID, product, filestest.Uploaded(t, tx, tenantID, "product_image"))
	case !lack.NoImage:
		AttachReadyImage(t, tx, tenantID, product)
	}
	if !lack.NoCategory {
		exec(`UPDATE catalog.products SET category_id = $1 WHERE seller_id = $2 AND id = $3`, s.Category, sellerID, product)
	}
	if !lack.NoAttr {
		exec(`INSERT INTO catalog.product_attribute_values (tenant_id, product_id, attribute_value_id) VALUES ($1, $2, $3)`,
			tenantID, product, s.AttributeValue)
	}
	if !lack.NoTag {
		exec(`INSERT INTO catalog.product_tags (tenant_id, product_id, tag_id) VALUES ($1, $2, $3)`, tenantID, product, s.Tag)
	}
	return product, variant
}

// AttachReadyImage gắn một ảnh ready mới (filestest.Image) vào mẫu productID ở vị trí 1 (không tăng version, không sự
// kiện). tx là chủ bảng hay crm_app trong tenancy.WithTenant của tenantID.
func AttachReadyImage(t testing.TB, tx pgx.Tx, tenantID, productID uuid.UUID) {
	t.Helper()
	if _, err := tx.Exec(t.Context(),
		`INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position) VALUES ($1, $2, $3, 1)`,
		tenantID, productID, filestest.Image(t, tx, tenantID, "product_image")); err != nil {
		t.Fatalf("shoptest: gắn ảnh: %v", err)
	}
}
