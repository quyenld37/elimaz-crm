package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// stepBudget: mỗi bước sau commit (lệnh tạo vận đơn, webhook → lệnh tra → sổ COD) phải xong trong 5 s (kế hoạch M1
// Task 33).
const stepBudget = 5 * time.Second

// Điểm dừng M1-A, một đơn đi trọn qua ba vai trò thật (ingest, api, worker trong tiến trình test), hãng giả thật
// (tools/fakecarrier, tiến trình riêng) và PostgreSQL thật: mẫu 1 biến thể giá 250000 → phiếu nhập 5 → ghi sổ → đơn 2
// cái ship 30000 (grand_total 530000, available 3) → vận đơn → awaiting_pickup → hãng báo picked_up → vận đơn shipping,
// held 0, in_transit 2, COD to_collect 530000 → hãng báo delivered → đơn delivered, on_hand 3, giữ hàng consumed, COD
// carrier_holding 530000; tổng sổ COD = 0, tổng sổ kho từng ngăn = số dư. Span nhận webhook (ingest), span lệnh
// refresh_status và span consumer cash cùng một TraceID.
//
// Dữ liệu có hai người bán (KH14, ADR-29; Q-T37-2): người bán PARTNER1 có mẫu đang bán, tồn 7 ở cùng kho. API chạy
// với người bán mặc định nên: danh sách chỉ có mẫu của ELIMAZ, đọc mẫu PARTNER1 → 404, lên đơn với biến thể PARTNER1
// → 422 mixed_sellers, tồn của PARTNER1 không đổi suốt luồng.
func TestMotDonDiTron(t *testing.T) {
	r := newOrderRun(t)
	s := r.s
	took := map[string]time.Duration{}
	mark := func(name string, start time.Time) { took[name] = time.Since(start) }

	start := time.Now()
	v := r.productWithStock("E2E01", 5)
	mark("tạo mẫu, nhập 5, ghi sổ", start)
	pv := r.partnerWithStock(7)

	start = time.Now()
	o := r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 2, 30000), http.StatusCreated)
	mark("lên đơn", start)
	orderID := str(o["id"])
	if o["status"] != "confirmed" || o["grandTotal"] != float64(530000) || o["codAmount"] != float64(530000) {
		t.Fatalf("đơn = %v, muốn confirmed, grandTotal 530000", o)
	}
	r.stockIs(v, stock{onHand: 5, held: 2, available: 3})
	// listProducts đòi đúng 1 mẫu: mẫu đang bán của PARTNER1 (tạo ở trên) không hiện qua API.
	if got := r.listedAvailable(); got != 3 {
		t.Fatalf("listProducts available = %d, muốn 3", got)
	}

	start = time.Now()
	sh := r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
		map[string]any{"carrierAccountId": s.shop.Account}, http.StatusAccepted)
	shipmentID := str(sh["id"])
	var tracking string
	waitFor(t, stepBudget, "vận đơn awaiting_pickup", func() bool {
		got := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
		tracking = str(got["trackingCode"])
		return got["status"] == "awaiting_pickup" && tracking != ""
	})
	mark("tạo vận đơn → awaiting_pickup", start)

	start = time.Now()
	r.advance(tracking, "picked_up")
	waitFor(t, stepBudget, "vận đơn shipping, tồn đang giao, COD phải thu", func() bool {
		got := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
		return got["status"] == "shipping" && r.stock(v) == stock{onHand: 5, inTransit: 2, available: 3} &&
			r.cod(shipmentID)["to_collect"] == 530000
	})
	mark("hãng báo picked_up → shipping, sổ COD", start)

	start = time.Now()
	r.advance(tracking, "delivered")
	waitFor(t, stepBudget, "đơn delivered, tồn trừ, COD hãng đang giữ", func() bool {
		got := r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)
		c := r.cod(shipmentID)
		return got["status"] == "delivered" && r.stock(v) == stock{onHand: 3, available: 3} &&
			c["carrier_holding"] == 530000 && c["to_collect"] == 0
	})
	mark("hãng báo delivered → đơn delivered, sổ COD", start)

	if n := r.count(`SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1 AND state <> 'consumed'`, v); n != 0 {
		t.Errorf("%d dòng giữ hàng chưa consumed", n)
	}
	if n := r.count(`SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1 AND state = 'consumed'`, v); n != 1 {
		t.Errorf("dòng giữ hàng consumed = %d, muốn 1", n)
	}
	if n := r.count(`SELECT count(*) FROM cash.cod_ledger_entries`); n == 0 {
		t.Error("sổ COD không có dòng nào")
	}
	if n := r.count(`SELECT coalesce(sum(amount), 0)::int FROM cash.cod_ledger_entries`); n != 0 {
		t.Errorf("tổng sổ COD = %d, muốn 0", n)
	}
	r.ledgerMatchesBalance(v)
	r.partnerUntouched(pv, 7)
	if n := s.count(`SELECT count(*) FROM platform.jobs WHERE status = 'dead'`); n != 0 {
		t.Errorf("%d việc dead", n)
	}
	for _, name := range []string{
		"tạo mẫu, nhập 5, ghi sổ", "lên đơn", "tạo vận đơn → awaiting_pickup",
		"hãng báo picked_up → shipping, sổ COD", "hãng báo delivered → đơn delivered, sổ COD",
	} {
		t.Logf("%-45s %v", name, took[name].Round(time.Millisecond))
	}
	r.traceLinksWebhookToCash()
}

