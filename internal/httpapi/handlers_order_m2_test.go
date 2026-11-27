package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// m2Prepare chèn danh mục hành chính mẫu (testkit.SeedAdminUnits) và cài đặt, nguồn đơn mặc định cho tenant của e.
func (e *orderAPIEnv) m2Prepare() {
	e.t.Helper()
	e.staff.SeedAdminUnits(e.t)
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		return order.EnsureDefaults(e.t.Context(), tx, e.shop.Tenant)
	})
}

func m2Line(v uuid.UUID, q int, discount int64) string {
	return fmt.Sprintf(`{"variantId":"%s","qty":%d,"lineDiscount":%d}`, v, q, discount)
}

const fullAddr = `{"name":"Chị Lan","phone":"0901234567","line":"12 Lê Lợi","provinceCode":"T01","wardCode":"T01001"}`

// m2Body: thân M2 với khách mới "Chị Lan", ý định intent, địa chỉ addr ("" → không gửi), phí fee, các dòng.
func m2Body(intent, addr string, fee int64, items ...string) string {
	b := `{"customer":{"fullName":"Chị Lan"},"intent":"` + intent + `","items":[` + strings.Join(items, ",") + `]`
	if addr != "" {
		b += `,"shipAddress":` + addr
	}
	return b + `,"shippingFee":` + fmt.Sprint(fee) + `}`
}

