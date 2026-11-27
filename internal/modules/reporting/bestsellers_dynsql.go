package reporting

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// bestSellersLevelSQL: cột ra và khóa gộp theo mức (bảng hằng; mức chỉ chọn giữa hai câu, không ghép giá trị người
// dùng). Mức sku: tên mẫu, màu, size của catalog.variants_v; mức product: tên của catalog.products_v, ba cột kia NULL.
// join kết thúc bằng "p.seller_id = ", nối với tham số người bán.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var bestSellersLevelSQL = map[string]struct{ cols, join, group, order string }{
	LevelSKU: {
		cols:  "v.product_id, v.variant_id, v.sku, v.product_name AS name, v.color, v.size",
		group: "v.product_id, v.variant_id, v.sku, v.product_name, v.color, v.size",
		order: "sku, variant_id",
	},
	LevelProduct: {
		cols:  "p.product_id, NULL::uuid AS variant_id, NULL::text AS sku, p.name, NULL::text AS color, NULL::text AS size",
		join:  "\n\tJOIN catalog.products_v p ON p.product_id = v.product_id AND p.seller_id = ",
		group: "p.product_id, p.name",
		order: "name, product_id",
	},
}

// bestSellersTemplate là câu bán chạy; {…} được thay bằng phần hằng của mức, tên CTE tập đơn, tham số $n (không giá
// trị người dùng nào được ghép vào câu).
//
// Dòng hàng đọc bằng LATERAL … OFFSET 0 (không cho planner gộp thành phép nối thường): mỗi đơn của tập tra chỉ mục
// (tenant_id, seller_id, order_id, …) của order_items; với phép nối thường, planner ước tập đơn delivered quá lớn rồi
// quét cả order_items (đo trên 100.000 đơn, TestBestSellers_ExplainNoSeqScan). lines MATERIALIZED để nhánh dòng hàng
// chạy một lần, không lặp theo từng biến thể của phép nối catalog.
const bestSellersTemplate = `WITH {order_set_cte},
lines AS MATERIALIZED (
	SELECT i.variant_id, os.order_id, i.quantity::bigint AS quantity, i.net_amount, 0::bigint AS returned
	FROM {order_set} os
	CROSS JOIN LATERAL (
		SELECT oi.variant_id, oi.quantity, oi.net_amount FROM ord.order_items_v oi
		WHERE oi.order_id = os.order_id AND oi.seller_id = {seller}
		OFFSET 0
	) i
	UNION ALL
	SELECT i.variant_id, NULL::uuid, 0, 0, (ri.sellable_qty + ri.damaged_qty)::bigint
	FROM ord.returns_v r
	JOIN ord.orders_v o ON o.id = r.order_id AND o.seller_id = {seller}
	JOIN ord.return_items_v ri ON ri.return_id = r.id
	JOIN ord.order_items_v i ON i.id = ri.order_item_id AND i.seller_id = {seller}
	WHERE {return_conds}
),
grouped AS (
	SELECT {cols}, sum(l.quantity)::bigint AS quantity, count(DISTINCT l.order_id) AS orders,
		sum(l.net_amount)::bigint AS goods_amount, sum(l.returned)::bigint AS returned_quantity
	FROM lines l
	JOIN catalog.variants_v v ON v.variant_id = l.variant_id AND v.seller_id = {seller}{join}
	GROUP BY {group}
)
SELECT product_id, variant_id, sku, name, color, size, quantity, orders, goods_amount, returned_quantity,
	count(*) OVER () AS total_rows
FROM grouped
ORDER BY quantity DESC, goods_amount DESC, {order}{limit}`

// bestSellersSQL dựng câu bán chạy (Task 232): dòng hàng của tập đơn theo cơ sở (orderSet: placed → placed_active cơ
// sở placed; delivered → delivered cơ sở completed, tức giao thành công trong kỳ) hợp với dòng phiếu khách trả nhận
// trong kỳ (sellable + damaged; cùng luật phiếu với cột customer_returned: không tính phiếu hủy) của đơn cùng người
// bán và trong phạm vi Page, không theo tập đơn (Q6); gộp theo biến thể hay mẫu; sắp số lượng giảm, tiền giảm,
// SKU/tên tăng, mã tăng; cột cuối là tổng số dòng trước LIMIT. limit <= 0: không LIMIT (tệp xuất). p đã Validate.
func bestSellersSQL(s DataScope, p *BestSellersParams, limit int) (string, []any, error) {
	period, err := p.Period()
	if err != nil {
		return "", nil, err
	}
	f, m := OrderFilter{Period: period, Basis: BasisPlaced}, MetricPlacedActive
	if p.basis() == SalesBasisDelivered {
		f, m = OrderFilter{Period: period, Basis: BasisCompleted}, MetricDelivered
	}
	b := &sqlBuilder{}
	cte, err := orderSet(s, f, m, nil, b)
	if err != nil {
		return "", nil, err
	}
	pageConds, err := filterConds(s, OrderFilter{}, b)
	if err != nil {
		return "", nil, err
	}
	lv := bestSellersLevelSQL[p.level()]
	seller := b.Arg(s.SellerID)
	returnConds := append([]string{
		"r.kind = 'customer_return'", "r.status <> 'cancelled'",
		"r.received_at >= " + b.Arg(period.Start), "r.received_at < " + b.Arg(period.End),
	}, pageConds...)
	join, lim := "", ""
	if lv.join != "" {
		join = lv.join + seller
	}
	if limit > 0 {
		lim = "\nLIMIT " + b.Arg(limit)
	}
	sql := strings.NewReplacer(
		"{order_set_cte}", cte, "{order_set}", orderSetName, "{seller}", seller,
		"{return_conds}", strings.Join(returnConds, "\n\t  AND "), "{cols}", lv.cols, "{join}", join,
		"{group}", lv.group, "{order}", lv.order, "{limit}", lim,
	).Replace(bestSellersTemplate)
	return sql, b.args, nil
}

// bestSellerRows đọc các dòng đã sắp, tối đa limit dòng (limit <= 0: mọi dòng, cho tệp xuất), và tổng số dòng.
func bestSellerRows(ctx context.Context, tx pgx.Tx, s DataScope, p *BestSellersParams, limit int) ([]BestSellerRow, int64, error) {
	if err := p.Validate(); err != nil {
		return nil, 0, err
	}
	sql, args, err := bestSellersSQL(s, p, limit)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("đọc bán chạy: %w", err)
	}
	var total int64
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (BestSellerRow, error) {
		var b BestSellerRow
		err := r.Scan(&b.ProductID, &b.VariantID, &b.SKU, &b.Name, &b.Color, &b.Size, &b.Quantity, &b.Orders,
			&b.GoodsAmount, &b.ReturnedQuantity, &total)
		return b, err //nolint:wrapcheck // CollectRows trả lỗi, bọc ngay dưới
	})
	if err != nil {
		return nil, 0, fmt.Errorf("đọc dòng bán chạy: %w", err)
	}
	if out == nil {
		out = []BestSellerRow{}
	}
	return out, total, nil
}
