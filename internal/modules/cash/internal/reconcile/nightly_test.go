package reconcile_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit/codtest"
)

// slot0 là kỳ đối chiếu đầu của test (19:30 UTC = 02:30 giờ Việt Nam ngày 21/11).
var slot0 = time.Date(2026, 11, 20, 19, 30, 0, 0, time.UTC) //nolint:gochecknoglobals // hằng của test

// command trả lệnh đối chiếu của module.
func command(t *testing.T, e *codtest.Env) commands.Handler { //nolint:ireturn // lệnh của module
	t.Helper()
	for _, h := range e.Mod.CommandHandlers() {
		if h.Kind().Name == cash.ReconcileScheduleName {
			return h
		}
	}
	t.Fatal("module không có lệnh cash.reconcile")
	return nil
}

// tryRun chạy lệnh h đối chiếu kỳ slot như Runner (ngoài transaction) và trả mã lần chạy (lỗi khi không done).
func tryRun(ctx context.Context, h commands.Handler, tenant uuid.UUID, slot time.Time) (uuid.UUID, error) {
	p, err := json.Marshal(map[string]time.Time{"slot": slot})
	if err != nil {
		return uuid.Nil, err //nolint:wrapcheck // test
	}
	out, err := h.Execute(ctx, queue.Job{ID: uuid.New(), TenantID: &tenant, Payload: p})
	if err != nil || out.Status != commands.StatusDone {
		return uuid.Nil, fmt.Errorf("đối chiếu kỳ %s: %+v %w", slot, out, err)
	}
	var res struct {
		RunID uuid.UUID `json:"runId"`
	}
	err = json.Unmarshal(out.Result, &res)
	return res.RunID, err //nolint:wrapcheck // test
}

