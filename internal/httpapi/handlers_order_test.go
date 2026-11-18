package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	orderClerk  = "len-don@elimaz.test"
	orderViewer = "xem-don@elimaz.test"
)

type orderAPIEnv struct {
	t     *testing.T
	staff *testkit.Staff
	keys  *testkit.AccessKeys
	srv   http.Handler
	shop  *invtest.Shop
}

func newOrderAPIEnv(t *testing.T) *orderAPIEnv {
	t.Helper()
	e := &orderAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: testkit.AccessKeyServer(t)}
	tenant := e.staff.Tenant(t, "A")
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, tenant)
		return nil
	})
	c := e.staff.User(t, tenant, orderClerk, "active")
	e.staff.Grant(t, tenant, c, []string{"order.create", "order.read"}, "all", "")
	v := e.staff.User(t, tenant, orderViewer, "active")
	e.staff.Grant(t, tenant, v, []string{"order.read"}, "all", "")
	e.srv = testServer(t, e.staff, e.keys, 0)
	return e
}

func (e *orderAPIEnv) variant(seller uuid.UUID, price int64, onHand int32) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		id = e.shop.Variant(e.t, tx, seller, price, onHand)
		return nil
	})
	return id
}

func orderBody(fee int64, items ...string) string {
	return `{"customer":{"fullName":"Chị Lan"},"items":[` + strings.Join(items, ",") +
		`],"shipAddress":{"name":"Chị Lan","phone":"+84901234567","line":"12 Lê Lợi, Q1"},"shippingFee":` +
		fmt.Sprint(fee) + `}`
}

func line(v uuid.UUID, q int) string { return fmt.Sprintf(`{"variantId":"%s","qty":%d}`, v, q) }

func (e *orderAPIEnv) post(key, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", orderClerk, key, body)
}

func (e *orderAPIEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.shop.Tenant, q, args...)
}

