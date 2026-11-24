package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
)

// guessClock là đồng hồ giả của bộ đếm đoán mã (mã giảm giá vẫn kiểm theo giờ thật).
type guessClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *guessClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *guessClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// guessEnv là orderEnv có CouponGuard ngưỡng nhỏ (SĐT 3, IP 6 lần sai / 15 phút, đồng hồ ở phút 5) và một mã đúng
// DUNG1 (giảm 50.000, không giới hạn lượt).
func guessEnv(t *testing.T) (*orderEnv, *guessClock) {
	t.Helper()
	clk := &guessClock{t: time.Date(2026, 11, 24, 10, 5, 0, 0, time.UTC)}
	o := newOrderEnv(t, func(d *httpapi.Deps) {
		d.CouponGuard = httpapi.NewCouponGuard(cache.NewRateLimiter(nil, clk.now, nil),
			cache.Limit{Name: "coupon_guess_customer", Max: 3, Window: 15 * time.Minute, FallbackMax: 3},
			cache.Limit{Name: "coupon_guess_ip", Max: 6, Window: 15 * time.Minute, FallbackMax: 6},
			[]byte("khoa-hmac-thu-cua-test-du-32-ky-tu!!"))
	})
	now := time.Now()
	o.app(func(tx pgx.Tx) error {
		_, err := promotion.Writer{}.CreateCoupon(t.Context(), tx, promotion.Actor{TenantID: o.tenant, UserID: o.owner.UserID},
			promotion.CouponInput{
				Code: "DUNG1", Name: "Mã đúng", DiscountKind: "fixed", Value: 50000,
				ValidFrom: now.Add(-24 * time.Hour), ValidTo: now.Add(30 * 24 * time.Hour),
			})
		return err //nolint:wrapcheck // test
	})
	return o, clk
}

// quoteAs báo giá một dòng line với mã code, SĐT ph, IP ip (rỗng: không gửi).
func (o *orderEnv) quoteAs(line, code, ph, ip string) *httptest.ResponseRecorder {
	o.t.Helper()
	body := fmt.Sprintf(`{"lines":[%s],"shippingFee":0,"couponCode":%q`, line, code)
	if ph != "" {
		body += fmt.Sprintf(`,"customerPhone":%q`, ph)
	}
	if ip != "" {
		body += fmt.Sprintf(`,"clientIp":%q`, ip)
	}
	return o.post(o.key, "/api/v1/web/quote", body+"}")
}

// couponOf đọc kết quả mã của báo giá 200.
func couponOf(t *testing.T, rec *httptest.ResponseRecorder) (applied bool, reason string) {
	t.Helper()
	var q quoteBody
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &q) != nil || q.Coupon == nil {
		t.Fatalf("báo giá có mã: %d %s", rec.Code, rec.Body.String())
	}
	return q.Coupon.Applied, q.Coupon.Reason
}

// locked: 429 coupon_attempts_exceeded có Retry-After là số giây trong (0, 900].
func locked(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	refused(t, rec, http.StatusTooManyRequests, "coupon_attempts_exceeded")
	if s, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || s <= 0 || s > 900 {
		t.Fatalf("%s: Retry-After %q", what, rec.Header().Get("Retry-After"))
	}
}

