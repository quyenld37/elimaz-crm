package customer_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

func (e *writeEnv) forOrder(in customer.OrderCustomerInput) (customer.OrderCustomer, error) {
	var got customer.OrderCustomer
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		got, err = e.w.FindOrCreateForOrder(e.t.Context(), tx, in, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return got, err
}

func (e *writeEnv) mustForOrder(in customer.OrderCustomerInput) customer.OrderCustomer {
	e.t.Helper()
	got, err := e.forOrder(in)
	if err != nil {
		e.t.Fatal(err)
	}
	return got
}

func (e *writeEnv) identities(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1`, id)
}

// Các ca của 05 mục 1 (DH12, DH13): SĐT người nhận chỉ thành danh tính khi nhân viên đánh dấu; SĐT của nhóm khác
// không bao giờ gắn hay gộp, chỉ báo PhoneConflictWith.
func TestFindOrCreateForOrder(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b := e.pair("Khách A", "Khách B", "0987654321")
	f := &forOrderFixture{e: e, a: a, b: b}
	f.customers = func() int { return e.count(`SELECT count(*) FROM customer.customers`) }
	f.start = f.customers()
	t.Run("DH12 SĐT người nhận không phải của khách", f.DH12)
	t.Run("DH13 SĐT của nhóm khác", f.DH13)
	t.Run("có mã khách, SĐT mới của khách", f.LinkNewPhone)
	t.Run("SĐT của hồ sơ con cùng nhóm", f.SameGroup)
	t.Run("không mã khách, SĐT của B", f.PhoneOfB)
	t.Run("không mã khách, SĐT mới không phải của khách", f.NewNotCustomers)
	t.Run("không mã khách, SĐT mới của khách", f.NewCustomers)
	t.Run("lead sang customer ở đơn đầu tiên", f.Promote)
	t.Run("lỗi", f.Errors)
}

func (f *forOrderFixture) DH12(t *testing.T) {
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{CustomerID: &f.a.ID, Phone: "0987654321", PhoneIsCustomers: false})
	if got.CustomerID != f.a.ID || got.RootID != f.a.ID || got.Created || got.PhoneConflictWith != nil ||
		got.Phone != "+84987654321" {
		t.Fatalf("kết quả: %+v", got)
	}
	if f.env(t).identities(f.a.ID) != 0 || f.customers() != f.start {
		t.Fatalf("A có %d danh tính, %d khách", f.env(t).identities(f.a.ID), f.customers())
	}
}

func (f *forOrderFixture) DH13(t *testing.T) {
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{CustomerID: &f.a.ID, Phone: "0987654321", PhoneIsCustomers: true})
	if got.CustomerID != f.a.ID || got.PhoneConflictWith == nil || *got.PhoneConflictWith != f.b.RootID || got.Created {
		t.Fatalf("kết quả: %+v", got)
	}
	if f.env(t).identities(f.a.ID) != 0 || f.env(t).count(`SELECT count(*) FROM customer.customer_merges`) != 0 {
		t.Fatal("không được gắn SĐT hay gộp")
	}
	var root uuid.UUID
	f.env(t).inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT root_id FROM customer.customers WHERE id = $1", f.a.ID).Scan(&root)
	})
	if root != f.a.ID {
		t.Fatalf("A đổi gốc: %s", root)
	}
}

func (f *forOrderFixture) LinkNewPhone(t *testing.T) {
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{CustomerID: &f.a.ID, Phone: "0911000001", PhoneIsCustomers: true})
	if got.CustomerID != f.a.ID || got.PhoneConflictWith != nil || f.env(t).identities(f.a.ID) != 1 {
		t.Fatalf("kết quả: %+v, %d danh tính", got, f.env(t).identities(f.a.ID))
	}
	if n := f.env(t).count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1 AND source = 'order'`, f.a.ID); n != 1 {
		t.Errorf("nguồn order: %d", n)
	}
	again := f.env(t).mustForOrder(customer.OrderCustomerInput{CustomerID: &f.a.ID, Phone: "0911000001", PhoneIsCustomers: true})
	if again.PhoneConflictWith != nil || f.env(t).identities(f.a.ID) != 1 {
		t.Errorf("SĐT đã của chính khách: %+v", again)
	}
}

