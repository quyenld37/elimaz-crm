package catalog_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

// Id đặt trước (Task 218: lệnh seed-web-fixture tạo bản ghi đúng id UUID v5 mà website tính từ id fixture): danh mục,
// mẫu, biến thể (khi tạo mẫu và khi thêm sau) giữ đúng id; ID rỗng → database tự sinh như cũ.
func TestCreateWithFixedID(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	catID, prodID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("category")), uuid.NewSHA1(uuid.NameSpaceURL, []byte("product"))
	v1, v2 := uuid.NewSHA1(uuid.NameSpaceURL, []byte("v1")), uuid.NewSHA1(uuid.NameSpaceURL, []byte("v2"))
	var cat, auto catalog.Category
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		if cat, err = e.writer().CreateCategory(t.Context(), tx, catalog.CategoryInput{ID: catID, Name: "Chân váy"}, e.actor); err != nil {
			return err //nolint:wrapcheck // test
		}
		auto, err = e.writer().CreateCategory(t.Context(), tx, catalog.CategoryInput{Name: "Áo"}, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if cat.ID != catID || auto.ID == uuid.Nil || auto.ID.Version() != 7 {
		t.Fatalf("id danh mục: %s (mong %s), tự sinh %s", cat.ID, catID, auto.ID)
	}
	in := dam01(e.seller)
	in.ID, in.Variants = prodID, in.Variants[:2]
	in.Variants[0].ID = v1
	p := e.mustCreate(in)
	if p.ID != prodID || p.Variants[0].ID != v1 || p.Variants[1].ID.Version() != 7 {
		t.Fatalf("id mẫu, biến thể: %s %s %s", p.ID, p.Variants[0].ID, p.Variants[1].ID)
	}
	var added catalog.Variant
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		added, err = e.writer().AddVariant(t.Context(), tx, e.seller, p.ID, p.Version,
			catalog.VariantInput{ID: v2, SKU: "DAM01-XANH-S", Color: "Xanh", Size: "S", Price: 350000}, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	got, err := e.get(prodID)
	if err != nil || added.ID != v2 || len(got.Variants) != 3 {
		t.Fatalf("đọc lại: %+v %v (biến thể thêm %s)", got, err, added.ID)
	}
	if n := e.count("SELECT count(*) FROM catalog.variants WHERE id = ANY($1::uuid[]) AND product_id = $2", []uuid.UUID{v1, v2}, prodID); n != 2 {
		t.Fatalf("mong 2 biến thể đúng id trong mẫu, có %d", n)
	}
}
