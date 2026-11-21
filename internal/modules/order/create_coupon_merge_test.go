package order_test

// Task 430 (review M2 A-02, C-01): lên đơn dùng mã gắn khách và mã giới hạn lượt mỗi khách sau khi gộp khách.

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// twoCustomers tạo hai khách riêng (mỗi người là gốc của chính mình).
func (e *orderEnv) twoCustomers() (a, b uuid.UUID) {
	e.t.Helper()
	ids := make([]uuid.UUID, 2)
	if err := e.app(func(tx pgx.Tx) error {
		for i, name := range []string{"Chị A", "Chị B"} {
			c, err := customerWriter().CreateCustomer(e.t.Context(), tx, customer.CreateInput{FullName: name},
				customer.Actor{ID: e.actor.UserID}, e.now())
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			ids[i] = c.ID
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return ids[0], ids[1]
}

// limitedCoupon tạo mã code 10%, 1 lượt mỗi khách, gắn khách bound (uuid.Nil: không gắn).
func (e *orderEnv) limitedCoupon(code string, bound uuid.UUID) {
	e.t.Helper()
	c := sale10(code, 0)
	c.perCustomer, c.boundCustomer = 1, bound
	e.coupon(c)
}

func (e *orderEnv) mergeInto(survivor, merged uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := customerWriter().MergeCustomers(e.t.Context(), tx, survivor, merged, customer.MatchedByStaff,
			customer.Actor{ID: e.actor.UserID}, e.now())
		return err //nolint:wrapcheck // test
	})
}

// couponOrder là đơn 1 × biến thể v của hồ sơ khách cust dùng mã code.
func (e *orderEnv) couponOrder(v, cust uuid.UUID, code string) order.CreateInput {
	in := e.m2Input(0, ln(v, 1, 0))
	in.CouponCode, in.Customer = code, customer.OrderCustomerInput{CustomerID: &cust}
	return in
}

// Mã gắn khách A, 1 lượt mỗi khách; gộp A vào B. Đơn của hồ sơ B dùng được mã (gốc hiện tại của A là B); đơn tiếp theo
// của nhóm (hồ sơ A) → customer_limit, cả nhóm chỉ một lượt.
func TestCreateCoupon_BoundCustomerAfterMerge(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	a, b := e.twoCustomers()
	e.limitedCoupon("RIENGA", a)
	if err := e.mergeInto(b, a); err != nil {
		t.Fatal(err)
	}
	v := e.variant(250000, 5)
	if o := e.mustCreateM2(e.couponOrder(v, b, "RIENGA")).Order; o.CouponDiscount != 25000 {
		t.Fatalf("giảm mã %d, mong 25.000", o.CouponDiscount)
	}
	if _, err := e.createM2(e.couponOrder(v, a, "RIENGA")); rejectReason(err) != promotion.ReasonCustomerLimit {
		t.Fatalf("đơn thứ hai của nhóm: %v, mong customer_limit", err)
	}
}

// Gộp A (đã dùng mã 1 lượt mỗi khách) vào B chạy song song với đơn của B dùng mã, 10 vòng: không deadlock
// (pg_stat_database.deadlocks không tăng), không lỗi khác; kết quả là một trong hai thứ tự: đơn trước gộp → đơn đạt (B
// còn riêng), gộp trước đơn → customer_limit (nhóm đã dùng).
func TestCreateCoupon_MergeRaceNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	before := deadlocks()
	v := e.variant(250000, 50)
	for i := range 10 {
		a, b := e.twoCustomers()
		code := fmt.Sprintf("DUA%02d", i)
		e.limitedCoupon(code, uuid.Nil)
		e.mustCreateM2(e.couponOrder(v, a, code))
		var orderErr, mergeErr error
		var wg sync.WaitGroup
		wg.Go(func() { _, orderErr = e.createM2(e.couponOrder(v, b, code)) })
		wg.Go(func() { mergeErr = e.mergeInto(b, a) })
		wg.Wait()
		if mergeErr != nil {
			t.Fatalf("vòng %d: gộp %v", i, mergeErr)
		}
		if orderErr != nil && rejectReason(orderErr) != promotion.ReasonCustomerLimit {
			t.Fatalf("vòng %d: đơn %v", i, orderErr)
		}
	}
	if after := deadlocks(); after != before {
		t.Fatalf("deadlock: %d → %d", before, after)
	}
}
