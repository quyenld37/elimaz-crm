package promotion_test

import (
	"errors"
	"flag"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid 200 (kế hoạch Task M2-06), trừ khi người chạy đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	os.Exit(m.Run())
}

// env: một database thử, tenant A có người bán (ELIMAZ, PARTNER1) và kho MAIN cho đơn thử.
type env struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	shop   *invtest.Shop
	user   uuid.UUID
	seq    atomic.Int64 // version giả của đơn cho mỗi lần Redeem (bên gọi thật tăng version đơn trước khi dùng mã)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.user = e.staff.User(t, e.tenant, "nv@elimaz.test", "active")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, e.tenant)
		return nil
	})
	return e
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của e.
func (e *env) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// customer tạo một khách (hồ sơ gốc là chính nó).
func (e *env) customer() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		id, err = customer.Create(e.t.Context(), tx, "Chị Lan")
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// order ghi một đơn rỗng (tổng 0) của khách cust bằng chủ bảng; promotion chỉ cần mã đơn cho khóa ngoại.
func (e *env) order(cust uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO ord.orders
			(tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, ship_address, subtotal,
			 cod_amount, grand_total, warehouse_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1,
			        'DH' || lpad(nextval('ord.order_code_seq')::text, 7, '0'), $2, 'manual', $3, 'confirmed', '{}',
			        0, 0, 0, $4)
			RETURNING id`, e.shop.Seller, cust, uuid.New(), e.shop.Warehouse).Scan(&id)
	})
	return id
}

// spec là các cột của một mã thử; trường rỗng lấy mặc định (10%, trần 50000, hạn từ 1 giờ trước tới 30 ngày sau).
type spec struct {
	code          string
	kind          string
	value         int64
	maxDiscount   *int64
	minSubtotal   int64
	maxUses       *int32
	perCustomer   *int32
	keepOnCancel  bool // release_on_cancel = false
	status        string
	boundPhone    string
	boundCustomer *uuid.UUID
	channels      []string
}

// coupon ghi mã s bằng chủ bảng; trả mã (id).
func (e *env) coupon(s spec) uuid.UUID {
	e.t.Helper()
	if s.code == "" {
		s.code = "M" + strings.ToUpper(uuid.NewString()[:8])
	}
	if s.kind == "" {
		s.kind, s.value = promotion.KindPercent, 1000
		if s.maxDiscount == nil {
			s.maxDiscount = ptr(int64(50000))
		}
	}
	if s.status == "" {
		s.status = promotion.StatusActive
	}
	if s.channels == nil {
		s.channels = []string{}
	}
	var phone *string
	if s.boundPhone != "" {
		phone = &s.boundPhone
	}
	now := time.Now()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO promotion.coupons
			(tenant_id, code, name, discount_kind, value, max_discount, min_subtotal, channels, bound_customer_id,
			 bound_phone, max_uses, per_customer_limit, release_on_cancel, valid_from, valid_to, status, created_by)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'Mã thử', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        $12, $13, $14, $15)
			RETURNING id`, s.code, s.kind, s.value, s.maxDiscount, s.minSubtotal, s.channels, s.boundCustomer, phone,
			s.maxUses, s.perCustomer, !s.keepOnCancel, now.Add(-time.Hour), now.Add(30*24*time.Hour), s.status,
			e.user).Scan(&id)
	})
	return id
}

