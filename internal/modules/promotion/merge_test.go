package promotion_test

// Task 430 (review M2 A-02, C-01): mã gắn khách và giới hạn lượt mỗi khách theo nhóm khách hiện tại (gốc hiện tại),
// đúng sau gộp và sau hoàn tác gộp.

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

func (e *env) customerWriter() customer.Writer {
	return customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
}

// merge gộp nhóm của merged vào nhóm của survivor; trả mã lần gộp.
func (e *env) merge(survivor, merged uuid.UUID) uuid.UUID {
	e.t.Helper()
	var m customer.Merge
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		m, err = e.customerWriter().MergeCustomers(e.t.Context(), tx, survivor, merged, customer.MatchedByStaff,
			customer.Actor{Type: customer.ActorStaff, ID: e.user}, time.Now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return m.ID
}

func (e *env) undoMerge(mergeID uuid.UUID) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		return e.customerWriter().UndoMerge(e.t.Context(), tx, mergeID,
			customer.Actor{Type: customer.ActorStaff, ID: e.user}, time.Now())
	}); err != nil {
		e.t.Fatal(err)
	}
}

// inputOf là đầu vào của đơn gắn hồ sơ profile (gốc đọc hiện tại), tạm tính 400000.
func (e *env) inputOf(profile uuid.UUID) promotion.EvalInput {
	e.t.Helper()
	in := e.input(profile, 400000)
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		in.CustomerRootID, err = customer.RootOf(e.t.Context(), tx, profile)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return in
}

// onePerCustomer tạo mã 1 lượt mỗi khách và hai khách riêng A, B.
func (e *env) onePerCustomer() (id uuid.UUID, code string, a, b uuid.UUID) {
	e.t.Helper()
	id = e.coupon(spec{perCustomer: ptr(int32(1))})
	return id, e.code(id), e.customer(), e.customer()
}

func (e *env) check(code string, in promotion.EvalInput) promotion.CheckResult {
	e.t.Helper()
	var r promotion.CheckResult
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = promotion.Check(e.t.Context(), tx, code, in)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return r
}

