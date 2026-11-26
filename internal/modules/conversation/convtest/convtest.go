// Package convtest dựng dữ liệu thử của hộp chat cho test của module conversation và lớp HTTP (Task 160): khách và hội
// thoại inbox trên một Page, nhân viên có grant theo Page (câu SQL nằm ở testkit).
package convtest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Conversation tạo một khách (customer.Create, tên "Khách <thread>") và một hội thoại inbox của khách trên Page page
// với mã luồng thread; trả mã hội thoại.
func Conversation(t testing.TB, s *testkit.Staff, tenant, page uuid.UUID, thread string) uuid.UUID {
	t.Helper()
	var customerID uuid.UUID
	if err := tenancy.WithTenant(t.Context(), s.Owner, tenant, func(tx pgx.Tx) (err error) {
		customerID, err = customer.Create(t.Context(), tx, "Khách "+thread)
		return err //nolint:wrapcheck // dữ liệu thử
	}); err != nil {
		t.Fatalf("convtest: tạo khách: %v", err)
	}
	return s.Conversation(t, tenant, page, customerID, thread)
}

// Staff chèn nhân viên active email có các quyền perms ở phạm vi page:<page> (page Nil → phạm vi all); trả mã.
func Staff(t testing.TB, s *testkit.Staff, tenant, page uuid.UUID, email string, perms ...string) uuid.UUID {
	t.Helper()
	id := s.User(t, tenant, email, "active")
	if page == uuid.Nil {
		s.Grant(t, tenant, id, perms, "all", "")
	} else {
		s.Grant(t, tenant, id, perms, "page", page.String())
	}
	return id
}

// Load nạp người gọi theo email (grant đọc từ database như lớp HTTP).
func Load(t testing.TB, s *testkit.Staff, email string) identity.AuthUser {
	t.Helper()
	u, err := identity.LoadUser(t.Context(), s.App, email)
	if err != nil {
		t.Fatalf("convtest: nạp %s: %v", email, err)
	}
	return u
}
