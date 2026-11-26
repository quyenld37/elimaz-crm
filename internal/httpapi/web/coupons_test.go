package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	birthdayPath = "/api/v1/web/birthday-batches"
	couponsPath  = "/api/v1/web/coupons"
)

// couponWeb là env đơn web có thêm khóa web.coupons.issue.
type couponWeb struct {
	*orderEnv

	issueKey string
}

func newCouponWeb(t *testing.T) *couponWeb {
	t.Helper()
	o := newOrderEnv(t, nil)
	return &couponWeb{orderEnv: o, issueKey: o.serviceKey("web.coupons.issue")}
}

func (c *couponWeb) postIdem(key, idem, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequestWithContext(c.t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.send(req, key, "Idempotency-Key", idem)
}

type batchCodesBody struct {
	BatchID string   `json:"batchId"`
	Created bool     `json:"created"`
	Codes   []string `json:"codes"`
}

//nolint:ireturn // test: bên gọi chọn kiểu thân câu trả lời
func decodeOK[T any](t *testing.T, name string, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
	}
	return v
}

// birthdayBody là lô quà sinh nhật n mã giảm 20.000, hạn từ hôm qua tới 30 ngày sau (cố định theo now của test).
func birthdayBody(name string, n int, value int64, now time.Time) string {
	return fmt.Sprintf(`{"name":%q,"quantity":%d,"template":{"name":"Quà sinh nhật","discountKind":"fixed","value":%d,`+
		`"channels":["web"],"validFrom":%q,"validTo":%q}}`, name, n, value,
		now.Add(-24*time.Hour).UTC().Format(time.RFC3339), now.Add(30*24*time.Hour).UTC().Format(time.RFC3339))
}

// [WB05] Website xin lô quà sinh nhật 5 mã → 200 created, 5 mã; gọi lại cùng tên (khóa khác) → cùng lô, created false;
// trao mã → givenAt (gọi lại giữ lần đầu, một audit); đơn web dùng mã → có lượt dùng, mã thành đã dùng; đơn thứ hai
// cùng mã → 422 coupon_invalid lý do exhausted. Audit của khóa dịch vụ: một web_create, một given.
func TestWebBirthdayCoupon_IssueGiveRedeem(t *testing.T) {
	t.Parallel()
	c := newCouponWeb(t)
	now := time.Now().Truncate(time.Second)
	b := decodeOK[batchCodesBody](t, "tạo lô", c.postIdem(c.issueKey, "b1", birthdayPath, birthdayBody("SN 12/2026", 5, 20000, now)))
	if !b.Created || len(b.Codes) != 5 || b.BatchID == "" {
		t.Fatalf("lô mới: %+v", b)
	}
	again := decodeOK[batchCodesBody](t, "gọi lại", c.postIdem(c.issueKey, "b2", birthdayPath, birthdayBody("SN 12/2026", 5, 20000, now)))
	if again.Created || again.BatchID != b.BatchID || strings.Join(again.Codes, ",") != strings.Join(b.Codes, ",") {
		t.Fatalf("gọi lại: %+v", again)
	}
	code := b.Codes[0]
	type given struct {
		Code    string    `json:"code"`
		GivenAt time.Time `json:"givenAt"`
	}
	g1 := decodeOK[given](t, "trao", c.postIdem(c.issueKey, "g1", couponsPath+"/"+code+"/given", ""))
	g2 := decodeOK[given](t, "trao lại", c.postIdem(c.issueKey, "g2", couponsPath+"/"+code+"/given", ""))
	if g1.Code != code || g1.GivenAt.IsZero() || !g2.GivenAt.Equal(g1.GivenAt) {
		t.Fatalf("trao: %+v %+v", g1, g2)
	}

	p := c.product("PSN", "[]")
	v := p.Variants[0].ID
	c.stock(v, 5)
	w := c.order(phone(31), code, quoteLine(v, 1, 350000))
	if w.total != 360000 {
		t.Fatalf("báo giá có mã sinh nhật: %d", w.total)
	}
	rec := c.place(w.ref, w)
	created(t, rec.Code, rec.Body.Bytes())
	if n := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM promotion.coupon_redemptions r
		JOIN promotion.coupons k ON k.id = r.coupon_id WHERE k.code = $1 AND r.status = 'active' AND k.used_count = 1`,
		code); n != 1 {
		t.Fatal("đơn web không ghi lượt dùng mã sinh nhật")
	}
	w2 := c.order(phone(32), "", quoteLine(v, 1, 350000))
	w2.coupon = code
	if r := refused(t, c.place(w2.ref, w2), http.StatusUnprocessableEntity, "coupon_invalid"); r.Reason != "exhausted" {
		t.Fatalf("đơn thứ hai: %+v", r)
	}
	for action, want := range map[string]int{"promotion.coupon_batch.web_create": 1, "promotion.coupon.given": 1} {
		if n := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = $1
			AND actor_type = 'service'`, action); n != want {
			t.Fatalf("audit %s: %d, mong %d", action, n, want)
		}
	}
}

