package reporting_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// vnAt là mốc giờ Việt Nam của ngày 2026-11-dd.
func vnAt(day, hour, minute, sec int) time.Time {
	return time.Date(2026, 11, day, hour, minute, sec, 0, reporting.VN)
}

// snapKey là một dòng chụp: (kho, biến thể).
type snapKey struct{ wh, variant uuid.UUID }

// snapVal là bốn ngăn của một dòng chụp.
type snapVal struct{ onHand, held, inTransit, damaged int }

// snapEnv là một tenant có một kho và áo A-S (người bán own) trên database riêng.
type snapEnv struct {
	s       *testkit.Staff
	tenant  uuid.UUID
	seed    *reportseed.Seed
	wh      uuid.UUID
	variant uuid.UUID
}

func newSnapEnv(t *testing.T, s *testkit.Staff) *snapEnv {
	t.Helper()
	if s == nil {
		s = testkit.OpenStaff(t)
	}
	tenant := s.Tenant(t, "snap-"+uuid.NewString())
	seed := reportseed.New(t, s.Owner, tenant)
	return &snapEnv{
		s: s, tenant: tenant, seed: seed, wh: seed.Warehouse("Kho W"), variant: seed.Variant(seed.Own(), "Áo A", "AO-A-S"),
	}
}

// handler dựng việc chụp tồn với đồng hồ cố định now (Grace 10 phút, MaxDays 7 như lịch thật).
func snapHandler(now time.Time, reg *metrics.Registry) *reporting.SnapshotHandler {
	return &reporting.SnapshotHandler{
		Now: func() time.Time { return now }, Grace: 10 * time.Minute, MaxDays: 7, Metrics: reg,
	}
}

// snapResult là kết quả việc chụp tồn.
type snapResult struct {
	Days []string `json:"days"`
	Rows int      `json:"rows"`
}

// run chạy một lượt chụp bằng crm_app trong ngữ cảnh tenant (như worker) và trả kết quả.
func (e *snapEnv) run(t *testing.T, h *reporting.SnapshotHandler) snapResult {
	t.Helper()
	var out json.RawMessage
	if err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = h.Handle(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Kind: reporting.SnapshotKindName}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("chụp tồn: %v", err)
	}
	var r snapResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("kết quả %s: %v", out, err)
	}
	if r.Days == nil {
		t.Fatalf("kết quả thiếu mảng days: %s", out)
	}
	return r
}

// days là các ngày đã chụp của tenant (tăng dần).
func (e *snapEnv) days(t *testing.T) []string {
	t.Helper()
	ctx := t.Context()
	var out []string
	if err := tenancy.WithTenant(ctx, e.s.App, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT day::text FROM reporting.stock_snapshot_days ORDER BY day`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("đọc ngày chụp: %v", err)
	}
	return out
}

// snapshot là các dòng chụp của ngày day.
func (e *snapEnv) snapshot(t *testing.T, day string) map[snapKey]snapVal {
	t.Helper()
	ctx := t.Context()
	out := map[snapKey]snapVal{}
	if err := tenancy.WithTenant(ctx, e.s.App, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT warehouse_id, variant_id, on_hand, held, in_transit, damaged
			FROM reporting.stock_daily_snapshots WHERE day = $1::date`, day)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var k snapKey
			var v snapVal
			if err := rows.Scan(&k.wh, &k.variant, &v.onHand, &v.held, &v.inTransit, &v.damaged); err != nil {
				return err //nolint:wrapcheck // test
			}
			out[k] = v
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("đọc bản chụp %s: %v", day, err)
	}
	return out
}

// count đếm bằng crm_app trong ngữ cảnh tenant.
func (e *snapEnv) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	return appCount(t, e.s, e.tenant, q, args...)
}

// planLedger là sổ của kế hoạch (Task 233 Step 1): 15/11 10:00 nhập 20; 16/11 09:00 giữ 2; 16/11 23:59:59 bỏ giữ 2,
// đang giao 2; 17/11 00:00:00 giao xong 2 (đang giao −2, có −2).
func (e *snapEnv) planLedger() {
	e.seed.Ledger(e.variant, e.wh, "on_hand", 20, "import", vnAt(15, 10, 0, 0))
	e.seed.Ledger(e.variant, e.wh, "held", 2, "order", vnAt(16, 9, 0, 0))
	e.seed.Ledger(e.variant, e.wh, "held", -2, "order", vnAt(16, 23, 59, 59))
	e.seed.Ledger(e.variant, e.wh, "in_transit", 2, "order", vnAt(16, 23, 59, 59))
	e.seed.Ledger(e.variant, e.wh, "in_transit", -2, "order", vnAt(17, 0, 0, 0))
	e.seed.Ledger(e.variant, e.wh, "on_hand", -2, "order", vnAt(17, 0, 0, 0))
}

