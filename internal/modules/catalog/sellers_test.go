package catalog_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/testkit"
)

// ao01 là mẫu AO01 một biến thể SKU AO01-DO-S của người bán seller.
func ao01(seller uuid.UUID) catalog.ProductInput {
	return catalog.ProductInput{SellerID: seller, Code: "AO01", Name: "Áo thử", Variants: []catalog.VariantInput{
		{SKU: "AO01-DO-S", Color: "Đỏ", Size: "S", Price: 100000},
	}}
}

// TestTwoSellers_SameSKUAndIsolation (KH14, ADR-29): mỗi người bán có mẫu AO01 cùng SKU AO01-DO-S tồn tại song song;
// với từng người bán: đọc, sửa mẫu, thêm và sửa biến thể, phân loại, ảnh bằng người bán của mình thì được, bằng người
// bán kia thì ErrNotFound và mẫu không đổi version.
func TestTwoSellers_SameSKUAndIsolation(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	sellers := []uuid.UUID{e.seller, e.partner}
	products := map[uuid.UUID]catalog.Product{}
	for _, s := range sellers {
		p, err := e.create(ao01(s))
		if err != nil {
			t.Fatalf("tạo AO01 của người bán %s: %v", s, err)
		}
		products[s] = p
	}
	other := func(s uuid.UUID) uuid.UUID {
		if s == e.seller {
			return e.partner
		}
		return e.seller
	}
	testkit.ForEachSeller(t, sellers, func(t *testing.T, seller uuid.UUID) {
		p, wrong := products[seller], other(seller)
		v := p.Variants[0]
		var got catalog.Product
		if err := e.app(func(tx pgx.Tx) (err error) {
			got, err = catalog.GetProduct(t.Context(), tx, seller, p.ID)
			return err //nolint:wrapcheck // test
		}); err != nil || got.Variants[0].SKU != "AO01-DO-S" {
			t.Fatalf("GetProduct của chính người bán: %+v, %v", got, err)
		}
		w := e.writer()
		for name, fn := range refusedWrites(t, e, w, wrong, p) {
			if err := e.app(fn); !errors.Is(err, catalog.ErrNotFound) {
				t.Errorf("%s bằng người bán khác: mong ErrNotFound, có %v", name, err)
			}
		}
		// Bằng chính người bán: sửa được, version tăng đúng 1 (các lần bị từ chối trên không đổi gì).
		var after catalog.Product
		if err := e.app(func(tx pgx.Tx) (err error) {
			after, err = w.UpdateProduct(t.Context(), tx, seller, p.ID, p.Version, catalog.ProductPatch{Name: ptr("Áo thử 2")}, e.actor)
			return err //nolint:wrapcheck // test
		}); err != nil || after.Version != p.Version+1 {
			t.Fatalf("UpdateProduct của chính người bán: %+v, %v", after, err)
		}
		var nv catalog.Variant
		if err := e.app(func(tx pgx.Tx) (err error) {
			nv, err = w.UpdateVariant(t.Context(), tx, seller, v.ID, v.Version, catalog.VariantPatch{Price: ptr(int64(120000))}, e.actor)
			return err //nolint:wrapcheck // test
		}); err != nil || nv.Price != 120000 {
			t.Fatalf("UpdateVariant của chính người bán: %+v, %v", nv, err)
		}
	})
	if n := e.count(`SELECT count(*) FROM catalog.variants WHERE sku = 'AO01-DO-S'`); n != 2 {
		t.Errorf("SKU AO01-DO-S có %d dòng, mong 2 (mỗi người bán một)", n)
	}
}

// errOf bỏ giá trị, giữ lỗi.
func errOf[T any](_ T, err error) error { return err }

// refusedWrites: mọi đường đọc và sửa mẫu p (biến thể đầu) bằng người bán wrong; mỗi hàm phải trả ErrNotFound.
func refusedWrites(t *testing.T, e *catalogEnv, w catalog.Writer, wrong uuid.UUID, p catalog.Product,
) map[string]func(tx pgx.Tx) error {
	t.Helper()
	ctx, v, file := t.Context(), p.Variants[0], e.image()
	return map[string]func(tx pgx.Tx) error{
		"GetProduct": func(tx pgx.Tx) error { return errOf(catalog.GetProduct(ctx, tx, wrong, p.ID)) },
		"UpdateProduct": func(tx pgx.Tx) error {
			return errOf(w.UpdateProduct(ctx, tx, wrong, p.ID, p.Version, catalog.ProductPatch{Name: ptr("X")}, e.actor))
		},
		"AddVariant": func(tx pgx.Tx) error {
			return errOf(w.AddVariant(ctx, tx, wrong, p.ID, p.Version,
				catalog.VariantInput{SKU: "AO01-DO-M", Color: "Đỏ", Size: "M", Price: 1}, e.actor))
		},
		"UpdateVariant": func(tx pgx.Tx) error {
			return errOf(w.UpdateVariant(ctx, tx, wrong, v.ID, v.Version, catalog.VariantPatch{Price: ptr(int64(1))}, e.actor))
		},
		"SetClassification": func(tx pgx.Tx) error {
			return errOf(w.SetClassification(ctx, tx, wrong, p.ID, p.Version, catalog.Classification{}, e.actor))
		},
		"AttachImage": func(tx pgx.Tx) error {
			return errOf(w.AttachImage(ctx, tx, wrong, p.ID, p.Version, file, nil, e.actor))
		},
		"ReorderImages": func(tx pgx.Tx) error { return w.ReorderImages(ctx, tx, wrong, p.ID, p.Version, nil, e.actor) },
		"ReorderVariants": func(tx pgx.Tx) error {
			return w.ReorderVariants(ctx, tx, wrong, p.ID, p.Version, []uuid.UUID{v.ID}, e.actor)
		},
		"DeleteImage": func(tx pgx.Tx) error {
			return errOf(w.DeleteImage(ctx, tx, wrong, p.ID, uuid.New(), e.actor))
		},
		"RestoreImage": func(tx pgx.Tx) error {
			return errOf(w.RestoreImage(ctx, tx, wrong, p.ID, uuid.New(), e.actor))
		},
	}
}
