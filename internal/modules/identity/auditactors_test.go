package identity_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// mustWebKey tạo khóa dịch vụ tên name (quyền web.catalog.read) của tenant chính, người tạo emailA.
func (e *keyEnv) mustWebKey(name string) identity.ServiceKey {
	e.t.Helper()
	in := webInput(e.actor(emailA), "web.catalog.read")
	in.Name = name
	var k identity.ServiceKey
	if err := tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) (err error) {
		k, _, err = identity.CreateServiceKey(e.t.Context(), tx, in)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return k
}

// recordMinutely ghi entries vào tenant chính, dòng thứ i lúc auditBase() + i phút.
func (e *keyEnv) recordMinutely(entries []identity.AuditEntry) {
	e.t.Helper()
	for i, en := range entries {
		en.TenantID, en.At = e.tenant, auditBase().Add(time.Duration(i)*time.Minute)
		if err := record(e.t, e.s, en); err != nil {
			e.t.Fatal(err)
		}
	}
}

// [Task 488] Người làm cho ô lọc nhật ký: mọi nhân viên (cả đã khóa) và mọi khóa dịch vụ (cả đã thu hồi) của tenant,
// xếp theo tên (collation của database); Active đúng trạng thái; không lẫn người của tenant khác.
func TestListAuditActors(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	kept, revoked := e.mustWebKey("Website"), e.mustWebKey("Aaa khóa cũ")
	if err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
		_, err := identity.RevokeServiceKey(t.Context(), tx, revoked.ID, revoked.Version, e.actor(emailA))
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	locked := e.s.User(t, e.tenant, "zz-locked@elimaz.test", "locked")
	var got []identity.AuditActor
	if err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) (err error) {
		got, err = identity.ListAuditActors(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	byID, pos := map[uuid.UUID]identity.AuditActor{}, map[uuid.UUID]int{}
	for i, a := range got {
		byID[a.ID], pos[a.ID] = a, i
	}
	// "Aaa khóa cũ" < "Website" < "zz-locked@…" theo mọi collation.
	if pos[revoked.ID] >= pos[kept.ID] || pos[kept.ID] >= pos[locked] {
		t.Errorf("không xếp theo tên: %+v", got)
	}
	for _, c := range []struct {
		id     uuid.UUID
		typ    string
		active bool
	}{
		{e.ids[emailA], "staff", true},
		{locked, "staff", false},
		{kept.ID, "service", true},
		{revoked.ID, "service", false},
	} {
		if a, ok := byID[c.id]; !ok || a.ActorType != c.typ || a.Active != c.active || a.Name == "" {
			t.Errorf("%s: %+v %v, muốn %s active %v", c.id, a, ok, c.typ, c.active)
		}
	}
	if _, ok := byID[e.ids["b2@elimaz.test"]]; ok {
		t.Error("lẫn nhân viên của tenant khác")
	}
}

// [Task 488] Dòng nhật ký về khóa dịch vụ có EntityName là tên khóa (cả đã thu hồi); loại khác, mã lạ → rỗng.
func TestListAuditLogs_ServiceKeyEntityName(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	key := e.mustWebKey("Website")
	e.recordMinutely([]identity.AuditEntry{
		{ActorType: "system", EntityType: "identity.service_key", EntityID: key.ID.String(), Action: "n.key"},
		{ActorType: "system", EntityType: "identity.service_key", EntityID: uuid.NewString(), Action: "n.unknown"},
		{ActorType: "system", EntityType: "identity.service_key", EntityID: "không-phải-uuid", Action: "n.bad"},
		{ActorType: "system", EntityType: "n.other", EntityID: key.ID.String(), Action: "n.other"},
	})
	got, _ := mustListAudit(t, e.s, e.tenant, identity.AuditFilter{})
	want := map[string]string{"n.key": key.Name, "n.unknown": "", "n.bad": "", "n.other": ""}
	seen := 0
	for _, r := range got {
		w, ok := want[r.Action]
		if !ok {
			continue
		}
		seen++
		if r.EntityName != w {
			t.Errorf("%s: tên mục %q, muốn %q", r.Action, r.EntityName, w)
		}
	}
	if seen != len(want) {
		t.Fatalf("thấy %d dòng, muốn %d", seen, len(want))
	}
}
