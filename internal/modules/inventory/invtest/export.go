package invtest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Export tạo và ghi sổ một phiếu xuất qty của variantID ở kho warehouseID trong tx (crm_app, tenancy.WithTenant) bằng
// public API của inventory (CreateDocument, ReplaceLines, w.PostDocument; tác nhân system): giảm có thể bán và phát
// stock.changed qua w. Dùng để test nơi đọc tồn (website) giả lập phần hàng không còn bán thường (đã giữ, đã xuất).
// Lỗi làm test dừng (chỉ gọi từ goroutine của test).
func Export(t testing.TB, tx pgx.Tx, w inventory.Writer, warehouseID, variantID uuid.UUID, qty int32) {
	t.Helper()
	ctx := t.Context()
	a := inventory.Actor{Type: inventory.ActorSystem}
	d, err := inventory.CreateDocument(ctx, tx, inventory.DocumentInput{WarehouseID: warehouseID, Kind: inventory.KindExport}, a)
	if err != nil {
		t.Fatalf("invtest: tạo phiếu xuất: %v", err)
	}
	if d, err = inventory.ReplaceLines(ctx, tx, d.ID, d.Version, []inventory.DocumentLine{{VariantID: variantID, Quantity: qty}}); err != nil {
		t.Fatalf("invtest: dòng phiếu xuất: %v", err)
	}
	if _, err := w.PostDocument(ctx, tx, d.ID, d.Version, a); err != nil {
		t.Fatalf("invtest: ghi sổ phiếu xuất: %v", err)
	}
}
