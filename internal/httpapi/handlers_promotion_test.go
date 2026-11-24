package httpapi

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

const (
	couponManager = "quan-ly-ma@elimaz.test"
	couponSale    = "ban-hang-ma@elimaz.test"
)

// vnTime là giờ Việt Nam (UTC+7) của ngày thử.
func vnTime(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.FixedZone("ICT", 7*3600))
}

type couponEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *cfaccesstest.AccessKeys
	srv    http.Handler
	tenant uuid.UUID
	now    time.Time
}

func newCouponEnv(t *testing.T) *couponEnv {
	t.Helper()
	e := &couponEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t)}
	e.tenant = e.staff.Tenant(t, "A")
	m := e.staff.User(t, e.tenant, couponManager, "active")
	e.staff.Grant(t, e.tenant, m, rolePerms(t, "manager"), "all", "")
	s := e.staff.User(t, e.tenant, couponSale, "active")
	e.staff.Grant(t, e.tenant, s, rolePerms(t, "sale"), "all", "")
	authE := &authEnv{t: t, keys: e.keys}
	e.now = vnTime(2026, 12, 15, 10)
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App,
		Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: &bytes.Buffer{}}, nil)), Access: authE.verifier(),
		Now: func() time.Time { return e.now },
	})
	return e
}

// createSale10 tạo mã SALE10 (sale10Body) bằng couponManager với khóa "c1", trả id.
func (e *couponEnv) createSale10() string {
	e.t.Helper()
	c := e.ok("tạo", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c1", sale10Body), http.StatusCreated)
	id, _ := c["id"].(string)
	return id
}

func (e *couponEnv) do(method, path, email, key, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

func (e *couponEnv) ok(name string, rec *httptest.ResponseRecorder, status int) map[string]any {
	e.t.Helper()
	return mustOK(e.t, name, rec, status)
}

func (e *couponEnv) check(email, query string) map[string]any {
	e.t.Helper()
	return e.ok("kiểm mã "+query, e.do(http.MethodGet, "/api/v1/coupon-check?"+query, email, "", ""), http.StatusOK)
}

func (e *couponEnv) usedCount(id string) int {
	return e.staff.CountIn(e.t, e.tenant, `SELECT used_count FROM promotion.coupons WHERE id = $1`, id)
}

const sale10Body = `{"code":"SALE10","name":"Giảm 10% tháng 12","discountKind":"percent","value":1000,
	"maxDiscount":50000,"maxUses":2,"validFrom":"2026-12-01T00:00:00+07:00","validTo":"2027-01-01T00:00:00+07:00"}`

func TestCouponAPI_CreateAndCheck(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	expect(t, "sale tạo mã", e.do(http.MethodPost, "/api/v1/coupons", couponSale, "c0", sale10Body),
		http.StatusForbidden, "forbidden")
	c := e.ok("KM01 tạo SALE10", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c1", sale10Body), http.StatusCreated)
	if c["code"] != "SALE10" || c["usedCount"] != float64(0) || c["remainingUses"] != float64(2) || c["bound"] != false ||
		c["version"] != float64(1) || c["status"] != "active" || c["maxDiscount"] != float64(50000) {
		t.Fatalf("mã vừa tạo: %v", c)
	}
	id, _ := c["id"].(string)
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'promotion.coupon.create'
		AND entity_id = $1 AND after->>'code' = 'SALE10'`, id); n != 1 {
		t.Errorf("audit tạo mã: %d dòng", n)
	}

	got := e.check(couponSale, "code=sale10&base=660000&channel=manual")
	if got["ok"] != true || got["discount"] != float64(50000) || got["remainingUses"] != float64(2) {
		t.Fatalf("KM01 ngày 15/12: %v", got)
	}
	if _, ok := got["reason"]; ok {
		t.Fatalf("mã đạt không có reason: %v", got)
	}
	e.now = vnTime(2027, 1, 1, 0)
	if got := e.check(couponSale, "code=sale10&base=660000&channel=manual"); got["ok"] != false ||
		got["reason"] != "expired" || got["discount"] != float64(0) {
		t.Fatalf("KM02 ngày 01/01/2027: %v", got)
	}
	e.now = vnTime(2026, 12, 15, 10)

	e.ok("mã tối thiểu 300.000", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c2",
		`{"code":"MIN300","name":"Đơn từ 300k","discountKind":"fixed","value":20000,"minSubtotal":300000,
		"validFrom":"2026-12-01T00:00:00+07:00","validTo":"2027-01-01T00:00:00+07:00"}`), http.StatusCreated)
	if got := e.check(couponSale, "code=MIN300&base=299999"); got["ok"] != false || got["reason"] != "min_subtotal" {
		t.Fatalf("KM02 min_subtotal: %v", got)
	}
	if got := e.check(couponSale, "code=KHONGCO&base=1000"); got["ok"] != false || got["reason"] != "not_found" {
		t.Fatalf("mã không có: %v", got)
	}

	expect(t, "tạo lại sale10", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c3",
		`{"code":"sale10","name":"x","discountKind":"fixed","value":1,"validFrom":"2026-12-01T00:00:00Z","validTo":"2026-12-02T00:00:00Z"}`),
		http.StatusConflict, "code_taken")
	expect(t, "hạn ngược", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c4",
		`{"name":"x","discountKind":"fixed","value":1,"validFrom":"2026-12-02T00:00:00Z","validTo":"2026-12-01T00:00:00Z"}`),
		http.StatusBadRequest, "invalid_input")
	expect(t, "SĐT sai", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c5",
		`{"name":"x","discountKind":"fixed","value":1,"boundPhone":"12345678","validFrom":"2026-12-01T00:00:00Z","validTo":"2026-12-02T00:00:00Z"}`),
		http.StatusUnprocessableEntity, "invalid_phone")
	gen := e.ok("mã sinh", e.do(http.MethodPost, "/api/v1/coupons", couponManager, "c6",
		`{"name":"Sinh mã","discountKind":"fixed","value":10000,"boundPhone":"0901234567","validFrom":"2026-12-01T00:00:00Z","validTo":"2026-12-02T00:00:00Z"}`),
		http.StatusCreated)
	if code, _ := gen["code"].(string); !regexp.MustCompile(`^[A-HJ-NP-Z2-9]{8}$`).MatchString(code) || gen["bound"] != true {
		t.Fatalf("mã sinh gắn SĐT: %v", gen)
	}
	if _, ok := gen["boundPhone"]; ok {
		t.Fatalf("không trả SĐT: %v", gen)
	}

	// checkCoupon 100 lần không tốn lượt.
	for range 100 {
		if got := e.check(couponSale, "code=SALE10&base=660000"); got["ok"] != true {
			t.Fatalf("kiểm mã: %v", got)
		}
	}
	if n := e.usedCount(id); n != 0 {
		t.Fatalf("used_count sau 100 lần kiểm: %d", n)
	}
}

// Đọc mã, danh sách mã (promotion.read của sale), mã không có, con trỏ sai.
func TestCouponAPI_Read(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	id := e.createSale10()
	read := e.ok("sale đọc mã", e.do(http.MethodGet, "/api/v1/coupons/"+id, couponSale, "", ""), http.StatusOK)
	if read["code"] != "SALE10" || read["remainingUses"] != float64(2) {
		t.Fatalf("getCoupon: %v", read)
	}
	expect(t, "mã không có", e.do(http.MethodGet, "/api/v1/coupons/"+uuid.NewString(), couponSale, "", ""),
		http.StatusNotFound, "not_found")
	page := e.ok("danh sách", e.do(http.MethodGet, "/api/v1/coupons?q=sale&limit=10", couponSale, "", ""), http.StatusOK)
	if items, _ := page["items"].([]any); len(items) != 1 {
		t.Fatalf("listCoupons q=sale: %v", page)
	}
	expect(t, "cursor sai", e.do(http.MethodGet, "/api/v1/coupons?cursor=abc", couponSale, "", ""),
		http.StatusBadRequest, "invalid_input")
}

func TestCouponAPI_UpdateAfterUse(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	id := e.createSale10()
	path := "/api/v1/coupons/" + id
	expect(t, "sale sửa", e.do(http.MethodPatch, path, couponSale, "u0", `{"version":1,"name":"x"}`),
		http.StatusForbidden, "forbidden")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons SET used_count = 1 WHERE id = $1`, id)
		return err //nolint:wrapcheck // test
	})
	expect(t, "đổi value sau khi dùng", e.do(http.MethodPatch, path, couponManager, "u1", `{"version":1,"value":2000}`),
		http.StatusConflict, "coupon_locked_after_use")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons SET used_count = 2 WHERE id = $1`, id)
		return err //nolint:wrapcheck // test
	})
	expect(t, "max_uses dưới used_count", e.do(http.MethodPatch, path, couponManager, "u2", `{"version":1,"maxUses":1}`),
		http.StatusUnprocessableEntity, "max_uses_below_used")
	got := e.ok("đổi valid_to", e.do(http.MethodPatch, path, couponManager, "u3",
		`{"version":1,"validTo":"2027-01-15T00:00:00+07:00","status":"disabled"}`), http.StatusOK)
	if got["version"] != float64(2) || got["status"] != "disabled" || got["usedCount"] != float64(2) ||
		got["remainingUses"] != float64(0) {
		t.Fatalf("sau khi sửa: %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'promotion.coupon.update'
		AND entity_id = $1 AND before->>'status' = 'active' AND after->>'status' = 'disabled'`, id); n != 1 {
		t.Errorf("audit sửa mã: %d dòng", n)
	}
	expect(t, "version cũ", e.do(http.MethodPatch, path, couponManager, "u4", `{"version":1,"name":"y"}`),
		http.StatusConflict, "version_conflict")
	if got := e.check(couponSale, "code=SALE10&base=660000"); got["ok"] != false || got["reason"] != "disabled" {
		t.Fatalf("mã đã tắt: %v", got)
	}
	redemptions := e.ok("lượt dùng", e.do(http.MethodGet, path+"/redemptions", couponSale, "", ""), http.StatusOK)
	if items, _ := redemptions["items"].([]any); len(items) != 0 {
		t.Fatalf("chưa có lượt dùng: %v", redemptions)
	}
}

