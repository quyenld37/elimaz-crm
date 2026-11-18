package catalog_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/barcode"
)

func (e *catalogEnv) updateVariant(id uuid.UUID, version int64, patch catalog.VariantPatch) (catalog.Variant, error) {
	var out catalog.Variant
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().UpdateVariant(e.t.Context(), tx, id, version, patch, e.actor)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *catalogEnv) setProductStatus(p catalog.Product, status string) catalog.Product {
	e.t.Helper()
	var out catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		cur, err := catalog.GetProduct(e.t.Context(), tx, p.SellerID, p.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = e.writer().UpdateProduct(e.t.Context(), tx, p.ID, cur.Version,
			catalog.ProductPatch{Status: ptr(catalog.ProductStatus(status))}, e.actor)
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		e.t.Fatalf("đổi trạng thái mẫu sang %s: %v", status, err)
	}
	return out
}

func TestUpdateVariant_PriceChanged(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	v := p.Variants[0]
	got, err := e.updateVariant(v.ID, v.Version, catalog.VariantPatch{Price: ptr(int64(390000))})
	if err != nil || got.Price != 390000 || got.Version != 2 {
		t.Fatalf("UpdateVariant: %+v, %v", got, err)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'price.changed' AND aggregate_type = 'variant' AND aggregate_id = $1 AND seq = 2", v.ID.String()); n != 1 {
		t.Errorf("mong 1 price.changed seq 2, có %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'price.changed' AND aggregate_id = $1
		AND payload = jsonb_build_object('productId', $2::text, 'sellerId', $3::text, 'variantIds', jsonb_build_array($1::text), 'price', 390000)`,
		v.ID.String(), p.ID.String(), e.seller.String()); n != 1 {
		t.Errorf("payload price.changed sai")
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1 AND seq = 2
		AND payload = jsonb_build_object('productId', $1::text, 'sellerId', $2::text, 'version', 2)`, p.ID.String(), e.seller.String()); n != 1 {
		t.Errorf("mong 1 product.updated seq 2 (version mẫu tăng khi sửa biến thể) đúng payload, có %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.variant.update' AND entity_id = $1
		AND before->>'price' = '350000' AND after->>'price' = '390000' AND request_id = 'req-test'`, v.ID.String()); n != 1 {
		t.Errorf("mong 1 dòng audit before.price = 350000, after.price = 390000, có %d", n)
	}
	// Sửa không đổi giá: không có price.changed mới, product.updated vẫn có.
	if _, err := e.updateVariant(v.ID, 2, catalog.VariantPatch{Color: ptr("Đỏ đô")}); err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'price.changed'"); n != 1 {
		t.Errorf("đổi màu không phát price.changed: có %d", n)
	}
	if _, err := e.updateVariant(v.ID, 2, catalog.VariantPatch{Price: ptr(int64(1))}); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if _, err := e.updateVariant(v.ID, 3, catalog.VariantPatch{Price: ptr(int64(-1))}); !errors.Is(err, catalog.ErrInvalidPrice) {
		t.Errorf("giá âm: mong ErrInvalidPrice, có %v", err)
	}
	if _, err := e.updateVariant(v.ID, 3, catalog.VariantPatch{Color: ptr("Đỏ"), Size: ptr("M")}); !errors.Is(err, catalog.ErrDuplicateOption) {
		t.Errorf("đổi size trùng biến thể khác: mong ErrDuplicateOption, có %v", err)
	}
	if _, err := e.updateVariant(uuid.New(), 1, catalog.VariantPatch{}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("biến thể không có: mong ErrNotFound, có %v", err)
	}
	cleared, err := e.updateVariant(v.ID, 3, catalog.VariantPatch{ClearCostPrice: true, Status: ptr("discontinued")})
	if err != nil || cleared.CostPrice != nil || cleared.Status != "discontinued" {
		t.Errorf("bỏ giá nhập, ngừng bán biến thể: %+v, %v", cleared, err)
	}
}

func TestAddVariant_ProductVersion(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	add := func(version int64, sku string) (catalog.Variant, error) {
		var out catalog.Variant
		err := e.app(func(tx pgx.Tx) error {
			var err error
			out, err = e.writer().AddVariant(t.Context(), tx, p.ID, version, catalog.VariantInput{SKU: sku, Color: "Xanh", Size: sku, Price: 1}, e.actor)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	v, err := add(1, "XS")
	if err != nil || v.ProductID != p.ID || v.SellerID != e.seller || !barcode.ValidEAN13(v.Barcode) {
		t.Fatalf("AddVariant: %+v, %v", v, err)
	}
	if _, err := add(1, "XXL"); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("version mẫu cũ: mong ErrVersionConflict, có %v", err)
	}
	got, err := e.get(p.ID)
	if err != nil || got.Version != 2 || len(got.Variants) != 7 {
		t.Errorf("sau khi thêm biến thể: version %d, %d biến thể, %v", got.Version, len(got.Variants), err)
	}
}

// KH12: mẫu ngừng bán vẫn xem được nhưng không bán được; mẫu nháp không bán được; VariantsForSale lọc trong một câu.
func TestVariantForSale_Discontinued(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	forSale := func(seller, id uuid.UUID) (catalog.SaleVariant, error) {
		var out catalog.SaleVariant
		err := e.app(func(tx pgx.Tx) error {
			var err error
			out, err = catalog.VariantForSale(t.Context(), tx, seller, id)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	v := p.Variants[0]
	if _, err := forSale(e.seller, v.ID); !errors.Is(err, catalog.ErrNotForSale) {
		t.Errorf("mẫu draft: mong ErrNotForSale, có %v", err)
	}
	p = e.setProductStatus(p, "active")
	sv, err := forSale(e.seller, v.ID)
	if err != nil || sv.ID != v.ID || sv.SKU != "DAM01-DO-S" || sv.Price != 350000 || sv.Name != "Đầm lụa xòe - Đỏ / S" || sv.ProductID != p.ID {
		t.Errorf("mẫu active: %+v, %v", sv, err)
	}
	if _, err := forSale(uuid.New(), v.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("người bán khác: mong ErrNotFound, có %v", err)
	}

	// Mẫu thứ hai active có một biến thể ngừng bán; người bán thứ hai có một biến thể bán được.
	other := dam01(e.seller)
	other.Code, other.Status = "DAM02", "active"
	for i := range other.Variants {
		other.Variants[i].SKU = "B" + other.Variants[i].SKU
	}
	p2, err := e.create(other)
	if err != nil {
		t.Fatal(err)
	}
	stopped := p2.Variants[0]
	if _, err := e.updateVariant(stopped.ID, stopped.Version, catalog.VariantPatch{Status: ptr("discontinued")}); err != nil {
		t.Fatal(err)
	}
	third := dam01(e.partnerSeller())
	third.Status = "active"
	p3, err := e.create(third)
	if err != nil {
		t.Fatal(err)
	}
	var m map[uuid.UUID]catalog.SaleVariant
	err = e.app(func(tx pgx.Tx) error {
		var err error
		m, err = catalog.VariantsForSale(t.Context(), tx, e.seller, []uuid.UUID{v.ID, stopped.ID, p3.Variants[0].ID})
		return err //nolint:wrapcheck // test
	})
	if err != nil || len(m) != 1 || m[v.ID].SKU != "DAM01-DO-S" {
		t.Errorf("VariantsForSale: mong đúng 1 phần tử, có %v, %v", m, err)
	}

	p = e.setProductStatus(p, "discontinued")
	if _, err := forSale(e.seller, v.ID); !errors.Is(err, catalog.ErrNotForSale) {
		t.Errorf("mẫu discontinued: mong ErrNotForSale, có %v", err)
	}
	if got, err := e.get(p.ID); err != nil || got.Status != "discontinued" {
		t.Errorf("GetProduct vẫn trả mẫu ngừng bán: %+v, %v", got.Status, err)
	}
	if _, err := forSale(e.seller, stopped.ID); !errors.Is(err, catalog.ErrNotForSale) {
		t.Errorf("biến thể ngừng bán: mong ErrNotForSale, có %v", err)
	}
}
