package identity_test

import (
	"errors"
	"fmt"
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

// auditBase là mốc giờ của dòng audit thử.
func auditBase() time.Time { return time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC) }

// listAudit đọc một trang ListAuditLogs bằng crm_app trong tenant.
func listAudit(t *testing.T, s *testkit.Staff, tenant uuid.UUID, f identity.AuditFilter,
) ([]identity.AuditLog, *identity.AuditCursor, error) {
	t.Helper()
	var (
		got  []identity.AuditLog
		next *identity.AuditCursor
	)
	err := tenancy.WithTenant(t.Context(), s.App, tenant, func(tx pgx.Tx) error {
		var err error
		got, next, err = identity.ListAuditLogs(t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	})
	return got, next, err //nolint:wrapcheck // test
}

func mustListAudit(t *testing.T, s *testkit.Staff, tenant uuid.UUID, f identity.AuditFilter,
) ([]identity.AuditLog, *identity.AuditCursor) {
	t.Helper()
	got, next, err := listAudit(t, s, tenant, f)
	if err != nil {
		t.Fatal(err)
	}
	return got, next
}

func auditIDs(rows []identity.AuditLog) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.EntityID)
	}
	return out
}

// [Task 457] 120 dòng (40 dòng cùng một mốc giờ: thứ tự theo id) → 3 trang 50, 50, 20, mới nhất trước, không trùng, không
// sót; trang cuối không có con trỏ; con trỏ qua chuỗi (String → ParseAuditCursor) cho cùng kết quả.
func TestListAuditLogs_Pages(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	for i := range 120 {
		at := auditBase().Add(time.Duration(i) * time.Minute)
		if i < 40 {
			at = auditBase()
		}
		e := identity.AuditEntry{
			TenantID: a, ActorType: "system", Action: "x.touch", EntityType: "x",
			EntityID: fmt.Sprintf("e-%03d", i), At: at,
		}
		if err := record(t, s, e); err != nil {
			t.Fatal(err)
		}
	}
	var (
		all   []identity.AuditLog
		sizes []int
		after *identity.AuditCursor
	)
	for range 5 {
		page, next := mustListAudit(t, s, a, identity.AuditFilter{After: after})
		all = append(all, page...)
		sizes = append(sizes, len(page))
		if next == nil {
			break
		}
		c, err := identity.ParseAuditCursor(next.String())
		if err != nil || !c.CreatedAt.Equal(next.CreatedAt) || c.ID != next.ID {
			t.Fatalf("con trỏ qua chuỗi: %v, %+v ≠ %+v", err, c, *next)
		}
		after = &c
	}
	if !slices.Equal(sizes, []int{50, 50, 20}) {
		t.Fatalf("cỡ trang: %v, muốn [50 50 20]", sizes)
	}
	assertAuditPageOrder(t, all, 120)
	if all[0].EntityID != "e-119" {
		t.Errorf("dòng đầu: %s, muốn e-119", all[0].EntityID)
	}
}

// assertAuditPageOrder: all có đúng n dòng khác nhau (không trùng, không sót), xếp (created_at, id) giảm dần.
func assertAuditPageOrder(t *testing.T, all []identity.AuditLog, n int) {
	t.Helper()
	seen := map[string]bool{}
	for _, id := range auditIDs(all) {
		if seen[id] {
			t.Errorf("trùng dòng %s", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Errorf("sót dòng: %d khác nhau, muốn %d", len(seen), n)
	}
	for i := 1; i < len(all); i++ {
		p, c := all[i-1], all[i]
		if c.CreatedAt.After(p.CreatedAt) || (c.CreatedAt.Equal(p.CreatedAt) && strings.Compare(c.ID.String(), p.ID.String()) > 0) {
			t.Fatalf("không mới nhất trước ở %d: %v %s rồi %v %s", i, p.CreatedAt, p.ID, c.CreatedAt, c.ID)
		}
	}
}

// [Task 457] Lọc theo hành động, người làm, thực thể, khoảng giờ [from, to); tenant khác không thấy; entityId thiếu
// entityType, from ≥ to → ErrInvalidInput; con trỏ hỏng → ErrInvalidInput.
func TestListAuditLogs_Filters(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	entries := []identity.AuditEntry{
		{ActorType: "staff", ActorID: "u-1", Action: "order.confirm", EntityType: "order", EntityID: "o-1"},
		{ActorType: "staff", ActorID: "u-2", Action: "order.confirm", EntityType: "order", EntityID: "o-2"},
		{ActorType: "staff", ActorID: "u-1", Action: "order.update", EntityType: "order", EntityID: "o-1"},
		{ActorType: "service", ActorID: "k-1", Action: "order.create", EntityType: "order", EntityID: "o-3"},
		{ActorType: "system", Action: "web.intake.pause", EntityType: "web_intake"},
	}
	for i, e := range entries {
		e.TenantID, e.At = a, auditBase().Add(time.Duration(i)*time.Hour)
		if err := record(t, s, e); err != nil {
			t.Fatal(err)
		}
		e.TenantID = b
		if err := record(t, s, e); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		f    identity.AuditFilter
		want []string // action/entity_id theo thứ tự mới nhất trước
	}{
		{"mọi dòng", identity.AuditFilter{}, []string{
			"web.intake.pause/", "order.create/o-3", "order.update/o-1",
			"order.confirm/o-2", "order.confirm/o-1",
		}},
		{"hành động", identity.AuditFilter{Action: "order.confirm"}, []string{"order.confirm/o-2", "order.confirm/o-1"}},
		{"người làm", identity.AuditFilter{ActorID: "u-1"}, []string{"order.update/o-1", "order.confirm/o-1"}},
		{
			"người làm và hành động",
			identity.AuditFilter{ActorID: "u-1", Action: "order.update"},
			[]string{"order.update/o-1"},
		},
		{"loại thực thể", identity.AuditFilter{EntityType: "web_intake"}, []string{"web.intake.pause/"}},
		{
			"thực thể",
			identity.AuditFilter{EntityType: "order", EntityID: "o-1"},
			[]string{"order.update/o-1", "order.confirm/o-1"},
		},
		{
			"từ (gồm mốc)",
			identity.AuditFilter{From: auditBase().Add(3 * time.Hour)},
			[]string{"web.intake.pause/", "order.create/o-3"},
		},
		{"tới (không gồm mốc)", identity.AuditFilter{To: auditBase().Add(time.Hour)}, []string{"order.confirm/o-1"}},
		{
			"khoảng giờ",
			identity.AuditFilter{From: auditBase().Add(time.Hour), To: auditBase().Add(3 * time.Hour)},
			[]string{"order.update/o-1", "order.confirm/o-2"},
		},
		{"không khớp", identity.AuditFilter{Action: "không.có"}, []string{}},
	}
	for _, c := range cases {
		got, next := mustListAudit(t, s, a, c.f)
		keys := make([]string, 0, len(got))
		for _, r := range got {
			keys = append(keys, r.Action+"/"+r.EntityID)
		}
		if !slices.Equal(keys, c.want) || next != nil {
			t.Errorf("%s: %v (next %v), muốn %v", c.name, keys, next, c.want)
		}
	}
	for name, f := range map[string]identity.AuditFilter{
		"entityId thiếu entityType": {EntityID: "o-1"},
		"from bằng to":              {From: auditBase(), To: auditBase()},
		"from sau to":               {From: auditBase().Add(time.Hour), To: auditBase()},
	} {
		if _, _, err := listAudit(t, s, a, f); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("%s: %v, muốn ErrInvalidInput", name, err)
		}
	}
	for _, bad := range []string{"", "abc", "zz_" + uuid.NewString(), "1a_không-phải-uuid", "-1_" + uuid.NewString()} {
		if _, err := identity.ParseAuditCursor(bad); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("con trỏ %q: %v, muốn ErrInvalidInput", bad, err)
		}
	}
}