// Tiêu chí "Xong" số 4: tồn 1, hai đơn song song (khóa khác nhau) → một 201, một 409 out_of_stock; tồn không âm.
func TestMotDonDiTron_HaiDonSongSongTon1(t *testing.T) {
	r := newOrderRun(t)
	v := r.productWithStock("E2E02", 1)
	var (
		wg       sync.WaitGroup
		statuses [2]int
		bodies   [2]map[string]any
		errs     [2]error
	)
	for i := range 2 {
		wg.Go(func() {
			statuses[i], bodies[i], errs[i] = r.call(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000))
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := []int{statuses[0], statuses[1]}
	slices.Sort(got)
	if !slices.Equal(got, []int{http.StatusCreated, http.StatusConflict}) {
		t.Fatalf("hai đơn song song = %v (%v), muốn một 201, một 409", statuses, bodies)
	}
	loser := bodies[0]
	if statuses[1] == http.StatusConflict {
		loser = bodies[1]
	}
	if loser["error"] != "out_of_stock" {
		t.Fatalf("đơn thua = %v, muốn out_of_stock", loser)
	}
	r.stockIs(v, stock{onHand: 1, held: 1})
	if n := r.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Fatalf("số đơn = %d, muốn 1", n)
	}
}

// Kịch bản chạy thử scripts/demo/mot-don-di-tron.sh (bash, curl, jq) chạy được với api và hãng giả của stack: in đủ
// các bước, kết thúc ở "delivered".
func TestMotDonDiTron_Script(t *testing.T) {
	r := newOrderRun(t)
	s := r.s
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(filepath.Dir(strings.TrimSpace(string(out))), "scripts", "demo", "mot-don-di-tron.sh")
	cmd := exec.CommandContext(t.Context(), "bash", script, s.api.URL)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin",
		"CRM_ACCESS_JWT=" + s.token, "CRM_ORIGIN=" + stackOrigin, "FAKECARRIER_URL=" + r.fc,
		"WAREHOUSE_ID=" + s.shop.Warehouse.String(), "CARRIER_ACCOUNT_ID=" + s.shop.Account.String(),
		"PRODUCT_CODE=SCRIPT01", "WAIT_SECONDS=5",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("script lỗi: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{
		"1. Tạo mẫu", "2. Phiếu nhập", "3. Lên đơn", "grandTotal 530000", "available 3",
		"4. Tạo vận đơn", "awaiting_pickup", "5. Hãng báo picked_up", "shipping", "6. Hãng báo delivered",
		"đơn delivered", "Xong",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("thiếu %q trong kết quả script:\n%s\nstderr:\n%s", want, got, stderr.String())
		}
	}
	if strings.Contains(got, s.token) || strings.Contains(got, "0901234567") {
		t.Fatalf("script in thẻ hay SĐT:\n%s", got)
	}
	t.Logf("kết quả script:\n%s", got)
}