// DH01 qua API: 201, confirmed, nhãn confirmed, tổng đúng, giữ hàng; audit không có dữ liệu cá nhân; getOrder trả cùng
// trường M2.
func TestOrderAPI_CreateM2Confirmed(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	v1, v2 := e.variant(e.shop.Seller, 250000, 5), e.variant(e.shop.Seller, 180000, 1)
	o := e.mustCreate("m2-1", m2Body("confirmed", fullAddr, 30000, m2Line(v1, 2, 20000), m2Line(v2, 1, 0)))
	for k, want := range map[string]any{
		"status": "confirmed", "label": "confirmed", "subtotal": float64(680000), "discountTotal": float64(20000),
		"lineDiscountTotal": float64(20000), "grandTotal": float64(690000), "codAmount": float64(690000),
		"version": float64(2), "addressComplete": true, "channel": "manual",
	} {
		if o[k] != want {
			t.Errorf("%s = %v, mong %v", k, o[k], want)
		}
	}
	if w, ok := o["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("warnings = %v, mong mảng rỗng", o["warnings"])
	}
	if n := e.count(`SELECT held FROM inventory.stock_available_v WHERE variant_id = $1`, v1); n != 2 {
		t.Errorf("held V1 = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.create' AND entity_id = $1
		AND after::text NOT LIKE '%901234567%' AND after::text NOT LIKE '%Lê Lợi%' AND after::text NOT LIKE '%Chị Lan%'`,
		o["id"]); n != 1 {
		t.Errorf("audit order.create không dữ liệu cá nhân: %d", n)
	}
	got := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/orders/"+fmt.Sprint(o["id"]), orderViewer, "", "")
	if g := decodeJSON(t, got); got.Code != http.StatusOK || g["version"] != float64(2) || g["label"] != "confirmed" ||
		g["discountTotal"] != float64(20000) {
		t.Errorf("getOrder: %d %s", got.Code, got.Body.String())
	}
}

// Tương thích M1 (Q30): thân kiểu M1 → 201 confirmed, addressComplete false.
func TestOrderAPI_M1BodyStillConfirmed(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	v := e.variant(e.shop.Seller, 1000, 5)
	o := e.mustCreate("m1", orderBody(0, line(v, 1)))
	if o["status"] != "confirmed" || o["addressComplete"] != false || o["version"] != float64(2) {
		t.Errorf("thân M1: %v", o)
	}
}

// Ý định và lỗi dữ liệu qua API.
func TestOrderAPI_CreateM2Refusals(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	v := e.variant(e.shop.Seller, 250000, 10)
	d := e.mustCreate("d1", m2Body("draft", "", 0, m2Line(v, 1, 0)))
	if d["status"] != "draft" || d["label"] != "draft" || d["draftExpiresAt"] == nil {
		t.Errorf("đơn nháp: %v", d)
	}
	if n := e.mustCreate("n1", m2Body("new", "", 0, m2Line(v, 1, 0))); n["status"] != "new" || n["addressComplete"] != false {
		t.Errorf("new không địa chỉ: %v", n)
	}
	noWard := strings.Replace(fullAddr, `,"wardCode":"T01001"`, "", 1)
	expect(t, "confirmed thiếu xã", e.post("r1", m2Body("confirmed", noWard, 0, m2Line(v, 1, 0))),
		http.StatusUnprocessableEntity, "address_incomplete")
	expect(t, "giảm dòng vượt", e.post("r2", m2Body("confirmed", fullAddr, 0, m2Line(v, 2, 500001))),
		http.StatusUnprocessableEntity, "line_discount_exceeds")
	expect(t, "phí 2^53", e.post("r3", m2Body("confirmed", fullAddr, 9007199254740992, m2Line(v, 1, 0))),
		http.StatusBadRequest, "bad_request")
	inactive := uuid.New()
	expect(t, "nguồn không có", e.post("r4", strings.Replace(m2Body("new", "", 0, m2Line(v, 1, 0)), `"intent"`,
		`"trafficSourceId":"`+inactive.String()+`","intent"`, 1)), http.StatusUnprocessableEntity, "traffic_source_inactive")
	expect(t, "biến thể không có", e.post("r5", m2Body("new", "", 0, m2Line(uuid.New(), 1, 0))),
		http.StatusUnprocessableEntity, "variant_not_found")
	both := strings.Replace(m2Body("new", "", 0, m2Line(v, 1, 0)), `{"customer"`,
		`{"customerId":"`+uuid.NewString()+`","customer"`, 1)
	expect(t, "customerId và customer", e.post("r6", both), http.StatusBadRequest, "invalid_input")
	expect(t, "confirmed không địa chỉ", e.post("r8", m2Body("confirmed", "", 0, m2Line(v, 1, 0))),
		http.StatusBadRequest, "invalid_input")
}

// orderForSomeoneElse tạo khách có sẵn rồi một đơn confirmed cho khách đó với người nhận có SĐT phone; extra là trường
// thêm của thân (có dấu phẩy đầu). Trả mã khách, đơn và mã biến thể.
func (e *orderAPIEnv) orderForSomeoneElse(key, phone, extra string) (a any, o map[string]any, v uuid.UUID) {
	e.m2Prepare()
	v = e.variant(e.shop.Seller, 1000, 5)
	a = e.mustCreate("a", orderBody(0, line(v, 1)))["customerId"]
	body := `{"customerId":"` + fmt.Sprint(a) + `"` + extra + `,"intent":"confirmed","items":[` + m2Line(v, 1, 0) +
		`],"shipAddress":` + strings.Replace(fullAddr, "0901234567", phone, 1) + `,"shippingFee":0}`
	return a, e.mustCreate(key, body), v
}

// DH12 qua API: khách A đặt hộ, SĐT người nhận của mẹ, phoneIsCustomers vắng → ship_phone là số đó, A không có danh
// tính mới.
func TestOrderAPI_OrderForSomeoneElse(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	a, o, _ := e.orderForSomeoneElse("dh12", "0912000111", "")
	if o["customerId"] != a {
		t.Errorf("khách = %v, mong %v", o["customerId"], a)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND ship_phone = '+84912000111'`, o["id"]); n != 1 {
		t.Errorf("ship_phone sai")
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1`, a); n != 0 {
		t.Errorf("A có %d danh tính, mong 0", n)
	}
}

// A-05 qua API (Task 435): phoneIsCustomers ở gốc thân đi cùng customerId → SĐT người nhận gắn vào hồ sơ chưa có số;
// gửi cùng customer → 400 invalid_input.
func TestOrderAPI_PhoneIsCustomersWithCustomerID(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	a, o, v := e.orderForSomeoneElse("a05", "0912000222", `,"phoneIsCustomers":true`)
	if o["customerId"] != a {
		t.Errorf("khách = %v, mong %v", o["customerId"], a)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1 AND value = '+84912000222'`, a); n != 1 {
		t.Errorf("A có %d danh tính +84912000222, mong 1", n)
	}
	both := strings.Replace(m2Body("new", "", 0, m2Line(v, 1, 0)), `{"customer"`, `{"phoneIsCustomers":true,"customer"`, 1)
	expect(t, "phoneIsCustomers cùng customer", e.post("a05b", both), http.StatusBadRequest, "invalid_input")
}

// DH07/AT06: bản ghi chống trùng hết hạn (đã xóa) mà gửi lại cùng khóa, cùng thân → 200 đơn cũ, vẫn 1 đơn; cùng khóa,
// khác dòng → 422 idempotency_key_reused.
func TestOrderAPI_BusinessKeyAfterExpiry(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	v := e.variant(e.shop.Seller, 250000, 5)
	body := m2Body("confirmed", fullAddr, 0, m2Line(v, 2, 0))
	first := e.mustCreate("k-exp", body)
	expire := func() {
		e.t.Helper()
		e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `DELETE FROM platform.idempotency_keys`)
			return err //nolint:wrapcheck // test
		})
	}
	expire()
	rec := e.post("k-exp", body)
	if again := decodeJSON(t, rec); rec.Code != http.StatusOK || again["id"] != first["id"] || again["code"] != first["code"] {
		t.Fatalf("gửi lại sau hết hạn: %d %s", rec.Code, rec.Body.String())
	}
	expire()
	expect(t, "cùng khóa khác dòng", e.post("k-exp", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0))),
		http.StatusUnprocessableEntity, "idempotency_key_reused")
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("%d đơn, mong 1", n)
	}
	if n := e.count(`SELECT held FROM inventory.stock_available_v WHERE variant_id = $1`, v); n != 2 {
		t.Errorf("held = %d, mong 2", n)
	}
}