// [Task 457] Tên người làm: nhân viên (display_name), khóa dịch vụ (tên khóa, cả khi đã thu hồi), hệ thống (rỗng); mã
// không có hay không phải uuid → tên rỗng; khách (customer) không có tên (dữ liệu cá nhân). Không trả giá trị
// before/after (SĐT, địa chỉ) — chỉ tên trường đổi; không có trường IP.
func TestListAuditLogs_ActorNamesNoValues(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	staffID := e.ids[emailA]
	key := e.mustWebKey("Website")
	pii := map[string]any{"phone": "0911222333", "address": "12 Hàng Bạc", "status": "new"}
	entries := []identity.AuditEntry{
		{
			ActorType: "staff", ActorID: staffID.String(), Action: "t.staff", Before: pii,
			After: map[string]any{"phone": "0988777666", "address": "99 Lý Thái Tổ", "status": "confirmed"},
		},
		{ActorType: "service", ActorID: key.ID.String(), Action: "t.service", After: pii},
		{ActorType: "system", Action: "t.system"},
		{ActorType: "staff", ActorID: uuid.NewString(), Action: "t.staff-unknown"},
		{ActorType: "staff", ActorID: "không-phải-uuid", Action: "t.staff-bad"},
		{ActorType: "customer", ActorID: uuid.NewString(), Action: "t.customer", After: pii},
		{ActorType: "service", ActorID: staffID.String(), Action: "t.service-is-staff-id"},
	}
	for i := range entries {
		entries[i].EntityType, entries[i].EntityID, entries[i].IP = "t", "t-1", "203.0.113.9"
	}
	e.recordMinutely(entries)
	got, _ := mustListAudit(t, e.s, e.tenant, identity.AuditFilter{EntityType: "t"})
	byAction := map[string]identity.AuditLog{}
	for _, r := range got {
		byAction[r.Action] = r
	}
	want := map[string]struct{ typ, name string }{
		"t.staff": {"staff", emailA}, "t.service": {"service", "Website"}, "t.system": {"system", ""},
		"t.staff-unknown": {"staff", ""}, "t.staff-bad": {"staff", ""}, "t.customer": {"customer", ""},
		"t.service-is-staff-id": {"service", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("số dòng: %d, muốn %d: %+v", len(got), len(want), got)
	}
	for action, w := range want {
		if r := byAction[action]; r.ActorType != w.typ || r.ActorName != w.name {
			t.Errorf("%s: loại %q tên %q, muốn %q %q", action, r.ActorType, r.ActorName, w.typ, w.name)
		}
	}
	if r := byAction["t.staff"]; !slices.Equal(r.ChangedFields, []string{"address", "phone", "status"}) ||
		r.ActorID != staffID.String() || r.EntityType != "t" || r.EntityID != "t-1" {
		t.Errorf("dòng nhân viên: %+v", r)
	}
	if r := byAction["t.system"]; r.ChangedFields == nil || len(r.ChangedFields) != 0 || r.ActorID != "" {
		t.Errorf("dòng hệ thống (không trường đổi → rỗng, không nil): %+v", r)
	}
	assertNoAuditValues(t, got, "0911222333", "0988777666", "Hàng Bạc", "Lý Thái Tổ", "203.0.113.9", "confirmed")
}
