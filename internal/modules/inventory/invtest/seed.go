// Package invtest là helper test cho module khác cần tồn kho (đơn, vận chuyển): SeedStock nạp tồn đúng cách (dòng số
// dư và dòng sổ cùng tx), để bất biến "tổng sổ = số dư" vẫn đúng trong test của họ.
//
// Khi nào không dùng: mã sản phẩm (chỉ dùng trong test); nhập kho thật đi qua chứng từ kho (Task M1-29).
package invtest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// SeedStock cộng onHand (> 0) vào on_hand của (variantID, warehouseID) trong tx (tenancy.WithTenant, crm_app hay
// chủ bảng): chèn dòng số dư nếu chưa có (người bán lấy từ catalog.variants_v), rồi một dòng sổ import (chứng từ
// mới, step seed, tác nhân system). Không phát sự kiện. Lỗi làm test dừng (chỉ gọi từ goroutine của test).
func SeedStock(t testing.TB, tx pgx.Tx, variantID, warehouseID uuid.UUID, onHand int32) {
	t.Helper()
	ctx := t.Context()
	q := store.New(tx)
	if onHand <= 0 {
		t.Fatalf("invtest: onHand phải > 0, có %d", onHand)
	}
	if err := q.EnsureBalance(ctx, store.EnsureBalanceParams{VariantID: variantID, WarehouseID: warehouseID}); err != nil {
		t.Fatalf("invtest: chèn số dư: %v", err)
	}
	row, err := q.ApplyDelta(ctx, store.ApplyDeltaParams{
		Bucket: "on_hand", Delta: onHand, VariantID: variantID, WarehouseID: warehouseID, Cond: "none",
	})
	if err != nil {
		t.Fatalf("invtest: cộng on_hand (biến thể hay kho không có trong tenant?): %v", err)
	}
	if err := q.InsertLedger(ctx, store.InsertLedgerParams{
		TenantID: row.TenantID, VariantID: variantID, WarehouseID: warehouseID, Bucket: "on_hand", Delta: onHand,
		DocType: "import", DocID: uuid.New(), Step: "seed", ActorType: "system", ActorID: pgtype.UUID{},
	}); err != nil {
		t.Fatalf("invtest: ghi sổ: %v", err)
	}
}