func (e *env) code(id uuid.UUID) string {
	e.t.Helper()
	var c string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT code::text FROM promotion.coupons WHERE id = $1`, id).Scan(&c)
	})
	return c
}

// input là đầu vào của khách cust, tạm tính base, kênh manual, người bán ELIMAZ, giờ hiện tại.
func (e *env) input(cust uuid.UUID, base int64) promotion.EvalInput {
	return promotion.EvalInput{
		Base: base, Channel: "manual", CustomerRootID: cust, Phone: logging.Phone("+84901111111"),
		SellerID: e.shop.Seller, Now: time.Now(),
	}
}

// writer là promotion.Writer đủ Events, Queue cho Redeem.
func (e *env) writer() promotion.Writer {
	return promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
}

func (e *env) redeem(code string, orderID uuid.UUID, in promotion.EvalInput) (promotion.Redemption, error) {
	var r promotion.Redemption
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = e.writer().Redeem(e.t.Context(), tx, code,
			promotion.OrderRef{TenantID: e.tenant, ID: orderID, Version: e.seq.Add(1)}, in)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// redeemed tạo mã s, một khách, một đơn của khách đó rồi dùng mã cho đơn (tạm tính 400000); trả mã, khách, đơn.
func (e *env) redeemed(s spec) (id, cust, orderID uuid.UUID) {
	e.t.Helper()
	id, cust = e.coupon(s), e.customer()
	orderID = e.order(cust)
	if _, err := e.redeem(e.code(id), orderID, e.input(cust, 400000)); err != nil {
		e.t.Fatal(err)
	}
	return id, cust, orderID
}

func (e *env) release(orderID uuid.UUID, reason string) (bool, error) {
	var ok bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		ok, err = promotion.Release(e.t.Context(), tx, orderID, reason)
		return err //nolint:wrapcheck // test
	})
	return ok, err
}

func (e *env) detach(orderID uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error { return promotion.Detach(e.t.Context(), tx, orderID) })
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func (e *env) usedCount(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, id)
}

// customerUsed trả used của (mã, khách); không có dòng → 0.
func (e *env) customerUsed(coupon, cust uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(max(used), 0) FROM promotion.coupon_customer_uses
		WHERE coupon_id = $1 AND customer_id = $2`, coupon, cust)
}

func reason(err error) promotion.Reason {
	if re, ok := errors.AsType[*promotion.RejectError](err); ok {
		return re.Reason
	}
	return ""
}

func pgCode(err error) string {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pe.Code
	}
	return ""
}

func TestRedeem_ConcurrentMaxUses(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{maxUses: ptr(int32(1))})
	code := e.code(id)
	const n = 20
	orders, custs := make([]uuid.UUID, n), make([]uuid.UUID, n)
	for i := range n {
		custs[i] = e.customer()
		orders[i] = e.order(custs[i])
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { _, errs[i] = e.redeem(code, orders[i], e.input(custs[i], 400000)) })
	}
	wg.Wait()
	ok, exhausted := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case reason(err) == promotion.ReasonExhausted && errors.Is(err, promotion.ErrCouponRejected):
			exhausted++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || exhausted != n-1 {
		t.Fatalf("thành công %d, exhausted %d; mong 1, %d", ok, exhausted, n-1)
	}
	if got := e.usedCount(id); got != 1 {
		t.Fatalf("used_count %d, mong 1", got)
	}
	if got := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1`, id); got != 1 {
		t.Fatalf("%d lượt, mong 1", got)
	}
}

func TestRedeem_PerCustomerLimit(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{perCustomer: ptr(int32(1))})
	code := e.code(id)
	cust := e.customer()
	o1, o2 := e.order(cust), e.order(cust)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, o := range []uuid.UUID{o1, o2} {
		wg.Go(func() { _, errs[i] = e.redeem(code, o, e.input(cust, 400000)) })
	}
	wg.Wait()
	nOK, nLimit := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			nOK++
		case reason(err) == promotion.ReasonCustomerLimit:
			nLimit++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if nOK != 1 || nLimit != 1 {
		t.Fatalf("thành công %d, customer_limit %d; mong 1, 1", nOK, nLimit)
	}
	other := e.customer()
	if _, err := e.redeem(code, e.order(other), e.input(other, 400000)); err != nil {
		t.Fatalf("khách khác: %v", err)
	}
	if got := e.usedCount(id); got != 2 {
		t.Fatalf("used_count %d, mong 2", got)
	}
	if e.customerUsed(id, cust) != 1 || e.customerUsed(id, other) != 1 {
		t.Fatalf("used của khách %d, %d; mong 1, 1", e.customerUsed(id, cust), e.customerUsed(id, other))
	}
}

func TestRedeem_ResultAndRejects(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{code: "GIAM10"})
	cust := e.customer()
	o := e.order(cust)
	// Mã không phân biệt hoa thường (citext).
	r, err := e.redeem("giam10", o, e.input(cust, 660000))
	if err != nil {
		t.Fatal(err)
	}
	if r != (promotion.Redemption{CouponID: id, Code: "GIAM10", Discount: 50000}) {
		t.Fatalf("Redemption = %+v", r)
	}
	if got := e.count(`SELECT discount FROM promotion.coupon_redemptions WHERE order_id = $1 AND status = 'active'
		AND customer_id = $2`, o, cust); got != 50000 {
		t.Fatalf("discount đã ghi %d", got)
	}

	o2 := e.order(cust)
	if _, err := e.redeem("KHONGCO", o2, e.input(cust, 1)); reason(err) != promotion.ReasonNotFound {
		t.Fatalf("mã không có: %v", err)
	}
	off := e.coupon(spec{status: promotion.StatusDisabled})
	if _, err := e.redeem(e.code(off), o2, e.input(cust, 400000)); reason(err) != promotion.ReasonDisabled {
		t.Fatalf("mã tắt: %v", err)
	}
	if e.usedCount(off) != 0 {
		t.Fatal("mã bị từ chối vẫn tăng lượt")
	}
	web := e.coupon(spec{channels: []string{"web"}})
	if _, err := e.redeem(e.code(web), o2, e.input(cust, 400000)); reason(err) != promotion.ReasonChannel {
		t.Fatalf("kênh: %v", err)
	}
	bound := e.coupon(spec{boundPhone: "+84912345678"})
	_, err = e.redeem(e.code(bound), o2, e.input(cust, 400000))
	if reason(err) != promotion.ReasonBoundOther {
		t.Fatalf("gắn SĐT: %v", err)
	}
	if strings.Contains(err.Error(), "912345678") {
		t.Fatal("lỗi chứa SĐT")
	}
	// Đơn đã có mã đang dùng (Q27): mã thứ hai, hay chính mã đó lần nữa, bị chặn; không đổi lượt.
	second := e.coupon(spec{})
	if _, err := e.redeem(e.code(second), o, e.input(cust, 400000)); !errors.Is(err, promotion.ErrOrderHasCoupon) {
		t.Fatalf("mã thứ hai cho cùng đơn: %v", err)
	}
	if _, err := e.redeem("GIAM10", o, e.input(cust, 400000)); !errors.Is(err, promotion.ErrOrderHasCoupon) {
		t.Fatalf("cùng mã lần hai cho cùng đơn: %v", err)
	}
	if e.usedCount(second) != 0 || e.usedCount(id) != 1 {
		t.Fatal("lần dùng bị chặn vẫn đổi lượt")
	}
	// Unique một lượt active mỗi đơn ở database là chốt cuối.
	if err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO promotion.coupon_redemptions
			(tenant_id, coupon_id, order_id, customer_id, discount, status)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 0, 'active')`, second, o, cust)
		return err //nolint:wrapcheck // test
	}); pgCode(err) != "23505" {
		t.Fatalf("mã thứ hai cho cùng đơn: %v", err)
	}
}

