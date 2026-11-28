package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Bài thử hỏng của M3 (kế hoạch M3, Task M3-48; Review Focus 1–5) trên stack đủ M3 với hãng giả thật. Phần lõi của
// từng ca có test cấp gói chạy trong ci-task (ghi trong docs/tasks/task-139.md); ở đây chạy trên ba vai trò thật.

// commandBudget: lệnh tạo vận đơn quá hạn gửi (25 s) thì thành lỗi → in_doubt; chờ thêm cho lượt nhận việc.
const commandBudget = 45 * time.Second

// (a) VC16: hãng nhận lệnh tạo rồi treo (tiến trình gửi coi như chết giữa chừng: không biết hãng đã tạo hay chưa) →
// lệnh tạo in_doubt, vận đơn "không rõ", không gửi lại; hãng giả có đúng một kiện. Tra lại (ResolveInDoubt: tra theo
// mã vận đơn) → lệnh done, vận đơn awaiting_pickup có mã hãng; hãng vẫn một kiện.
func TestM3Chaos_CreateInDoubt(t *testing.T) {
	r := newM3Run(t)
	v := r.productWithStock("M3CHAOSA", 5)
	orderID := str(r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000), http.StatusCreated)["id"])
	r.fakeMode("timeout_after_create")
	shipmentID := str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.Account}, http.StatusAccepted)["id"])
	waitFor(t, commandBudget, "lệnh tạo in_doubt", func() bool {
		return r.s.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment'
			AND status = $1`, queue.StatusInDoubt) == 1
	})
	if n := r.fakeParcels(); n != 1 {
		t.Fatalf("hãng giả có %d kiện, muốn 1", n)
	}
	sh := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
	if sh["status"] != "creating" || sh["createState"] != "unknown" {
		t.Fatalf("vận đơn khi lệnh in_doubt = %v, muốn creating, createState unknown", sh)
	}
	r.fakeMode("ok")
	done, requeued, still, err := commands.ResolveInDoubt(t.Context(), r.s.app, r.s.cmds, 10)
	if err != nil || done != 1 || requeued != 0 || still != 0 {
		t.Fatalf("ResolveInDoubt = %d %d %d %v, muốn 1 0 0", done, requeued, still, err)
	}
	r.awaitingTracking(shipmentID)
	if n := r.fakeParcels(); n != 1 {
		t.Fatalf("sau tra lại hãng giả có %d kiện, muốn 1", n)
	}
	r.stockIs(v, stock{onHand: 5, held: 1, available: 4})
	r.noDeadJobs()
}

// (b) Database ngừng trong lúc 50 webhook hãng đến (pool của ingest qua proxy có công tắc): 25 vận đơn chờ thu gom,
// mỗi vận đơn hai tin (lấy hàng, giao thành công) → mọi webhook 200 (giữ ở đĩa). Database về → mỗi vận đơn tới
// delivered, xuất kho đúng một lần (tồn 25 → 0), COD hãng giữ đúng một lần; tổng sổ COD = 0, sổ kho = số dư.
func TestM3Chaos_WebhooksDuringDatabaseOutage(t *testing.T) {
	var proxy *switchProxy
	r := newOrderRunWith(t, stackOptions{m3: true, ingestURL: func(appURL string) string {
		p, u := startSwitchProxy(t, appURL)
		proxy = p
		return u
	}})
	const n = 25
	v := r.productWithStock("M3CHAOSB", n)
	_, trackings := r.awaitingShipments(v, n)
	proxy.SetDown(true)
	for _, to := range []string{fake.RawPickedUp, fake.RawDelivered} {
		for _, tr := range trackings {
			r.advance(tr, to) // đòi ingest trả 200
		}
	}
	proxy.SetDown(false)
	waitFor(t, 6*stepBudget, "25 vận đơn delivered, COD hãng giữ", func() bool {
		return r.count(`SELECT count(*) FROM fulfillment.shipments WHERE status = 'delivered'`) == n &&
			r.count(`SELECT coalesce(sum(balance), 0)::int FROM cash.cod_balances_v WHERE account = 'carrier_holding'`) == n*280000
	})
	r.stockIs(v, stock{})
	if n := r.count(`SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1 AND bucket = 'on_hand'
		AND delta < 0`, v); n != 25 {
		t.Errorf("số lần xuất kho = %d, muốn 25", n)
	}
	r.codLedgerZero()
	r.ledgerMatchesBalance(v)
	r.noDeadJobs()
}

// (c) Database ngừng giữa lúc lệnh đọc tệp ghi lô dòng bảng kê (30.000 dòng, lô 1.000 dòng mỗi transaction; pool của
// api, worker qua proxy có công tắc — như tiến trình ghi chết sau vài lô): cắt khi đã có lô commit, chưa đủ dòng; nối
// lại → lệnh chạy lại, bảng kê parsed đủ 30.000 dòng, mỗi số dòng một lần.
func TestM3Chaos_StatementBatchRestart(t *testing.T) {
	var proxy *switchProxy
	r := newOrderRunWith(t, stackOptions{m3: true, appURL: func(appURL string) string {
		p, u := startSwitchProxy(t, appURL)
		proxy = p
		return u
	}})
	const lines = 30000
	var b strings.Builder
	b.WriteString("ma_van_don,tien_cod,phi_van_chuyen,phi_khac,thuc_nhan\n")
	for i := range lines {
		fmt.Fprintf(&b, "EXT%06d,100000,20000,0,80000\n", i+1)
	}
	id := r.importStatement(b.String())
	var cut int
	waitFor(t, 3*stepBudget, "lô dòng đầu đã commit", func() bool {
		cut = r.count(`SELECT count(*) FROM cash.cod_statement_lines WHERE statement_id = $1`, id)
		return cut >= 1000
	})
	proxy.SetDown(true)
	if cut >= lines {
		t.Fatalf("lệnh đọc tệp đã ghi đủ %d dòng trước khi cắt database: bài thử không cắt được giữa chừng", cut)
	}
	time.Sleep(time.Second)
	proxy.SetDown(false)
	testkit.MakeDueSoon(t, r.s.owner) // lệnh đọc tệp not_sent sau lỗi database: không chờ lịch thử lại
	head := r.statementAfterOutage(id, 18*stepBudget)
	if head["lineCount"] != float64(lines) {
		t.Fatalf("bảng kê sau khi chạy lại = %v, muốn %d dòng", head, lines)
	}
	if n := r.count(`SELECT count(DISTINCT line_no) FROM cash.cod_statement_lines WHERE statement_id = $1`, id); n != lines {
		t.Fatalf("%d số dòng khác nhau, muốn %d", n, lines)
	}
	if n := r.count(`SELECT count(*) FROM cash.cod_statement_lines WHERE statement_id = $1`, id); n != lines {
		t.Fatalf("%d dòng, muốn %d (không trùng)", n, lines)
	}
	t.Logf("cắt database khi đã có %d / %d dòng", cut, lines)
}

// (d) TI07: hai kế toán chốt cùng một bảng kê (cùng version) cùng lúc, trong lúc consumer sổ COD đang ghi tin giao
// thành công của 5 vận đơn khác → đúng một lần chốt 200, lần kia 409 statement_changed; 10 vận đơn của bảng kê mỗi
// vận đơn COD về shop đúng một lần; 5 vận đơn kia COD hãng giữ; tổng sổ COD = 0.
func TestM3Chaos_TwoAccountantsConfirm(t *testing.T) {
	r := newM3Run(t)
	v := r.productWithStock("M3CHAOSD", 15)
	ships, trackings := r.awaitingShipments(v, 15)
	for _, tr := range trackings {
		r.advance(tr, fake.RawPickedUp)
	}
	for _, tr := range trackings[:10] {
		r.advance(tr, fake.RawDelivered)
	}
	waitFor(t, 3*stepBudget, "10 vận đơn delivered", func() bool {
		return r.count(`SELECT count(*) FROM fulfillment.shipments WHERE status = 'delivered'`) == 10
	})
	var b strings.Builder
	b.WriteString("ma_van_don,tien_cod,phi_van_chuyen,phi_khac,thuc_nhan\n")
	for _, tr := range trackings[:10] {
		fmt.Fprintf(&b, "%s,280000,25000,0,255000\n", tr)
	}
	id := r.importStatement(b.String())
	head := r.waitStatement(id, "matched", 3*stepBudget)

	r.race(http.MethodPost, "/api/v1/cod-statements/"+id+"/confirm", map[string]any{"version": head["version"]},
		func() {
			for _, tr := range trackings[10:] {
				r.advance(tr, fake.RawDelivered) // consumer sổ COD chạy cùng lúc với hai lần chốt
			}
		}, http.StatusOK, http.StatusConflict, "statement_changed")
	waitFor(t, 3*stepBudget, "5 vận đơn còn lại COD hãng giữ", func() bool {
		for _, id := range ships[10:] {
			if r.cod(id)["carrier_holding"] != 280000 {
				return false
			}
		}
		return true
	})
	for _, id := range ships[:10] {
		if c := r.cod(id); c["shop_received"] != 255000 || c["carrier_fee"] != 25000 || c["carrier_holding"] != 0 {
			t.Errorf("vận đơn %s sau chốt: %v", id, c)
		}
	}
	r.codLedgerZero()
	r.noDeadJobs()
}

// awaitingShipments lên n đơn 1 cái của biến thể v (ship 30.000), gửi hãng giả bằng tài khoản của người bán mặc định
// và chờ mọi vận đơn chờ thu gom; trả mã vận đơn, mã hãng theo cùng thứ tự.
func (r *orderRun) awaitingShipments(v uuid.UUID, n int) (ships, trackings []string) {
	r.t.Helper()
	ships, trackings = make([]string, n), make([]string, n)
	for i := range n {
		orderID := str(r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000), http.StatusCreated)["id"])
		ships[i] = str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
			map[string]any{"carrierAccountId": r.s.shop.Account}, http.StatusAccepted)["id"])
	}
	for i, id := range ships {
		trackings[i] = r.awaitingTracking(id)
	}
	return ships, trackings
}

// fakeMode đặt chế độ tạo kiện của hãng giả (POST /admin/mode).
func (r *orderRun) fakeMode(mode string) {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.t.Context(), http.MethodPost, r.fc+"/admin/mode",
		strings.NewReader(`{"mode":"`+mode+`"}`))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if status, body := call(r.t, req); status != http.StatusOK {
		r.t.Fatalf("hãng giả đặt chế độ %s = %d %s", mode, status, body)
	}
}

// fakeParcels đọc số kiện hãng giả đang giữ (GET /health).
func (r *orderRun) fakeParcels() int {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.t.Context(), http.MethodGet, r.fc+"/health", nil)
	if err != nil {
		r.t.Fatal(err)
	}
	status, body := call(r.t, req)
	var h struct {
		Parcels int `json:"parcels"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &h) != nil {
		r.t.Fatalf("hãng giả /health = %d %s", status, body)
	}
	return h.Parcels
}

