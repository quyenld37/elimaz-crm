package inventory

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// ImportForTest nhập q cái (on_hand + q) qua applyDelta, chèn dòng số dư nếu chưa có (helper của test thuộc tính).
func ImportForTest(ctx context.Context, tx pgx.Tx, variantID, warehouseID uuid.UUID, q int32) error {
	if err := store.New(tx).EnsureBalance(ctx, store.EnsureBalanceParams{VariantID: variantID, WarehouseID: warehouseID}); err != nil {
		return fmt.Errorf("chèn số dư: %w", err)
	}
	_, ok, err := applyDelta(ctx, tx, variantID, warehouseID, bucketOnHand, q, condNone,
		doc{Type: "import", ID: uuid.New()}, "test_import", Actor{Type: ActorSystem})
	if err == nil && !ok {
		err = fmt.Errorf("nhập kho thử: không có dòng số dư")
	}
	return err
}
