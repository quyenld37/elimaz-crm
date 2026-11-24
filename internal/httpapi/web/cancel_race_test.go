package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// cancelEnv là orderEnv có thêm khóa web.orders.read (đọc) và web.orders.cancel (hủy).
type cancelEnv struct {
	*orderEnv
	readKey, cancelKey string
}

func newCancelEnv(t *testing.T) *cancelEnv {
	t.Helper()
	o := newOrderEnv(t, nil)
	return &cancelEnv{orderEnv: o, readKey: o.serviceKey("web.orders.read"), cancelKey: o.serviceKey("web.orders.cancel")}
}

// placeOne tạo một đơn web new (1 × biến thể v) qua HTTP; trả mã ngoài và mã đơn.
func (c *cancelEnv) placeOne(v uuid.UUID, n int) (string, uuid.UUID) {
	c.t.Helper()
	w := c.order(phone(n), "", quoteLine(v, 1, 350000))
	rec := c.place(w.ref, w)
	return w.ref, created(c.t, rec.Code, rec.Body.Bytes()).OrderID
}

func (c *cancelEnv) cancel(key, ref, idem, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequestWithContext(c.t.Context(), http.MethodPost, ordersPath+"/"+ref+"/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.send(req, key, "Idempotency-Key", idem)
}

func (c *cancelEnv) orderStatus(id uuid.UUID) string {
	c.t.Helper()
	var s string
	c.staff.Exec(c.t, c.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(c.t.Context(), "SELECT status FROM ord.orders WHERE id = $1", id).Scan(&s)
	})
	return s
}

type webView struct {
	ExternalRef   string `json:"externalRef"`
	Status        string `json:"status"`
	DisplayStatus string `json:"displayStatus"`
	Cancellable   bool   `json:"cancellable"`
	GrandTotal    int64  `json:"grandTotal"`
	Lines         []struct {
		Qty int `json:"qty"`
	} `json:"lines"`
}

