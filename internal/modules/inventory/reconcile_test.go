package inventory_test

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/testkit"
)

// reconcile chạy Reconcile bằng crm_app trong ngữ cảnh tenant của e, với statement_timeout của việc nền (30 s).
func (e *invEnv) reconcile(batch int) inventory.Run {
	e.t.Helper()
	return e.reconcileWithin(batch, "30s")
}

// reconcileWithin chạy Reconcile với statement_timeout của transaction là timeout (cú pháp PostgreSQL, ví dụ "30s").
func (e *invEnv) reconcileWithin(batch int, timeout string) inventory.Run {
	e.t.Helper()
	var run inventory.Run
	if err := e.app(func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), "SELECT set_config('statement_timeout', $1, true)", timeout); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		run, err = inventory.Reconcile(e.t.Context(), tx, batch)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("Reconcile: %v", err)
	}
	return run
}

// seedLarge chạy câu dựng dữ liệu thử lớn bằng chủ bảng trong ngữ cảnh tenant của e, với statement_timeout riêng 10 phút:
// hạn 30 s của pool là hạn của ứng dụng; máy tải cao thì câu chèn 50.000 dòng từng quá hạn đó (57014, Task 421). Hạn
// riêng chỉ còn là chốt chống treo.
func (e *invEnv) seedLarge(q string, args ...any) {
	e.t.Helper()
	if err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), "SET LOCAL statement_timeout = '10min'"); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(e.t.Context(), q, args...)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("dựng dữ liệu thử: %v", err)
	}
}

// owner chạy câu sửa thẳng bằng chủ bảng trong ngữ cảnh tenant của e (giả lỗi dữ liệu).
func (e *invEnv) owner(q string, args ...any) {
	e.t.Helper()
	if err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), q, args...)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("sửa thẳng bằng chủ bảng: %v", err)
	}
}

// cleanHistory chạy chuỗi thao tác của Task M1-28–35 (nạp, giữ, gửi, giao, nhả, phiếu nhập, xuất, hàng lỗi, kiểm
// kho) trên ba biến thể; trả ba biến thể.
func (e *invEnv) cleanHistory() []uuid.UUID {
	e.t.Helper()
	vs := e.variants(3)
	for _, v := range vs {
		e.seed(v, 10)
	}
	held, err := e.reserve(sysActor(), e.line(vs[0], 2), e.line(vs[1], 3), e.line(vs[2], 1))
	if err != nil {
		e.t.Fatalf("Reserve: %v", err)
	}
	if err := e.startTransit(held[:2], sysActor()); err != nil {
		e.t.Fatalf("StartTransit: %v", err)
	}
	if err := e.consume(held[:1], sysActor()); err != nil {
		e.t.Fatalf("Consume: %v", err)
	}
	if err := e.release(held[2:], "test"); err != nil {
		e.t.Fatalf("Release: %v", err)
	}
	for _, d := range []inventory.DocumentInput{
		{Kind: inventory.KindImport}, {Kind: inventory.KindExport}, {Kind: inventory.KindDamage},
	} {
		if _, err := e.post(e.newDoc(d, line(vs[2], 1)), staffActor()); err != nil {
			e.t.Fatalf("ghi sổ phiếu %s: %v", d.Kind, err)
		}
	}
	s := e.mustStart()
	e.mustCount(s.ID, vs[0], 9)
	if _, err := e.applyStocktake(s.ID); err != nil {
		e.t.Fatalf("ApplyStocktake: %v", err)
	}
	return vs
}

func (e *invEnv) mismatchCount(runID uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT mismatch_count FROM inventory.reconcile_runs_v WHERE run_id = $1", runID).
			Scan(&n)
	}); err != nil {
		e.t.Fatalf("đọc reconcile_runs_v: %v", err)
	}
	return n
}

