package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// report là kết quả `verify` cho một món. Problems rỗng nghĩa là đạt.
type report struct {
	Units          map[string]int // số đơn vị theo trạng thái
	Orders         int            // số đơn có dòng đơn của món
	SumLineQty     int            // tổng qty các dòng đơn của món
	SumClaims      int            // tổng flash_sale_claims.quantity của món
	Customers      int            // số khách có claims > 0
	RunningKeys    int            // dòng khóa chống trùng còn `running` (chỉ để xem)
	ResultsByState map[string]int // kết quả đã lưu trong khóa chống trùng
	Problems       []string
}

func (r report) OK() bool { return len(r.Problems) == 0 }

func (r report) String() string {
	return fmt.Sprintf("units=%v orders=%d sum_line_qty=%d sum_claims=%d customers=%d running_keys=%d results=%v problems=%v",
		r.Units, r.Orders, r.SumLineQty, r.SumClaims, r.Customers, r.RunningKeys, r.ResultsByState, r.Problems)
}

// verify kiểm các quy luật của đường mua cho một món (kế hoạch Task 1; (a), (b) viết cho cả qty > 1,
// xem task-01 "Quyết định thay chủ shop" 4):
//
//	(a) tổng qty dòng đơn = số đơn vị held + sold, và mỗi đơn có đúng một dòng đơn, một dòng giữ hàng;
//	(b) mỗi dòng đơn gắn đúng qty đơn vị cùng món (không đơn vị nào gắn với dòng đơn khác món);
//	(c) tổng flash_sale_claims.quantity = số đơn vị đã lấy;
//	(d) không khách nào vượt limit, và phần giới hạn của mỗi khách = số suất trong các đơn của khách;
//	(e) không số nào âm.
func verify(ctx context.Context, pool *pgxpool.Pool, tenant, item string, limit int) (report, error) {
	rep := report{Units: map[string]int{}, ResultsByState: map[string]int{}}
	problem := func(format string, args ...any) { rep.Problems = append(rep.Problems, fmt.Sprintf(format, args...)) }

	err := inTenantTx(ctx, pool, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT status, count(*) FROM spike.flash_sale_units
			WHERE tenant_id = $1 AND item_id = $2 GROUP BY status`, tenant, item)
		if err != nil {
			return err
		}
		var (
			status string
			n      int
		)
		if _, err := pgx.ForEachRow(rows, []any{&status, &n}, func() error { rep.Units[status] = n; return nil }); err != nil {
			return err
		}
		taken := rep.Units["held"] + rep.Units["sold"]

		if err := tx.QueryRow(ctx, `SELECT count(DISTINCT order_id), coalesce(sum(qty), 0) FROM spike.order_items
			WHERE tenant_id = $1 AND item_id = $2`, tenant, item).Scan(&rep.Orders, &rep.SumLineQty); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(quantity), 0), count(*) FILTER (WHERE quantity > 0)
			FROM spike.flash_sale_claims WHERE tenant_id = $1 AND item_id = $2`, tenant, item).
			Scan(&rep.SumClaims, &rep.Customers); err != nil {
			return err
		}

		ti := []any{tenant, item}
		checks := []struct {
			name, sql string
			args      []any
		}{
			{"(a) đơn không có đúng một dòng đơn", `SELECT count(*) FROM spike.orders o
				WHERE o.tenant_id = $1 AND (SELECT count(*) FROM spike.order_items oi
				                            WHERE oi.tenant_id = o.tenant_id AND oi.order_id = o.id) <> 1`, []any{tenant}},
			{"(a) dòng đơn không có đúng một dòng giữ hàng cùng qty", `SELECT count(*) FROM spike.order_items oi
				WHERE oi.tenant_id = $1 AND oi.item_id = $2
				  AND (SELECT count(*) FROM spike.stock_reservations sr
				       WHERE sr.tenant_id = oi.tenant_id AND sr.order_item_id = oi.id AND sr.qty = oi.qty) <> 1`, ti},
			{"(b) dòng đơn gắn số đơn vị khác qty", `SELECT count(*) FROM spike.order_items oi
				WHERE oi.tenant_id = $1 AND oi.item_id = $2
				  AND oi.qty <> (SELECT count(*) FROM spike.flash_sale_units u
				                 WHERE u.tenant_id = oi.tenant_id AND u.order_item_id = oi.id AND u.item_id = oi.item_id)`, ti},
			{"(b) đơn vị gắn với dòng đơn của món khác", `SELECT count(*) FROM spike.flash_sale_units u
				JOIN spike.order_items oi ON oi.tenant_id = u.tenant_id AND oi.id = u.order_item_id
				WHERE u.tenant_id = $1 AND u.item_id = $2 AND oi.item_id <> u.item_id`, ti},
			{"(d) khách vượt giới hạn", `SELECT count(*) FROM spike.flash_sale_claims
				WHERE tenant_id = $1 AND item_id = $2 AND quantity > $3`, []any{tenant, item, limit}},
			{"(d) phần giới hạn của khách khác số suất trong đơn của khách", `SELECT count(*) FROM spike.flash_sale_claims c
				WHERE c.tenant_id = $1 AND c.item_id = $2
				  AND c.quantity <> (SELECT coalesce(sum(oi.qty), 0) FROM spike.orders o
				                     JOIN spike.order_items oi ON oi.tenant_id = o.tenant_id AND oi.order_id = o.id
				                     WHERE o.tenant_id = c.tenant_id AND o.customer_id = c.customer_id AND oi.item_id = c.item_id)`, ti},
			{"(e) số âm hoặc bằng 0 ở qty, quantity âm", `SELECT
				(SELECT count(*) FROM spike.flash_sale_claims WHERE tenant_id = $1 AND item_id = $2 AND quantity < 0) +
				(SELECT count(*) FROM spike.order_items WHERE tenant_id = $1 AND item_id = $2 AND qty <= 0)`, ti},
		}
		for _, c := range checks {
			var bad int
			if err := tx.QueryRow(ctx, c.sql, c.args...).Scan(&bad); err != nil {
				return fmt.Errorf("%s: %w", c.name, err)
			}
			if bad != 0 {
				problem("%s: %d", c.name, bad)
			}
		}

		if err := tx.QueryRow(ctx, `SELECT count(*) FROM spike.idempotency_keys WHERE tenant_id = $1 AND status = 'running'`,
			tenant).Scan(&rep.RunningKeys); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT coalesce(response_body->>'result', '?'), count(*) FROM spike.idempotency_keys
			WHERE tenant_id = $1 AND status = 'done' GROUP BY 1`, tenant)
		if err != nil {
			return err
		}
		var result string
		if _, err := pgx.ForEachRow(rows, []any{&result, &n}, func() error { rep.ResultsByState[result] = n; return nil }); err != nil {
			return err
		}

		if rep.SumLineQty != taken {
			problem("(a) tổng qty dòng đơn %d ≠ số đơn vị held+sold %d", rep.SumLineQty, taken)
		}
		if rep.SumClaims != taken {
			problem("(c) tổng claims %d ≠ số đơn vị đã lấy %d", rep.SumClaims, taken)
		}
		// Mỗi kết quả "bought" đã lưu ứng với đúng một đơn (đếm cả tenant: khóa chống trùng không ghi món).
		var tenantOrders int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM spike.orders WHERE tenant_id = $1`, tenant).Scan(&tenantOrders); err != nil {
			return err
		}
		if rep.ResultsByState["bought"] != tenantOrders {
			problem("số kết quả bought đã lưu %d ≠ số đơn của tenant %d", rep.ResultsByState["bought"], tenantOrders)
		}
		return nil
	})
	if err != nil {
		return report{}, fmt.Errorf("verify: %w", err)
	}
	return rep, nil
}