// [WB03] qua HTTP: khóa web.orders.read đọc được đơn web vừa đặt (đủ trường, nhãn new); JSON không có SĐT, địa chỉ,
// tên người nhận, ghi chú của khách; khóa thiếu quyền → 403; mã không có → 404.
func TestWebGetOrder_HTTP(t *testing.T) {
	t.Parallel()
	c := newCancelEnv(t)
	v := c.product("PV", "[]").Variants[0].ID
	c.stock(v, 5)
	ref, _ := c.placeOne(v, 11)
	rec := c.get(c.readKey, ordersPath+"/"+ref)
	var got webView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.ExternalRef != ref ||
		got.Status != "new" || got.DisplayStatus != "new" || !got.Cancellable || len(got.Lines) != 1 ||
		got.Lines[0].Qty != 1 || got.GrandTotal != 380000 {
		t.Fatalf("tra đơn: %d %s", rec.Code, rec.Body.String())
	}
	for _, pii := range []string{"910000011", "Lê Lợi", "Chị Mai", "Giao giờ hành chính"} {
		if strings.Contains(rec.Body.String(), pii) {
			t.Fatalf("tra đơn lộ %q: %s", pii, rec.Body.String())
		}
	}
	if rec := c.get(c.key, ordersPath+"/"+ref); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa web.orders.create đọc đơn: %d %s", rec.Code, rec.Body.String())
	}
	if rec := c.get(c.readKey, ordersPath+"/KHONG-CO"); rec.Code != http.StatusNotFound {
		t.Fatalf("mã không có: %d %s", rec.Code, rec.Body.String())
	}
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	rec = c.get(c.readKey, ordersPath+"/changes?since="+since+"&limit=10")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"externalRef":"`+ref+`"`) ||
		!strings.Contains(rec.Body.String(), `"hasMore":false`) {
		t.Fatalf("luồng thay đổi: %d %s", rec.Code, rec.Body.String())
	}
	if rec := c.get(c.readKey, ordersPath+"/changes?since="+since+"&limit=0"); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit 0: %d %s", rec.Code, rec.Body.String())
	}
}

// Hủy đơn new qua HTTP → 200 {cancelled: true}, đơn cancelled, giữ hàng nhả (bán được lại 5); gửi lại cùng khóa → cùng
// thân, Idempotent-Replayed; khóa mới → 422 cannot_cancel (không lưu); đúng 1 audit order.cancel của khóa dịch vụ, không
// chép lời nhắn. Khóa không có web.orders.cancel → 403.
func TestWebCancelOrder_HTTP(t *testing.T) {
	t.Parallel()
	c := newCancelEnv(t)
	v := c.product("PC", "[]").Variants[0].ID
	c.stock(v, 5)
	ref, id := c.placeOne(v, 12)
	if rec := c.cancel(c.readKey, ref, "k-0", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa đọc hủy đơn: %d %s", rec.Code, rec.Body.String())
	}
	a := c.cancel(c.cancelKey, ref, "k-1", `{"note":"Đặt nhầm màu"}`)
	if a.Code != http.StatusOK || strings.TrimSpace(a.Body.String()) != `{"cancelled":true}` || c.orderStatus(id) != "cancelled" {
		t.Fatalf("hủy: %d %s, đơn %s", a.Code, a.Body.String(), c.orderStatus(id))
	}
	if n := c.staff.CountIn(t, c.tenant, `SELECT coalesce(sum(r.quantity), 0)::int FROM inventory.stock_reservations r
		JOIN ord.order_items oi ON oi.id = r.order_item_id WHERE oi.order_id = $1 AND r.state = 'held'`, id); n != 0 {
		t.Fatalf("còn giữ hàng held %d", n)
	}
	b := c.cancel(c.cancelKey, ref, "k-1", `{"note":"Đặt nhầm màu"}`)
	if b.Code != http.StatusOK || b.Body.String() != a.Body.String() || b.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại cùng khóa: %d %s %v", b.Code, b.Body.String(), b.Header())
	}
	refused(t, c.cancel(c.cancelKey, ref, "k-2", `{}`), http.StatusUnprocessableEntity, "cannot_cancel")
	if n := c.storedKeys("k-2"); n != 0 {
		t.Fatalf("từ chối được lưu theo khóa (%d)", n)
	}
	if n := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND action = 'order.cancel' AND actor_type = 'service' AND after->>'cancelReason' = 'customer_changed_mind'
		AND after::text NOT LIKE '%nhầm màu%'`, id.String()); n != 1 {
		t.Fatalf("mong 1 audit order.cancel không chép lời nhắn, có %d", n)
	}
}

// shipVia là Gửi hãng của nhân viên: fulfillment.Service.Ship (không adapter: lệnh tạo vận đơn ghi vào jobs) bằng tài
// khoản hãng giả account cho đơn id, trong một transaction riêng (crm_app).
func (c *cancelEnv) shipVia(svc *fulfillment.Service, account, id uuid.UUID) error {
	return tenancy.WithTenant(c.t.Context(), c.staff.App, c.tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		_, err := svc.Ship(c.t.Context(), tx, fulfillment.ShipInput{OrderID: id, CarrierAccountID: account, WeightGrams: 500},
			order.Actor{TenantID: c.tenant, UserID: c.owner.UserID, Type: "staff"})
		return err //nolint:wrapcheck // test
	}, db.TxAPI)
}

// shipper dựng tài khoản hãng giả của người bán và fulfillment.Service (lệnh tạo vận đơn ghi vào jobs).
func (c *cancelEnv) shipper() (*fulfillment.Service, uuid.UUID) {
	c.t.Helper()
	var account uuid.UUID
	c.staff.Exec(c.t, c.tenant, func(tx pgx.Tx) error {
		var err error
		account, err = fulfillment.EnsureCarrierAccount(c.t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: c.seller, WarehouseID: c.warehouse(), CarrierKey: "fake", Name: "Hãng giả", ExternalAccountRef: "fake-web",
		})
		return err //nolint:wrapcheck // test
	})
	return fulfillment.New(fulfillment.Deps{Queue: queue.NewQueue(), Events: events.NewRegistry()}).Service, account
}