func TestRelease(t *testing.T) {
	e := newEnv(t)
	id, cust, o := e.redeemed(spec{perCustomer: ptr(int32(1))})
	ok, err := e.release(o, "customer_changed_mind")
	if err != nil || !ok {
		t.Fatalf("Release = %v, %v", ok, err)
	}
	if e.usedCount(id) != 0 || e.customerUsed(id, cust) != 0 {
		t.Fatalf("sau trả lượt: used_count %d, used %d", e.usedCount(id), e.customerUsed(id, cust))
	}
	if got := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE order_id = $1 AND status = 'released'
		AND released_at IS NOT NULL AND release_reason = 'customer_changed_mind'`, o); got != 1 {
		t.Fatalf("dòng released: %d", got)
	}
	ok, err = e.release(o, "customer_changed_mind")
	if err != nil || ok {
		t.Fatalf("Release lần hai = %v, %v", ok, err)
	}
	if e.usedCount(id) != 0 || e.customerUsed(id, cust) != 0 {
		t.Fatal("Release lần hai đổi lượt")
	}

	keep := e.coupon(spec{keepOnCancel: true})
	o2 := e.order(cust)
	if _, err := e.redeem(e.code(keep), o2, e.input(cust, 400000)); err != nil {
		t.Fatal(err)
	}
	ok, err = e.release(o2, "other")
	if err != nil || ok {
		t.Fatalf("mã tắt cờ: Release = %v, %v", ok, err)
	}
	if e.usedCount(keep) != 1 || e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE order_id = $1
		AND status = 'active'`, o2) != 1 {
		t.Fatal("mã tắt cờ: lượt bị trả")
	}
	// Đơn không có mã.
	if ok, err := e.release(e.order(cust), "other"); err != nil || ok {
		t.Fatalf("đơn không mã: %v, %v", ok, err)
	}
}

