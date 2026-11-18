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

	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
)

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
	id, err := q.GetSellerByCode(t.Context(), code)
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