// KH13: dữ liệu sạch → ok; sửa thẳng held (kèm sổ, giả mất dòng giữ hàng) của X → đúng một mục held_vs_reservations
// của X; sửa thẳng on_hand của Y → đúng một mục ledger_vs_balance của Y; view đếm đúng; tenant khác không thấy.
func TestReconcile_ReportsExactVariant(t *testing.T) {
	e := newInvEnv(t)
	vs := e.cleanHistory()
	run := e.reconcile(2) // lô 2 trên 3 dòng số dư: đi qua ranh giới lô
	if !run.OK || run.Checked != 3 || len(run.Mismatches) != 0 || run.ID == uuid.Nil || time.Since(run.RanAt) > time.Minute {
		t.Fatalf("dữ liệu sạch: %+v", run)
	}
	if n := e.mismatchCount(run.ID); n != 0 {
		t.Fatalf("mismatch_count dữ liệu sạch = %d", n)
	}

	x, y := vs[1], vs[2]
	e.owner("UPDATE inventory.stock_balances SET held = held + 1 WHERE variant_id = $1", x)
	e.owner(`INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id,
		step, actor_type) VALUES ($1, $2, $3, 'held', 1, 'adjust', gen_random_uuid(), 'bug', 'system')`,
		e.tenant, x, e.warehouse)
	run = e.reconcile(0)
	want1 := inventory.Mismatch{
		VariantID: x, WarehouseID: e.warehouse, Check: inventory.CheckHeldVsReservations,
		Expected: 0, Actual: 1,
	}
	if run.OK || run.Checked != 3 || len(run.Mismatches) != 1 || run.Mismatches[0] != want1 {
		t.Fatalf("held của X: %+v", run)
	}
	if n := e.mismatchCount(run.ID); n != 1 {
		t.Fatalf("mismatch_count = %d, mong 1", n)
	}

	before := e.balance(y).OnHand
	e.owner("UPDATE inventory.stock_balances SET on_hand = on_hand + 5 WHERE variant_id = $1", y)
	run = e.reconcile(0)
	wantY := inventory.Mismatch{
		VariantID: y, WarehouseID: e.warehouse, Check: inventory.CheckLedgerVsBalance,
		Bucket: "on_hand", Expected: int64(before), Actual: int64(before) + 5,
	}
	if run.OK || len(run.Mismatches) != 2 || !hasMismatch(run, want1) || !hasMismatch(run, wantY) {
		t.Fatalf("on_hand của Y: %+v", run)
	}
	if n := e.mismatchCount(run.ID); n != 2 {
		t.Fatalf("mismatch_count = %d, mong 2", n)
	}
	e.checkMismatchKeys(run.ID, 2)

	// Tenant B không thấy lần chạy của A; lần chạy của B (không có số dư) ok.
	b := newInvEnvOn(t, e.staff, "B")
	if n := b.count("SELECT count(*) FROM inventory.reconcile_runs_v"); n != 0 {
		t.Fatalf("tenant B thấy %d lần chạy của A", n)
	}
	if r := b.reconcile(0); !r.OK || r.Checked != 0 {
		t.Fatalf("tenant B: %+v", r)
	}
	if n := e.count("SELECT count(*) FROM inventory.reconcile_runs_v"); n != 3 {
		t.Fatalf("tenant A có %d lần chạy, mong 3", n)
	}
}

// checkMismatchKeys: cột mismatches của lần chạy id có n mục, mỗi mục chỉ có mã, tên phép so, ngăn và số.
func (e *invEnv) checkMismatchKeys(id uuid.UUID, n int) {
	e.t.Helper()
	var raw string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT mismatches::text FROM inventory.reconcile_runs WHERE id = $1", id).Scan(&raw)
	})
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil || len(items) != n {
		e.t.Fatalf("mismatches: %s (%v)", raw, err)
	}
	allowed := map[string]bool{"variantId": true, "warehouseId": true, "check": true, "bucket": true, "expected": true, "actual": true}
	for _, it := range items {
		for k := range it {
			if !allowed[k] {
				e.t.Fatalf("mismatches có khóa lạ %q: %s", k, raw)
			}
		}
	}
}

func hasMismatch(r inventory.Run, m inventory.Mismatch) bool {
	for _, x := range r.Mismatches {
		if x == m {
			return true
		}
	}
	return false
}

// Sửa thẳng held không kèm sổ → hai mục của cùng biến thể (sổ ngăn held và giữ hàng); in_transit lệch giữ hàng →
// mục in_transit_vs_reservations.
func TestReconcile_EachCheck(t *testing.T) {
	e := newInvEnv(t)
	vs := e.cleanHistory()
	e.owner("UPDATE inventory.stock_balances SET held = held + 1 WHERE variant_id = $1", vs[0])
	e.owner(`UPDATE inventory.stock_balances SET in_transit = in_transit + 2 WHERE variant_id = $1`, vs[1])
	e.owner(`INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id,
		step, actor_type) VALUES ($1, $2, $3, 'in_transit', 2, 'adjust', gen_random_uuid(), 'bug', 'system')`,
		e.tenant, vs[1], e.warehouse)
	run := e.reconcile(0)
	got := map[string]uuid.UUID{}
	for _, m := range run.Mismatches {
		got[m.Check+"/"+m.Bucket] = m.VariantID
	}
	if len(run.Mismatches) != 3 || got["ledger_vs_balance/held"] != vs[0] || got["held_vs_reservations/"] != vs[0] ||
		got["in_transit_vs_reservations/"] != vs[1] {
		t.Fatalf("mục lệch: %+v", run.Mismatches)
	}
}