// orderRun: stack có hãng giả thật (tools/fakecarrier chạy tiến trình riêng, gửi webhook về ingest của stack).
type orderRun struct {
	t  *testing.T
	s  *stack
	fc string // địa chỉ hãng giả
}

func newOrderRun(t *testing.T) *orderRun {
	t.Helper()
	port := freePort(t)
	fc := "http://127.0.0.1:" + strconv.Itoa(port)
	s := newStack(t, stackOptions{fakeCarrierURL: fc})
	bin := buildPackage(t, "./tools/fakecarrier")
	_, _, stdout, stderr := startRole(t, bin, []string{
		"PORT=" + strconv.Itoa(port), "FAKECARRIER_WEBHOOK_SECRET=" + secret32, "FAKECARRIER_DATA=-",
		"FAKECARRIER_WEBHOOK_URL=" + s.ingest.URL + "/hooks/fakecarrier",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := waitReady(ctx, fc+"/health"); err != nil {
		t.Fatalf("hãng giả chưa sẵn sàng: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	return &orderRun{t: t, s: s, fc: fc}
}

// call gửi yêu cầu tới api của stack bằng thẻ chủ shop (ghi: Origin, JSON, Idempotency-Key mới); trả mã và thân JSON.
// Không dùng t (gọi được từ goroutine).
func (r *orderRun) call(method, path string, body any) (int, map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("mã hóa thân: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(r.t.Context(), method, r.s.api.URL+path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("dựng yêu cầu: %w", err)
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", r.s.token)
	if method != http.MethodGet {
		req.Header.Set("Origin", stackOrigin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("gửi %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("đọc thân %s %s (%d): %w", method, path, resp.StatusCode, err)
	}
	return resp.StatusCode, m, nil
}

func (r *orderRun) mustCall(method, path string, body any, want int) map[string]any {
	r.t.Helper()
	status, m, err := r.call(method, path, body)
	if err != nil || status != want {
		r.t.Fatalf("%s %s = %d %v (%v), muốn %d", method, path, status, m, err, want)
	}
	return m
}

// productWithStock tạo mẫu code (một biến thể giá 250000, đang bán), phiếu nhập qty ở kho MAIN và ghi sổ; trả biến thể.
func (r *orderRun) productWithStock(code string, qty int) uuid.UUID {
	r.t.Helper()
	p := r.mustCall(http.MethodPost, "/api/v1/catalog/products", map[string]any{
		"code": code, "name": "Đầm thử " + code, "status": "active",
		"variants": []map[string]any{{"sku": code + "-DO-S", "color": "Đỏ", "size": "S", "price": 250000}},
	}, http.StatusCreated)
	vs, _ := p["variants"].([]any)
	if len(vs) != 1 {
		r.t.Fatalf("mẫu = %v, muốn 1 biến thể", p)
	}
	v, err := uuid.Parse(str(vs[0].(map[string]any)["id"])) //nolint:forcetypeassert // test
	if err != nil {
		r.t.Fatal(err)
	}
	d := r.mustCall(http.MethodPost, "/api/v1/inventory/documents",
		map[string]any{"warehouseId": r.s.shop.Warehouse, "kind": "import"}, http.StatusCreated)
	id := str(d["id"])
	d = r.mustCall(http.MethodPut, "/api/v1/inventory/documents/"+id+"/lines", map[string]any{
		"version": d["version"], "lines": []map[string]any{{"variantId": v, "quantity": qty, "unitCost": 120000}},
	}, http.StatusOK)
	d = r.mustCall(http.MethodPost, "/api/v1/inventory/documents/"+id+"/post",
		map[string]any{"version": d["version"]}, http.StatusOK)
	if d["status"] != "posted" {
		r.t.Fatalf("phiếu nhập = %v, muốn posted", d)
	}
	r.stockIs(v, stock{onHand: int32(qty), available: int32(qty)}) //nolint:gosec // G115: qty nhỏ do test đặt
	return v
}

// partnerWithStock tạo biến thể đang bán của người bán PARTNER1 (chủ bảng, không qua API: API chỉ làm việc với người bán
// mặc định) và nạp qty ở kho MAIN; trả biến thể.
func (r *orderRun) partnerWithStock(qty int32) uuid.UUID {
	r.t.Helper()
	var pv uuid.UUID
	r.inTenant(func(tx pgx.Tx) error {
		pv = r.s.shop.Variant(r.t, tx, r.s.shop.Partner, 250000, qty)
		return nil
	})
	r.stockIs(pv, stock{onHand: qty, available: qty})
	return pv
}

// partnerUntouched: tồn của biến thể PARTNER1 vẫn qty, sổ = số dư; đọc mẫu PARTNER1 qua API → 404; lên đơn với biến
// thể PARTNER1 → 422 mixed_sellers và tồn không đổi.
func (r *orderRun) partnerUntouched(pv uuid.UUID, qty int32) {
	r.t.Helper()
	r.stockIs(pv, stock{onHand: qty, available: qty})
	r.ledgerMatchesBalance(pv)
	path := "/api/v1/catalog/products/" + r.s.shop.Product(r.s.shop.Partner).String()
	if st, body, err := r.call(http.MethodGet, path, nil); err != nil || st != http.StatusNotFound || body["error"] != "not_found" {
		r.t.Errorf("đọc mẫu của PARTNER1 = %d %v (%v), muốn 404 not_found", st, body, err)
	}
	if st, body, err := r.call(http.MethodPost, "/api/v1/orders", orderReq(pv, 1, 0)); err != nil ||
		st != http.StatusUnprocessableEntity || body["error"] != "mixed_sellers" {
		r.t.Errorf("lên đơn với biến thể PARTNER1 = %d %v (%v), muốn 422 mixed_sellers", st, body, err)
	}
	r.stockIs(pv, stock{onHand: qty, available: qty})
}

func orderReq(v uuid.UUID, qty int, fee int64) map[string]any {
	return map[string]any{
		"customer":    map[string]any{"fullName": "Chị Lan"},
		"items":       []map[string]any{{"variantId": v, "qty": qty}},
		"shipAddress": map[string]any{"name": "Chị Lan", "phone": "+84901234567", "line": "12 Lê Lợi, Q1"},
		"shippingFee": fee,
	}
}

// advance gọi trang quản trị của hãng giả: đổi trạng thái kiện tracking sang to và gửi webhook (đòi ingest trả 200).
func (r *orderRun) advance(tracking, to string) {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.t.Context(), http.MethodPost,
		r.fc+"/admin/parcels/"+tracking+"/advance", strings.NewReader(`{"to":"`+to+`"}`))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	status, got := call(r.t, req)
	if status != http.StatusOK || !strings.Contains(got, `"webhookStatus":200`) {
		r.t.Fatalf("hãng giả advance %s = %d %s", to, status, got)
	}
}

// listedAvailable: cột available của mẫu duy nhất trong listProducts?warehouseId=MAIN.
func (r *orderRun) listedAvailable() int {
	r.t.Helper()
	page := r.mustCall(http.MethodGet, "/api/v1/catalog/products?warehouseId="+r.s.shop.Warehouse.String(), nil, http.StatusOK)
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		r.t.Fatalf("listProducts = %v, muốn 1 mẫu", page)
	}
	a, _ := items[0].(map[string]any)["available"].(float64) //nolint:forcetypeassert // test
	return int(a)
}

// stock là số dư một biến thể ở kho MAIN.
type stock struct{ onHand, held, inTransit, damaged, available int32 }

// stock đọc số dư (inventory.Availability, chủ bảng trong tenant).
func (r *orderRun) stock(v uuid.UUID) stock {
	r.t.Helper()
	var b []inventory.Balance
	r.inTenant(func(tx pgx.Tx) (err error) {
		b, err = inventory.Availability(r.t.Context(), tx, r.s.shop.Warehouse, []uuid.UUID{v})
		return err //nolint:wrapcheck // test
	})
	return stock{b[0].OnHand, b[0].Held, b[0].InTransit, b[0].Damaged, b[0].Available}
}

func (r *orderRun) stockIs(v uuid.UUID, want stock) {
	r.t.Helper()
	if got := r.stock(v); got != want {
		r.t.Fatalf("tồn = %+v, muốn %+v", got, want)
	}
}

// cod đọc số dư từng ngăn sổ COD của vận đơn qua cash.cod_balances_v.
func (r *orderRun) cod(shipmentID string) map[string]int64 {
	r.t.Helper()
	var out map[string]int64
	r.inTenant(func(tx pgx.Tx) (err error) {
		out, err = shiptest.CODBalances(r.t.Context(), tx, uuid.MustParse(shipmentID))
		return err //nolint:wrapcheck // test
	})
	return out
}

// ledgerMatchesBalance: tổng sổ kho từng ngăn của biến thể = số dư ngăn đó.
func (r *orderRun) ledgerMatchesBalance(v uuid.UUID) {
	r.t.Helper()
	b := r.stock(v)
	want := map[string]int32{"on_hand": b.onHand, "held": b.held, "in_transit": b.inTransit, "damaged": b.damaged}
	got := map[string]int32{"on_hand": 0, "held": 0, "in_transit": 0, "damaged": 0}
	r.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(r.t.Context(), `SELECT bucket, sum(delta)::int FROM inventory.stock_ledger
			WHERE variant_id = $1 AND warehouse_id = $2 GROUP BY bucket`, v, r.s.shop.Warehouse)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for rows.Next() {
			var k string
			var n int32
			if err := rows.Scan(&k, &n); err != nil {
				return err //nolint:wrapcheck // test
			}
			got[k] = n
		}
		return rows.Err()
	})
	for k, w := range want {
		if got[k] != w {
			r.t.Errorf("sổ kho ngăn %s = %d, số dư %d", k, got[k], w)
		}
	}
}

// count chạy câu đếm bằng chủ bảng trong ngữ cảnh tenant (bảng có RLS).
func (r *orderRun) count(q string, args ...any) int {
	r.t.Helper()
	var n int
	r.inTenant(func(tx pgx.Tx) error { return tx.QueryRow(r.t.Context(), q, args...).Scan(&n) })
	return n
}

// inTenant chạy fn bằng chủ bảng trong ngữ cảnh tenant của stack; lỗi làm test đỏ.
func (r *orderRun) inTenant(fn func(tx pgx.Tx) error) {
	r.t.Helper()
	if err := tenancy.WithTenant(r.t.Context(), r.s.owner, r.s.tenant, fn); err != nil {
		r.t.Fatal(err)
	}
}

// traceLinksWebhookToCash: có một dấu vết chứa span nhận webhook /hooks/fakecarrier (ingest), span lệnh
// fulfillment.refresh_status và span consumer cash (job event.cash). Span job kết thúc sau commit nên chờ tối đa 5 s.
func (r *orderRun) traceLinksWebhookToCash() {
	r.t.Helper()
	want := []string{"nhận webhook", "job fulfillment.refresh_status", "job event.cash"}
	var last []string
	deadline := time.Now().Add(stepBudget)
	for {
		spans := r.s.spansSnapshot()
		for _, sp := range spans {
			if sp.Name() != "job event.cash" {
				continue
			}
			id := sp.SpanContext().TraceID()
			if last = missingInTrace(spans, id, want); len(last) == 0 {
				r.t.Logf("dấu vết %s nối webhook → refresh_status → cash", id)
				return
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("không dấu vết nào có đủ %v (lần cuối thiếu %v)", want, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
