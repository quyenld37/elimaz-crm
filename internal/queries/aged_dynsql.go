package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AgedOrderRef là đơn của một dòng đơn (danh sách "Giữ hàng quá hạn", Task M3-22).
type AgedOrderRef struct {
	OrderID uuid.UUID
	Code    string
}

// agedOrdersSQL, agedSKUsSQL: câu cố định (không dựng lúc chạy); nằm ở đây vì gói queries không có khối sqlc.
const (
	agedOrdersSQL = `
SELECT i.id, o.id, o.code
FROM ord.order_items_v AS i
JOIN ord.orders_v AS o ON o.id = i.order_id
WHERE i.id = ANY($1::uuid[])`
	agedSKUsSQL = `
SELECT v.variant_id, v.sku
FROM catalog.variants_v AS v
WHERE v.variant_id = ANY($1::uuid[])`
)

// AgedRefs trả đơn (mã, số) của từng dòng đơn trong orderItemIDs và SKU của từng biến thể trong variantIDs, đọc qua
// ord.order_items_v, ord.orders_v, catalog.variants_v trong tenant của tx (tenancy.WithTenant), để ghép vào dòng của
// inventory.AgedReservations (inventory không được đọc view của order). Mã không có thì không có trong kết quả. Mọi
// người bán của tenant: danh sách theo kho, không theo người bán.
func AgedRefs(ctx context.Context, tx pgx.Tx, orderItemIDs, variantIDs []uuid.UUID) (map[uuid.UUID]AgedOrderRef, map[uuid.UUID]string, error) {
	orders := make(map[uuid.UUID]AgedOrderRef, len(orderItemIDs))
	skus := make(map[uuid.UUID]string, len(variantIDs))
	if len(orderItemIDs) > 0 {
		if err := scanEach(ctx, tx, agedOrdersSQL, orderItemIDs, func(r pgx.Rows) error {
			var item uuid.UUID
			var o AgedOrderRef
			if err := r.Scan(&item, &o.OrderID, &o.Code); err != nil {
				return err //nolint:wrapcheck // bọc ở bên gọi
			}
			orders[item] = o
			return nil
		}); err != nil {
			return nil, nil, fmt.Errorf("đọc mã đơn của giữ hàng: %w", err)
		}
	}
	if len(variantIDs) > 0 {
		if err := scanEach(ctx, tx, agedSKUsSQL, variantIDs, func(r pgx.Rows) error {
			var v uuid.UUID
			var sku string
			if err := r.Scan(&v, &sku); err != nil {
				return err //nolint:wrapcheck // bọc ở bên gọi
			}
			skus[v] = sku
			return nil
		}); err != nil {
			return nil, nil, fmt.Errorf("đọc SKU của giữ hàng: %w", err)
		}
	}
	return orders, skus, nil
}

// scanEach chạy sql với một tham số và gọi scan cho từng dòng.
func scanEach(ctx context.Context, tx pgx.Tx, sql string, arg any, scan func(pgx.Rows) error) error {
	rows, err := tx.Query(ctx, sql, arg)
	if err != nil {
		return err //nolint:wrapcheck // bọc ở bên gọi
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err() //nolint:wrapcheck // bọc ở bên gọi
}