// Mã gắn khách A; gộp A vào B: đơn của nhóm (hồ sơ B hay A, gốc B) dùng được mã ở Check, Redeem, Reprice. Hoàn tác:
// nhóm B không còn chứa A → bound_other; hồ sơ A dùng được. Mã gắn B (gốc giữ lại): hồ sơ A sau gộp dùng được, sau hoàn
// tác thì không.
func TestBoundCustomer_FollowsMerge(t *testing.T) {
	e := newEnv(t)
	a, b := e.customer(), e.customer()
	boundA := e.code(e.coupon(spec{boundCustomer: &a}))
	boundB := e.code(e.coupon(spec{boundCustomer: &b}))
	if r := e.check(boundA, e.inputOf(b)); r.OK || r.Reason != promotion.ReasonBoundOther {
		t.Fatalf("trước gộp, B dùng mã của A: %+v", r)
	}
	m := e.merge(b, a)
	for name, p := range map[string]uuid.UUID{"hồ sơ B": b, "hồ sơ A": a} {
		if r := e.check(boundA, e.inputOf(p)); !r.OK {
			t.Fatalf("sau gộp, %s Check mã của A: %+v", name, r)
		}
		if r := e.check(boundB, e.inputOf(p)); !r.OK {
			t.Fatalf("sau gộp, %s Check mã của B: %+v", name, r)
		}
	}
	o := e.order(b)
	if _, err := e.redeem(boundA, o, e.inputOf(b)); err != nil {
		t.Fatalf("sau gộp, Redeem mã của A cho đơn của B: %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.Reprice(e.t.Context(), tx, o, e.inputOf(b).Base)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("sau gộp, Reprice: %v", err)
	}
	if _, err := e.release(o, "khách hủy"); err != nil {
		t.Fatal(err)
	}
	e.undoMerge(m)
	if r := e.check(boundA, e.inputOf(b)); r.OK || r.Reason != promotion.ReasonBoundOther {
		t.Fatalf("sau hoàn tác, B dùng mã của A: %+v", r)
	}
	if r := e.check(boundA, e.inputOf(a)); !r.OK {
		t.Fatalf("sau hoàn tác, A dùng mã của A: %+v", r)
	}
	if r := e.check(boundB, e.inputOf(a)); r.OK || r.Reason != promotion.ReasonBoundOther {
		t.Fatalf("sau hoàn tác, A dùng mã của B: %+v", r)
	}
	if _, err := e.redeem(boundB, e.order(a), e.inputOf(a)); reason(err) != promotion.ReasonBoundOther {
		t.Fatalf("sau hoàn tác, Redeem mã của B cho A: %v", err)
	}
}

// Giới hạn 1 lượt mỗi khách: A đã dùng; gộp A vào B → nhóm đã dùng hết (Check, Redeem cho hồ sơ B hay A đều
// customer_limit). Hoàn tác → B dùng được, A vẫn hết.
func TestPerCustomerLimit_FollowsMerge(t *testing.T) {
	e := newEnv(t)
	id, code, a, b := e.onePerCustomer()
	if _, err := e.redeem(code, e.order(a), e.inputOf(a)); err != nil {
		t.Fatal(err)
	}
	m := e.merge(b, a)
	if r := e.check(code, e.inputOf(b)); r.OK || r.Reason != promotion.ReasonCustomerLimit {
		t.Fatalf("sau gộp, Check của B: %+v", r)
	}
	for name, p := range map[string]uuid.UUID{"hồ sơ B": b, "hồ sơ A": a} {
		if _, err := e.redeem(code, e.order(p), e.inputOf(p)); reason(err) != promotion.ReasonCustomerLimit {
			t.Fatalf("sau gộp, Redeem cho %s: %v, mong customer_limit", name, err)
		}
	}
	if got := e.usedCount(id); got != 1 {
		t.Fatalf("used_count %d, mong 1", got)
	}
	e.undoMerge(m)
	if _, err := e.redeem(code, e.order(b), e.inputOf(b)); err != nil {
		t.Fatalf("sau hoàn tác, Redeem cho B: %v", err)
	}
	if _, err := e.redeem(code, e.order(a), e.inputOf(a)); reason(err) != promotion.ReasonCustomerLimit {
		t.Fatalf("sau hoàn tác, Redeem cho A: %v, mong customer_limit", err)
	}
	if e.customerUsed(id, a) != 1 || e.customerUsed(id, b) != 1 {
		t.Fatalf("used của A, B: %d, %d; mong 1, 1", e.customerUsed(id, a), e.customerUsed(id, b))
	}
}

// Giới hạn 2: A và B mỗi người đã dùng 1 khi còn riêng; gộp → nhóm đủ 2 (tổng, không phải lớn nhất).
func TestPerCustomerLimit_SumsGroup(t *testing.T) {
	e := newEnv(t)
	code := e.code(e.coupon(spec{perCustomer: ptr(int32(2))}))
	a, b := e.customer(), e.customer()
	for _, p := range []uuid.UUID{a, b} {
		if _, err := e.redeem(code, e.order(p), e.inputOf(p)); err != nil {
			t.Fatal(err)
		}
	}
	e.merge(b, a)
	if r := e.check(code, e.inputOf(b)); r.OK || r.Reason != promotion.ReasonCustomerLimit {
		t.Fatalf("Check của nhóm đã dùng 2: %+v", r)
	}
	if _, err := e.redeem(code, e.order(a), e.inputOf(a)); reason(err) != promotion.ReasonCustomerLimit {
		t.Fatalf("Redeem của nhóm đã dùng 2: %v", err)
	}
}

// Lượt dùng lúc đang gộp ghi theo hồ sơ của đơn: gộp A vào B, đơn của hồ sơ A dùng mã; hoàn tác → lượt thuộc A, B dùng
// được.
func TestPerCustomerLimit_UseDuringMergeThenUndo(t *testing.T) {
	e := newEnv(t)
	id, code, a, b := e.onePerCustomer()
	m := e.merge(b, a)
	if _, err := e.redeem(code, e.order(a), e.inputOf(a)); err != nil {
		t.Fatal(err)
	}
	if r := e.check(code, e.inputOf(b)); r.OK || r.Reason != promotion.ReasonCustomerLimit {
		t.Fatalf("lúc gộp, Check của B: %+v", r)
	}
	e.undoMerge(m)
	if r := e.check(code, e.inputOf(b)); !r.OK {
		t.Fatalf("sau hoàn tác, Check của B: %+v", r)
	}
	if r := e.check(code, e.inputOf(a)); r.OK || r.Reason != promotion.ReasonCustomerLimit {
		t.Fatalf("sau hoàn tác, Check của A: %+v", r)
	}
	if got := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1 AND customer_id = $2`, id, a); got != 1 {
		t.Fatal("lượt của đơn phải ghi hồ sơ của đơn (A)")
	}
}

// Trả lượt sau gộp: lượt của A (ghi trước gộp) trả về đúng dòng của A; nhóm dùng lại được.
func TestRelease_AfterMerge(t *testing.T) {
	e := newEnv(t)
	id, code, a, b := e.onePerCustomer()
	o := e.order(a)
	if _, err := e.redeem(code, o, e.inputOf(a)); err != nil {
		t.Fatal(err)
	}
	e.merge(b, a)
	if ok, err := e.release(o, "khách hủy"); err != nil || !ok {
		t.Fatalf("Release = %v, %v", ok, err)
	}
	if e.customerUsed(id, a) != 0 {
		t.Fatalf("used của A %d, mong 0", e.customerUsed(id, a))
	}
	if _, err := e.redeem(code, e.order(b), e.inputOf(b)); err != nil {
		t.Fatalf("nhóm dùng lại sau trả lượt: %v", err)
	}
}

// Redeem thiếu hồ sơ của đơn (CustomerID rỗng) → lỗi lập trình, không phải từ chối; không đổi lượt.
func TestRedeem_RequiresCustomerID(t *testing.T) {
	e := newEnv(t)
	id := e.coupon(spec{})
	cust := e.customer()
	in := e.input(cust, 400000)
	in.CustomerID = uuid.Nil
	_, err := e.redeem(e.code(id), e.order(cust), in)
	if err == nil || errors.Is(err, promotion.ErrCouponRejected) {
		t.Fatalf("lỗi = %v, mong lỗi lập trình", err)
	}
	if e.usedCount(id) != 0 {
		t.Fatal("đổi lượt khi thiếu khách")
	}
}
