package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// In nhãn, phiếu nhặt, đóng gói qua HTTP (kế hoạch M3, Task M3-16; VC04, AT08).

const (
	labelPrinter = "in-nhan@elimaz.test"
	labelW2      = "in-nhan-kho2@elimaz.test"
)

// labelMemCarrier là memCarrier có in nhãn theo lô (PDF có các mã hãng).
type labelMemCarrier struct{ *memCarrier }

func (labelMemCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Create: true, LookupByRef: true, Labels: true, LabelsBatch: true}
}

func (labelMemCarrier) Labels(_ context.Context, _ fulfillment.Account, codes []string) (fulfillment.Label, error) {
	return fulfillment.Label{PDF: []byte("%PDF-1.4\n" + strings.Join(codes, "\n")), Pages: len(codes)}, nil
}

type labelAPIEnv struct {
	*shipAPIEnv
	store *files.MemStore
}

// newLabelAPIEnv: shipAPIEnv có máy chủ với Service in nhãn (kho tệp trong bộ nhớ), worker chạy lệnh tạo và lệnh tải
// nhãn; labelPrinter có fulfillment.print, fulfillment.pack toàn shop, labelW2 chỉ ở kho W2.
func newLabelAPIEnv(t *testing.T) *labelAPIEnv {
	t.Helper()
	fulfillment.RegisterFilePurposes()
	e := &labelAPIEnv{shipAPIEnv: newShipAPIEnv(t), store: files.NewMemStore()}
	tenant := e.shop.Tenant
	var w2 uuid.UUID
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, tenant, e.shop.Seller).Scan(&w2)
	})
	perms := []string{"fulfillment.print", "fulfillment.pack"}
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, labelPrinter, "active"), perms, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, labelW2, "active"), perms, "warehouse", w2.String())
	svc := labelService(t, e)
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = svc
	e.srv = NewServer(d)
	runner := commands.NewRunner(e.staff.App, append(e.cmds, fulfillment.NewLabelsCommand(svc)))
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{Commands: runner, WorkerID: "test/" + uuid.NewString()})
	return e
}

// labelService là Service có hãng in nhãn (labelMemCarrier trên hãng giả của e) và kho tệp của e.
func labelService(t *testing.T, e *labelAPIEnv) *fulfillment.Service {
	t.Helper()
	carriers, err := fulfillment.NewRegistry(labelMemCarrier{e.carrier})
	if err != nil {
		t.Fatal(err)
	}
	return fulfillment.New(fulfillment.Deps{
		Files: e.store, Carriers: carriers, DB: e.staff.App, Events: events.NewRegistry(), Queue: queue.NewQueue(),
		Bus: &testkit.RecordingBus{},
	}).Service
}

func (e *labelAPIEnv) as(user, method, path, key, body string) (int, string) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, method, path, user, key, body)
	return rec.Code, rec.Body.String()
}

// shippedOrder tạo đơn có lines và vận đơn đã có mã hãng; trả mã đơn, mã số đơn (DH…), mã vận đơn.
func (e *labelAPIEnv) shippedOrder(key string, lines ...string) (orderID, code, shipmentID string) {
	e.t.Helper()
	o := e.mustCreate(key, orderBody(30000, lines...))
	orderID, code = fmt.Sprint(o["id"]), fmt.Sprint(o["code"])
	st, s := e.post("/api/v1/orders/"+orderID+"/shipments", "s-"+key, fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0]))
	if st != http.StatusAccepted {
		e.t.Fatalf("tạo vận đơn = %d %v", st, s)
	}
	e.drain()
	return orderID, code, fmt.Sprint(s["id"])
}

