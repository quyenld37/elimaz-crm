package identity_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// StaffWithPermissionAt (Task 160): nhân viên active có quyền ở phạm vi all hoặc đúng Page; Page khác, bị khóa, vai
// trò lưu trữ, quyền khác, tenant khác → không có; sắp theo tên hiển thị.
func TestStaffWithPermissionAt(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	page, other := uuid.New(), uuid.New()
	read := []string{"conversation.read"}
	mk := func(tenant uuid.UUID, email string, perms []string, scope, id string) uuid.UUID {
		u := s.User(t, tenant, email, "active")
		s.Grant(t, tenant, u, perms, scope, id)
		return u
	}
	all := mk(a, "b-all@elimaz.test", read, "all", "")
	onPage := mk(a, "a-page@elimaz.test", read, "page", page.String())
	mk(a, "c-other@elimaz.test", read, "page", other.String())
	mk(a, "d-reply@elimaz.test", []string{"conversation.reply"}, "page", page.String())
	locked := mk(a, "e-locked@elimaz.test", read, "page", page.String())
	s.SetStatus(t, a, locked, "locked")
	archived := mk(a, "f-archived@elimaz.test", read, "page", page.String())
	s.Exec(t, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE identity.roles SET status = 'archived' WHERE id IN
			(SELECT role_id FROM identity.user_role_grants WHERE user_id = $1)`, archived)
		return err //nolint:wrapcheck // test
	})
	mk(b, "g-tenant-b@elimaz.test", read, "all", "")
	var got []identity.User
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) (err error) {
		got, err = identity.StaffWithPermissionAt(t.Context(), tx, "conversation.read",
			identity.Scope{Type: identity.ScopePage, ID: page.String()})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 0, len(got))
	for _, u := range got {
		ids = append(ids, u.ID)
		if u.Status != "active" {
			t.Fatalf("trạng thái = %q", u.Status)
		}
	}
	if want := []uuid.UUID{onPage, all}; !slices.Equal(ids, want) {
		t.Fatalf("nhân viên = %v, muốn %v (a-page trước b-all)", ids, want)
	}
}
