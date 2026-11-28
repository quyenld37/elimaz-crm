package perfseed

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// smallOptions là cỡ nhỏ của bài đo (Step 1 của kế hoạch: 1.000 khách): cùng tỷ lệ với cỡ thật (1,3 SĐT mỗi khách,
// 1,6 dòng mỗi đơn, tenant thứ hai bằng 10% số khách).
func smallOptions() Options {
	return Options{
		Customers: 1000, Orders: 5000, Tenant2: 100, ExtraPhonePermille: 300, Months: 24, Seed: 7,
		Now: time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC),
	}
}

type seededDB struct {
	adminURL string
	owner    *db.DB
	app      *db.DB
	tenant   uuid.UUID
}

// openSeedable dựng database thử đã migrate, tenant "Thử tải" có người bán, kho, 6 biến thể (như loadkit m2seed dựng
// qua API trước khi seedperf chạy).
func openSeedable(t *testing.T) seededDB {
	t.Helper()
	adminURL, ownerURL, appURL := testkit.StartPostgres(t)
	owner := testkit.OpenDB(t, ownerURL)
	app := testkit.OpenDB(t, appURL)
	tenant, err := tenancy.SeedTenant(t.Context(), owner, tenantName)
	if err != nil {
		t.Fatal(err)
	}
	var vs []shoptest.SeedVariant
	for _, sku := range []string{"PF-01", "PF-02", "PF-03", "PF-04", "PF-05", "PF-06"} {
		vs = append(vs, shoptest.SeedVariant{SKU: sku, Price: 199_000, Stock: 10})
	}
	shoptest.SeedCatalog(t, owner, tenant, shoptest.SeedOptions{Variants: vs})
	return seededDB{adminURL: adminURL, owner: owner, app: app, tenant: tenant}
}

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// count chạy một câu đếm trong ngữ cảnh tenant bằng chủ bảng (RLS áp cả chủ bảng).
func count(t *testing.T, d *db.DB, tenant uuid.UUID, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := tenancy.WithTenant(t.Context(), d, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), q, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// TestSeed_SmallCounts (Step 1): seedperf trên 1.000 khách nạp đúng số dòng của từng bảng, mỗi SĐT một lần, đúng dạng
// +8490xxxxxxx; dữ liệu thỏa mọi ràng buộc mà trigger, khóa ngoại kiểm (tắt lúc nạp); chỉ số khách khớp đơn; thống kê
// của ANALYZE có; mẫu cho k6 đúng; câu tìm của module customer chạy trên dữ liệu đó và không lẫn tenant thứ hai.
func TestSeed_SmallCounts(t *testing.T) {
	t.Parallel()
	s := openSeedable(t)
	o := smallOptions()
	sample, err := Seed(t.Context(), s.adminURL, o, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	t2 := sample.Tenant2ID
	wantPhones := int64(o.Customers + o.Customers*o.ExtraPhonePermille/1000)
	for _, c := range []struct {
		what   string
		tenant uuid.UUID
		q      string
		want   int64
	}{
		{"khách tenant 1", s.tenant, `SELECT count(*) FROM customer.customers`, int64(o.Customers)},
		{"khách tenant 2", t2, `SELECT count(*) FROM customer.customers`, int64(o.Tenant2)},
		{"SĐT tenant 1", s.tenant, `SELECT count(*) FROM customer.customer_identities WHERE kind = 'phone'`, wantPhones},
		{"SĐT khác nhau tenant 1", s.tenant, `SELECT count(DISTINCT value) FROM customer.customer_identities WHERE kind = 'phone'`, wantPhones},
		{"SĐT tenant 2", t2, `SELECT count(*) FROM customer.customer_identities WHERE kind = 'phone'`, int64(o.Tenant2)},
		{"đơn", s.tenant, `SELECT count(*) FROM ord.orders`, int64(o.Orders)},
		{"dòng đơn", s.tenant, `SELECT count(*) FROM ord.order_items`, int64(o.Orders * 8 / 5)},
		{"SĐT sai dạng hay value_reversed sai", s.tenant, `SELECT count(*) FROM customer.customer_identities
			WHERE value !~ '^\+8490[0-9]{7}$' OR value_reversed <> reverse(substr(value, 2))`, 0},
		{"khách không phải gốc của chính mình", s.tenant, `SELECT count(*) FROM customer.customers WHERE root_id <> id`, 0},
		{"name_search sai", s.tenant, `SELECT count(*) FROM customer.customers WHERE name_search = '' OR name_search <> lower(name_search)`, 0},
		{"danh tính mồ côi", s.tenant, `SELECT count(*) FROM customer.customer_identities i
			WHERE NOT EXISTS (SELECT 1 FROM customer.customers c WHERE c.tenant_id = i.tenant_id AND c.id = i.customer_id)`, 0},
		{"đơn mồ côi (khách, người bán, kho)", s.tenant, `SELECT count(*) FROM ord.orders o
			WHERE NOT EXISTS (SELECT 1 FROM customer.customers c WHERE c.tenant_id = o.tenant_id AND c.id = o.customer_id)
			   OR NOT EXISTS (SELECT 1 FROM catalog.sellers s WHERE s.tenant_id = o.tenant_id AND s.id = o.seller_id)
			   OR NOT EXISTS (SELECT 1 FROM inventory.warehouses w WHERE w.tenant_id = o.tenant_id AND w.id = o.warehouse_id)`, 0},
		{"dòng đơn mồ côi (đơn, biến thể)", s.tenant, `SELECT count(*) FROM ord.order_items i
			WHERE NOT EXISTS (SELECT 1 FROM ord.orders o WHERE o.tenant_id = i.tenant_id AND o.seller_id = i.seller_id AND o.id = i.order_id)
			   OR NOT EXISTS (SELECT 1 FROM catalog.variants v WHERE v.tenant_id = i.tenant_id AND v.seller_id = i.seller_id AND v.id = i.variant_id)`, 0},
		{"đơn sai tổng (check_order_totals)", s.tenant, `SELECT count(*) FROM ord.orders o
			LEFT JOIN LATERAL (SELECT coalesce(sum(quantity::bigint * unit_price), 0) AS sub, coalesce(sum(line_discount), 0) AS ld,
			       coalesce(sum(allocated_order_discount), 0) AS alloc
			       FROM ord.order_items i WHERE i.tenant_id = o.tenant_id AND i.order_id = o.id) s ON true
			WHERE s.sub <> o.subtotal OR s.ld <> o.line_discount_total OR s.alloc <> o.order_discount + o.coupon_discount`, 0},
		{"đơn ngoài 24 tháng", s.tenant, `SELECT count(*) FROM ord.orders
			WHERE created_at < $1::timestamptz - interval '24 months' OR created_at >= $1::timestamptz`, 0},
		{"chỉ số khách khác đơn", s.tenant, `SELECT count(*) FROM customer.customers c
			LEFT JOIN LATERAL (SELECT count(*) FILTER (WHERE status NOT IN ('draft', 'cancelled')) AS n,
			       count(*) FILTER (WHERE status = 'delivered') AS d, count(*) FILTER (WHERE status = 'returned') AS r,
			       coalesce(sum(grand_total) FILTER (WHERE status = 'delivered'), 0) AS spent,
			       max(created_at) FILTER (WHERE status NOT IN ('draft', 'cancelled')) AS last
			       FROM ord.orders o WHERE o.tenant_id = c.tenant_id AND o.customer_id = c.id) s ON true
			WHERE c.order_count <> s.n OR c.delivered_count <> s.d OR c.returned_count <> s.r OR c.total_spent <> s.spent
			   OR c.last_order_at IS DISTINCT FROM s.last OR c.lifecycle <> CASE WHEN s.n > 0 THEN 'customer' ELSE 'lead' END`, 0},
	} {
		args := []any{}
		if c.what == "đơn ngoài 24 tháng" {
			args = append(args, o.Now)
		}
		if got := count(t, s.owner, c.tenant, c.q, args...); got != c.want {
			t.Errorf("%s: %d, muốn %d", c.what, got, c.want)
		}
	}
	// Ba trạng thái kết thúc đều có (85/7/8%), không đơn nào giữ hàng.
	for _, st := range []string{"delivered", "returned", "cancelled"} {
		if got := count(t, s.owner, s.tenant, `SELECT count(*) FROM ord.orders WHERE status = $1`, st); got == 0 {
			t.Errorf("không đơn nào %s", st)
		}
	}
	// VACUUM ANALYZE đã chạy: thống kê của planner (reltuples, lần analyze cuối) có ở cả bốn bảng. pg_stats ẩn dòng của
	// bảng có RLS với role không bỏ qua RLS nên đọc pg_class, pg_stat_user_tables.
	var analyzed int64
	if err := s.owner.Pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_user_tables s JOIN pg_class c ON c.oid = s.relid
		WHERE s.schemaname || '.' || s.relname IN ('customer.customers', 'customer.customer_identities', 'ord.orders', 'ord.order_items')
		  AND s.last_analyze IS NOT NULL AND s.last_vacuum IS NOT NULL AND c.reltuples > 0`).Scan(&analyzed); err != nil || analyzed != 4 {
		t.Errorf("bảng đã VACUUM ANALYZE: %d (%v), muốn 4", analyzed, err)
	}
	checkSample(t, sample, o)
	checkSearch(t, s, sample)

	// Chạy lại: từ chối, không nạp thêm.
	if _, err := Seed(t.Context(), s.adminURL, o, quietLogger()); !errors.Is(err, ErrAlreadySeeded) {
		t.Errorf("chạy lại: %v, muốn ErrAlreadySeeded", err)
	}
	if got := count(t, s.owner, s.tenant, `SELECT count(*) FROM customer.customers`); got != int64(o.Customers) {
		t.Errorf("sau lần chạy lại: %d khách, muốn %d", got, o.Customers)
	}
}

var codeRe = regexp.MustCompile(`^KH9[0-9]{8}$`)

func checkSample(t *testing.T, sample Sample, o Options) {
	t.Helper()
	if sample.Phones != o.Customers+o.Customers*o.ExtraPhonePermille/1000 || sample.PhoneMul != phoneMul ||
		sample.PhoneAdd != phoneAdd || sample.PhoneSpace != PhoneSpace || sample.PhonePrefix != phonePrefix {
		t.Errorf("mẫu: công thức SĐT %+v", sample)
	}
	if len(sample.CustomerIDs) == 0 || len(sample.CustomerIDs) > sampleIDs || len(sample.Codes) == 0 || len(sample.Names) == 0 {
		t.Fatalf("mẫu thiếu: %d mã khách, %d mã, %d tên", len(sample.CustomerIDs), len(sample.Codes), len(sample.Names))
	}
	for _, c := range sample.Codes {
		if !codeRe.MatchString(c) {
			t.Errorf("mã khách mẫu %q sai dạng", c)
		}
	}
	if sample.Orders != o.Orders || sample.Items != o.Orders*8/5 {
		t.Errorf("mẫu: %d đơn, %d dòng", sample.Orders, sample.Items)
	}
}

// checkSearch: customer.Search (mã thật của GET /customers?q=) bằng crm_app trên dữ liệu đã nạp tìm được khách theo
// SĐT đủ, đuôi số, mã, tên; SĐT 0 có ở cả hai tenant nhưng mỗi tenant chỉ thấy khách của mình (RLS).
func checkSearch(t *testing.T, s seededDB, sample Sample) {
	t.Helper()
	search := func(tenant uuid.UUID, q string) []customer.SearchHit {
		var hits []customer.SearchHit
		if err := tenancy.WithTenant(t.Context(), s.app, tenant, func(tx pgx.Tx) error {
			var err error
			hits, err = customer.Search(context.WithoutCancel(t.Context()), tx, q, 20)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatalf("tìm %q: %v", q, err)
		}
		return hits
	}
	p0 := fakePhone(0)
	for _, c := range []struct {
		what    string
		tenant  uuid.UUID
		q       string
		matched string
	}{
		{"SĐT đủ tenant 1", s.tenant, "0" + p0[3:], customer.MatchedByPhone},
		{"SĐT đủ tenant 2", sample.Tenant2ID, p0, customer.MatchedByPhone},
		{"đuôi 6 số", s.tenant, p0[len(p0)-6:], customer.MatchedByPhoneTail},
		{"mã", s.tenant, sample.Codes[0], customer.MatchedByCode},
		{"tên", s.tenant, sample.Names[0], customer.MatchedByName},
	} {
		hits := search(c.tenant, c.q)
		if len(hits) == 0 || hits[0].MatchedBy != c.matched {
			t.Errorf("%s: %d kết quả %+v, muốn ≥ 1 khớp %s", c.what, len(hits), hits, c.matched)
		}
	}
	h1, h2 := search(s.tenant, p0), search(sample.Tenant2ID, p0)
	if len(h1) != 1 || len(h2) != 1 || h1[0].RootID == h2[0].RootID {
		t.Errorf("SĐT 0: tenant 1 %d khách, tenant 2 %d khách, muốn mỗi tenant đúng một khách của mình", len(h1), len(h2))
	}
}

// TestSeed_RefusesForeignData: database có khách của tenant khác (không phải hai tenant đo) → ErrNotPerfDB, không nạp.
func TestSeed_RefusesForeignData(t *testing.T) {
	t.Parallel()
	s := openSeedable(t)
	other, err := tenancy.SeedTenant(t.Context(), s.owner, "Shop thật")
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(t.Context(), s.app, other, func(tx pgx.Tx) error {
		_, err := customer.Create(t.Context(), tx, "Khách thật")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(t.Context(), s.adminURL, smallOptions(), quietLogger()); !errors.Is(err, ErrNotPerfDB) {
		t.Fatalf("database có khách thật: %v, muốn ErrNotPerfDB", err)
	}
	if got := count(t, s.owner, s.tenant, `SELECT count(*) FROM customer.customers`); got != 0 {
		t.Errorf("đã nạp %d khách vào database có dữ liệu khác", got)
	}
}