func TestDetach_ThenRedeemAgain(t *testing.T) {
	e := newEnv(t)
	id, cust, o := e.redeemed(spec{keepOnCancel: true, perCustomer: ptr(int32(1)), maxUses: ptr(int32(1))})
	if err := e.detach(o); err != nil {
		t.Fatal(err)
	}
	if e.usedCount(id) != 0 || e.customerUsed(id, cust) != 0 {
		t.Fatal("Detach không trả lượt (kể cả khi release_on_cancel tắt)")
	}
	if err := e.detach(o); err != nil {
		t.Fatalf("Detach lần hai: %v", err)
	}
	r, err := e.redeem(e.code(id), o, e.input(cust, 660000))
	if err != nil {
		t.Fatalf("Redeem lại: %v", err)
	}
	if r.Discount != 50000 {
		t.Fatalf("discount %d", r.Discount)
	}
	if got := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE order_id = $1`, o); got != 1 {
		t.Fatalf("%d dòng lượt, mong 1", got)
	}
	if got := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE order_id = $1 AND status = 'active'
		AND released_at IS NULL AND release_reason IS NULL AND discount = 50000`, o); got != 1 {
		t.Fatal("dòng cũ không về active sạch")
	}
	if e.usedCount(id) != 1 || e.customerUsed(id, cust) != 1 {
		t.Fatalf("used_count %d, used %d; mong 1, 1", e.usedCount(id), e.customerUsed(id, cust))
	}
}

func TestReprice(t *testing.T) {
	e := newEnv(t)
	id, cust, o := e.redeemed(spec{minSubtotal: 300000})
	reprice := func(base int64) (int64, error) {
		var d int64
		err := e.app(func(tx pgx.Tx) error {
			var err error
			d, err = promotion.Reprice(e.t.Context(), tx, o, e.input(cust, base))
			return err //nolint:wrapcheck // test
		})
		return d, err
	}
	d, err := reprice(660000)
	if err != nil || d != 50000 {
		t.Fatalf("Reprice = %d, %v", d, err)
	}
	if got := e.count(`SELECT discount FROM promotion.coupon_redemptions WHERE order_id = $1`, o); got != 50000 {
		t.Fatalf("discount đã ghi %d", got)
	}
	if _, err := reprice(299999); reason(err) != promotion.ReasonMinSubtotal {
		t.Fatalf("dưới min_subtotal: %v", err)
	}
	if e.usedCount(id) != 1 {
		t.Fatal("Reprice đổi lượt")
	}
	// Đơn không có mã: phần giảm 0.
	o2 := e.order(cust)
	err = e.app(func(tx pgx.Tx) error {
		d, err = promotion.Reprice(e.t.Context(), tx, o2, e.input(cust, 100))
		return err //nolint:wrapcheck // test
	})
	if err != nil || d != 0 {
		t.Fatalf("đơn không mã: %d, %v", d, err)
	}
}

func TestCheck(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{maxUses: ptr(int32(2)), perCustomer: ptr(int32(1))})
	code := e.code(id)
	cust := e.customer()
	check := func(code string, cust uuid.UUID, base int64) promotion.CheckResult {
		t.Helper()
		var r promotion.CheckResult
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			r, err = promotion.Check(e.t.Context(), tx, code, e.input(cust, base))
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := check(code, cust, 400000)
	if !r.OK || r.Discount != 40000 || r.Reason != "" || r.RemainingUses == nil || *r.RemainingUses != 2 ||
		r.CouponID != id {
		t.Fatalf("Check = %+v", r)
	}
	if e.usedCount(id) != 0 {
		t.Fatal("Check đổi lượt")
	}
	if _, err := e.redeem(code, e.order(cust), e.input(cust, 400000)); err != nil {
		t.Fatal(err)
	}
	if r := check(code, cust, 400000); r.OK || r.Reason != promotion.ReasonCustomerLimit || r.Discount != 0 {
		t.Fatalf("khách đã dùng: %+v", r)
	}
	other := e.customer()
	if r := check(code, other, 400000); !r.OK || *r.RemainingUses != 1 {
		t.Fatalf("khách khác: %+v", r)
	}
	if _, err := e.redeem(code, e.order(other), e.input(other, 400000)); err != nil {
		t.Fatal(err)
	}
	if r := check(code, e.customer(), 400000); r.OK || r.Reason != promotion.ReasonExhausted ||
		*r.RemainingUses != 0 {
		t.Fatalf("hết lượt: %+v", r)
	}
	if r := check("KHONGCO", cust, 400000); r.OK || r.Reason != promotion.ReasonNotFound {
		t.Fatalf("không có: %+v", r)
	}
	if r := check(e.code(e.coupon(spec{minSubtotal: 500000})), cust, 400000); r.OK ||
		r.Reason != promotion.ReasonMinSubtotal || r.RemainingUses != nil {
		t.Fatalf("min_subtotal: %+v", r)
	}
}

