package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Gửi hãng qua HTTP (kế hoạch M3, Task M3-12): VC03, bấm hai lần, gửi song song, "Gửi lại", xử lý "không rõ".

// memCarrier là hãng giả trong bộ nhớ khóa "fake" cho test HTTP: ok tạo kiện theo mã CRM (cùng mã trả kiện cũ);
// timeout_no_create quá giờ mà chưa tạo; fail_before_create chắc chưa gửi; inconclusive: tra theo mã không chắc.
type memCarrier struct {
	fulfillment.Unsupported

	mu           sync.Mutex
	mode         string
	inconclusive bool
	creates      int
	parcels      map[string]string
}

func newMemCarrier() *memCarrier { return &memCarrier{mode: "ok", parcels: map[string]string{}} }

func (*memCarrier) Key() string { return "fake" }

func (*memCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Create: true, LookupByRef: true}
}

func (c *memCarrier) set(mode string, inconclusive bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode, c.inconclusive = mode, inconclusive
}

func (c *memCarrier) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.creates
}

var errMemTimeout = errors.New("memCarrier: quá giờ")

func (c *memCarrier) CreateShipment(_ context.Context, _ fulfillment.Account, r fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creates++
	switch c.mode {
	case "timeout_no_create":
		return fulfillment.CreateResult{}, errMemTimeout
	case "fail_before_create":
		return fulfillment.CreateResult{}, fulfillment.ErrNotSent
	}
	tr, ok := c.parcels[r.Code]
	if !ok {
		tr = fmt.Sprintf("MC%04d", len(c.parcels)+1)
		c.parcels[r.Code] = tr
	}
	return fulfillment.CreateResult{TrackingCode: tr, Fee: 22000, FeeKnown: true}, nil
}

func (c *memCarrier) LookupByCode(_ context.Context, _ fulfillment.Account, code string) (fulfillment.LookupResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inconclusive {
		return fulfillment.LookupResult{Inconclusive: true}, nil
	}
	tr, ok := c.parcels[code]
	return fulfillment.LookupResult{Found: ok, TrackingCode: tr}, nil
}

// shipServer dựng máy chủ thử có fulfillment.Service (sổ sự kiện riêng) cho e.
func shipServer(t *testing.T, e *orderAPIEnv) http.Handler {
	t.Helper()
	return NewServer(shipDeps(t, e))
}

// shipDeps là Deps của máy chủ thử cho e có fulfillment.Service (sổ sự kiện riêng).
func shipDeps(t *testing.T, e *orderAPIEnv) Deps {
	t.Helper()
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(),
	}).Service
	return d
}

type shipAPIEnv struct {
	*orderAPIEnv
	carrier  *memCarrier
	cmds     []commands.Handler
	worker   *queue.Worker
	accounts [2]uuid.UUID
}

func newShipAPIEnv(t *testing.T) *shipAPIEnv {
	t.Helper()
	e := &shipAPIEnv{orderAPIEnv: newOrderAPIEnv(t), carrier: newMemCarrier()}
	tenant := e.shop.Tenant
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		for i, ref := range []string{"fake-1", "fake-9"} {
			id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
				SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: "fake", Name: "Hãng giả " + ref,
				ExternalAccountRef: ref,
			})
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			e.accounts[i] = id
		}
		return nil
	})
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipAll, "active"), []string{"fulfillment.ship", "fulfillment.read"}, "all", "")
	e.srv = shipServer(t, e.orderAPIEnv)
	e.cmds = []commands.Handler{fulfillment.NewCreateCommand(fulfillment.CommandOptions{
		DB: e.staff.App, Adapters: []fulfillment.CarrierAdapter{e.carrier}, Events: events.NewRegistry(),
		Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{},
	})}
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "test/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, e.cmds),
	})
	return e
}

func (e *shipAPIEnv) drain() {
	e.t.Helper()
	e.staff.DrainJobs(e.t, e.worker)
}

// confirmedOrder tạo đơn 1 món (đơn tay của M2 là confirmed) và trả mã.
func (e *shipAPIEnv) confirmedOrder(key string) string {
	e.t.Helper()
	v := e.variant(e.shop.Seller, 250000, 5)
	return fmt.Sprint(e.mustCreate(key, orderBody(30000, line(v, 1)))["id"])
}

func (e *shipAPIEnv) post(path, key, body string) (int, map[string]any) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, key, body)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeJSON(e.t, rec)
}

func (e *shipAPIEnv) get(path string) (int, map[string]any) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodGet, path, shipAll, "", "")
	return rec.Code, decodeJSON(e.t, rec)
}