// ADR-46: dòng số dư của V1 bị khóa lâu hơn lock_timeout → 503 retry_later có Retry-After, không còn dòng khóa chống
// trùng, không đơn; gửi lại cùng khóa sau khi nhả → 201, đúng 1 đơn.
func TestOrderAPI_CreateRetryLater(t *testing.T) {
	// Không t.Parallel (Task 474): holdBalanceLock tự nhả khóa sau 2 s giờ thật, dưới tải test đỏ sai.
	e := newOrderAPIEnvWith(t, 50*time.Millisecond)
	e.m2Prepare()
	v := e.variant(e.shop.Seller, 250000, 5)
	unlock := e.holdBalanceLock(v)
	body := m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0))
	rec := e.post("k-lock", body)
	unlock()
	expect(t, "số dư bị khóa", rec, http.StatusServiceUnavailable, "retry_later")
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("thiếu Retry-After: %v", rec.Header())
	}
	if n := e.count(`SELECT count(*) FROM platform.idempotency_keys`) + e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Fatalf("còn %d dòng khóa hay đơn", n)
	}
	e.mustCreate("k-lock", body)
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("%d đơn, mong 1", n)
	}
}

// holdBalanceLock khóa (FOR UPDATE) dòng số dư của biến thể v trong một transaction khác tới khi hàm trả về được gọi
// (tối đa 2 s); hàm trả về nhả khóa và chờ transaction đó xong.
func (e *orderAPIEnv) holdBalanceLock(v uuid.UUID) func() {
	e.t.Helper()
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.staff.TryExec(context.Background(), e.shop.Tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(),
				`SELECT 1 FROM inventory.stock_balances WHERE variant_id = $1 FOR UPDATE`, v); err != nil {
				return err //nolint:wrapcheck // test
			}
			close(locked)
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
			return nil
		})
	}()
	<-locked
	return func() {
		close(release)
		if err := <-done; err != nil {
			e.t.Fatal(err)
		}
	}
}

// withFields chèn các trường JSON extra (không ngoặc) vào thân b, trước "intent".
func withFields(b, extra string) string { return strings.Replace(b, `"intent"`, extra+`,"intent"`, 1) }

