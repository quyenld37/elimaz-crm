package reporting

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// movementSums: các cột của LATERAL trên sổ tồn (inventory.stock_ledger_v l) của một (biến thể, kho) từ đầu kỳ ($start)
// trở đi; $end là đầu ngày sau ngày cuối kỳ. Chỉ ngăn on_hand vào các cột phát sinh; doc_type theo đủ bảy loại của sổ
// M1 (G4): import → nhập, return → nhập trả, order → bán (đổi dấu), export và damage (phần on_hand của phiếu xuất hàng
// lỗi) → xuất khác (đổi dấu), stocktake và adjust → điều chỉnh (có dấu); loại khác ở ngăn on_hand đếm vào unknown để
// Go báo lỗi. since: tổng on_hand từ đầu kỳ; after: tổng on_hand, damaged từ cuối kỳ (số dư trừ đi ra số lúc đó).
const movementSums = `coalesce(sum(l.delta) FILTER (WHERE l.bucket = 'on_hand'), 0)::bigint AS since,
		coalesce(sum(l.delta) FILTER (WHERE l.bucket = 'on_hand' AND l.created_at >= %[2]s), 0)::bigint AS after,
		coalesce(sum(l.delta) FILTER (WHERE l.bucket = 'damaged' AND l.created_at >= %[2]s), 0)::bigint AS damaged_after,
		coalesce(sum(l.delta) FILTER (WHERE %[1]s AND l.doc_type = 'import'), 0)::bigint AS imported,
		coalesce(sum(l.delta) FILTER (WHERE %[1]s AND l.doc_type = 'return'), 0)::bigint AS returned_in,
		coalesce(-sum(l.delta) FILTER (WHERE %[1]s AND l.doc_type = 'order'), 0)::bigint AS sold,
		coalesce(-sum(l.delta) FILTER (WHERE %[1]s AND l.doc_type IN ('export', 'damage')), 0)::bigint AS exported_other,
		coalesce(sum(l.delta) FILTER (WHERE %[1]s AND l.doc_type IN ('stocktake', 'adjust')), 0)::bigint AS adjusted,
		count(*) FILTER (WHERE %[1]s AND l.doc_type NOT IN ('import', 'return', 'order', 'export', 'damage', 'stocktake',
			'adjust')) AS unknown`

// movementsSQL dựng câu một trang (limit + 1 dòng) của xuất nhập tồn: mỗi dòng số dư (inventory.stock_available_v)
// trong phạm vi, sổ của nó từ đầu kỳ qua LATERAL (chỉ mục stock_ledger_balance: tenant, biến thể, kho, mốc). Cuối kỳ =
// số dư hiện tại − sổ từ cuối kỳ; đầu kỳ = số dư hiện tại − sổ từ đầu kỳ (cùng cách tính với chụp tồn của Task 233, số
// dư luôn bằng tổng sổ; không cộng lại cả sổ từ ngày đầu). Ẩn dòng mọi số bằng 0; số dư, sổ, tên đọc trong một câu.
func movementsSQL(s DataScope, a stockPage, period Period, b *sqlBuilder) (string, error) {
	if period.IsZero() || !period.Start.Before(period.End) {
		return "", fmt.Errorf("xuất nhập tồn thiếu kỳ: %w", ErrBadParams)
	}
	start, end := b.Arg(period.Start), b.Arg(period.End)
	inPeriod := "l.bucket = 'on_hand' AND l.created_at < " + end
	f, err := stockFilter(s, a, b)
	if err != nil {
		return "", err
	}
	return `SELECT ` + stockVariantCols + `,
	x.on_hand - m.since, m.imported, m.returned_in, m.sold, m.exported_other, m.adjusted, x.on_hand - m.after,
	x.damaged - m.damaged_after, m.unknown
FROM inventory.stock_available_v x
` + stockVariantJoin + `
CROSS JOIN LATERAL (
	SELECT ` + fmt.Sprintf(movementSums, inPeriod, end) + `
	FROM inventory.stock_ledger_v l
	WHERE l.variant_id = x.variant_id AND l.warehouse_id = x.warehouse_id AND l.created_at >= ` + start + `
) m
WHERE ` + f + `
	AND (x.on_hand - m.since <> 0 OR x.on_hand - m.after <> 0 OR x.damaged - m.damaged_after <> 0 OR m.imported <> 0
		OR m.returned_in <> 0 OR m.sold <> 0 OR m.exported_other <> 0 OR m.adjusted <> 0 OR m.unknown <> 0)
ORDER BY ` + stockKeyOrder + `
LIMIT ` + b.Arg(a.limit+1), nil
}

// movementRows đọc tối đa a.limit + 1 dòng xuất nhập tồn của kỳ; dòng có doc_type lạ ở ngăn on_hand trong kỳ →
// ErrUnknownDocType.
func movementRows(ctx context.Context, tx pgx.Tx, s DataScope, a stockPage, period Period) ([]MovementRow, error) {
	b := &sqlBuilder{}
	q, err := movementsSQL(s, a, period, b)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc xuất nhập tồn: %w", err)
	}
	var unknown int64
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MovementRow, error) {
		var x MovementRow
		var n int64
		err := r.Scan(&x.VariantID, &x.SKU, &x.ProductName, &x.Color, &x.Size, &x.WarehouseID, &x.Opening, &x.Imported,
			&x.ReturnedIn, &x.Sold, &x.ExportedOther, &x.Adjusted, &x.Closing, &x.DamagedClosing, &n)
		unknown += n
		return x, err //nolint:wrapcheck // CollectRows trả lỗi, bên ngoài bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng xuất nhập tồn: %w", err)
	}
	if unknown > 0 {
		return nil, fmt.Errorf("xuất nhập tồn kỳ %s – %s: %d dòng sổ: %w", period.FromDay, period.ToDay, unknown, ErrUnknownDocType)
	}
	return out, nil
}
