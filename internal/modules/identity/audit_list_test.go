package identity_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// ListEntityAudit (Task M2-24, Task 425): dòng của đúng thực thể, mới nhất trước; ListedFields là mảng chuỗi after.fields (rỗng khi
// không có, khi không phải mảng); không mang giá trị before/after; thực thể khác, tenant khác không lẫn vào.
func TestListEntityAudit(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	edit := orderEntry(a)
	create := identity.AuditEntry{
		TenantID: a, ActorType: "staff", ActorID: "u-0", Action: "order.create", EntityType: "order", EntityID: "o-1",
		After: map[string]any{"phone": "0911000111", "status": "new", "fields": "shipAddress"},
	}
	update := identity.AuditEntry{
		TenantID: a, ActorType: "staff", ActorID: "u-2", Action: "order.update", EntityType: "order", EntityID: "o-1",
		After: map[string]any{"phone": "0933000333", "fields": []string{"lines", "shipAddress"}},
	}
	other := orderEntry(a)
	other.EntityID = "o-2"
	otherTenant := orderEntry(b)
	for _, e := range []identity.AuditEntry{create, edit, update, other, otherTenant} {
		if err := record(t, s, e); err != nil {
			t.Fatal(err)
		}
	}
	got := listOrderAudit(t, s, a)
	if len(got) != 3 {
		t.Fatalf("ListEntityAudit: %d dòng, muốn 3: %+v", len(got), got)
	}
	u, e, c := got[0], got[1], got[2]
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
		t.Errorf("không mới nhất trước: %v, %v, %v", u.CreatedAt, e.CreatedAt, c.CreatedAt)
	}
	assertNoAuditValues(t, got, "0911000111", "0911222333", "0922333444", "0933000333")
}

// assertNoAuditValues: bản JSON của các dòng không có trường giá trị before/after và không chứa SĐT đầy đủ nào đã gài
// (Task 451: tìm mẩu 4 chữ số từng bắt nhầm chữ số trong UUID của dòng).
func assertNoAuditValues(t *testing.T, got []identity.AuditRecord, phones ...string) {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for k := range r {
			if l := strings.ToLower(k); strings.Contains(l, "before") || strings.Contains(l, "after") {
				t.Errorf("AuditRecord có trường giá trị %q: %s", k, raw)
			}
		}
	}
	for _, p := range phones {
		if strings.Contains(string(raw), p) {
			t.Errorf("AuditRecord mang SĐT %s của before/after: %s", p, raw)
		}
	}
}

// Task 425: thực thể có hơn 500 dòng audit → trả đúng 500 dòng mới nhất: dòng ghi sau cùng đứng đầu, dòng cũ nhất bị bỏ.
func TestListEntityAudit_KeepsNewest500(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	first := orderEntry(a)
	first.Action = "order.create"
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		if err := identity.RecordAudit(t.Context(), tx, first); err != nil {
			return err //nolint:wrapcheck // test
		}
		for range 500 {
			if err := identity.RecordAudit(t.Context(), tx, orderEntry(a)); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	last := orderEntry(a)
	last.Action, last.ActorID = "order.update", "u-9"
	if err := record(t, s, last); err != nil {
		t.Fatal(err)
	}
	got := listOrderAudit(t, s, a)
	if len(got) != 500 {
		t.Fatalf("ListEntityAudit: %d dòng, muốn 500", len(got))
	}
	if got[0].Action != "order.update" || got[0].ActorID != "u-9" {
		t.Errorf("dòng đầu: %+v, muốn dòng ghi sau cùng (order.update của u-9)", got[0])
	}
	for _, r := range got {
		if r.Action == "order.create" {
			t.Fatalf("dòng cũ nhất (order.create) vẫn có trong 500 dòng: %+v", r)
		}
	}
}

// Task 436: At khác zero là mốc của dòng (mốc của bên gọi lệch now() của transaction ±5 phút vẫn lưu đúng mốc đó, thứ
// tự đọc theo mốc đó); At zero → now() của transaction. Một đồng hồ: mọi mốc tính từ now() đã đọc của transaction.
func TestRecordAudit_At(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	ahead, behind, def := orderEntry(a), orderEntry(a), orderEntry(a)
	ahead.ActorID, behind.ActorID, def.ActorID = "u-ahead", "u-behind", "u-default"
	var txNow time.Time
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), "SELECT now()").Scan(&txNow); err != nil {
			return err //nolint:wrapcheck // test
		}
		ahead.At, behind.At = txNow.Add(5*time.Minute), txNow.Add(-5*time.Minute)
		for _, e := range []identity.AuditEntry{ahead, behind, def} {
			if err := identity.RecordAudit(t.Context(), tx, e); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got := listOrderAudit(t, s, a)
	if len(got) != 3 || got[0].ActorID != "u-ahead" || got[1].ActorID != "u-default" || got[2].ActorID != "u-behind" {
		t.Fatalf("thứ tự theo mốc: %+v", got)
	}
	if !got[0].CreatedAt.Equal(ahead.At) ||
		!got[2].CreatedAt.Equal(behind.At) {
		t.Errorf("mốc lưu %v, %v; muốn %v, %v", got[0].CreatedAt, got[2].CreatedAt, ahead.At, behind.At)
	}
	if !got[1].CreatedAt.Equal(txNow) {
		t.Errorf("At zero lưu %v, muốn now() của transaction %v", got[1].CreatedAt, txNow)
	}
}

// listOrderAudit đọc audit của đơn o-1 trong tenant tenantID bằng ListEntityAudit (crm_app, trong ngữ cảnh tenant).
func listOrderAudit(t *testing.T, s *testkit.Staff, tenantID uuid.UUID) []identity.AuditRecord {
	t.Helper()
	var got []identity.AuditRecord
	if err := tenancy.WithTenant(t.Context(), s.App, tenantID, func(tx pgx.Tx) error {
		var err error
		got, err = identity.ListEntityAudit(t.Context(), tx, "order", "o-1")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return got
}
