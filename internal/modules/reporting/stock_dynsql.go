package reporting

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// stockKeyOrder là thứ tự keyset của hai báo cáo tồn: SKU theo thứ tự byte (COLLATE "C", không phụ thuộc collation
// của database; SKU chỉ có A-Z 0-9 . _ -), biến thể, kho.
const stockKeyOrder = `v.sku COLLATE "C", x.variant_id, x.warehouse_id`

// stockVariantCols là các cột biến thể, kho đầu mỗi dòng của ba câu tồn (nguồn x nối catalog.variants_v v).
const stockVariantCols = "x.variant_id, v.sku, v.product_name, v.color, v.size, x.warehouse_id"

// stockVariantJoin nối nguồn x với biến thể cùng người bán (ADR-29).
const stockVariantJoin = "JOIN catalog.variants_v v ON v.variant_id = x.variant_id AND v.seller_id = x.seller_id"

// stockFilter dựng phần WHERE chung của ba câu tồn trên nguồn x (có variant_id, warehouse_id, seller_id) nối
// catalog.variants_v v: người bán own (ADR-29), phạm vi kho (không AllWarehouses → chỉ WarehouseIDs), kho lọc, tiền tố
// SKU (khoảng [p, p || U+10FFFF) theo thứ tự byte của text_pattern_ops như catalog.search_variants, 0123), con trỏ.
func stockFilter(s DataScope, a stockPage, b *sqlBuilder) (string, error) {
	if s.SellerID == uuid.Nil {
		return "", fmt.Errorf("báo cáo tồn thiếu người bán: %w", ErrBadParams)
	}
	q := "x.seller_id = " + b.Arg(s.SellerID)
	if !s.AllWarehouses {
		q += " AND x.warehouse_id = ANY(" + b.Arg(append([]uuid.UUID{}, s.WarehouseIDs...)) + "::uuid[])" // nil → mảng rỗng
	}
	if a.warehouse != nil {
		q += " AND x.warehouse_id = " + b.Arg(*a.warehouse)
	}
	if a.prefix != "" {
		q += " AND v.sku ~>=~ " + b.Arg(a.prefix) + " AND v.sku ~<~ " + b.Arg(a.prefix+"\U0010FFFF")
	}
	if a.cursor != nil {
		q += " AND (" + stockKeyOrder + ") > (" + b.Arg(a.cursor.SKU) + ", " + b.Arg(a.cursor.VariantID) + "::uuid, " +
			b.Arg(a.cursor.WarehouseID) + "::uuid)"
	}
	return q, nil
}

// stockSQL dựng câu một trang (limit + 1 dòng) của báo cáo tồn: day rỗng → số dư hiện tại (inventory.stock_available_v,
// cột available của view); có ngày → bản chụp của ngày đó (reporting.stock_daily_snapshots). Dòng mọi ngăn bằng 0 bị
// ẩn ở cả hai nguồn.
func stockSQL(s DataScope, a stockPage, day string, b *sqlBuilder) (string, error) {
	src, avail := "inventory.stock_available_v x", "x.available"
	var where string
	if day != "" {
		src, avail = "reporting.stock_daily_snapshots x", "x.on_hand - x.damaged - x.held - x.in_transit"
		where = "x.day = " + b.Arg(day) + "::date AND "
	}
	f, err := stockFilter(s, a, b)
	if err != nil {
		return "", err
	}
	return `SELECT ` + stockVariantCols + `, w.name,
	x.on_hand::bigint, x.held::bigint, x.in_transit::bigint, x.damaged::bigint, (` + avail + `)::bigint
FROM ` + src + `
` + stockVariantJoin + `
JOIN inventory.warehouses_v w ON w.warehouse_id = x.warehouse_id
WHERE ` + where + f + `
	AND (x.on_hand <> 0 OR x.held <> 0 OR x.in_transit <> 0 OR x.damaged <> 0)
ORDER BY ` + stockKeyOrder + `
LIMIT ` + b.Arg(a.limit+1), nil
}

// stockRows đọc tối đa a.limit + 1 dòng của báo cáo tồn (day rỗng: hôm nay).
func stockRows(ctx context.Context, tx pgx.Tx, s DataScope, a stockPage, day string) ([]StockRow, error) {
	b := &sqlBuilder{}
	q, err := stockSQL(s, a, day, b)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc báo cáo tồn: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (StockRow, error) {
		var x StockRow
		err := r.Scan(&x.VariantID, &x.SKU, &x.ProductName, &x.Color, &x.Size, &x.WarehouseID, &x.WarehouseName,
			&x.OnHand, &x.Held, &x.InTransit, &x.Damaged, &x.Available)
		return x, err //nolint:wrapcheck // CollectRows trả lỗi, bên ngoài bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng báo cáo tồn: %w", err)
	}
	return out, nil
}
