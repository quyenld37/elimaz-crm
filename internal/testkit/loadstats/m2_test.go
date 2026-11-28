package loadstats_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

func addCoupon(t *testing.T, s *testkit.Staff, tenant uuid.UUID, code string, maxUses any, used int) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), s.Owner, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO promotion.coupons (tenant_id, code, name, discount_kind, value, max_uses,
			used_count, valid_from, valid_to, created_by) VALUES ($1, $2::text, $2::text, 'fixed', 10000, $3, $4,
			now() - interval '1 hour', now() + interval '1 day', $5)`, tenant, code, maxUses, used, uuid.New())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
}

// ReadM2 đọc mã giảm giá của đúng tenant (cùng mã ở tenant khác không lẫn), used_count so với lượt active, số lần thử
// lại transaction từ phút của from (bỏ phút trước), dung lượng database và bốn bảng lớn; chưa có đơn → 0.
func TestReadM2(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "Thử tải")
	addCoupon(t, s, tenant, "M2LT01", nil, 0)
	addCoupon(t, s, tenant, "M2LT02", 100, 3)              // used_count lệch: không có lượt active nào
	addCoupon(t, s, s.Tenant(t, "Khác"), "M2LT01", nil, 5) // cùng mã ở tenant khác: không lẫn
	ctx := t.Context()
	from := time.Now().UTC().Truncate(time.Minute)
	for _, m := range []struct {
		bucket time.Time
		name   string
		n      int
	}{{from, "tx_retry", 4}, {from.Add(time.Minute), "tx_retry_exhausted", 2}, {from.Add(-time.Minute), "tx_retry_exhausted", 9}} {
		if _, err := s.Owner.Pool.Exec(ctx, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum, max)
			VALUES ($1, 'p', $2, '40001', $3, 0, 0)`, m.bucket, m.name, m.n); err != nil {
			t.Fatal(err)
		}
	}
	got, err := loadstats.ReadM2(ctx, s.Owner, tenant, []string{"M2LT01", "M2LT02"}, from.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	want := []loadstats.CouponStat{{Code: "M2LT01"}, {Code: "M2LT02", MaxUses: 100, Used: 3}}
	if !reflect.DeepEqual(got.Coupons, want) {
		t.Errorf("mã: %+v, muốn %+v", got.Coupons, want)
	}
	if got.TxRetry != 4 || got.TxExhausted != 2 {
		t.Errorf("thử lại %d, hết lượt %d; muốn 4, 2 (bỏ phút trước from)", got.TxRetry, got.TxExhausted)
	}
	if got.DatabaseBytes <= 0 || len(got.Tables) != len(loadstats.M2Tables()) || got.Orders != 0 || got.CouponOrders != 0 {
		t.Errorf("dung lượng %d, bảng %+v, đơn %d/%d", got.DatabaseBytes, got.Tables, got.Orders, got.CouponOrders)
	}
}
