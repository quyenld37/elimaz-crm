package customer_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func (e *writeEnv) hasPhone(tenant, id uuid.UUID, p string) bool {
	e.t.Helper()
	var ok bool
	if err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		ok, err = customer.HasPhone(e.t.Context(), tx, id, logging.Phone(p))
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return ok
}

// HasPhone (kế hoạch M5, Task 202, A15): SĐT là danh tính phone của bất kỳ hồ sơ nào trong nhóm gộp của khách (cả
// hồ sơ đã gộp vào gốc và ngược lại); dạng nhập khác (0…, 84…, khoảng trắng) vẫn khớp; SĐT của khách khác, SĐT sai
// dạng, khách không có, tenant khác → false.
func TestHasPhone(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.mustForOrder(customer.OrderCustomerInput{FullName: "Chị A", Phone: "0901111222", PhoneIsCustomers: true})
	b := e.mustForOrder(customer.OrderCustomerInput{FullName: "Chị B", Phone: "0902222333", PhoneIsCustomers: true})
	c := e.mustForOrder(customer.OrderCustomerInput{FullName: "Anh C", Phone: "0903333444", PhoneIsCustomers: true})
	if !e.hasPhone(e.tenant, a.CustomerID, "0901111222") || !e.hasPhone(e.tenant, a.CustomerID, "+84 901 111 222") ||
		!e.hasPhone(e.tenant, a.CustomerID, "84901111222") {
		t.Error("SĐT của chính khách: mong true với mọi dạng nhập")
	}
	if e.hasPhone(e.tenant, a.CustomerID, "0902222333") {
		t.Error("SĐT của khách khác: mong false")
	}
	if e.hasPhone(e.tenant, a.CustomerID, "abc") || e.hasPhone(e.tenant, a.CustomerID, "") {
		t.Error("SĐT sai dạng: mong false")
	}
	if e.hasPhone(e.tenant, uuid.New(), "0901111222") {
		t.Error("khách không có: mong false")
	}
	if e.hasPhone(e.other, a.CustomerID, "0901111222") {
		t.Error("tenant khác: mong false")
	}
	e.mustMerge(a.CustomerID, b.CustomerID)
	if !e.hasPhone(e.tenant, a.CustomerID, "0902222333") || !e.hasPhone(e.tenant, b.CustomerID, "0901111222") {
		t.Error("sau gộp: SĐT của mọi hồ sơ trong nhóm phải khớp từ cả gốc lẫn hồ sơ đã gộp")
	}
	if e.hasPhone(e.tenant, a.CustomerID, "0903333444") || e.hasPhone(e.tenant, c.CustomerID, "0901111222") {
		t.Error("nhóm khác: mong false")
	}
}