func (e *labelAPIEnv) packing(orderID string) string {
	e.t.Helper()
	var p string
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT packing_status FROM ord.orders WHERE id = $1`, orderID).Scan(&p)
	})
	return p
}

// VC04 qua HTTP: in 2 vận đơn → 202 một lô 2 vận đơn (cùng khóa → cùng thân); kho W2 → 404 không ghi lô; không
// quyền in → 403; 201 mã → 400 (kiểm hợp đồng); worker → lô ready có url; kho W2 đọc lô → 404.
func TestLabelBatchAPI(t *testing.T) {
	e := newLabelAPIEnv(t)
	v := e.variant(e.shop.Seller, 250000, 10)
	_, _, s1 := e.shippedOrder("o1", line(v, 1))
	_, _, s2 := e.shippedOrder("o2", line(v, 1))
	body := fmt.Sprintf(`{"shipmentIds":["%s","%s"]}`, s1, s2)

	if code, b := e.as(labelW2, http.MethodPost, "/api/v1/label-batches", "w2", body); code != http.StatusNotFound {
		t.Fatalf("kho W2 in vận đơn kho MAIN = %d %s, muốn 404", code, b)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.label_batches"); n != 0 {
		t.Fatalf("có %d lô sau 404", n)
	}
	if code, _ := e.as(shipAll, http.MethodPost, "/api/v1/label-batches", "x", body); code != http.StatusForbidden {
		t.Fatalf("không quyền in = %d, muốn 403", code)
	}
	code1, b1 := e.as(labelPrinter, http.MethodPost, "/api/v1/label-batches", "k1", body)
	code2, b2 := e.as(labelPrinter, http.MethodPost, "/api/v1/label-batches", "k1", body)
	if code1 != http.StatusAccepted || code2 != http.StatusAccepted || b1 != b2 {
		t.Fatalf("in hai lần cùng khóa: %d %d\n%s\n%s", code1, code2, b1, b2)
	}
	if !strings.Contains(b1, `"count":2`) || !strings.Contains(b1, `"skipped":[]`) {
		t.Fatalf("thân = %s, muốn một lô 2 vận đơn", b1)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.label_batches"); n != 1 {
		t.Fatalf("lô = %d, muốn 1", n)
	}
	var batchID string
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id::text FROM fulfillment.label_batches`).Scan(&batchID)
	})
	many := make([]string, 201)
	for i := range many {
		many[i] = `"` + uuid.NewString() + `"`
	}
	if code, b := e.as(labelPrinter, http.MethodPost, "/api/v1/label-batches", "k201",
		`{"shipmentIds":[`+strings.Join(many, ",")+`]}`); code != http.StatusBadRequest {
		t.Fatalf("201 mã = %d %s, muốn 400", code, b)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.label_batch.create'"); n != 1 {
		t.Fatalf("audit = %d, muốn 1", n)
	}

	path := "/api/v1/label-batches/" + batchID
	if code, b := e.as(labelPrinter, http.MethodGet, path, "", ""); code != http.StatusOK || !strings.Contains(b, `"status":"pending"`) ||
		strings.Contains(b, `"url"`) {
		t.Fatalf("lô trước worker = %d %s", code, b)
	}
	e.drain()
	code, b := e.as(labelPrinter, http.MethodGet, path, "", "")
	if code != http.StatusOK || !strings.Contains(b, `"status":"ready"`) || !strings.Contains(b, `"url":"`) {
		t.Fatalf("lô sau worker = %d %s", code, b)
	}
	if code, _ := e.as(labelW2, http.MethodGet, path, "", ""); code != http.StatusNotFound {
		t.Fatalf("kho W2 đọc lô = %d, muốn 404", code)
	}
	if code, _ := e.as(labelPrinter, http.MethodGet, "/api/v1/label-batches/"+uuid.NewString(), "", ""); code != http.StatusNotFound {
		t.Fatalf("lô lạ = %d, muốn 404", code)
	}
}