// parseKind là lệnh đọc tệp bảng kê của cash (statement.ParseKind; gói nội bộ của cash).
const parseKind = "command.cash.statement_parse"

// statementAfterOutage chờ bảng kê id matched sau khi database của worker vừa ngắt (Task 527). Lần ghi kết quả của lệnh
// đọc tệp có thể rơi đúng lúc ngắt: lệnh còn running tới hết giữ chỗ của worker (90 s, queue.defaultLease) rồi
// in_doubt, và cmd/crm tra lại in_doubt bằng lịch mỗi commands.ResolveInDoubtEvery (stack e2e không chạy lịch đó). Mỗi
// vòng làm đúng hai việc ấy theo giờ của database, tua nhanh như workercrash_test: lệnh running đã quá một lần chạy
// (25 s, ParseKind: giữ chỗ còn dưới 65 s) thì hết giữ chỗ ngay (worker quét → in_doubt); lệnh in_doubt thì
// ResolveInDoubt (Lookup: chưa parsed → chạy lại, dòng đã ghi bỏ qua). Hỏi API theo nhịp waitForAPI.
func (r *orderRun) statementAfterOutage(id string, budget time.Duration) map[string]any {
	r.t.Helper()
	var head map[string]any
	r.t.Cleanup(func() {
		if r.t.Failed() {
			r.t.Logf("bảng kê cuối cùng thấy: %v; lệnh đọc tệp in_doubt: %d, running: %d, pending: %d", head,
				r.s.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'in_doubt'`, parseKind),
				r.s.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'running'`, parseKind),
				r.s.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'pending'`, parseKind))
		}
	})
	waitForAPI(r.t, budget, "bảng kê matched sau khi database về", func() bool {
		if _, err := r.s.owner.Pool.Exec(r.t.Context(), `UPDATE platform.jobs SET lease_until = now() - interval '1 second'
			WHERE kind = $1 AND status = 'running' AND lease_until < now() + interval '65 seconds'`, parseKind); err != nil {
			r.t.Fatalf("hết giữ chỗ lệnh đọc tệp: %v", err)
		}
		if r.s.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = $2`, parseKind,
			queue.StatusInDoubt) > 0 {
			if _, _, _, err := commands.ResolveInDoubt(r.t.Context(), r.s.app, r.s.cmds, 10); err != nil {
				r.t.Fatalf("tra lại lệnh in_doubt: %v", err)
			}
		}
		got := r.mustCall(http.MethodGet, "/api/v1/cod-statements/"+id+"?limit=1", nil, http.StatusOK)
		head, _ = got["statement"].(map[string]any)
		return head["status"] == "matched"
	})
	return head
}
