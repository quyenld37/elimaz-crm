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

// ListEntityAudit (Task M2-24): dòng của đúng thực thể, cũ trước; ListedFields là mảng chuỗi after.fields (rỗng khi
// không có, khi không phải mảng); không mang giá trị before/after; thực thể khác, tenant khác không lẫn vào.
func TestListEntityAudit(t *testing.T) {
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	edit := orderEntry(a)
	create := identity.AuditEntry{
		TenantID: a, ActorType: "staff", ActorID: "u-0", Action: "order.create", EntityType: "order", EntityID: "o-1",
		After: map[string]any{"phone": "0911", "status": "new", "fields": "shipAddress"},
	}
	update := identity.AuditEntry{
		TenantID: a, ActorType: "staff", ActorID: "u-2", Action: "order.update", EntityType: "order", EntityID: "o-1",
		After: map[string]any{"phone": "0933", "fields": []string{"lines", "shipAddress"}},
	}
	other := orderEntry(a)
	other.EntityID = "o-2"
	otherTenant := orderEntry(b)
	for _, e := range []identity.AuditEntry{create, edit, update, other, otherTenant} {
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
	if len(got) != 3 {
		t.Fatalf("ListEntityAudit: %d dòng, muốn 3: %+v", len(got), got)
	}
	c, e, u := got[0], got[1], got[2]
	if c.Action != "order.create" || c.ActorID != "u-0" || c.ActorType != "staff" || c.ListedFields == nil ||
		len(c.ListedFields) != 0 {
		t.Errorf("dòng tạo (fields không phải mảng): %+v", c)
	}
	if e.Action != "order.confirm" || e.ActorID != "u-1" || len(e.ListedFields) != 0 {
		t.Errorf("dòng xác nhận (không có fields): %+v", e)
	}
	if u.Action != "order.update" || u.ActorID != "u-2" || !slices.Equal(u.ListedFields, []string{"lines", "shipAddress"}) {
		t.Errorf("dòng sửa: %+v", u)
	}
	if c.CreatedAt.After(e.CreatedAt) || e.CreatedAt.After(u.CreatedAt) {
		t.Errorf("không cũ trước: %v, %v, %v", c.CreatedAt, e.CreatedAt, u.CreatedAt)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "0911") || strings.Contains(string(raw), "0922") ||
		strings.Contains(string(raw), "0933") {
		t.Errorf("AuditRecord mang giá trị before/after: %s", raw)
	}
}