// Phiếu nhặt qua HTTP: vB ở 2 đơn (2 + 1) → một dòng qty 3, orderCodes hai mã; JSON không có khóa phone, address;
// kho W2 → 404; mã lạ → 404.
func TestPickingListAPI(t *testing.T) {
	e := newLabelAPIEnv(t)
	vA := e.variant(e.shop.Seller, 100000, 10)
	vB := e.variant(e.shop.Seller, 250000, 10)
	_, c1, s1 := e.shippedOrder("o1", line(vA, 1), line(vB, 2))
	_, c2, s2 := e.shippedOrder("o2", line(vB, 1))
	path := "/api/v1/picking-list?shipmentIds=" + s1 + "&shipmentIds=" + s2
	code, b := e.as(labelPrinter, http.MethodGet, path, "", "")
	if code != http.StatusOK {
		t.Fatalf("phiếu nhặt = %d %s", code, b)
	}
	got := decodeJSON(t, serveAs(e.srv, e.keys, http.MethodGet, path, labelPrinter, "", ""))
	lines, _ := got["lines"].([]any)
	orders, _ := got["orders"].([]any)
	var qty3 map[string]any
	for _, l := range lines {
		if m := l.(map[string]any); m["qty"] == float64(3) { //nolint:forcetypeassert // test
			qty3 = m
		}
	}
	if len(lines) != 2 || qty3 == nil || len(orders) != 2 {
		t.Fatalf("phiếu nhặt = %s, muốn 2 dòng (một dòng qty 3), 2 đơn", b)
	}
	codes, _ := qty3["orderCodes"].([]any)
	want := []any{c1, c2}
	if c2 < c1 {
		want = []any{c2, c1}
	}
	if !slices.Equal(codes, want) {
		t.Fatalf("orderCodes = %v, muốn %s, %s", codes, c1, c2)
	}
	for _, k := range []string{`"phone"`, `"address"`, `"shipTo"`, `"receiver`} {
		if strings.Contains(b, k) {
			t.Fatalf("phiếu nhặt có khóa %s: %s", k, b)
		}
	}
	if code, _ := e.as(labelW2, http.MethodGet, path, "", ""); code != http.StatusNotFound {
		t.Fatalf("kho W2 = %d, muốn 404", code)
	}
	if code, _ := e.as(labelPrinter, http.MethodGet, path+"&shipmentIds="+uuid.NewString(), "", ""); code != http.StatusNotFound {
		t.Fatalf("mã lạ = %d, muốn 404", code)
	}
}

// Đóng gói qua HTTP: packed hai đơn → updated; rồi picking → already_further (chỉ đi tới); in lại sau khi packed →
// packing_status vẫn packed (AT08); kho W2 → 404, không đơn nào đổi; audit một dòng mỗi đơn đổi.
func TestPackingAPI(t *testing.T) {
	e := newLabelAPIEnv(t)
	v := e.variant(e.shop.Seller, 250000, 10)
	o1, _, s1 := e.shippedOrder("o1", line(v, 1))
	o2, _, _ := e.shippedOrder("o2", line(v, 1))
	body := func(to string) string { return fmt.Sprintf(`{"orderIds":["%s","%s"],"to":"%s"}`, o1, o2, to) }

	if code, b := e.as(labelW2, http.MethodPost, "/api/v1/orders/packing", "w2", body("packed")); code != http.StatusNotFound {
		t.Fatalf("kho W2 đóng gói = %d %s, muốn 404", code, b)
	}
	if p := e.packing(o1); p != "none" {
		t.Fatalf("sau 404: packing_status = %s, muốn none", p)
	}
	code, b := e.as(labelPrinter, http.MethodPost, "/api/v1/orders/packing", "p1", body("packed"))
	if code != http.StatusOK || strings.Count(b, `"result":"updated"`) != 2 {
		t.Fatalf("packed = %d %s, muốn 2 updated", code, b)
	}
	code, b = e.as(labelPrinter, http.MethodPost, "/api/v1/orders/packing", "p2", body("picking"))
	if code != http.StatusOK || strings.Count(b, `"result":"already_further"`) != 2 {
		t.Fatalf("picking sau packed = %d %s, muốn 2 already_further", code, b)
	}
	if code, b := e.as(labelPrinter, http.MethodPost, "/api/v1/label-batches", "l1",
		fmt.Sprintf(`{"shipmentIds":["%s"]}`, s1)); code != http.StatusAccepted {
		t.Fatalf("in lại = %d %s", code, b)
	}
	e.drain()
	if p1, p2 := e.packing(o1), e.packing(o2); p1 != "packed" || p2 != "packed" {
		t.Fatalf("sau in lại: %s %s, muốn packed packed", p1, p2)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'order.packing'"); n != 2 {
		t.Fatalf("audit đóng gói = %d, muốn 2", n)
	}
	if code, _ := e.as(labelPrinter, http.MethodPost, "/api/v1/orders/packing", "p3",
		fmt.Sprintf(`{"orderIds":["%s"],"to":"packed"}`, uuid.NewString())); code != http.StatusNotFound {
		t.Fatalf("đơn lạ = %d, muốn 404", code)
	}
}