// [WB11] Hai đơn web cùng lúc dùng cùng mã sinh nhật → đúng một đơn có mã, đơn kia 422 coupon_invalid exhausted.
func TestWebBirthdayCoupon_ParallelOrdersOneWins(t *testing.T) {
	t.Parallel()
	c := newCouponWeb(t)
	b := decodeOK[batchCodesBody](t, "tạo lô", c.postIdem(c.issueKey, "b1", birthdayPath, birthdayBody("SN song song", 1, 20000, time.Now())))
	p := c.product("PSS", "[]")
	v := p.Variants[0].ID
	c.stock(v, 5)
	x, y := c.order(phone(41), b.Codes[0], quoteLine(v, 1, 350000)), c.order(phone(42), b.Codes[0], quoteLine(v, 1, 350000))
	rs := c.race(x, y)
	lose := winnerLoser(t, rs)
	if r := refused(t, rs[lose], http.StatusUnprocessableEntity, "coupon_invalid"); r.Reason != "exhausted" {
		t.Fatalf("đơn thua: %s", rs[lose].Body)
	}
	if n := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM promotion.coupon_redemptions WHERE status = 'active'`); n != 1 {
		t.Fatalf("%d lượt dùng, mong 1", n)
	}
}

// Từ chối: cùng tên khác giá trị → 422 batch_conflict (không lưu theo khóa: sửa đúng rồi gửi lại cùng khóa → 200);
// khóa thiếu web.coupons.issue → 403; trao mã không có → 404 not_found; 5001 mã → 400.
func TestWebCoupons_Refusals(t *testing.T) {
	t.Parallel()
	c := newCouponWeb(t)
	now := time.Now().Truncate(time.Second)
	decodeOK[batchCodesBody](t, "tạo lô", c.postIdem(c.issueKey, "b1", birthdayPath, birthdayBody("SN", 2, 20000, now)))
	expectErr := func(name string, rec *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		var body struct {
			Error string `json:"error"`
		}
		if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &body) != nil || (code != "" && body.Error != code) {
			t.Fatalf("%s: mong %d %s, có %d %s", name, status, code, rec.Code, rec.Body.String())
		}
	}
	expectErr("khác giá trị", c.postIdem(c.issueKey, "b2", birthdayPath, birthdayBody("SN", 2, 30000, now)),
		http.StatusUnprocessableEntity, "batch_conflict")
	decodeOK[batchCodesBody](t, "cùng khóa sau khi sửa", c.postIdem(c.issueKey, "b2", birthdayPath, birthdayBody("SN-2", 2, 30000, now)))
	expectErr("khóa đọc đơn", c.postIdem(c.readKeyFor(), "b3", birthdayPath, birthdayBody("SN-3", 2, 20000, now)),
		http.StatusForbidden, "forbidden")
	expectErr("mã không có", c.postIdem(c.issueKey, "g1", couponsPath+"/KHONGCO1/given", ""), http.StatusNotFound, "not_found")
	expectErr("5001 mã", c.postIdem(c.issueKey, "b4", birthdayPath, birthdayBody("SN-4", 5001, 20000, now)),
		http.StatusBadRequest, "")
}

func (c *couponWeb) readKeyFor() string { return c.serviceKey("web.orders.read") }

type importReport struct {
	Inserted  int      `json:"inserted"`
	Skipped   int      `json:"skipped"`
	Conflicts []string `json:"conflicts"`
}

func importItem(code, kind string, value int64, from, to time.Time, extra string) string {
	return fmt.Sprintf(`{"code":%q,"discountKind":%q,"value":%d,"validFrom":%q,"validTo":%q,"batchName":"SN cũ"%s}`,
		code, kind, value, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), extra)
}

// [WB05] Nhập mã còn hạn của website qua API: 4 mã (một đã dùng, một trùng mã CRM khác giá trị, một hết hạn) → inserted
// 3, conflicts [TRUNG1], một audit; báo giá web với mã nhập 10% → giảm theo phần vạn; mã đã dùng → reason exhausted;
// 501 mục → 400; khóa thiếu quyền → 403.
func TestWebImportCoupons_API(t *testing.T) {
	t.Parallel()
	c := newCouponWeb(t)
	now := time.Now().Truncate(time.Second)
	if rec := c.postIdem(c.issueKey, "s1", couponsPath+"/import", `{"items":[`+
		importItem("TRUNG1", "fixed", 10000, now.Add(-time.Hour), now.Add(time.Hour), "")+`]}`); rec.Code != http.StatusOK {
		t.Fatalf("mã có sẵn: %d %s", rec.Code, rec.Body)
	}
	items := []string{
		importItem("CU-OK", "percent", 10, now.Add(-time.Hour), now.Add(240*time.Hour), `,"maxDiscount":50000,"givenAt":"`+
			now.Add(-time.Hour).UTC().Format(time.RFC3339)+`"`),
		importItem("CU-DUNG", "fixed", 50000, now.Add(-time.Hour), now.Add(240*time.Hour), `,"used":true`),
		importItem("TRUNG1", "fixed", 60000, now.Add(-time.Hour), now.Add(time.Hour), ""),
		importItem("CU-HET", "fixed", 50000, now.Add(-48*time.Hour), now.Add(-24*time.Hour), ""),
	}
	r := decodeOK[importReport](t, "nhập", c.postIdem(c.issueKey, "i1", couponsPath+"/import", `{"items":[`+strings.Join(items, ",")+`]}`))
	if r.Inserted != 3 || r.Skipped != 0 || strings.Join(r.Conflicts, ",") != "TRUNG1" {
		t.Fatalf("báo cáo: %+v", r)
	}
	if n := c.staff.CountIn(t, c.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'promotion.coupon.web_import'
		AND actor_type = 'service' AND (after->>'inserted')::int = 3`); n != 1 {
		t.Fatalf("audit nhập: %d", n)
	}
	p := c.product("PIM", "[]")
	v := p.Variants[0].ID
	c.stock(v, 5)
	if q := c.quote(c.key, fmt.Sprintf(`{"lines":[%s],"shippingFee":30000,"couponCode":"CU-OK","customerPhone":%q}`,
		quoteLine(v, 1, 350000), phone(51))); len(q.Lines) != 1 || q.Lines[0].CouponDiscount != 35000 {
		t.Fatalf("báo giá mã nhập 10%%: %+v", q)
	}
	if q := c.quote(c.key, fmt.Sprintf(`{"lines":[%s],"shippingFee":30000,"couponCode":"CU-DUNG","customerPhone":%q}`,
		quoteLine(v, 1, 350000), phone(52))); q.Coupon == nil || q.Coupon.Applied || q.Coupon.Reason != "exhausted" {
		t.Fatalf("báo giá mã đã dùng: %+v", q.Coupon)
	}
	many := make([]string, 501)
	for i := range many {
		many[i] = importItem(fmt.Sprintf("M%05d", i), "fixed", 1000, now, now.Add(time.Hour), "")
	}
	if rec := c.postIdem(c.issueKey, "i2", couponsPath+"/import", `{"items":[`+strings.Join(many, ",")+`]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("501 mục: %d %s", rec.Code, rec.Body)
	}
	if rec := c.postIdem(c.readKeyFor(), "i3", couponsPath+"/import", `{"items":[`+items[0]+`]}`); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền: %d", rec.Code)
	}
}