func (f *forOrderFixture) SameGroup(t *testing.T) {
	child := f.env(t).create(customer.CreateInput{FullName: "Khách A cũ", Phone: "0911000002"})
	f.env(t).staff.Exec(t, f.env(t).tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE customer.customers SET root_id = $1 WHERE id = $2", f.a.ID, child.ID)
		return err //nolint:wrapcheck // test
	})
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{CustomerID: &f.a.ID, Phone: "0911000002", PhoneIsCustomers: true})
	if got.PhoneConflictWith != nil || f.env(t).identities(f.a.ID) != 1 || f.env(t).identities(child.ID) != 1 {
		t.Fatalf("cùng nhóm không làm gì: %+v", got)
	}
	p := f.env(t).get(child.ID)
	if p.ID != f.a.ID || len(p.MemberIDs) != 2 || len(p.Identities) != 2 {
		t.Errorf("hồ sơ theo gốc: %+v", p)
	}
}

func (f *forOrderFixture) PhoneOfB(t *testing.T) {
	before := f.customers()
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{FullName: "Ai đó", Phone: "0987 654 321", PhoneIsCustomers: true})
	if got.CustomerID != f.b.ID || got.RootID != f.b.RootID || got.Created || f.customers() != before {
		t.Fatalf("kết quả: %+v", got)
	}
}

func (f *forOrderFixture) NewNotCustomers(t *testing.T) {
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{FullName: "Khách C", Phone: "0987654321", PhoneIsCustomers: false})
	if !got.Created || got.CustomerID == f.b.ID || f.env(t).identities(got.CustomerID) != 0 {
		t.Fatalf("kết quả: %+v", got)
	}
}

func (f *forOrderFixture) NewCustomers(t *testing.T) {
	got := f.env(t).mustForOrder(customer.OrderCustomerInput{FullName: "Khách D", Phone: "0933000003", PhoneIsCustomers: true})
	if !got.Created || f.env(t).identities(got.CustomerID) != 1 {
		t.Fatalf("kết quả: %+v", got)
	}
	p := f.env(t).get(got.CustomerID)
	if p.Lifecycle != "customer" || p.Version != 1 {
		t.Errorf("khách mới từ đơn: %+v", p.Customer)
	}
}

func (f *forOrderFixture) Promote(t *testing.T) {
	if p := f.env(t).get(f.b.ID); p.Lifecycle != "customer" || p.Version != 2 {
		t.Fatalf("B sau đơn đầu: %+v", p.Customer)
	}
	f.env(t).mustForOrder(customer.OrderCustomerInput{CustomerID: &f.b.ID})
	if p := f.env(t).get(f.b.ID); p.Version != 2 {
		t.Errorf("đơn thứ hai không đổi version: %+v", p.Customer)
	}
}

func (f *forOrderFixture) Errors(t *testing.T) {
	missing := uuid.New()
	for _, x := range []struct {
		name string
		in   customer.OrderCustomerInput
		want error
	}{
		{"mã khách không có", customer.OrderCustomerInput{CustomerID: &missing}, customer.ErrNotFound},
		{"SĐT sai", customer.OrderCustomerInput{FullName: "X", Phone: "09"}, customer.ErrInvalidPhone},
		{"không tên, không mã", customer.OrderCustomerInput{}, customer.ErrInvalidInput},
	} {
		if _, err := f.env(t).forOrder(x.in); !errors.Is(err, x.want) {
			t.Errorf("%s: %v", x.name, err)
		}
	}
}

// forOrderFixture: khách A (không SĐT), B (SĐT 0987654321) và số khách lúc đầu, dùng chung các ca của
// TestFindOrCreateForOrder (chạy theo thứ tự, ca sau thấy dữ liệu của ca trước).
type forOrderFixture struct {
	e         *writeEnv
	a, b      customer.Customer
	customers func() int
	start     int
}

// env trả writeEnv của ca con t (lỗi làm đỏ đúng ca con, không gọi Fatal của test cha từ goroutine khác).
func (f *forOrderFixture) env(t *testing.T) *writeEnv {
	e := *f.e
	e.t = t
	return &e
}