// VC03: đơn confirmed, tài khoản active → 202 creating (như M1); worker → awaiting_pickup, có mã hãng, nhãn "Chờ thu
// gom", gọi hãng 1 lần. Cùng Idempotency-Key → cùng thân, một vận đơn; khóa khác → 409 shipment_exists. Danh sách vận
// đơn của đơn có 1 dòng; đơn lạ → 404.
func TestCreateShipmentAPI_VC03(t *testing.T) {
	t.Parallel()
	e := newShipAPIEnv(t)
	orderID := e.confirmedOrder("o1")
	path := "/api/v1/orders/" + orderID + "/shipments"
	body := fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0])
	rec1 := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "k1", body)
	rec2 := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "k1", body)
	if rec1.Code != http.StatusAccepted || rec2.Code != http.StatusAccepted || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("bấm hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	s := decodeJSON(t, rec1)
	if s["status"] != "creating" || s["createState"] != "pending" || s["label"] != "Đang gửi hãng" || s["accountName"] != "Hãng giả fake-1" {
		t.Fatalf("vận đơn mới = %v", s)
	}
	if code, b := e.post(path, "k2", body); code != http.StatusConflict || b["error"] != "shipment_exists" {
		t.Fatalf("khóa khác = %d %v, muốn 409 shipment_exists", code, b)
	}
	e.drain()
	code, got := e.get(fmt.Sprintf("/api/v1/shipments/%v", s["id"]))
	if code != http.StatusOK || got["status"] != "awaiting_pickup" || got["trackingCode"] == nil || got["trackingCode"] == "" ||
		got["label"] != "Chờ thu gom" || got["createState"] != "created" || got["fee"] != float64(22000) {
		t.Fatalf("sau worker = %d %v", code, got)
	}
	if _, has := got["shipTo"]; has {
		t.Fatal("ShipmentView không được có địa chỉ giao")
	}
	if e.carrier.calls() != 1 {
		t.Fatalf("gọi hãng %d lần, muốn 1", e.carrier.calls())
	}
	// getShipment trả mốc và mã lỗi đồng bộ cuối (ShipmentView của M3).
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET last_synced_at = now(), last_sync_error = 'carrier_timeout'
			WHERE id = $1`, s["id"])
		return err //nolint:wrapcheck // test
	})
	if code, got := e.get(fmt.Sprintf("/api/v1/shipments/%v", s["id"])); code != http.StatusOK ||
		got["lastSyncError"] != "carrier_timeout" || got["lastSyncedAt"] == nil {
		t.Fatalf("mốc đồng bộ = %d %v", code, got)
	}
	code, page := e.get(path)
	if items, _ := page["items"].([]any); code != http.StatusOK || len(items) != 1 {
		t.Fatalf("listOrderShipments = %d %v", code, page)
	}
	if code, _ := e.get("/api/v1/orders/" + uuid.NewString() + "/shipments"); code != http.StatusNotFound {
		t.Fatalf("đơn lạ = %d, muốn 404", code)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments"); n != 1 {
		t.Fatalf("vận đơn = %d, muốn 1", n)
	}
}

// Hai nhân viên gửi song song: 50 yêu cầu, 2 tài khoản hãng, khóa khác nhau → đúng một vận đơn chưa hủy (một 202), các
// câu trả lời còn lại 409 shipment_exists.
func TestCreateShipmentAPI_Parallel(t *testing.T) {
	t.Parallel()
	e := newShipAPIEnv(t)
	orderID := e.confirmedOrder("o1")
	path := "/api/v1/orders/" + orderID + "/shipments"
	codes := make([]int, 50)
	errs := make([]string, 50)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			body := fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[i%2])
			rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, fmt.Sprintf("p%d", i), body)
			codes[i] = rec.Code
			if rec.Code != http.StatusAccepted {
				errs[i] = rec.Body.String()
			}
		})
	}
	wg.Wait()
	ok := 0
	for i, c := range codes {
		switch c {
		case http.StatusAccepted:
			ok++
		case http.StatusConflict:
		default:
			t.Fatalf("yêu cầu %d = %d %s", i, c, errs[i])
		}
	}
	if ok != 1 {
		t.Fatalf("%d yêu cầu 202, muốn 1", ok)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE status <> 'cancelled'"); n != 1 {
		t.Fatalf("vận đơn chưa hủy = %d, muốn 1", n)
	}
}

// "Không rõ" qua HTTP: quá giờ mà chưa tạo, hãng không tra được → vẫn in_doubt; resend → 409 create_in_progress;
// resolve-create created FK7777 → 200 awaiting_pickup mã FK7777, audit có kết quả và lý do. Đơn khác: chắc chưa gửi →
// resend 202, gọi hãng lần hai, awaiting_pickup; resolve-create khi không unknown → 409. Tài khoản auth_failed → 409
// carrier_account_unavailable; người có quyền ở kho khác → resend 403.
func TestCreateShipmentAPI_ResendResolve(t *testing.T) {
	t.Parallel()
	e := newShipAPIEnv(t)
	tenant := e.shop.Tenant
	w2 := uuid.New()
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipW2, "active"), []string{"fulfillment.ship", "fulfillment.read"}, "warehouse", w2.String())

	o1 := e.confirmedOrder("o1")
	e.carrier.set("timeout_no_create", true)
	code, s := e.post("/api/v1/orders/"+o1+"/shipments", "c1", fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0]))
	if code != http.StatusAccepted {
		t.Fatalf("createShipment = %d %v", code, s)
	}
	e.drain()
	if _, _, still, err := commands.ResolveInDoubt(t.Context(), e.staff.App, e.cmds, 10); err != nil || still != 1 {
		t.Fatalf("ResolveInDoubt còn %d, %v", still, err)
	}
	id := fmt.Sprint(s["id"])
	if code, b := e.post("/api/v1/shipments/"+id+"/resend", "r1", ""); code != http.StatusConflict || b["error"] != "create_in_progress" {
		t.Fatalf("resend khi không rõ = %d %v", code, b)
	}
	code, got := e.post("/api/v1/shipments/"+id+"/resolve-create", "x1",
		`{"result":"created","trackingCode":"FK7777","reason":"thấy trên trang hãng"}`)
	if code != http.StatusOK || got["status"] != "awaiting_pickup" || got["trackingCode"] != "FK7777" || got["needsReview"] != false {
		t.Fatalf("resolve-create = %d %v", code, got)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.resolve_create'
		AND after->>'reason' = 'thấy trên trang hãng' AND after->>'result' = 'created'`); n != 1 {
		t.Fatalf("audit resolve_create = %d, muốn 1", n)
	}
	if code, b := e.post("/api/v1/shipments/"+id+"/resolve-create", "x2", `{"result":"not_created","reason":"x"}`); code != http.StatusConflict {
		t.Fatalf("resolve-create lần hai = %d %v", code, b)
	}

	o2 := e.confirmedOrder("o2")
	e.carrier.set("fail_before_create", false)
	_, s2 := e.post("/api/v1/orders/"+o2+"/shipments", "c2", fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[1]))
	e.drain()
	id2 := fmt.Sprint(s2["id"])
	if _, v := e.get("/api/v1/shipments/" + id2); v["createState"] != "not_sent" || v["status"] != "creating" {
		t.Fatalf("chắc chưa gửi = %v", v)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+id2+"/resend", shipW2, "r2", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("resend ở kho khác = %d, muốn 403", rec.Code)
	}
	e.carrier.set("ok", false)
	before := e.carrier.calls()
	if code, b := e.post("/api/v1/shipments/"+id2+"/resend", "r3", ""); code != http.StatusAccepted || b["createState"] != "pending" {
		t.Fatalf("resend = %d %v", code, b)
	}
	e.drain()
	if _, v := e.get("/api/v1/shipments/" + id2); v["status"] != "awaiting_pickup" || e.carrier.calls() != before+1 {
		t.Fatalf("sau resend = %v, gọi hãng %d → %d", v, before, e.carrier.calls())
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.resend'"); n != 1 {
		t.Fatalf("audit resend = %d, muốn 1", n)
	}

	o3 := e.confirmedOrder("o3")
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.carrier_accounts SET status = 'auth_failed' WHERE id = $1", e.accounts[0])
		return err //nolint:wrapcheck // test
	})
	if code, b := e.post("/api/v1/orders/"+o3+"/shipments", "c3", fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0])); code != http.StatusConflict || b["error"] != "carrier_account_unavailable" {
		t.Fatalf("tài khoản auth_failed = %d %v", code, b)
	}
}

// Đơn chưa confirmed (đã hủy) → 409 order_not_shippable (Task 95 ghi chú: M1 trả 404).
func TestCreateShipmentAPI_NotShippable(t *testing.T) {
	t.Parallel()
	e := newShipAPIEnv(t)
	o := e.confirmedOrder("o1")
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE ord.orders SET status = 'cancelled', cancelled_at = now() WHERE id = $1", o)
		return err //nolint:wrapcheck // test
	})
	if code, b := e.post("/api/v1/orders/"+o+"/shipments", "c1", fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0])); code != http.StatusConflict || b["error"] != "order_not_shippable" {
		t.Fatalf("đơn đã hủy = %d %v", code, b)
	}
}