// Threat model M5 (B1, I "mã bị dò"): báo giá — 3 mã sai của một SĐT → 200 not_found; lần 4 (mã sai hay mã ĐÚNG) → 429
// coupon_attempts_exceeded kèm Retry-After; báo giá không mã vẫn 200; SĐT khác, IP khác dùng mã đúng → áp được. SĐT
// mới trên cùng IP: IP đủ 6 lần sai thì chặn cả SĐT chưa sai lần nào. Tạo đơn của SĐT bị chặn có mã → 429, không đơn,
// không lưu theo khóa; không mã → 201. Hết cửa sổ 15 phút → mã đúng áp được.
func TestWebCoupon_GuessRateLimited(t *testing.T) {
	t.Parallel()
	o, clk := guessEnv(t)
	p := o.product("PGM", "[]")
	v := p.Variants[0].ID
	o.stock(v, 10)
	line := quoteLine(v, 1, 350000)
	const ipA, ipB = "203.0.113.7", "198.51.100.9"

	for i := range 3 {
		if applied, reason := couponOf(t, o.quoteAs(line, fmt.Sprintf("SAI%d", i), phone(40), ipA)); applied || reason != "not_found" {
			t.Fatalf("mã sai %d: applied %v, %s", i, applied, reason)
		}
	}
	locked(t, o.quoteAs(line, "SAI9", phone(40), ipA), "mã sai lần 4")
	locked(t, o.quoteAs(line, "DUNG1", phone(40), ipA), "mã đúng sau khi bị chặn")
	if rec := o.quoteAs(line, "", phone(40), ipA); rec.Code != http.StatusOK {
		t.Fatalf("báo giá không mã khi bị chặn: %d %s", rec.Code, rec.Body)
	}
	if applied, _ := couponOf(t, o.quoteAs(line, "DUNG1", phone(41), ipB)); !applied {
		t.Fatal("SĐT khác, IP khác: mã đúng không áp được")
	}

	// IP A đã 3 lần sai: thêm 3 lần sai từ 3 SĐT mới → IP đủ 6 → SĐT chưa sai lần nào trên IP A cũng bị chặn.
	for i := range 3 {
		couponOf(t, o.quoteAs(line, "SAIX", phone(50+i), ipA))
	}
	locked(t, o.quoteAs(line, "DUNG1", phone(60), ipA), "SĐT mới trên IP đã quá ngưỡng")

	blocked := webOrder{
		ref: "WEB-CHAN-1", phone: phone(40), coupon: "DUNG1", ip: ipB, via: order.SubmittedSync, lines: []string{line},
		total: 300000, fp: "x",
	}
	locked(t, o.place(blocked.ref, blocked), "tạo đơn có mã của SĐT bị chặn")
	if o.storedKeys(blocked.ref) != 0 || o.ordersOf(blocked.ref) != 0 {
		t.Fatal("429 đoán mã được lưu theo khóa hoặc tạo đơn")
	}
	plain := o.order(phone(40), "", line)
	plain.ip = ipB
	if rec := o.place(plain.ref, plain); rec.Code != http.StatusCreated {
		t.Fatalf("tạo đơn không mã của SĐT bị chặn: %d %s", rec.Code, rec.Body)
	}

	clk.add(10 * time.Minute)
	if applied, _ := couponOf(t, o.quoteAs(line, "DUNG1", phone(40), ipA)); !applied {
		t.Fatal("hết cửa sổ: mã đúng không áp được")
	}
}

// Tạo đơn gửi thẳng với mã sai → 422 coupon_invalid not_found, được đếm: sau 3 lần, đơn có mã ĐÚNG của SĐT đó → 429
// (không đơn); SĐT khác tạo đơn mã đúng → 201. Website không gửi clientIp → lần sai đếm theo khóa dịch vụ (IP 6):
// 3 lần sai nữa của SĐT khác làm báo giá có mã không IP bị chặn, báo giá có IP vẫn qua.
func TestWebCoupon_GuessRateLimitedOnCreate(t *testing.T) {
	t.Parallel()
	o, _ := guessEnv(t)
	p := o.product("PGC", "[]")
	v := p.Variants[0].ID
	o.stock(v, 10)
	line := quoteLine(v, 1, 350000)
	plain := o.order(phone(70), "", line) // báo giá không mã: không đếm
	for i := range 3 {
		w := plain
		w.ref, w.coupon = fmt.Sprintf("WEB-SAI-%d", i), fmt.Sprintf("SAI%d", i)
		if r := refused(t, o.place(w.ref, w), http.StatusUnprocessableEntity, "coupon_invalid"); r.Reason != "not_found" {
			t.Fatalf("lý do: %+v", r)
		}
	}
	good := o.order(phone(71), "DUNG1", line)
	blocked := good
	blocked.ref, blocked.phone = "WEB-CHAN-2", phone(70)
	locked(t, o.place(blocked.ref, blocked), "đơn mã đúng của SĐT đã 3 lần sai")
	if o.ordersOf(blocked.ref) != 0 {
		t.Fatal("đơn bị chặn vẫn được tạo")
	}
	if rec := o.place(good.ref, good); rec.Code != http.StatusCreated {
		t.Fatalf("SĐT khác, mã đúng: %d %s", rec.Code, rec.Body)
	}

	// Không clientIp: 3 lần sai trước + 3 lần sai của SĐT 72 → khóa dịch vụ đủ 6.
	for i := range 3 {
		w := o.order(phone(72), "", line)
		w.coupon = fmt.Sprintf("SAIK%d", i)
		refused(t, o.place(w.ref, w), http.StatusUnprocessableEntity, "coupon_invalid")
	}
	locked(t, o.quoteAs(line, "DUNG1", phone(73), ""), "báo giá không IP khi khóa dịch vụ quá ngưỡng")
	if applied, _ := couponOf(t, o.quoteAs(line, "DUNG1", phone(73), "192.0.2.1")); !applied {
		t.Fatal("báo giá có IP: mã đúng không áp được")
	}
}