func run(t *testing.T, e *codtest.Env, slot time.Time) uuid.UUID {
	t.Helper()
	id, err := tryRun(t.Context(), command(t, e), e.Tenant, slot)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// page đọc phát hiện của lần chạy id (nil: gần nhất).
func page(t *testing.T, e *codtest.Env, id *uuid.UUID) cash.ReconcileFindingPage {
	t.Helper()
	var p cash.ReconcileFindingPage
	e.MustApp(func(tx pgx.Tx) error {
		var err error
		p, err = e.Mod.Service.ListReconcileFindings(t.Context(), tx, id, nil, 200)
		return err //nolint:wrapcheck // test
	})
	return p
}

// findings trả các phát hiện của lần chạy id theo loại.
func findings(t *testing.T, e *codtest.Env, id uuid.UUID, kind string) []cash.ReconcileFinding {
	t.Helper()
	var out []cash.ReconcileFinding
	for _, f := range page(t, e, &id).Items {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func runRow(t *testing.T, e *codtest.Env, id uuid.UUID) (ok bool, n int) {
	t.Helper()
	e.MustApp(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT ok, findings FROM cash.reconcile_runs WHERE id = $1", id).Scan(&ok, &n)
	})
	return ok, n
}

func ptr[T any](v T) *T { return &v }

// fleet dựng vận đơn ở đủ trạng thái, sổ ghi bằng consumer cash (đúng); trả mã theo tên.
func fleet(e *codtest.Env) map[string]uuid.UUID {
	pick, deliver := []string{"picked_up"}, []string{"picked_up", "delivered"}
	out := map[string]uuid.UUID{
		"creating":         e.Shipment(codtest.Ship{Tracking: "FK1001", COD: 300000, Status: fulfillment.StatusCreating}),
		"awaiting_pickup":  e.Shipment(codtest.Ship{Tracking: "FK1002", COD: 300000, Status: "awaiting_pickup"}),
		"V3 shipping":      e.Shipment(codtest.Ship{Tracking: "FK1003", COD: 450000, Status: fulfillment.StatusShipping, Steps: [][]string{pick}}),
		"delivery_failed":  e.Shipment(codtest.Ship{Tracking: "FK1004", COD: 200000, Status: "delivery_failed", Steps: [][]string{pick}}),
		"returned":         e.Shipment(codtest.Ship{Tracking: "FK1005", COD: 350000, Status: "returned", Steps: [][]string{pick, {"returning"}}}),
		"cancelled":        e.Shipment(codtest.Ship{Tracking: "FK1006", COD: 300000, Status: fulfillment.StatusCancelled}),
		"cancel_requested": e.Shipment(codtest.Ship{Tracking: "FK1007", COD: 300000, Status: "cancel_requested"}),
		"partial": e.Shipment(codtest.Ship{
			Tracking: "FK1008", COD: 450000, Status: fulfillment.StatusDelivered,
			Steps: [][]string{deliver}, CODCollected: ptr[int64](200000), Partial: true,
		}),
	}
	v4 := e.Shipment(codtest.Ship{Tracking: "FK1009", COD: 630000, Status: fulfillment.StatusDelivered, Steps: [][]string{deliver}})
	e.Remit(v4, "FK1009", 630000, 32000, e.Now.Add(-48*time.Hour))
	out["delivered remitted"] = v4
	out["delivered"] = e.Shipment(codtest.Ship{Tracking: "FK1010", COD: 500000, Status: fulfillment.StatusDelivered, Steps: [][]string{deliver}})
	return out
}

// recAlerter ghi lại mọi cảnh báo.
type recAlerter struct {
	mu   sync.Mutex
	keys []string
	text []string
}

func (a *recAlerter) Notify(_ context.Context, key, _, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.keys, a.text = append(a.keys, key), append(a.text, text)
	return nil
}

// injectUnbalanced chèn (chủ bảng, tắt trigger cân hai vế) một bút toán một dòng to_collect +1000 cho vận đơn v; trả mã
// bút toán.
func injectUnbalanced(t *testing.T, e *codtest.Env, v3 uuid.UUID) uuid.UUID {
	t.Helper()
	bad := uuid.Must(uuid.NewV7())
	e.Staff.Exec(t, e.Tenant, func(tx pgx.Tx) error {
		for _, q := range []string{
			"ALTER TABLE cash.cod_transfers DISABLE TRIGGER cod_transfers_balanced",
			"ALTER TABLE cash.cod_ledger_entries DISABLE TRIGGER cod_ledger_entries_balanced",
			"INSERT INTO cash.cod_transfers (id, tenant_id, shipment_id, kind, idempotency_key, reason, actor_type) " +
				"VALUES ('" + bad.String() + "', '" + e.Tenant.String() + "', '" + v3.String() + "', 'adjustment', 'chen-tay', 'test', 'system')",
			"INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account, amount) " +
				"VALUES ('" + e.Tenant.String() + "', '" + bad.String() + "', '" + v3.String() + "', 'to_collect', 1000)",
			"ALTER TABLE cash.cod_ledger_entries ENABLE TRIGGER cod_ledger_entries_balanced",
			"ALTER TABLE cash.cod_transfers ENABLE TRIGGER cod_transfers_balanced",
		} {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return bad
}

// checkAlert: đúng một lệnh cảnh báo; gửi → key "cod-reconcile:2026-11-22", nội dung có số lượng, không mã đơn, mã
// vận đơn của v3.
func checkAlert(t *testing.T, e *codtest.Env, v3 uuid.UUID) {
	t.Helper()
	var payload []byte
	if n := e.Count("SELECT count(*) FROM platform.jobs WHERE kind = $1", cash.ReconcileAlertKind); n != 1 {
		t.Fatalf("lệnh cảnh báo: %d, mong 1", n)
	}
	e.MustApp(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT payload FROM platform.jobs WHERE kind = $1", cash.ReconcileAlertKind).Scan(&payload)
	})
	al := &recAlerter{}
	out, err := cash.NewReconcileAlertCommand(al).Execute(t.Context(), queue.Job{TenantID: &e.Tenant, Payload: payload})
	if err != nil || out.Status != commands.StatusDone || len(al.keys) != 1 || al.keys[0] != "cod-reconcile:2026-11-22" {
		t.Fatalf("gửi cảnh báo: %+v %v %v", out, err, al.keys)
	}
	var orderCode string
	e.MustApp(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT o.code FROM fulfillment.shipments_v s JOIN ord.orders_v o ON o.id = s.order_id
			WHERE s.id = $1`, v3).Scan(&orderCode)
	})
	if txt := al.text[0]; !strings.Contains(txt, "3 phát hiện") || strings.Contains(txt, orderCode) ||
		strings.Contains(txt, "FK1003") || strings.Contains(txt, v3.String()) {
		t.Errorf("nội dung cảnh báo mang mã đơn hay vận đơn, hoặc thiếu số lượng: %q", txt)
	}
}

// TI03: vận đơn ở đủ trạng thái, sổ đúng → lần chạy ok, 0 phát hiện, không lệnh cảnh báo. Chèn (chủ bảng, tắt
// trigger) một bút toán một dòng to_collect +1000 cho V3 → lần chạy kỳ sau: balance_mismatch cho V3, ledger_nonzero 1000,
// transfer_unbalanced cho bút toán bị chèn; một lệnh cảnh báo "cod-reconcile:<ngày>" chỉ mang số lượng (không mã đơn,
// mã vận đơn); listReconcileFindings mặc định là lần gần nhất.
func TestNightly_TI03(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	ships := fleet(e)
	first := run(t, e, slot0)
	if ok, n := runRow(t, e, first); !ok || n != 0 || len(page(t, e, &first).Items) != 0 {
		t.Fatalf("sổ đúng: ok=%v findings=%d, phát hiện %+v", ok, n, page(t, e, &first).Items)
	}
	if n := e.Count("SELECT count(*) FROM platform.jobs WHERE kind = $1", cash.ReconcileAlertKind); n != 0 {
		t.Errorf("sổ đúng mà có %d lệnh cảnh báo", n)
	}

	v3 := ships["V3 shipping"]
	bad := injectUnbalanced(t, e, v3)
	second := run(t, e, slot0.Add(24*time.Hour))
	mism := findings(t, e, second, cash.FindingBalanceMismatch)
	if len(mism) != 1 || mism[0].ShipmentID == nil || *mism[0].ShipmentID != v3 || mism[0].Detail["toCollect"] != "451000" ||
		mism[0].Detail["status"] != "shipping" || mism[0].Detail["expectedToCollect"] != "450000" {
		t.Errorf("balance_mismatch: %+v", mism)
	}
	if nz := findings(t, e, second, cash.FindingLedgerNonzero); len(nz) != 1 || nz[0].Detail["total"] != "1000" || nz[0].ShipmentID != nil {
		t.Errorf("ledger_nonzero: %+v", nz)
	}
	if ub := findings(t, e, second, cash.FindingTransferUnbalanced); len(ub) != 1 || ub[0].Detail["transferId"] != bad.String() ||
		ub[0].Detail["lines"] != "1" || ub[0].Detail["total"] != "1000" {
		t.Errorf("transfer_unbalanced: %+v", ub)
	}
	if ok, n := runRow(t, e, second); ok || n != 3 {
		t.Errorf("lần chạy 2: ok=%v findings=%d, mong false 3", ok, n)
	}
	if latest := page(t, e, nil); latest.RunID == nil || *latest.RunID != second || len(latest.Items) != 3 {
		t.Errorf("mặc định lần gần nhất: %+v", latest)
	}

	checkAlert(t, e, v3)
}

// Vận đơn có phát hiện remitted_then_returned còn mở (tiền đã về rồi hãng sửa thành hoàn) → lần chạy không báo
// balance_mismatch lặp lại, chỉ ghi skipped (lý do remitted_then_returned), lần chạy vẫn ok. Vận đơn vừa đổi trạng thái
// (sự kiện chưa vào sổ) → skipped recent_status.
func TestNightly_SkipsOpenReturnedFinding(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	s := codtest.Ship{Tracking: "FK2001", COD: 630000, Status: fulfillment.StatusDelivered, Steps: [][]string{{"picked_up", "delivered"}}}
	v := e.Shipment(s)
	e.Remit(v, s.Tracking, 630000, 32000, e.Now.Add(-48*time.Hour))
	s.Status = "returning"
	e.Staff.Exec(t, e.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.shipments SET status = 'returning' WHERE id = $1", v)
		return err //nolint:wrapcheck // test
	})
	e.Event(v, s, []string{"delivery_reversed", "returning"})
	if n := e.Count("SELECT count(*) FROM cash.reconcile_findings WHERE shipment_id = $1 AND kind = 'remitted_then_returned'", v); n != 1 {
		t.Fatalf("phát hiện theo sự kiện: %d, mong 1", n)
	}
	fresh := e.Shipment(codtest.Ship{Tracking: "FK2002", COD: 100000, Status: fulfillment.StatusShipping, StatusAt: e.Now.Add(-time.Minute)})

	id := run(t, e, slot0)
	if m := findings(t, e, id, cash.FindingBalanceMismatch); len(m) != 0 {
		t.Errorf("báo lặp balance_mismatch: %+v", m)
	}
	reasons := map[uuid.UUID]string{}
	for _, f := range findings(t, e, id, cash.FindingSkipped) {
		reasons[*f.ShipmentID] = f.Detail["reason"]
	}
	if reasons[v] != "remitted_then_returned" || reasons[fresh] != "recent_status" || len(reasons) != 2 {
		t.Errorf("skipped: %v", reasons)
	}
	if ok, n := runRow(t, e, id); !ok || n != 0 {
		t.Errorf("skipped không tính là lệch: ok=%v findings=%d", ok, n)
	}
}

// Vận đơn có điều chỉnh của nhân viên sau lần chốt bảng kê gần nhất → skipped adjusted (số dư lệch đích có chủ đích).
func TestNightly_SkipsAdjustedSinceConfirm(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	v := e.Shipment(codtest.Ship{Tracking: "FK3001", COD: 500000, Status: fulfillment.StatusShipping, Steps: [][]string{{"picked_up"}}})
	e.MustApp(func(tx pgx.Tx) error {
		_, err := cash.Post(t.Context(), tx, cash.Transfer{
			ShipmentID: v, Kind: cash.KindAdjustment, IdempotencyKey: "adj-test", Reason: "ghi nhầm",
			Actor:   cash.Actor{Type: cash.ActorStaff, ID: e.User},
			Entries: []cash.Entry{{Account: cash.AccountToCollect, Amount: -1000}, {Account: cash.AccountCarrierHolding, Amount: 1000}},
		})
		return err //nolint:wrapcheck // test
	})
	id := run(t, e, slot0)
	sk := findings(t, e, id, cash.FindingSkipped)
	if len(sk) != 1 || *sk[0].ShipmentID != v || sk[0].Detail["reason"] != "adjusted" ||
		len(findings(t, e, id, cash.FindingBalanceMismatch)) != 0 {
		t.Errorf("phát hiện: %+v", page(t, e, &id).Items)
	}
}

// Lịch chạy hai lần cùng kỳ (giao lại việc, hai worker cùng lúc) → một reconcile_runs, cùng mã lần chạy; Lookup của kỳ
// đã chạy → done, kỳ chưa chạy → not_sent. Payload hỏng → rejected.
func TestNightly_OneRunPerSlot(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	e.Shipment(codtest.Ship{Tracking: "FK4001", COD: 450000, Status: fulfillment.StatusShipping, Steps: [][]string{{"picked_up"}}})
	ids, errs := make([]uuid.UUID, 4), make([]error, 4)
	h := command(t, e)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() { ids[i], errs[i] = tryRun(t.Context(), h, e.Tenant, slot0) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(ids, func(id uuid.UUID) bool { return id != ids[0] || id == uuid.Nil }) {
		t.Errorf("mã lần chạy khác nhau: %v", ids)
	}
	if n := e.Count("SELECT count(*) FROM cash.reconcile_runs WHERE slot = $1", slot0); n != 1 {
		t.Errorf("lần chạy của kỳ: %d, mong 1", n)
	}
	lk, ok := command(t, e).(commands.Looker)
	if !ok {
		t.Fatal("lệnh đối chiếu không có Lookup")
	}
	for slot, want := range map[time.Time]string{slot0: commands.StatusDone, slot0.Add(24 * time.Hour): commands.StatusNotSent} {
		p, _ := json.Marshal(map[string]time.Time{"slot": slot})
		if got, err := lk.Lookup(t.Context(), queue.Job{TenantID: &e.Tenant, Payload: p}); err != nil || got != want {
			t.Errorf("Lookup kỳ %s: %s %v, mong %s", slot, got, err, want)
		}
	}
	out, err := command(t, e).Execute(t.Context(), queue.Job{TenantID: &e.Tenant, Payload: []byte(`{}`)})
	if err != nil || out.Status != commands.StatusRejected {
		t.Errorf("payload thiếu kỳ: %+v %v", out, err)
	}
}

// Lịch của module: cash.reconcile mỗi 86.400 s theo tenant; dòng platform.schedules chèn sẵn ở 19:30 UTC (02:30 giờ Việt
// Nam). Chưa chạy lần nào → trang phát hiện rỗng, không mã lần chạy; mã lần chạy lạ → ErrReconcileRunNotFound.
func TestNightly_ScheduleAndEmptyList(t *testing.T) {
	t.Parallel()
	e := codtest.New(t)
	var found bool
	for _, s := range e.Mod.Schedules() {
		if s.Name == cash.ReconcileScheduleName {
			found = s.Every == 24*time.Hour && s.PerTenant && s.Kind.Name == cash.ReconcileScheduleName
		}
	}
	if !found {
		t.Errorf("lịch cash.reconcile: %+v", e.Mod.Schedules())
	}
	var next time.Time
	var every int
	e.Staff.Exec(t, e.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT next_run_at, every_seconds FROM platform.schedules WHERE name = $1",
			cash.ReconcileScheduleName).Scan(&next, &every)
	})
	if u := next.UTC(); u.Hour() != 19 || u.Minute() != 30 || every != 86400 {
		t.Errorf("dòng lịch: %v mỗi %d s", next, every)
	}
	if p := page(t, e, nil); p.RunID != nil || len(p.Items) != 0 {
		t.Errorf("chưa chạy: %+v", p)
	}
	err := e.App(func(tx pgx.Tx) error {
		_, err := e.Mod.Service.ListReconcileFindings(t.Context(), tx, ptr(uuid.New()), nil, 0)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, cash.ErrReconcileRunNotFound) {
		t.Errorf("lần chạy lạ: %v", err)
	}
}
