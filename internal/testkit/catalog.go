package testkit

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// SeedVariant là một biến thể cần dựng: Seller 0 = ELIMAZ (người bán mặc định), 1 = người bán thứ hai (PARTNER1);
// Stock > 0 nạp tồn ở kho chính; Discontinued đặt ngừng bán.
type SeedVariant struct {
	Seller       int
	SKU          string
	Price        int64
	Stock        int32
	Discontinued bool
}

// SeedOptions là dữ liệu cần dựng của SeedCatalog.
type SeedOptions struct {
	Variants []SeedVariant
}

// Catalog là dữ liệu đã dựng: hai người bán (thứ tự như SeedVariant.Seller), kho chính, mã biến thể theo SKU.
type Catalog struct {
	Sellers   [2]uuid.UUID
	Warehouse uuid.UUID
	Variants  map[string]uuid.UUID
}

// SeedCatalog dựng hai người bán (ADR-29, KH14), kho chính và các biến thể của o cho tenantID bằng d (chủ bảng, như
// SeedSellers), mỗi người bán một mẫu "Áo thử"; tồn nạp bằng invtest.SeedStock (dòng số dư kèm dòng sổ). Dựng trên
// helper của catalog, inventory, không ghi SQL thẳng vào bảng của module khác (kế hoạch M2, Task M2-02). Lỗi làm test
// dừng (chỉ gọi từ goroutine của test).
func SeedCatalog(t testing.TB, d *db.DB, tenantID uuid.UUID, o SeedOptions) Catalog {
	t.Helper()
	c := Catalog{Variants: make(map[string]uuid.UUID, len(o.Variants))}
	if err := tenancy.WithTenant(t.Context(), d, tenantID, func(tx pgx.Tx) error {
		shop := invtest.NewShop(t, tx, tenantID)
		c.Sellers, c.Warehouse = [2]uuid.UUID{shop.Seller, shop.Partner}, shop.Warehouse
		var products [2]uuid.UUID
		for _, v := range o.Variants {
			if v.Seller < 0 || v.Seller > 1 {
				return fmt.Errorf("SeedVariant.Seller phải là 0 hay 1, có %d", v.Seller)
			}
			if _, dup := c.Variants[v.SKU]; dup || v.SKU == "" {
				return fmt.Errorf("SKU %q rỗng hay trùng", v.SKU)
			}
			seller := c.Sellers[v.Seller]
			if products[v.Seller] == uuid.Nil {
				products[v.Seller] = catalogtest.Product(t, tx, tenantID, seller, fmt.Sprintf("SC%02d", v.Seller+1))
			}
			id := catalogtest.VariantWithSKU(t, tx, tenantID, seller, products[v.Seller], v.SKU, v.Price)
			if v.Stock > 0 {
				invtest.SeedStock(t, tx, id, c.Warehouse, v.Stock)
			}
			if v.Discontinued {
				catalogtest.Discontinue(t, tx, seller, id)
			}
			c.Variants[v.SKU] = id
		}
		return nil
	}); err != nil {
		t.Fatalf("testkit: dựng danh mục thử: %v", err)
	}
	return c
}
