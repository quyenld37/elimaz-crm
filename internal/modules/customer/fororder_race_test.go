package customer_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// 10 yêu cầu tìm-hoặc-tạo song song cùng SĐT mới: đúng 1 khách, 1 danh tính, cả 10 cùng CustomerID; không hồ sơ mồ côi
// (bên thua lùi savepoint), đúng một sự kiện customer.created.
func TestFindOrCreateForOrderRace(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	const n = 10
	got := make([]customer.OrderCustomer, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			got[i], errs[i] = e.forOrder(customer.OrderCustomerInput{
				FullName: "Khách Đua", Phone: "0901000001", PhoneIsCustomers: true,
			})
		})
	}
	wg.Wait()
	expectOneCreated(t, errs, func(i int) (uuid.UUID, bool) { return got[i].CustomerID, got[i].Created })
	if c := e.count(`SELECT count(*) FROM customer.customers WHERE full_name = 'Khách Đua'`); c != 1 {
		t.Errorf("số khách: %d", c)
	}
	if c := e.count(`SELECT count(*) FROM customer.customer_identities WHERE value = '+84901000001'`); c != 1 {
		t.Errorf("số danh tính: %d", c)
	}
	if c := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created'`); c != 1 {
		t.Errorf("số sự kiện customer.created: %d", c)
	}
	if c := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.create'`); c != 1 {
		t.Errorf("số audit customer.create: %d", c)
	}
}

// Hai đường cùng khách, cùng SĐT chạy song song nhiều lần: FindOrCreateForOrder (có mã khách, cờ bật) và AddIdentity.
// Cả hai khóa dòng khách trước danh tính (ADR-46 nhóm 5) nên không deadlock (pg_stat_database.deadlocks không tăng);
// mỗi vòng đúng một danh tính, kết quả là một trong hai cách hợp lệ.
func TestFindOrCreateForOrderVsAddIdentityNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	before := deadlocks()
	for i := range 20 {
		a := e.create(customer.CreateInput{FullName: "Khách Hai Đường"})
		phone := fmt.Sprintf("09020%05d", i)
		var orderErr, addErr error
		var got customer.OrderCustomer
		var wg sync.WaitGroup
		wg.Go(func() {
			got, orderErr = e.forOrder(customer.OrderCustomerInput{CustomerID: &a.ID, Phone: phone, PhoneIsCustomers: true})
		})
		wg.Go(func() {
			_, addErr = e.addIdentity(e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityPhone, Value: phone})
		})
		wg.Wait()
		if orderErr != nil || got.PhoneConflictWith != nil {
			t.Fatalf("vòng %d: đơn %+v %v", i, got, orderErr)
		}
		var taken *customer.IdentityTakenError
		if addErr != nil && (!errors.As(addErr, &taken) || taken.OwnerRootID != a.ID) {
			t.Fatalf("vòng %d: AddIdentity %v", i, addErr)
		}
		if n := e.identities(a.ID); n != 1 {
			t.Fatalf("vòng %d: %d danh tính", i, n)
		}
	}
	e.staff.Count(t, "SELECT 0 FROM pg_stat_force_next_flush()")
	if after := deadlocks(); after != before {
		t.Fatalf("deadlock: %d → %d", before, after)
	}
}

// expectOneCreated: mọi lời gọi song song (errs[i], kết quả at(i) = mã khách, đã tạo) không lỗi, cùng một mã khách và
// đúng một lời gọi đã tạo.
func expectOneCreated(t *testing.T, errs []error, at func(i int) (uuid.UUID, bool)) {
	t.Helper()
	created := 0
	ids := map[uuid.UUID]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("lời gọi %d: %v", i, err)
		}
		id, c := at(i)
		ids[id] = true
		if c {
			created++
		}
	}
	if len(ids) != 1 || created != 1 {
		t.Fatalf("mã khách %v, số lần tạo %d", ids, created)
	}
}