// [WB05] Mã nhập gắn SĐT (boundPhone, Task 491): báo giá với đúng SĐT → giảm; SĐT khác → bound_other; SĐT gắn mã sai →
// 422 invalid_phone, không nhập.
func TestWebImportCoupons_BoundPhone(t *testing.T) {
	t.Parallel()
	c := newCouponWeb(t)
	now := time.Now().Truncate(time.Second)
	item := importItem("SN-SDT", "fixed", 40000, now.Add(-time.Hour), now.Add(240*time.Hour),
		fmt.Sprintf(`,"boundPhone":%q`, phone(61)))
	if r := decodeOK[importReport](t, "nhập", c.postIdem(c.issueKey, "b1", couponsPath+"/import",
		`{"items":[`+item+`]}`)); r.Inserted != 1 {
		t.Fatalf("nhập: %+v", r)
	}
	p := c.product("PSDT", "[]")
	v := p.Variants[0].ID
	c.stock(v, 5)
	if q := c.quote(c.key, fmt.Sprintf(`{"lines":[%s],"shippingFee":30000,"couponCode":"SN-SDT","customerPhone":%q}`,
		quoteLine(v, 1, 350000), phone(61))); q.Coupon == nil || !q.Coupon.Applied {
		t.Fatalf("đúng SĐT: %+v", q.Coupon)
	}
	if q := c.quote(c.key, fmt.Sprintf(`{"lines":[%s],"shippingFee":30000,"couponCode":"SN-SDT","customerPhone":%q}`,
		quoteLine(v, 1, 350000), phone(62))); q.Coupon == nil || q.Coupon.Applied || q.Coupon.Reason != "bound_other" {
		t.Fatalf("SĐT khác: %+v", q.Coupon)
	}
	bad := importItem("SN-SAI", "fixed", 40000, now.Add(-time.Hour), now.Add(240*time.Hour), `,"boundPhone":"0012345678"`)
	if rec := c.postIdem(c.issueKey, "b2", couponsPath+"/import", `{"items":[`+bad+`]}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("SĐT sai: %d %s", rec.Code, rec.Body)
	}
}
