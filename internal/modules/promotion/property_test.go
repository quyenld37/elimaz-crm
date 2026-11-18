package promotion_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/promotion"
)

// propCoupon là mã thử của quy luật.
type propCoupon struct {
	id          uuid.UUID
	code        string
	maxUses     *int32
	perCustomer *int32
	keep        bool // release_on_cancel = false
}

type propOrder struct{ id, cust uuid.UUID }

// model là trạng thái mong đợi: active[i] là chỉ số mã đang dùng của đơn i (-1: không có).
type model struct {
	e       *env
	coupons []propCoupon
	custs   []uuid.UUID
	orders  []propOrder
	active  []int
}

// reasonDup: kết quả mong đợi khi đơn đã có lượt mã active (errOrderHasCoupon).
const reasonDup promotion.Reason = "dup"

// TestRedemption_Invariants (rapid, 200 lượt; quy trình mục 6 câu 11): chuỗi ngẫu nhiên Redeem/Release/Detach trên
// 1–3 mã, 1–5 khách, 1–8 đơn → sau mỗi bước: used_count của mã = số lượt active của mã; used của mỗi (mã, khách) =
// số lượt active của khách đó với mã; không bao giờ vượt max_uses, per_customer_limit; kết quả từng bước khớp mô hình
// (thành công, exhausted, customer_limit, đơn đã có mã, trả lượt hay không).
func TestRedemption_Invariants(t *testing.T) {
	e := newEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		m := newModel(rt, e)
		for range rapid.IntRange(1, 12).Draw(rt, "steps") {
			oi := rapid.IntRange(0, len(m.orders)-1).Draw(rt, "order")
			switch op := rapid.SampledFrom([]string{"redeem", "release", "detach"}).Draw(rt, "op"); op {
			case "redeem":
				m.redeem(rt, oi, rapid.IntRange(0, len(m.coupons)-1).Draw(rt, "coupon"))
			default:
				m.release(rt, oi, op == "detach")
			}
			if msg := m.check(); msg != "" {
				rt.Fatal(msg)
			}
		}
	})
}

func newModel(rt *rapid.T, e *env) *model {
	m := &model{e: e}
	m.coupons = make([]propCoupon, rapid.IntRange(1, 3).Draw(rt, "coupons"))
	for i := range m.coupons {
		c := &m.coupons[i]
		if rapid.Bool().Draw(rt, "hasMax") {
			c.maxUses = ptr(rapid.Int32Range(1, 4).Draw(rt, "maxUses"))
		}
		if rapid.Bool().Draw(rt, "hasLimit") {
			c.perCustomer = ptr(rapid.Int32Range(1, 2).Draw(rt, "perCustomer"))
		}
		c.keep = rapid.Bool().Draw(rt, "keep")
		c.id = e.coupon(spec{maxUses: c.maxUses, perCustomer: c.perCustomer, keepOnCancel: c.keep})
		c.code = e.code(c.id)
	}
	m.custs = make([]uuid.UUID, rapid.IntRange(1, 5).Draw(rt, "customers"))
	for i := range m.custs {
		m.custs[i] = e.customer()
	}
	m.orders = make([]propOrder, rapid.IntRange(1, 8).Draw(rt, "orders"))
	m.active = make([]int, len(m.orders))
	for i := range m.orders {
		c := m.custs[rapid.IntRange(0, len(m.custs)-1).Draw(rt, "orderCustomer")]
		m.orders[i], m.active[i] = propOrder{id: e.order(c), cust: c}, -1
	}
	return m
}

// countActive: số đơn đang dùng mã ci (của khách cust; nil: mọi khách) theo mô hình.
func (m *model) countActive(ci int, cust *uuid.UUID) int32 {
	var n int32
	for oi, a := range m.active {
		if a == ci && (cust == nil || m.orders[oi].cust == *cust) {
			n++
		}
	}
	return n
}

func (m *model) redeem(rt *rapid.T, oi, ci int) {
	o, c := m.orders[oi], m.coupons[ci]
	_, err := m.e.redeem(c.code, o.id, m.e.input(o.cust, 400000))
	var want promotion.Reason
	switch {
	case m.active[oi] >= 0:
		want = reasonDup
	case c.maxUses != nil && m.countActive(ci, nil) >= *c.maxUses:
		want = promotion.ReasonExhausted
	case c.perCustomer != nil && m.countActive(ci, &o.cust) >= *c.perCustomer:
		want = promotion.ReasonCustomerLimit
	}
	switch {
	case want == "" && err == nil:
		m.active[oi] = ci
	case want == reasonDup && errors.Is(err, promotion.ErrOrderHasCoupon):
	case want != "" && want != reasonDup && reason(err) == want:
	default:
		rt.Fatalf("Redeem: lỗi %v, mong %q", err, want)
	}
}

func (m *model) release(rt *rapid.T, oi int, detach bool) {
	o := m.orders[oi]
	var released bool
	var err error
	if detach {
		err = m.e.detach(o.id)
		released = m.active[oi] >= 0
	} else {
		released, err = m.e.release(o.id, "other")
	}
	if err != nil {
		rt.Fatalf("trả lượt (detach %v): %v", detach, err)
	}
	want := m.active[oi] >= 0 && (detach || !m.coupons[m.active[oi]].keep)
	if released != want {
		rt.Fatalf("trả lượt (detach %v) = %v, mong %v", detach, released, want)
	}
	if want {
		m.active[oi] = -1
	}
}

// check so database với mô hình; trả mô tả vi phạm đầu tiên (rỗng nếu đúng).
func (m *model) check() string {
	for ci, c := range m.coupons {
		used := m.e.usedCount(c.id)
		rows := m.e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1
			AND status = 'active'`, c.id)
		if used != rows || int32(used) != m.countActive(ci, nil) { //nolint:gosec // G115: số nhỏ trong test
			return fmt.Sprintf("mã %d: used_count %d, dòng active %d, mô hình %d", ci, used, rows, m.countActive(ci, nil))
		}
		if c.maxUses != nil && used > int(*c.maxUses) {
			return fmt.Sprintf("mã %d vượt max_uses: %d > %d", ci, used, *c.maxUses)
		}
		for _, cust := range m.custs {
			u := m.e.customerUsed(c.id, cust)
			r := m.e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1 AND customer_id = $2
				AND status = 'active'`, c.id, cust)
			if u != r || int32(u) != m.countActive(ci, &cust) { //nolint:gosec // G115: số nhỏ trong test
				return fmt.Sprintf("mã %d, khách %s: used %d, dòng active %d", ci, cust, u, r)
			}
			if c.perCustomer != nil && u > int(*c.perCustomer) {
				return fmt.Sprintf("mã %d vượt per_customer_limit: %d > %d", ci, u, *c.perCustomer)
			}
		}
	}
	return ""
}
