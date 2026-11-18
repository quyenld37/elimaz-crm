// Package catalogtest là helper test cho module khác cần sản phẩm (đơn, vận chuyển): tạo người bán, mẫu, biến thể
// bằng đúng câu ghi của catalog (không sự kiện, không audit), để test của họ không chép SQL của bảng catalog.*.
//
// Khi nào không dùng: mã sản phẩm (chỉ dùng trong test); test của chính catalog đi qua catalog.Writer.
package catalogtest

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
)

// PartnerCode là mã người bán thứ hai (kind partner) trong dữ liệu thử hai người bán (Task M1-37, ADR-29).
const PartnerCode = "PARTNER1"

// Sellers tạo (nếu chưa có) hai người bán của tenantID: ELIMAZ (kind own, như dữ liệu khởi tạo) và PARTNER1 (kind
// partner); trả mã của hai người theo thứ tự đó. tx là chủ bảng trong tenancy.WithTenant của tenantID.
func Sellers(t testing.TB, tx pgx.Tx, tenantID uuid.UUID) (elimaz, partner uuid.UUID) {
	t.Helper()
	elimaz, err := catalog.EnsureDefaultSeller(t.Context(), tx, tenantID)
	if err != nil {
		t.Fatalf("catalogtest: người bán mặc định: %v", err)
	}
	return elimaz, Seller(t, tx, tenantID, PartnerCode, "partner")
}

// Seller tạo người bán code (kind own | partner) của tenantID nếu chưa có và trả mã. tx là chủ bảng trong
// tenancy.WithTenant (crm_app không ghi được sellers). Lỗi làm test dừng (chỉ gọi từ goroutine của test).
func Seller(t testing.TB, tx pgx.Tx, tenantID uuid.UUID, code, kind string) uuid.UUID {
	t.Helper()
	q := store.New(tx)
	if err := q.InsertSellerIfMissing(t.Context(), store.InsertSellerIfMissingParams{
		TenantID: tenantID, Code: code, Name: code, Kind: kind,
	}); err != nil {
		t.Fatalf("catalogtest: ghi người bán: %v", err)
	}
	id, err := q.GetSellerByCode(t.Context(), store.GetSellerByCodeParams{TenantID: tenantID, Code: code})
	if err != nil {
		t.Fatalf("catalogtest: đọc người bán: %v", err)
	}
	return id
}

// Product tạo mẫu đang bán code tên "Áo thử" của người bán sellerID và trả mã.
func Product(t testing.TB, tx pgx.Tx, tenantID, sellerID uuid.UUID, code string) uuid.UUID {
	t.Helper()
	row, err := store.New(tx).InsertProduct(t.Context(), store.InsertProductParams{
		TenantID: tenantID, SellerID: sellerID, Code: code, Name: "Áo thử", NameSearch: "ao thu", Article: []byte("[]"),
		Status: "active",
	})
	if err != nil {
		t.Fatalf("catalogtest: ghi mẫu: %v", err)
	}
	return row.ID
}

// Variant tạo biến thể thứ n (SKU "<mã mẫu>-n", barcode "BC<n>", màu Đỏ, size "Sn", giá price) của mẫu productID và
// trả mã; tên bán của nó là "Áo thử - Đỏ / Sn". n phải khác nhau trong một tenant.
func Variant(t testing.TB, tx pgx.Tx, tenantID, sellerID, productID uuid.UUID, n int, price int64) uuid.UUID {
	t.Helper()
	row, err := store.New(tx).InsertVariant(t.Context(), store.InsertVariantParams{
		TenantID: tenantID, SellerID: sellerID, ProductID: productID, Sku: fmt.Sprintf("SKU-%d", n),
		Barcode: fmt.Sprintf("BC%06d", n), Color: "Đỏ", Size: fmt.Sprintf("S%d", n), Price: price,
	})
	if err != nil {
		t.Fatalf("catalogtest: ghi biến thể: %v", err)
	}
	return row.ID
}

// VariantWithSKU tạo biến thể mã hàng sku (barcode "BC-<sku>", màu Đỏ, size bằng sku, giá price) của mẫu productID và
// trả mã; dùng khi test cần tra biến thể theo SKU (testkit.SeedCatalog, kế hoạch M2 Task M2-02). sku phải khác nhau
// trong một người bán.
func VariantWithSKU(t testing.TB, tx pgx.Tx, tenantID, sellerID, productID uuid.UUID, sku string, price int64) uuid.UUID {
	t.Helper()
	row, err := store.New(tx).InsertVariant(t.Context(), store.InsertVariantParams{
		TenantID: tenantID, SellerID: sellerID, ProductID: productID, Sku: sku, Barcode: "BC-" + sku, Color: "Đỏ",
		Size: sku, Price: price,
	})
	if err != nil {
		t.Fatalf("catalogtest: ghi biến thể %s: %v", sku, err)
	}
	return row.ID
}

// Discontinue đặt biến thể variantID của người bán sellerID là ngừng bán (catalog.VariantDiscontinued) bằng câu khóa
// và sửa biến thể của catalog (không sự kiện, không audit).
func Discontinue(t testing.TB, tx pgx.Tx, sellerID, variantID uuid.UUID) {
	t.Helper()
	q := store.New(tx)
	v, err := q.LockVariant(t.Context(), store.LockVariantParams{SellerID: sellerID, ID: variantID})
	if err != nil {
		t.Fatalf("catalogtest: khóa biến thể: %v", err)
	}
	if _, err := q.UpdateVariant(t.Context(), store.UpdateVariantParams{
		Sku: v.Sku, Barcode: v.Barcode, Color: v.Color, ColorHex: v.ColorHex, Size: v.Size, Price: v.Price,
		CostPrice: v.CostPrice, Status: catalog.VariantDiscontinued, SellerID: sellerID, ID: variantID, Version: v.Version,
	}); err != nil {
		t.Fatalf("catalogtest: ngừng bán biến thể: %v", err)
	}
}
