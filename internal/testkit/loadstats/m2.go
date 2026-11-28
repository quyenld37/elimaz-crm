package loadstats

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Số đo database của bài thử tải M2 (kế hoạch M2 Task 88, tools/loadkit m2stats): mã giảm giá, đơn của lượt, số lần
// thử lại transaction, dung lượng. Chỉ đọc, bằng chủ bảng.

const (
	sqlM2Coupons = `SELECT c.code::text, coalesce(c.max_uses, 0), c.used_count,
	(SELECT count(*) FROM promotion.coupon_redemptions r WHERE r.tenant_id = c.tenant_id AND r.coupon_id = c.id AND r.status = 'active')
FROM promotion.coupons c WHERE c.code = ANY ($1::text[]) ORDER BY c.code`
	sqlM2Orders = `SELECT count(*), count(*) FILTER (WHERE coupon_discount > 0) FROM ord.orders WHERE created_at >= $1`
	sqlM2Retry  = `SELECT coalesce(sum(count) FILTER (WHERE name = 'tx_retry'), 0)::bigint,
	coalesce(sum(count) FILTER (WHERE name = 'tx_retry_exhausted'), 0)::bigint
FROM platform.metrics_minute WHERE name IN ('tx_retry', 'tx_retry_exhausted') AND bucket >= date_trunc('minute', $1::timestamptz)`
	sqlM2Size   = `SELECT pg_database_size(current_database())`
	sqlM2Tables = `SELECT n.nspname || '.' || c.relname, pg_total_relation_size(c.oid), c.reltuples::bigint
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND n.nspname || '.' || c.relname = ANY ($1::text[]) ORDER BY 1`
)

// M2Tables là các bảng lớn của bài M2 (dung lượng ghi vào báo cáo).
func M2Tables() []string {
	return []string{"customer.customers", "customer.customer_identities", "ord.orders", "ord.order_items"}
}

// CouponStat là số đo của một mã: used_count so với số lượt dùng còn hiệu lực.
type CouponStat struct {
	Code    string `json:"code"`
	MaxUses int32  `json:"maxUses"`
	Used    int32  `json:"used"`
	Active  int64  `json:"active"`
}

// TableSize là dung lượng (cả index, TOAST) và số dòng ước của một bảng.
type TableSize struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Rows  int64  `json:"rows"`
}

// M2 là số đo database riêng của bài M2.
type M2 struct {
	Coupons       []CouponStat `json:"coupons"`
	Orders        int64        `json:"orders"`        // đơn tạo từ lúc from
	CouponOrders  int64        `json:"coupon_orders"` // trong đó có giảm theo mã
	TxRetry       int64        `json:"tx_retry"`
	TxExhausted   int64        `json:"tx_retry_exhausted"`
	DatabaseBytes int64        `json:"database_bytes"`
	Tables        []TableSize  `json:"tables"`
}

// ReadM2 đọc số đo M2 của tenant: các mã codes (đúng tenant, RLS), đơn tạo từ lúc from, số lần thử lại transaction từ
// phút của from (mọi tiến trình), dung lượng database và M2Tables.
func ReadM2(ctx context.Context, owner *db.DB, tenant uuid.UUID, codes []string, from time.Time) (M2, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var s M2
	if err := owner.Pool.QueryRow(ctx, sqlM2Retry, from).Scan(&s.TxRetry, &s.TxExhausted); err != nil {
		return s, fmt.Errorf("đọc số lần thử lại transaction: %w", err)
	}
	if err := owner.Pool.QueryRow(ctx, sqlM2Size).Scan(&s.DatabaseBytes); err != nil {
		return s, fmt.Errorf("đọc dung lượng database: %w", err)
	}
	var err error
	if s.Tables, err = readTables(ctx, owner); err != nil {
		return s, err
	}
	err = tenancy.WithTenant(ctx, owner, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, sqlM2Orders, from).Scan(&s.Orders, &s.CouponOrders); err != nil {
			return fmt.Errorf("đếm đơn của lượt: %w", err)
		}
		rows, err := tx.Query(ctx, sqlM2Coupons, codes)
		if err != nil {
			return fmt.Errorf("đọc mã giảm giá: %w", err)
		}
		s.Coupons, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (CouponStat, error) {
			var c CouponStat
			return c, r.Scan(&c.Code, &c.MaxUses, &c.Used, &c.Active)
		})
		if err != nil {
			return fmt.Errorf("đọc mã giảm giá: %w", err)
		}
		return nil
	})
	return s, err //nolint:wrapcheck // đã bọc bên trong
}

func readTables(ctx context.Context, owner *db.DB) ([]TableSize, error) {
	rows, err := owner.Pool.Query(ctx, sqlM2Tables, M2Tables())
	if err != nil {
		return nil, fmt.Errorf("đọc dung lượng bảng: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TableSize, error) {
		var t TableSize
		return t, r.Scan(&t.Name, &t.Bytes, &t.Rows)
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dung lượng bảng: %w", err)
	}
	return out, nil
}
