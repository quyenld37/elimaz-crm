package customer_test

// Task 430 (review M2 A-04): tìm-hoặc-tạo khách khi lên đơn mà không gắn SĐT mới dùng gốc đọc dưới khóa cho promote
// (và cho mã giảm giá của bên gọi): một lần gộp chưa commit đang giữ hồ sơ thì lên đơn chờ, rồi thấy gốc mới.

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Hồ sơ X (lead) đang được gộp vào S (lead) trong một transaction chưa commit; lên đơn cho X (chọn hồ sơ có sẵn không
// gắn SĐT, hoặc tìm theo SĐT của X) chạy song song → chờ khóa của lần gộp; sau khi gộp commit: RootID là S, S thành
// customer, X vẫn lead (không promote hồ sơ đã thành con).
func TestFindOrCreateForOrder_LocksRootAgainstMerge(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]func(x customer.Customer) customer.OrderCustomerInput{
		"hồ sơ có sẵn, không gắn SĐT": func(x customer.Customer) customer.OrderCustomerInput {
			return customer.OrderCustomerInput{CustomerID: &x.ID}
		},
		"tìm theo SĐT của khách": func(customer.Customer) customer.OrderCustomerInput {
			return customer.OrderCustomerInput{FullName: "Ai đó", Phone: "0903000001", PhoneIsCustomers: true}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newWriteEnv(t)
			x := e.create(customer.CreateInput{FullName: "Khách X", Phone: "0903000001"})
			s := e.create(customer.CreateInput{FullName: "Khách S"})
			merged, release := make(chan error, 1), make(chan struct{})
			mergeDone := make(chan error, 1)
			go func() {
				mergeDone <- tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
					_, err := e.w.MergeCustomers(t.Context(), tx, s.ID, x.ID, customer.MatchedByStaff, e.actor, e.now)
					merged <- err
					<-release
					return err //nolint:wrapcheck // test
				})
			}()
			if err := <-merged; err != nil {
				close(release)
				t.Fatal(err)
			}
			var got customer.OrderCustomer
			orderDone := make(chan error, 1)
			go func() {
				var err error
				got, err = e.forOrder(input(x))
				orderDone <- err
			}()
			e.waitLockWaiter(t)
			close(release)
			if err := <-mergeDone; err != nil {
				t.Fatal(err)
			}
			if err := <-orderDone; err != nil {
				t.Fatal(err)
			}
			if got.CustomerID != x.ID || got.RootID != s.ID {
				t.Fatalf("CustomerID %v, RootID %v; mong %v, %v", got.CustomerID, got.RootID, x.ID, s.ID)
			}
			if l := e.get(s.ID).Lifecycle; l != string(customer.LifecycleCustomer) {
				t.Errorf("gốc S: %s, mong customer", l)
			}
			if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND lifecycle = 'lead'`, x.ID); n != 1 {
				t.Errorf("hồ sơ con X bị promote")
			}
		})
	}
}

// waitLockWaiter chờ tới khi có một phiên của database thử đang chờ khóa (lên đơn chờ lần gộp chưa commit); quá 10 s
// là chốt chống treo.
func (e *writeEnv) waitLockWaiter(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for e.staff.Count(t, `SELECT count(*)::int FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock'`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("lên đơn không chờ khóa của lần gộp")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