// fillLargeTenant dựng 50.000 số dư (5.000 biến thể × 10 kho) khớp sổ. Dùng chung cho test đúng/sai ở đây và test
// thời gian (reconcile_perf_test.go, nhãn perf).
func (e *invEnv) fillLargeTenant() {
	e.t.Helper()
	e.seedLarge(`INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
		SELECT $1, $2, 'W' || g, 'Kho ' || g FROM generate_series(1, 9) AS g`, e.tenant, e.seller)
	e.seedLarge(`INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
		SELECT $1, $2, $3, 'BIG-' || g, 'BG' || lpad(g::text, 7, '0'), 'Đỏ', 'Z' || g, 100000
		FROM generate_series(1, 5000) AS g`, e.tenant, e.seller, e.product)
	e.seedLarge(`INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id, on_hand)
		SELECT $1, v.id, w.id, v.seller_id, 1 + (abs(hashtext(v.id::text || w.id::text)) % 20)
		FROM catalog.variants AS v CROSS JOIN inventory.warehouses AS w WHERE v.sku LIKE 'BIG-%'`, e.tenant)
	e.seedLarge(`INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id,
		step, actor_type)
		SELECT tenant_id, variant_id, warehouse_id, 'on_hand', on_hand, 'import', gen_random_uuid(), 'seed', 'system'
		FROM inventory.stock_balances`)
	e.seedLarge("ANALYZE inventory.stock_balances")
	e.seedLarge("ANALYZE inventory.stock_ledger")
}

// 50.000 số dư → đối soát đếm đủ, không lệch. Hạn thời gian (mỗi câu dưới statement_timeout 30 s, cả lượt dưới 60 s)
// nằm ở TestReconcile_LargeTenantTimeBudget (nhãn perf, make perf-test), không ở đây: thời gian phụ thuộc máy bận hay
// rảnh; ở đây statement_timeout 10 phút chỉ là chốt chống treo.
func TestReconcile_LargeTenant(t *testing.T) {
	e := newInvEnv(t)
	e.fillLargeTenant()
	run := e.reconcileWithin(0, "10min")
	if !run.OK || run.Checked != 50000 {
		t.Fatalf("ok %v, checked %d", run.OK, run.Checked)
	}
}

