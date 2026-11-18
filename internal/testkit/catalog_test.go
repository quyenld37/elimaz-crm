package testkit_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestSeedCatalog: 3 biến thể của ELIMAZ, 1 của người bán thứ hai (một ngừng bán) → tồn ở kho chính đúng số nạp,
// biến thể đúng người bán, giá, SKU; biến thể ngừng bán → ErrNotForSale (kế hoạch M2, Task M2-02).
func TestSeedCatalog(t *testing.T) {
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	c := testkit.SeedCatalog(t, s.Owner, tenant, testkit.SeedOptions{Variants: []testkit.SeedVariant{
		{SKU: "AO-DO-S", Price: 250000, Stock: 5},
		{SKU: "AO-DO-M", Price: 260000, Stock: 0},
		{SKU: "AO-XANH-S", Price: 270000, Stock: 3, Discontinued: true},
		{Seller: 1, SKU: "P-TUI", Price: 99000, Stock: 7},
	}})
	if c.Sellers[0] == uuid.Nil || c.Sellers[1] == uuid.Nil || c.Sellers[0] == c.Sellers[1] || c.Warehouse == uuid.Nil {
		t.Fatalf("người bán, kho sai: %+v", c)
	}
	if len(c.Variants) != 4 {
		t.Fatalf("mong 4 biến thể theo SKU, có %v", c.Variants)
	}
	stock := map[string]int32{"AO-DO-S": 5, "AO-DO-M": 0, "AO-XANH-S": 3, "P-TUI": 7}
	if err := tenancy.WithTenant(t.Context(), s.App, tenant, func(tx pgx.Tx) error {
		ids := make([]uuid.UUID, 0, len(c.Variants))
		for _, id := range c.Variants {
			ids = append(ids, id)
		}
		bal, err := inventory.Availability(t.Context(), tx, c.Warehouse, ids)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		checkStock(t, c, bal, stock)
		v, err := catalog.VariantForSale(t.Context(), tx, c.Sellers[0], c.Variants["AO-DO-S"])
		if err != nil || v.SKU != "AO-DO-S" || v.Price != 250000 {
			t.Errorf("biến thể ELIMAZ: %+v, %v", v, err)
		}
		checkSaleErrors(t, tx, c)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func checkStock(t *testing.T, c testkit.Catalog, bal []inventory.Balance, stock map[string]int32) {
	t.Helper()
	for sku, id := range c.Variants {
		for _, b := range bal {
			if b.VariantID == id && (b.OnHand != stock[sku] || b.Available != stock[sku]) {
				t.Errorf("%s: tồn %d, có thể bán %d; mong %d", sku, b.OnHand, b.Available, stock[sku])
			}
		}
	}
}

// checkSaleErrors: biến thể người bán thứ hai bán được dưới người bán đó, không có dưới ELIMAZ; biến thể ngừng bán →
// ErrNotForSale.
func checkSaleErrors(t *testing.T, tx pgx.Tx, c testkit.Catalog) {
	t.Helper()
	if _, err := catalog.VariantForSale(t.Context(), tx, c.Sellers[1], c.Variants["P-TUI"]); err != nil {
		t.Errorf("biến thể của người bán thứ hai: %v", err)
	}
	if _, err := catalog.VariantForSale(t.Context(), tx, c.Sellers[0], c.Variants["P-TUI"]); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("biến thể người bán khác dưới ELIMAZ: mong ErrNotFound, có %v", err)
	}
	if _, err := catalog.VariantForSale(t.Context(), tx, c.Sellers[0], c.Variants["AO-XANH-S"]); !errors.Is(err, catalog.ErrNotForSale) {
		t.Errorf("biến thể ngừng bán: mong ErrNotForSale, có %v", err)
	}
}