func TestDaysToSnapshot(t *testing.T) {
	t.Parallel()
	str := func(s string) *string { return &s }
	first := vnAt(15, 10, 0, 0)
	early := vnAt(1, 8, 0, 0)
	cases := []struct {
		name  string
		last  *string
		first *time.Time
		now   time.Time
		max   int
		want  []string
	}{
		{"chưa có sổ", nil, nil, vnAt(18, 8, 0, 0), 7, nil},
		{"từ ngày sổ đầu", nil, &first, vnAt(18, 8, 0, 0), 7, []string{"2026-11-15", "2026-11-16", "2026-11-17"}},
		{"chưa qua mốc + 10 phút", nil, &first, vnAt(18, 0, 5, 0), 7, []string{"2026-11-15", "2026-11-16"}},
		{"đúng mốc + 10 phút", nil, &first, vnAt(18, 0, 10, 0), 7, []string{"2026-11-15", "2026-11-16", "2026-11-17"}},
		{
			"sau ngày chụp gần nhất", str("2026-11-08"), &early, vnAt(18, 8, 0, 0), 7,
			[]string{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13", "2026-11-14", "2026-11-15"},
		},
		{
			"sổ đầu sau ngày chụp gần nhất", str("2026-11-08"), &first, vnAt(18, 8, 0, 0), 7,
			[]string{"2026-11-15", "2026-11-16", "2026-11-17"},
		},
		{"đã chụp tới hôm qua", str("2026-11-17"), &first, vnAt(18, 8, 0, 0), 7, nil},
		{"tối đa 2", nil, &first, vnAt(18, 8, 0, 0), 2, []string{"2026-11-15", "2026-11-16"}},
		{"max 0", nil, &first, vnAt(18, 8, 0, 0), 0, nil},
		// 23:59:59 giờ VN ngày 14 là 16:59:59Z: ngày VN của dòng sổ đầu là 14, không phải 15 (theo UTC cũng 14).
		{"sổ đầu lúc 23:59:59", nil, new(vnAt(14, 23, 59, 59)), vnAt(16, 0, 10, 0), 7, []string{"2026-11-14", "2026-11-15"}},
		// 00:30 giờ VN ngày 15 là 17:30Z ngày 14: ngày VN là 15.
		{"sổ đầu lúc 00:30", nil, new(vnAt(15, 0, 30, 0)), vnAt(16, 0, 10, 0), 7, []string{"2026-11-15"}},
	}
	for _, c := range cases {
		got := reporting.DaysToSnapshot(c.last, c.first, c.now, 10*time.Minute, c.max)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}

// Sổ của kế hoạch, Now = 18/11 08:00 giờ VN → chụp 15, 16, 17 đúng số; chạy lần hai không có gì mới.
func TestSnapshot_PlanLedger(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.planLedger()
	h := snapHandler(vnAt(18, 8, 0, 0), nil)
	r := e.run(t, h)
	if want := []string{"2026-11-15", "2026-11-16", "2026-11-17"}; !slices.Equal(r.Days, want) || r.Rows != 3 {
		t.Fatalf("lần 1: %+v, muốn days %v rows 3", r, want)
	}
	k := snapKey{e.wh, e.variant}
	for day, want := range map[string]snapVal{
		"2026-11-15": {20, 0, 0, 0}, "2026-11-16": {20, 0, 2, 0}, "2026-11-17": {18, 0, 0, 0},
	} {
		got := e.snapshot(t, day)
		if len(got) != 1 || got[k] != want {
			t.Errorf("ngày %s: %+v, muốn %+v", day, got, want)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM reporting.stock_snapshot_days WHERE row_count = 1 AND cutoff_at = (day + 1)::timestamp AT TIME ZONE 'Asia/Ho_Chi_Minh'`); n != 3 {
		t.Errorf("dòng ngày có row_count 1 và mốc 24:00 giờ VN: %d, muốn 3", n)
	}
	rows := e.count(t, `SELECT count(*) FROM reporting.stock_daily_snapshots`)
	r2 := e.run(t, h)
	if len(r2.Days) != 0 || r2.Rows != 0 {
		t.Errorf("lần 2: %+v, muốn rỗng", r2)
	}
	if n := e.count(t, `SELECT count(*) FROM reporting.stock_daily_snapshots`); n != rows {
		t.Errorf("số dòng chụp đổi sau lần 2: %d → %d", rows, n)
	}
}

// Ngày 17 chỉ chụp khi mốc 24:00 đã qua 10 phút: 18/11 00:05 chưa, 00:11 có.
func TestSnapshot_Grace(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.planLedger()
	if r := e.run(t, snapHandler(vnAt(18, 0, 5, 0), nil)); !slices.Equal(r.Days, []string{"2026-11-15", "2026-11-16"}) {
		t.Fatalf("00:05: %+v", r)
	}
	if r := e.run(t, snapHandler(vnAt(18, 0, 11, 0), nil)); !slices.Equal(r.Days, []string{"2026-11-17"}) {
		t.Fatalf("00:11: %+v", r)
	}
	if got := e.snapshot(t, "2026-11-17")[snapKey{e.wh, e.variant}]; got != (snapVal{18, 0, 0, 0}) {
		t.Errorf("ngày 17: %+v", got)
	}
}

// Ngày chụp gần nhất 08/11, Now 18/11: lần 1 chụp 09–15 (7 ngày), lần 2 chụp 16–17, lần 3 không có gì. Gauge
// stock_snapshot_lag_days là số ngày còn thiếu sau mỗi lần: 2, 0, 0.
func TestSnapshot_CatchUp(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.seed.Ledger(e.variant, e.wh, "on_hand", 5, "import", vnAt(1, 9, 0, 0))
	e.seed.Ledger(e.variant, e.wh, "on_hand", 3, "import", vnAt(12, 9, 0, 0))
	if err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO reporting.stock_snapshot_days (tenant_id, day, cutoff_at, row_count)
			VALUES (current_setting('app.tenant_id')::uuid, '2026-11-08', '2026-11-08T17:00:00Z', 0)`)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	reg := metrics.NewRegistry()
	h := snapHandler(vnAt(18, 8, 0, 0), reg)
	want := [][]string{
		{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13", "2026-11-14", "2026-11-15"},
		{"2026-11-16", "2026-11-17"},
		{},
	}
	for i, w := range want {
		r := e.run(t, h)
		if !slices.Equal(r.Days, w) {
			t.Fatalf("lần %d: %v, muốn %v", i+1, r.Days, w)
		}
		if lag := lagGauge(t, reg); lag != float64(len(want[min(i+1, len(want)-1)])) {
			t.Errorf("lần %d: lag %v", i+1, lag)
		}
	}
	k := snapKey{e.wh, e.variant}
	if got := e.snapshot(t, "2026-11-11")[k]; got != (snapVal{5, 0, 0, 0}) {
		t.Errorf("ngày 11: %+v", got)
	}
	if got := e.snapshot(t, "2026-11-12")[k]; got != (snapVal{8, 0, 0, 0}) {
		t.Errorf("ngày 12: %+v", got)
	}
}

// lagGauge đọc giá trị cuối của gauge stock_snapshot_lag_days (rút hết registry).
func lagGauge(t *testing.T, reg *metrics.Registry) float64 {
	t.Helper()
	for _, row := range reg.Drain() {
		if row.Name == "stock_snapshot_lag_days" && row.Kind == metrics.KindGauge {
			return row.Sum
		}
	}
	t.Fatal("thiếu gauge stock_snapshot_lag_days")
	return -1
}

// Hai lượt cùng lúc trên hai transaction, tenant chưa chụp lần nào: lượt sau chờ lượt trước commit rồi không chèn gì;
// mỗi ngày đúng một dòng ngày, dòng chụp không trùng, số dòng bằng chạy một mình.
func TestSnapshot_Concurrent(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.planLedger()
	h := snapHandler(vnAt(18, 8, 0, 0), nil)
	ctx := t.Context()
	release := make(chan struct{})
	aDone, aErr := e.startRun(ctx, h, release)
	var ra snapResult
	select {
	case ra = <-aDone:
	case err := <-aErr:
		t.Fatalf("lượt A: %v", err)
	}
	free := make(chan struct{})
	close(free)
	bOut, bErr := e.startRun(ctx, h, free)
	waitLockWait(t, e.s)
	close(release)
	if err := <-aErr; err != nil {
		t.Fatalf("lượt A: %v", err)
	}
	if err := <-bErr; err != nil {
		t.Fatalf("lượt B: %v", err)
	}
	rb := <-bOut
	if len(ra.Days) != 3 || ra.Rows != 3 || len(rb.Days) != 0 || rb.Rows != 0 {
		t.Errorf("A %+v, B %+v; muốn A 3 ngày 3 dòng, B rỗng", ra, rb)
	}
	if got := e.days(t); !slices.Equal(got, []string{"2026-11-15", "2026-11-16", "2026-11-17"}) {
		t.Errorf("ngày chụp: %v", got)
	}
	if n := e.count(t, `SELECT count(*) FROM reporting.stock_daily_snapshots`); n != 3 {
		t.Errorf("dòng chụp: %d, muốn 3", n)
	}
}

// startRun chạy một lượt chụp trong goroutine: gửi kết quả qua out khi Handle xong, rồi giữ transaction mở tới khi
// release đóng; lỗi (nil khi commit xong) gửi qua errc.
func (e *snapEnv) startRun(ctx context.Context, h *reporting.SnapshotHandler, release <-chan struct{}) (<-chan snapResult, <-chan error) {
	out, errc := make(chan snapResult, 1), make(chan error, 1)
	go func() {
		errc <- tenancy.WithTenant(ctx, e.s.App, e.tenant, func(tx pgx.Tx) error {
			raw, err := h.Handle(ctx, queue.Job{ID: uuid.New(), TenantID: &e.tenant}, tx)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			var r snapResult
			_ = json.Unmarshal(raw, &r)
			out <- r
			<-release
			return nil
		})
	}()
	return out, errc
}

// waitLockWait chờ (theo sự kiện, trần 10 s chống treo) tới khi có một phiên của database thử đang chờ khóa.
func waitLockWait(t *testing.T, s *testkit.Staff) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		var n int
		if err := s.Owner.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n); err != nil {
			t.Fatalf("đọc pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("lượt B không chờ khóa của lượt A")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Biến thể mọi ngăn bằng 0 → không có dòng; biến thể có hàng từ 17/11 → không có dòng của 15, 16.
func TestSnapshot_ZeroAndLateVariants(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.planLedger()
	zero := e.seed.Variant(e.seed.Own(), "Áo A", "AO-A-L")
	e.seed.Ledger(zero, e.wh, "on_hand", 5, "import", vnAt(15, 11, 0, 0))
	e.seed.Ledger(zero, e.wh, "on_hand", -5, "export", vnAt(15, 12, 0, 0))
	late := e.seed.Variant(e.seed.Own(), "Váy C", "VAY-C")
	e.seed.Ledger(late, e.wh, "on_hand", 3, "import", vnAt(17, 10, 0, 0))
	r := e.run(t, snapHandler(vnAt(18, 8, 0, 0), nil))
	if r.Rows != 4 {
		t.Errorf("số dòng: %d, muốn 4 (áo A-S ba ngày, váy C ngày 17)", r.Rows)
	}
	for _, day := range []string{"2026-11-15", "2026-11-16", "2026-11-17"} {
		got := e.snapshot(t, day)
		if _, ok := got[snapKey{e.wh, zero}]; ok {
			t.Errorf("ngày %s có dòng của biến thể bằng 0", day)
		}
		_, hasLate := got[snapKey{e.wh, late}]
		if hasLate != (day == "2026-11-17") {
			t.Errorf("ngày %s: có dòng váy C = %v", day, hasLate)
		}
	}
	if got := e.snapshot(t, "2026-11-17")[snapKey{e.wh, late}]; got != (snapVal{3, 0, 0, 0}) {
		t.Errorf("váy C ngày 17: %+v", got)
	}
}

// Dòng sổ ghi muộn (created_at trước mốc 17/11) sau khi đã chụp ngày 17 → bản chụp ngày 17 không đổi.
func TestSnapshot_AppendOnlyAfterLateLedger(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.planLedger()
	h := snapHandler(vnAt(18, 8, 0, 0), nil)
	e.run(t, h)
	before := e.snapshot(t, "2026-11-17")
	e.seed.Ledger(e.variant, e.wh, "on_hand", 4, "adjust", vnAt(17, 20, 0, 0))
	if r := e.run(t, h); len(r.Days) != 0 {
		t.Errorf("lượt sau: %+v", r)
	}
	if after := e.snapshot(t, "2026-11-17"); len(after) != len(before) || after[snapKey{e.wh, e.variant}] != before[snapKey{e.wh, e.variant}] {
		t.Errorf("bản chụp ngày 17 đổi: %+v → %+v", before, after)
	}
}

// Tenant B chạy → không đọc, không ghi gì của A (B chưa có sổ: không ngày nào; A giữ nguyên).
func TestSnapshot_TenantIsolation(t *testing.T) {
	t.Parallel()
	a := newSnapEnv(t, nil)
	a.planLedger()
	h := snapHandler(vnAt(18, 8, 0, 0), nil)
	a.run(t, h)
	b := &snapEnv{s: a.s, tenant: a.s.Tenant(t, "snap-B")}
	if r := b.run(t, h); len(r.Days) != 0 || r.Rows != 0 {
		t.Errorf("tenant B: %+v, muốn rỗng", r)
	}
	if n := b.count(t, `SELECT count(*) FROM reporting.stock_snapshot_days`); n != 0 {
		t.Errorf("tenant B có %d ngày", n)
	}
	if n := a.count(t, `SELECT count(*) FROM reporting.stock_daily_snapshots`); n != 3 {
		t.Errorf("tenant A có %d dòng chụp, muốn 3", n)
	}
	// B có sổ riêng → chỉ chụp sổ của B, cùng ngày với A nhưng dòng riêng.
	b.seed = reportseed.New(t, a.s.Owner, b.tenant)
	b.wh, b.variant = b.seed.Warehouse("Kho B"), b.seed.Variant(b.seed.Own(), "Áo B", "AO-B")
	b.seed.Ledger(b.variant, b.wh, "on_hand", 7, "import", vnAt(17, 9, 0, 0))
	if r := b.run(t, h); !slices.Equal(r.Days, []string{"2026-11-17"}) || r.Rows != 1 {
		t.Errorf("tenant B có sổ: %+v", r)
	}
	if got := a.snapshot(t, "2026-11-17"); len(got) != 1 {
		t.Errorf("tenant A ngày 17 thấy %d dòng", len(got))
	}
}

// Sổ ghi theo now() lúc bắt đầu transaction: transaction bắt đầu 23:59:59 có thể commit sau transaction bắt đầu
// 00:00:01. Tổng sổ trước mốc khi đó có thể âm (không ứng với số dư nào đã commit); giá trị chụp kẹp về 0 để việc không
// chết vì CHECK ≥ 0 và không chặn mọi ngày sau (Quyết định thay chủ shop QĐ-233-1).
func TestSnapshot_NegativePrefixClamped(t *testing.T) {
	t.Parallel()
	e := newSnapEnv(t, nil)
	e.seed.Ledger(e.variant, e.wh, "on_hand", 5, "import", vnAt(16, 0, 0, 1))
	e.seed.Ledger(e.variant, e.wh, "on_hand", -2, "order", vnAt(15, 23, 59, 59))
	r := e.run(t, snapHandler(vnAt(17, 8, 0, 0), nil))
	if !slices.Equal(r.Days, []string{"2026-11-15", "2026-11-16"}) {
		t.Fatalf("ngày: %+v", r)
	}
	if got := e.snapshot(t, "2026-11-15"); len(got) != 0 {
		t.Errorf("ngày 15: %+v, muốn không dòng (kẹp 0)", got)
	}
	if got := e.snapshot(t, "2026-11-16")[snapKey{e.wh, e.variant}]; got != (snapVal{3, 0, 0, 0}) {
		t.Errorf("ngày 16: %+v", got)
	}
}

// Việc không có tenant → lỗi vĩnh viễn (dead ngay, không thử lại).
func TestSnapshot_NoTenant(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	err := s.App.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := snapHandler(vnAt(18, 8, 0, 0), nil).Handle(t.Context(), queue.Job{ID: uuid.New()}, tx)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("lỗi = %v, muốn ErrPermanent", err)
	}
}

// Loại việc và lịch: reporting.stock_snapshot mỗi 15 phút theo tenant, đúng chính sách mặc định (8 lần rồi dead, không
// hết hạn — việc lỗi mãi phải tới dead để có cảnh báo); NewSnapshotHandler đặt Grace 10 phút, MaxDays 7.
func TestSnapshotKindAndSchedule(t *testing.T) {
	t.Parallel()
	k := reporting.SnapshotKind()
	if k.Name != "reporting.stock_snapshot" || k.Policy != queue.DefaultPolicy {
		t.Errorf("Kind = %+v", k)
	}
	h := reporting.NewSnapshotHandler(reporting.Deps{})
	if h.Kind() != k || h.Grace != 10*time.Minute || h.MaxDays != 7 || h.Now == nil {
		t.Errorf("handler = %+v", h)
	}
	var found int
	for _, s := range reporting.Schedules() {
		if s.Name == "reporting.stock_snapshot" {
			found++
			if s.Every != 15*time.Minute || !s.PerTenant || s.Kind != k {
				t.Errorf("lịch = %+v", s)
			}
		}
	}
	if found != 1 {
		t.Errorf("số lịch reporting.stock_snapshot = %d", found)
	}
}