// reconcileNextRun đọc next_run_at của dòng lịch chèn sẵn ở migration và kiểm nó là 19:30 UTC kế tiếp.
func (e *invEnv) reconcileNextRun() time.Time {
	e.t.Helper()
	var next time.Time
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT next_run_at FROM platform.schedules
		WHERE name = $1 AND every_seconds = 86400 AND per_tenant`, inventory.ReconcileSchedule).Scan(&next); err != nil {
		e.t.Fatalf("dòng lịch chèn sẵn: %v", err)
	}
	next = next.UTC()
	if next.Hour() != 19 || next.Minute() != 30 || next.Second() != 0 || time.Until(next) > 24*time.Hour ||
		time.Since(next) > time.Hour {
		e.t.Fatalf("next_run_at = %v, mong 19:30 UTC kế tiếp", next)
	}
	return next
}

// checkMismatchCommand chạy lệnh cảnh báo lệch sổ trên payload đã ghi: khóa stock-mismatch:<tenant>, mức normal,
// văn bản chỉ có số; việc không tenant → rejected.
func (e *invEnv) checkMismatchCommand(payload json.RawMessage, tenant uuid.UUID, text string) {
	e.t.Helper()
	n := &fakeNotifier{}
	cmd := inventory.NewStockMismatchCommand(n, nil)
	if cmd.Kind().Name != inventory.StockMismatchAlertKind || cmd.Kind().Policy.Mode != queue.ModeCommand ||
		!cmd.AutoRetryNotSent() {
		e.t.Fatalf("Kind sai: %+v", cmd.Kind())
	}
	out, err := cmd.Execute(e.t.Context(), queue.Job{Kind: cmd.Kind().Name, Payload: payload, TenantID: &tenant})
	if err != nil || out.Status != commands.StatusDone {
		e.t.Fatalf("Execute: %+v, %v", out, err)
	}
	if n.key != "stock-mismatch:"+e.tenant.String() || n.severity != "normal" || n.text != text ||
		!regexp.MustCompile(`^[0-9]+ mã hàng lệch sổ$`).MatchString(n.text) {
		e.t.Fatalf("cảnh báo sai: %+v", n)
	}
	if out, err := cmd.Execute(e.t.Context(), queue.Job{Kind: cmd.Kind().Name, Payload: payload}); err != nil ||
		out.Status != commands.StatusRejected {
		e.t.Fatalf("lệnh không tenant: %+v, %v", out, err)
	}
}

// Lịch: dòng platform.schedules chèn sẵn ở 19:30 UTC (02:30 giờ Việt Nam); Tick lúc 10:00 UTC cùng ngày không ghi
// việc; Tick lúc 19:30 UTC ghi đúng một việc mỗi tenant. Worker chạy việc → mỗi tenant một lần chạy; tenant lệch có
// đúng một lệnh cảnh báo; lệnh gửi khóa stock-mismatch:<tenant>, văn bản chỉ có số.
func TestReconcile_NightlySchedule(t *testing.T) {
	e := newInvEnv(t)
	vs := e.cleanHistory()
	e.owner("UPDATE inventory.stock_balances SET on_hand = on_hand + 1 WHERE variant_id = ANY ($1)", vs[:2])
	b := newInvEnvOn(t, e.staff, "B")
	b.stock(4)

	next := e.reconcileNextRun()

	r := schedules.NewRegistry()
	if err := r.Define(schedules.Schedule{
		Name: inventory.ReconcileSchedule, Every: inventory.ReconcileEvery, PerTenant: true, Kind: inventory.ReconcileKind(),
	}); err != nil {
		t.Fatal(err)
	}
	tk := schedules.NewTicker(e.staff.App, queue.NewQueue(), r, schedules.TickerOptions{})
	morning := time.Date(next.Year(), next.Month(), next.Day(), 10, 0, 0, 0, time.UTC)
	if n, err := tk.Tick(t.Context(), morning); err != nil || n != 0 {
		t.Fatalf("Tick 10:00 UTC: %d việc, %v", n, err)
	}
	if n, err := tk.Tick(t.Context(), next); err != nil || n != 2 {
		t.Fatalf("Tick 19:30 UTC: %d việc, mong 2 (mỗi tenant một), %v", n, err)
	}
	jobs := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1", inventory.ReconcileKind().Name)
	if jobs != 2 {
		t.Fatalf("có %d việc đối chiếu, mong 2", jobs)
	}

	w := queue.NewWorker(e.staff.App, []queue.Handler{inventory.NewReconcileHandler(queue.NewQueue())},
		queue.WorkerOptions{WorkerID: "w", Concurrency: 2})
	testkit.MakeDueSoon(t, e.staff.Owner) // việc vừa ghi có thể lệch hạn vì đồng hồ PostgreSQL lùi (Task 451)
	if _, err := w.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'",
		inventory.ReconcileKind().Name); n != 2 {
		t.Fatalf("%d việc đối chiếu done, mong 2", n)
	}
	if n := e.count("SELECT count(*) FROM inventory.reconcile_runs_v WHERE NOT ok AND mismatch_count = 2"); n != 1 {
		t.Fatalf("tenant A: %d lần chạy lệch 2 mục, mong 1", n)
	}
	if n := b.count("SELECT count(*) FROM inventory.reconcile_runs_v WHERE ok AND checked = 1"); n != 1 {
		t.Fatalf("tenant B: %d lần chạy ok, mong 1", n)
	}
	var (
		payload json.RawMessage
		tenant  uuid.UUID
	)
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1", inventory.StockMismatchAlertKind); n != 1 {
		t.Fatalf("có %d lệnh cảnh báo lệch sổ, mong 1", n)
	}
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), "SELECT payload, tenant_id FROM platform.jobs WHERE kind = $1",
		inventory.StockMismatchAlertKind).Scan(&payload, &tenant); err != nil || tenant != e.tenant {
		t.Fatalf("lệnh cảnh báo: tenant %v, %v", tenant, err)
	}

	e.checkMismatchCommand(payload, tenant, "2 mã hàng lệch sổ")
}