func TestCouponAPI_Batch(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	tpl := `{"name":"Quà","discountKind":"fixed","value":50000,"validFrom":"2026-12-01T00:00:00Z","validTo":"2027-01-01T00:00:00Z"}`
	expect(t, "sale tạo đợt", e.do(http.MethodPost, "/api/v1/coupon-batches", couponSale, "b0",
		`{"name":"Đợt","quantity":2,"template":`+tpl+`}`), http.StatusForbidden, "forbidden")
	b := e.ok("đợt 3 SĐT", e.do(http.MethodPost, "/api/v1/coupon-batches", couponManager, "b1",
		`{"name":"Sinh nhật","quantity":3,"template":`+tpl+`,"boundPhones":["0901000001","0901000002","0901000003"]}`),
		http.StatusCreated)
	bid, _ := b["id"].(string)
	if b["quantity"] != float64(3) || b["prefix"] != "" {
		t.Fatalf("đợt: %v", b)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(DISTINCT bound_phone) FROM promotion.coupons
		WHERE batch_id = $1 AND max_uses = 1`, bid); n != 3 {
		t.Fatalf("3 mã, mỗi mã một SĐT: %d", n)
	}
	got := e.ok("đọc đợt", e.do(http.MethodGet, "/api/v1/coupon-batches/"+bid, couponSale, "", ""), http.StatusOK)
	if tp, _ := got["template"].(map[string]any); tp["value"] != float64(50000) || tp["discountKind"] != "fixed" {
		t.Fatalf("getCouponBatch: %v", got)
	}
	page := e.ok("mã của đợt", e.do(http.MethodGet, "/api/v1/coupons?batchId="+url.QueryEscape(bid), couponSale, "", ""),
		http.StatusOK)
	items, _ := page["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("listCoupons batchId: %v", page)
	}
	for _, it := range items {
		if m, _ := it.(map[string]any); m["bound"] != true || m["batchId"] != bid {
			t.Fatalf("mã của đợt: %v", m)
		}
	}
	expect(t, "số lượng khác số SĐT", e.do(http.MethodPost, "/api/v1/coupon-batches", couponManager, "b2",
		`{"name":"Sai","quantity":2,"template":`+tpl+`,"boundPhones":["0901000001"]}`), http.StatusBadRequest, "invalid_input")
	expect(t, "đợt không có", e.do(http.MethodGet, fmt.Sprintf("/api/v1/coupon-batches/%s", uuid.NewString()), couponSale, "", ""),
		http.StatusNotFound, "not_found")
}

// Lượt dùng của mã: mã đơn ghép từ module order, số giảm, trạng thái; không SĐT.
func TestCouponAPI_Redemptions(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	id := e.createSale10()
	var o order.Order
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		shop := shiptest.NewShop(t, tx, e.tenant)
		v := shop.Variant(t, tx, shop.Seller, 400000, 5)
		var err error
		if o, err = shop.Order(t.Context(), tx, shop.Seller, uuid.Nil, 0, order.ItemInput{VariantID: v, Qty: 1}); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.Redeem(t.Context(), tx, "SALE10",
			promotion.OrderRef{TenantID: e.tenant, ID: o.ID, Version: o.Version}, promotion.EvalInput{
				Base: 400000, Channel: "manual", CustomerID: o.CustomerID, CustomerRootID: o.CustomerID, SellerID: shop.Seller,
				Now: vnTime(2026, 12, 15, 10),
			})
		return err //nolint:wrapcheck // test
	})
	orderID, orderCode := o.ID, o.Code
	page := e.ok("lượt dùng", e.do(http.MethodGet, "/api/v1/coupons/"+id+"/redemptions", couponSale, "", ""), http.StatusOK)
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("một lượt: %v", page)
	}
	if r, _ := items[0].(map[string]any); r["orderId"] != orderID.String() || r["orderCode"] != orderCode ||
		r["discount"] != float64(40000) || r["status"] != "active" || len(r) != 6 {
		t.Fatalf("lượt dùng (6 trường, không SĐT): %v", r)
	}
	expect(t, "mã không có", e.do(http.MethodGet, "/api/v1/coupons/"+uuid.NewString()+"/redemptions", couponSale, "", ""),
		http.StatusNotFound, "not_found")
}

// getCouponBatch (M2) trả thêm loại lô và số đếm (Task 197): lô sinh nhật 10 mã, website đã trao cả 10, 3 mã đã dùng →
// givenCount 10, usedCount 3, expiredCount 0; listCoupons?batchId có givenAt; listCouponBatches?purpose=birthday chỉ
// trả lô sinh nhật (lô manual của nhân viên không vào), không purpose → cả hai, mới nhất trước; purpose lạ → 400;
// người không có promotion.read → 403 ở cả hai.
func TestCouponAPI_BatchStatsAndList(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	tpl := `{"name":"Quà","discountKind":"fixed","value":50000,"validFrom":"2026-12-01T00:00:00Z","validTo":"2027-01-01T00:00:00Z"}`
	m := e.ok("đợt tay", e.do(http.MethodPost, "/api/v1/coupon-batches", couponManager, "b1",
		`{"name":"Tay","quantity":1,"template":`+tpl+`}`), http.StatusCreated)
	if m["purpose"] != "manual" {
		t.Fatalf("createCouponBatch purpose: %v", m)
	}
	var bid uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		b, cs, _, err := promotion.Writer{}.EnsureWebBatch(t.Context(), tx, promotion.WebBatchInput{
			Name: "SN 12/2026", Purpose: promotion.PurposeBirthday, Quantity: 10,
			Template: promotion.CouponTemplate{
				Name: "Quà", DiscountKind: promotion.KindFixed, Value: 50000,
				ValidFrom: vnTime(2026, 12, 1, 0), ValidTo: vnTime(2027, 1, 1, 0),
			},
		}, promotion.Actor{TenantID: e.tenant, UserID: uuid.New()})
		bid = b.ID
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(t.Context(), `UPDATE promotion.coupons SET given_at = $2,
			used_count = CASE WHEN code = ANY($3) THEN 1 ELSE 0 END WHERE batch_id = $1`,
			b.ID, e.now.Add(-time.Hour), cs[:3])
		return err //nolint:wrapcheck // test
	})
	got := e.ok("đọc đợt", e.do(http.MethodGet, "/api/v1/coupon-batches/"+bid.String(), couponSale, "", ""), http.StatusOK)
	if got["purpose"] != "birthday" || got["givenCount"] != float64(10) || got["usedCount"] != float64(3) ||
		got["expiredCount"] != float64(0) || got["quantity"] != float64(10) {
		t.Fatalf("getCouponBatch: %v", got)
	}
	page := e.ok("mã của đợt", e.do(http.MethodGet, "/api/v1/coupons?batchId="+bid.String(), couponSale, "", ""), http.StatusOK)
	items, _ := page["items"].([]any)
	if len(items) != 10 {
		t.Fatalf("mã của đợt: %v", page)
	}
	for _, it := range items {
		c, _ := it.(map[string]any)
		s, _ := c["givenAt"].(string)
		if at, err := time.Parse(time.RFC3339, s); err != nil || !at.Equal(e.now.Add(-time.Hour)) {
			t.Fatalf("givenAt: %v", c)
		}
	}
	ids := func(q string) []any {
		p := e.ok("listCouponBatches "+q, e.do(http.MethodGet, "/api/v1/coupon-batches"+q, couponSale, "", ""), http.StatusOK)
		out := []any{}
		its, _ := p["items"].([]any)
		for _, it := range its {
			b, _ := it.(map[string]any)
			out = append(out, b["id"])
		}
		return out
	}
	if got := ids("?purpose=birthday"); len(got) != 1 || got[0] != bid.String() {
		t.Fatalf("purpose=birthday: %v", got)
	}
	if got := ids(""); len(got) != 2 || got[0] != bid.String() || got[1] != m["id"] {
		t.Fatalf("mọi loại: %v", got)
	}
	expect(t, "purpose lạ", e.do(http.MethodGet, "/api/v1/coupon-batches?purpose=vip", couponSale, "", ""),
		http.StatusBadRequest, "")
	u := e.staff.User(t, e.tenant, "kho-ma@elimaz.test", "active")
	e.staff.Grant(t, e.tenant, u, []string{"order.read"}, "all", "")
	expect(t, "không promotion.read", e.do(http.MethodGet, "/api/v1/coupon-batches", "kho-ma@elimaz.test", "", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "không promotion.read đọc đợt", e.do(http.MethodGet, "/api/v1/coupon-batches/"+bid.String(),
		"kho-ma@elimaz.test", "", ""), http.StatusForbidden, "forbidden")
}