// confirm xác nhận đơn id (nhân viên Chủ) để Gửi hãng được.
func (c *cancelEnv) confirm(id uuid.UUID) {
	c.t.Helper()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	a := order.Actor{TenantID: c.tenant, UserID: c.owner.UserID, Type: "staff"}
	c.app(func(tx pgx.Tx) error {
		o, err := order.Get(c.t.Context(), tx, c.seller, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = w.Confirm(c.t.Context(), tx, c.seller, id, o.Version, a, time.Now().UTC())
		return err //nolint:wrapcheck // test
	})
}

// [WB12] Lặp 30 lần: đơn web confirmed chưa vận đơn; cùng lúc khách hủy (HTTP webCancelOrder) và nhân viên Gửi hãng
// (Ship) → mỗi lần đúng một trong hai: đơn cancelled, Ship bị từ chối, không vận đơn chưa hủy; hoặc có đúng một vận đơn,
// đơn vẫn confirmed, khách nhận {cancelled: false, requestRef} và có một yêu cầu hủy mở. Không lần nào vừa hủy vừa có
// vận đơn; không deadlock (mọi yêu cầu trả lời). Thêm một lần theo thứ tự chắc chắn (hủy rồi mới Gửi hãng) để nhánh
// hủy thắng luôn được kiểm.
func TestWebCancelOrder_RaceWithShip(t *testing.T) {
	t.Parallel()
	c := newCancelEnv(t)
	v := c.product("PR", "[]").Variants[0].ID
	c.stock(v, 40)
	svc, account := c.shipper()
	wins := map[string]int{}
	for i := range 30 {
		ref, id := c.placeOne(v, 100+i)
		c.confirm(id)
		var rec *httptest.ResponseRecorder
		var shipErr error
		var wg sync.WaitGroup
		wg.Go(func() { rec = c.cancel(c.cancelKey, ref, uuid.NewString(), `{}`) })
		wg.Go(func() { shipErr = c.shipVia(svc, account, id) })
		wg.Wait()
		var out struct {
			Cancelled  bool   `json:"cancelled"`
			RequestRef string `json:"requestRef"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("lần %d: hủy %d %s", i, rec.Code, rec.Body.String())
		}
		live := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1
			AND status <> 'cancelled'`, id)
		openReq := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM ord.order_requests WHERE order_id = $1
			AND kind = 'cancel' AND status = 'received'`, id)
		st := c.orderStatus(id)
		switch {
		case out.Cancelled && shipErr != nil && live == 0 && st == "cancelled" && openReq == 0:
			wins["hủy"]++
		case !out.Cancelled && out.RequestRef != "" && shipErr == nil && live == 1 && st == "confirmed" && openReq == 1:
			wins["gửi hãng"]++
		default:
			t.Fatalf("lần %d: kết quả lẫn: hủy %+v, Ship %v, vận đơn chưa hủy %d, đơn %s, yêu cầu mở %d",
				i, out, shipErr, live, st, openReq)
		}
	}
	t.Logf("30 lần tranh: %v", wins)
	// Thứ tự chắc chắn của nhánh hủy thắng: hủy xong rồi mới Gửi hãng → Ship từ chối, không vận đơn.
	ref, id := c.placeOne(v, 200)
	c.confirm(id)
	if rec := c.cancel(c.cancelKey, ref, uuid.NewString(), `{}`); rec.Code != http.StatusOK ||
		strings.TrimSpace(rec.Body.String()) != `{"cancelled":true}` {
		t.Fatalf("hủy trước: %d %s", rec.Code, rec.Body.String())
	}
	if err := c.shipVia(svc, account, id); err == nil || c.staff.CountIn(t, c.tenant,
		`SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1`, id) != 0 {
		t.Fatalf("Gửi hãng đơn đã hủy: %v", err)
	}
}