func TestSchema_UsedCountCheck(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{maxUses: ptr(int32(1))})
	for _, q := range []string{
		`UPDATE promotion.coupons SET used_count = -1 WHERE id = $1`,
		`UPDATE promotion.coupons SET used_count = 2 WHERE id = $1`,
	} {
		err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q, id)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != "23514" {
			t.Fatalf("%s: %v, mong vi phạm CHECK", q, err)
		}
	}
}

func TestSchema_CouponsView(t *testing.T) {
	e := newEnv(t)
	testkit.CheckViews(t, e.staff.Owner, "promotion", map[string]string{
		"coupons_v": "id, tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal, " +
			"channels, seller_id, max_uses, used_count, per_customer_limit, valid_from, valid_to, status, " +
			"created_at, updated_at",
	})
	for _, c := range testkit.Columns(t, e.staff.Owner, "promotion", "coupons_v") {
		if c.Name == "bound_phone" || c.Name == "bound_customer_id" {
			t.Fatalf("view có cột %s", c.Name)
		}
	}
	e.coupon(spec{})
	if got := e.count(`SELECT count(*) FROM promotion.coupons_v`); got != 1 {
		t.Fatalf("tenant A thấy %d mã", got)
	}
	b := e.staff.Tenant(t, "B")
	var n int
	if err := tenancy.WithTenant(t.Context(), e.staff.App, b, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM promotion.coupons_v`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("tenant B thấy %d mã của A", n)
	}
}

// TestSchema_Rules: bốn bảng promotion đúng luật tenant (RLS, unique, khóa ngoại ghép) và luật (g) người bán
// (coupons_code_key có ghi chú seller-exempt).
func TestSchema_Rules(t *testing.T) {
	e := newEnv(t)
	testkit.CheckSchemaRulesClean(t, e.staff.Owner)
}

// Redeem phát coupon.used (aggregate order_coupon = mã đơn, seq = version đơn của bên gọi; payload chỉ mã và số);
// mã bị từ chối không phát; Writer thiếu Events hoặc Queue → lỗi, không dùng lượt.
func TestRedeem_EmitsCouponUsed(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{maxUses: ptr(int32(5))})
	cust := e.customer()
	o := e.order(cust)
	var r promotion.Redemption
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = e.writer().Redeem(t.Context(), tx, e.code(id), promotion.OrderRef{TenantID: e.tenant, ID: o, Version: 2},
			e.input(cust, 660000))
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'coupon.used'
		AND aggregate_type = 'order_coupon' AND aggregate_id = $1 AND seq = 2 AND payload->>'couponId' = $2
		AND payload->>'orderId' = $1 AND payload->>'customerId' = $3 AND (payload->>'discount')::bigint = $4
		AND (SELECT count(*) FROM jsonb_object_keys(payload)) = 4`,
		o.String(), id.String(), cust.String(), r.Discount); n != 1 {
		t.Errorf("sự kiện coupon.used: %d, mong 1", n)
	}
	o2 := e.order(cust)
	if _, err := e.redeem(e.code(id), o2, e.input(cust, 0)); err != nil {
		t.Fatalf("lượt thứ hai (phần giảm 0): %v", err)
	}
	o3 := e.order(cust)
	if _, err := e.redeem("KHONGCO", o3, e.input(cust, 400000)); reason(err) != promotion.ReasonNotFound {
		t.Fatalf("mã không có: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'coupon.used'
		AND aggregate_id = $1`, o3.String()); n != 0 {
		t.Errorf("mã bị từ chối vẫn phát sự kiện: %d", n)
	}
	err := e.app(func(tx pgx.Tx) error {
		ref := promotion.OrderRef{TenantID: e.tenant, ID: o3, Version: 1}
		_, err := promotion.Writer{}.Redeem(t.Context(), tx, e.code(id), ref, e.input(cust, 660000))
		return err //nolint:wrapcheck // test
	})
	if err == nil {
		t.Fatal("Writer thiếu Events, Queue mà Redeem không lỗi")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT used_count FROM promotion.coupons WHERE id = $1`, id); n != 2 {
		t.Errorf("used_count = %d, mong 2", n)
	}
}