func TestOrderAPI_CreateGet(t *testing.T) {
	e := newOrderAPIEnv(t)
	v := e.variant(e.shop.Seller, 250000, 5)
	rec := e.post("o1", orderBody(30000, line(v, 2)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("createOrder: %d %s", rec.Code, rec.Body.String())
	}
	o := decodeJSON(t, rec)
	items, _ := o["items"].([]any)
	if o["status"] != "confirmed" || o["grandTotal"] != float64(530000) || o["codAmount"] != float64(530000) ||
		o["subtotal"] != float64(500000) || len(items) != 1 || !regexp.MustCompile(`^DH[0-9]{7,}$`).MatchString(fmt.Sprint(o["code"])) {
		t.Fatalf("đơn trả về sai: %v", o)
	}
	if _, ok := o["sellerId"]; ok {
		t.Errorf("API không hiện người bán: %v", o)
	}
	id := fmt.Sprint(o["id"])
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.create' AND entity_id = $1
		AND after->>'code' = $2 AND after::text NOT LIKE '%0901234567%' AND after::text NOT LIKE '%Lê Lợi%'`,
		id, o["code"]); n != 1 {
		t.Errorf("audit order.create (không dữ liệu cá nhân): %d dòng", n)
	}
	got := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/orders/"+id, orderViewer, "", "")
	if got.Code != http.StatusOK || decodeJSON(t, got)["code"] != o["code"] {
		t.Errorf("getOrder: %d %s", got.Code, got.Body.String())
	}
	expect(t, "đơn không có", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/orders/"+uuid.NewString(), orderViewer, "", ""),
		http.StatusNotFound, "not_found")
	expect(t, "chỉ xem không lên đơn", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", orderViewer, "o2",
		orderBody(0, line(v, 1))), http.StatusForbidden, "forbidden")
	expect(t, "SĐT sai dạng", e.post("o3", strings.Replace(orderBody(0, line(v, 1)), "+84901234567", "09-01", 1)),
		http.StatusBadRequest, "bad_request")
}

// TestOrderAPI_LastUnitRace: hai đơn khác khóa tranh chiếc cuối → một 201, một 409 out_of_stock; một đơn; held 1.
func TestOrderAPI_LastUnitRace(t *testing.T) {
	e := newOrderAPIEnv(t)
	v := e.variant(e.shop.Seller, 100000, 1)
	recs := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.post(fmt.Sprintf("race-%d", i), orderBody(0, line(v, 1))) })
	}
	wg.Wait()
	created, refused := 0, 0
	for _, r := range recs {
		switch r.Code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			var b map[string]any
			if err := json.Unmarshal(r.Body.Bytes(), &b); err != nil || b["error"] != "out_of_stock" ||
				b["variantId"] != v.String() || b["available"] != float64(0) || b["request_id"] == "" {
				t.Errorf("thân 409: %s", r.Body.String())
			}
			refused++
		default:
			t.Errorf("mã lạ %d %s", r.Code, r.Body.String())
		}
	}
	if created != 1 || refused != 1 {
		t.Fatalf("201: %d, 409: %d; mong 1 và 1", created, refused)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("%d đơn, mong 1", n)
	}
	if n := e.count(`SELECT held FROM inventory.stock_available_v WHERE variant_id = $1`, v); n != 1 {
		t.Errorf("held = %d, mong 1", n)
	}
}

// TestOrderAPI_SameKey50: 50 yêu cầu song song cùng Idempotency-Key → một đơn; mọi câu trả lời 201 cùng thân hoặc 409
// request_in_progress.
func TestOrderAPI_SameKey50(t *testing.T) {
	e := newOrderAPIEnv(t)
	v := e.variant(e.shop.Seller, 1000, 100)
	body := orderBody(0, line(v, 1))
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.post("k-50", body) })
	}
	wg.Wait()
	var first string
	for _, r := range recs {
		switch r.Code {
		case http.StatusCreated:
			if first == "" {
				first = r.Body.String()
			}
			if r.Body.String() != first {
				t.Errorf("thân khác nhau:\n%s\n%s", first, r.Body.String())
			}
		case http.StatusConflict:
			if decodeErr(t, r).Error != "request_in_progress" {
				t.Errorf("409 lạ: %s", r.Body.String())
			}
		default:
			t.Errorf("mã lạ %d %s", r.Code, r.Body.String())
		}
	}
	if first == "" {
		t.Fatal("không có câu trả lời 201")
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("%d đơn, mong 1", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1 {
		t.Errorf("%d khách, mong 1", n)
	}
}

func TestOrderAPI_Refusals(t *testing.T) {
	e := newOrderAPIEnv(t)
	own := e.variant(e.shop.Seller, 1000, 5)
	p := e.variant(e.shop.Partner, 1000, 5)
	big := e.variant(e.shop.Seller, catalog.MaxMoney, 5)
	gone := e.variant(e.shop.Seller, 1000, 5)
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET status = 'discontinued' WHERE id = $1`, gone)
		return err //nolint:wrapcheck // test
	})
	expect(t, "KH14 biến thể người bán khác", e.post("r1", orderBody(0, line(own, 1), line(p, 1))),
		http.StatusUnprocessableEntity, "mixed_sellers")
	expect(t, "KH12 ngừng bán", e.post("r2", orderBody(0, line(gone, 1))), http.StatusUnprocessableEntity, "variant_not_for_sale")
	expect(t, "tràn tổng", e.post("r3", orderBody(0, line(big, 2))), http.StatusUnprocessableEntity, "total_overflow")
	// Từ chối không lưu theo khóa: gửi lại cùng khóa sau khi sửa thân vẫn được xử lý như yêu cầu mới.
	if rec := e.post("r1", orderBody(0, line(own, 1))); rec.Code != http.StatusCreated {
		t.Errorf("gửi lại khóa đã bị từ chối: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("%d đơn, mong 1", n)
	}
}

// TestOrderAPI_Contract: đúng một operation cho POST /api/v1/orders, tên createOrder (Q-X2: M2 không tạo lại).
func TestOrderAPI_Contract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "openapi", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Count(s, "\n  /api/v1/orders:\n") != 1 || strings.Count(s, "operationId: createOrder\n") != 1 {
		t.Fatal("openapi.yaml phải có đúng một path /api/v1/orders và một operationId createOrder")
	}
	_, path, _ := strings.Cut(s, "\n  /api/v1/orders:\n")
	if !strings.HasPrefix(path, "    post:\n      operationId: createOrder\n") {
		t.Error("POST /api/v1/orders phải là createOrder")
	}
}