// couponNow tạo mã code 10% trần 50.000, maxUses lượt, hạn [now+fromH giờ, now+toH giờ) (handler dùng giờ thật).
func (e *orderAPIEnv) couponNow(code string, maxUses int32, fromH, toH int) {
	e.t.Helper()
	now := time.Now().UTC()
	maxDiscount := int64(50000)
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := promotion.Writer{}.CreateCoupon(e.t.Context(), tx, promotion.Actor{TenantID: e.shop.Tenant}, promotion.CouponInput{
			Code: code, Name: "Mã " + code, DiscountKind: promotion.KindPercent, Value: 1000, MaxDiscount: &maxDiscount,
			ReleaseOnCancel: true, ValidFrom: now.Add(time.Duration(fromH) * time.Hour),
			ValidTo: now.Add(time.Duration(toH) * time.Hour), MaxUses: optUses(maxUses),
		})
		return err //nolint:wrapcheck // test
	})
}

func optUses(n int32) *int32 {
	if n == 0 {
		return nil
	}
	return &n
}

// Mã giảm giá và ngưỡng duyệt qua API (Task M2-17): SALE10 → 201 couponDiscount 50.000, grandTotal 640.000; hết lượt
// → 422 coupon_rejected reason exhausted, không đơn mới; hết hạn → reason expired; nhân viên chỉ có order.create giảm
// tay 100.001/1.000.000 → new, chờ duyệt (nhãn awaiting_discount_approval); quản lý (vai trò mẫu manager) → confirmed.
func TestOrderAPI_CouponAndDiscountApproval(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	e.m2Prepare()
	mgr := "quan-ly-don@elimaz.test"
	e.staff.Grant(t, e.shop.Tenant, e.staff.User(t, e.shop.Tenant, mgr, "active"), rolePerms(t, "manager"), "all", "")
	e.couponNow("SALE10", 1, -24, 24)
	e.couponNow("KM02", 0, -48, -24)
	v1, v2 := e.variant(e.shop.Seller, 250000, 10), e.variant(e.shop.Seller, 180000, 10)
	dh01 := func(code string) string {
		return withFields(m2Body("confirmed", fullAddr, 30000, m2Line(v1, 2, 20000), m2Line(v2, 1, 0)),
			`"couponCode":"`+code+`"`)
	}
	o := e.mustCreate("c1", dh01("sale10"))
	if o["couponDiscount"] != float64(50000) || o["grandTotal"] != float64(640000) || o["codAmount"] != float64(640000) ||
		o["discountTotal"] != float64(70000) {
		t.Errorf("đơn có mã: %v", o)
	}
	orders := e.count(`SELECT count(*) FROM ord.orders`)
	for key, c := range map[string]struct{ code, reason string }{
		"c2": {"SALE10", "exhausted"}, "c3": {"KM02", "expired"}, "c4": {"KHONGCO", "not_found"},
	} {
		rec := e.post(key, dh01(c.code))
		body := decodeJSON(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || body["error"] != "coupon_rejected" || body["reason"] != c.reason {
			t.Errorf("mã %s: %d %v, mong 422 coupon_rejected %s", c.code, rec.Code, body, c.reason)
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != orders {
		t.Errorf("mã bị từ chối vẫn tạo đơn: %d → %d", orders, n)
	}

	v := e.variant(e.shop.Seller, 1000000, 10)
	dh16 := withFields(m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)), `"orderDiscount":100001`)
	s := e.mustCreate("a1", dh16)
	if s["status"] != "new" || s["pendingReason"] != "discount_approval" || s["label"] != "awaiting_discount_approval" {
		t.Errorf("nhân viên giảm vượt ngưỡng: %v", s)
	}
	rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", mgr, "a2", dh16)
	m := decodeJSON(t, rec)
	if rec.Code != http.StatusCreated || m["status"] != "confirmed" || m["pendingReason"] != nil {
		t.Errorf("quản lý giảm vượt ngưỡng: %d %v", rec.Code, m)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND discount_approved_by IS NOT NULL`, m["id"]); n != 1 {
		t.Errorf("đơn của quản lý không ghi discount_approved_by")
	}
}
