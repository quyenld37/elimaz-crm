package identity_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// ListEntityAudit (Task M2-24): dòng của đúng thực thể, cũ trước; tên trường đổi, HasBefore; không mang giá trị
// before/after; thực thể khác, tenant khác không lẫn vào.
func TestListEntityAudit(t *testing.T) {
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	edit := orderEntry(a)
	create := identity.AuditEntry{
		TenantID: a, ActorType: "staff", ActorID: "u-0", Action: "order.create", EntityType: "order", EntityID: "o-1",
		After: map[string]any{"phone": "0911", "status": "new"},
	}
	other := orderEntry(a)
	other.EntityID = "o-2"
	otherTenant := orderEntry(b)
	for _, e := range []identity.AuditEntry{create, edit, other, otherTenant} {
		if err := record(t, s, e); err != nil {
			t.Fatal(err)
		}
	}
	var got []identity.AuditRecord
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		var err error
		got, err = identity.ListEntityAudit(t.Context(), tx, "order", "o-1")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListEntityAudit: %d dòng, muốn 2: %+v", len(got), got)
	}
	c, e := got[0], got[1]
	if c.Action != "order.create" || c.HasBefore || c.ActorID != "u-0" || c.ActorType != "staff" {
		t.Errorf("dòng tạo: %+v", c)
	}
	if e.Action != "order.confirm" || !e.HasBefore || e.ActorID != "u-1" || !slices.Equal(e.ChangedFields, []string{"phone", "status"}) {
		t.Errorf("dòng sửa: %+v", e)
	}
	if c.CreatedAt.After(e.CreatedAt) {
		t.Errorf("không cũ trước: %v sau %v", c.CreatedAt, e.CreatedAt)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "0911") || strings.Contains(string(raw), "0922") {
		t.Errorf("AuditRecord mang giá trị before/after: %s", raw)
	}
}
