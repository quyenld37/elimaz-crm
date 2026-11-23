package identity_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

var errRollback = errors.New("thử: lỗi nghiệp vụ sau khi ghi audit")

func orderEntry(tenantID uuid.UUID) identity.AuditEntry {
	return identity.AuditEntry{
		TenantID: tenantID, ActorType: "staff", ActorID: "u-1", Action: "order.confirm",
		EntityType: "order", EntityID: "o-1",
		Before: map[string]any{"phone": "0911222333", "status": "new"},
		After:  map[string]any{"phone": "0922333444", "status": "confirmed"},
		IP:     "203.0.113.7", RequestID: "req-1",
	}
}

func record(t *testing.T, s *testkit.Staff, e identity.AuditEntry) error {
	t.Helper()
	return tenancy.WithTenant(t.Context(), s.App, e.TenantID, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		return identity.RecordAudit(t.Context(), tx, e)
	})
}

// countAudit đếm dòng audit của tenant (bằng crm_app, trong ngữ cảnh tenant).
func countAudit(t *testing.T, s *testkit.Staff, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := tenancy.WithTenant(t.Context(), s.App, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM identity.audit_logs").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// staffWithOrderAudit mở database thử có tenant A và một dòng audit orderEntry(A).
func staffWithOrderAudit(t *testing.T) (*testkit.Staff, uuid.UUID) {
	t.Helper()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	if err := record(t, s, orderEntry(a)); err != nil {
		t.Fatal(err)
	}
	return s, a
}

// Tiêu chí: Before/After có SĐT → changed_fields có phone, status; JSON của dòng không chứa 0911222333, 0922333444;
// after.status = "confirmed"; các cột khác lưu đúng.
func TestRecordAuditStoresFieldNamesNotPersonalValues(t *testing.T) {
	t.Parallel()
	s, a := staffWithOrderAudit(t)
	var (
		rowJSON, actorType, actorID, action, entityType, entityID, ip, requestID string
		changed                                                                  []string
		afterStatus                                                              string
	)
	err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `
			SELECT row_to_json(l)::text, l.actor_type, l.actor_id, l.action, l.entity_type, l.entity_id,
			       host(l.ip), l.request_id, l.changed_fields, l.after->>'status'
			FROM identity.audit_logs AS l`).
			Scan(&rowJSON, &actorType, &actorID, &action, &entityType, &entityID, &ip, &requestID, &changed, &afterStatus)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(changed, []string{"phone", "status"}) {
		t.Fatalf("changed_fields = %v, mong [phone status]", changed)
	}
	for _, v := range []string{"0911222333", "0922333444"} {
		if strings.Contains(rowJSON, v) {
			t.Fatalf("dòng audit chứa giá trị SĐT %s: %s", v, rowJSON)
		}
	}
	if afterStatus != "confirmed" {
		t.Fatalf("after.status = %q, mong confirmed", afterStatus)
	}
	got := []string{actorType, actorID, action, entityType, entityID, ip, requestID}
	want := []string{"staff", "u-1", "order.confirm", "order", "o-1", "203.0.113.7", "req-1"}
	if !slices.Equal(got, want) {
		t.Fatalf("cột = %v, mong %v", got, want)
	}
}

// Tiêu chí: RecordAudit rồi transaction lỗi → 0 dòng (audit đi cùng transaction của thay đổi).
func TestRecordAuditRolledBackWithTransaction(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		if err := identity.RecordAudit(t.Context(), tx, orderEntry(a)); err != nil {
			return err //nolint:wrapcheck // test
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("mong lỗi nghiệp vụ, có %v", err)
	}
	if n := countAudit(t, s, a); n != 0 {
		t.Fatalf("transaction lỗi mà còn %d dòng audit", n)
	}
}

// execInTenant chạy một câu lệnh trong ngữ cảnh tenant bằng pool d, trả lỗi của nó.
func execInTenant(t *testing.T, d *db.DB, tenantID uuid.UUID, q string) error {
	t.Helper()
	return tenancy.WithTenant(t.Context(), d, tenantID, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		_, err := tx.Exec(t.Context(), q)
		return err //nolint:wrapcheck // test
	})
}

// Tiêu chí: UPDATE bằng chủ bảng → lỗi chứa append-only (cả DELETE, TRUNCATE, cả khi RLS lọc hết dòng);
// bằng crm_app → lỗi quyền.
func TestAuditLogsAppendOnly(t *testing.T) {
	t.Parallel()
	s, a := staffWithOrderAudit(t)
	stmts := []string{
		"UPDATE identity.audit_logs SET action = 'x'",
		"DELETE FROM identity.audit_logs",
		"TRUNCATE identity.audit_logs",
	}
	for _, q := range stmts {
		// Chủ bảng, trong ngữ cảnh tenant (thấy dòng) và ngoài ngữ cảnh (RLS lọc hết dòng).
		inTenant := execInTenant(t, s.Owner, a, q)
		_, outside := s.Owner.Pool.Exec(t.Context(), q)
		for name, err := range map[string]error{"trong tenant": inTenant, "ngoài tenant": outside} {
			if err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Errorf("chủ bảng %s, %q: mong lỗi append-only, có %v", name, q, err)
			}
		}
		appErr := execInTenant(t, s.App, a, q)
		if appErr == nil || !strings.Contains(appErr.Error(), "permission denied") {
			t.Errorf("crm_app %q: mong lỗi quyền, có %v", q, appErr)
		}
	}
	if n := countAudit(t, s, a); n != 1 {
		t.Fatalf("sau các lệnh bị chặn mong còn 1 dòng, có %d", n)
	}
}

// Tiêu chí: tenant B không đọc được dòng của A; không ghi được dòng mang tenant A khi đang ở ngữ cảnh B.
func TestAuditTenantIsolation(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	b := s.Tenant(t, "B")
	if err := record(t, s, orderEntry(a)); err != nil {
		t.Fatal(err)
	}
	if n := countAudit(t, s, b); n != 0 {
		t.Fatalf("tenant B thấy %d dòng của A", n)
	}
	if n := countAudit(t, s, a); n != 1 {
		t.Fatalf("tenant A mong 1 dòng, có %d", n)
	}
	var n int
	if err := s.App.Pool.QueryRow(t.Context(), "SELECT count(*) FROM identity.audit_logs").Scan(&n); err != nil || n != 0 {
		t.Fatalf("ngoài ngữ cảnh mong 0 dòng, có %d, %v", n, err)
	}
	err := tenancy.WithTenant(t.Context(), s.App, b, func(tx pgx.Tx) error {
		return identity.RecordAudit(t.Context(), tx, orderEntry(a))
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("ghi dòng tenant A trong ngữ cảnh B phải vi phạm RLS, có %v", err)
	}
}

// Đầu vào sai → ErrInvalidAudit, không ghi; chuỗi rỗng của cột tùy chọn lưu NULL; IP dạng ip:port lưu phần ip.
func TestRecordAuditValidation(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	bad := map[string]func(*identity.AuditEntry){
		"thiếu tenant":       func(e *identity.AuditEntry) { e.TenantID = uuid.Nil },
		"actor_type lạ":      func(e *identity.AuditEntry) { e.ActorType = "robot" },
		"action rỗng":        func(e *identity.AuditEntry) { e.Action = "" },
		"entity_type rỗng":   func(e *identity.AuditEntry) { e.EntityType = "" },
		"IP sai":             func(e *identity.AuditEntry) { e.IP = "không-phải-ip" },
		"giá trị không JSON": func(e *identity.AuditEntry) { e.After = map[string]any{"status": make(chan int)} },
	}
	for name, mutate := range bad {
		e := orderEntry(a)
		mutate(&e)
		err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
			return identity.RecordAudit(t.Context(), tx, e)
		})
		if !errors.Is(err, identity.ErrInvalidAudit) {
			t.Errorf("%s: mong ErrInvalidAudit, có %v", name, err)
		}
		if err != nil && (strings.Contains(err.Error(), "0911222333") || strings.Contains(err.Error(), "0922333444")) {
			t.Errorf("%s: lỗi chứa giá trị dữ liệu: %v", name, err)
		}
	}
	if n := countAudit(t, s, a); n != 0 {
		t.Fatalf("đầu vào sai mà có %d dòng", n)
	}

	for _, actor := range []string{"staff", "system", "carrier", "customer", "service"} {
		e := identity.AuditEntry{TenantID: a, ActorType: actor, Action: "job.retry", EntityType: "job", IP: "[2001:db8::1]:443"}
		if err := record(t, s, e); err != nil {
			t.Fatalf("actor %s: %v", actor, err)
		}
	}
	var nulls, v6 int
	if err := tenancy.WithTenant(t.Context(), s.App, a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `
			SELECT count(*) FILTER (WHERE actor_id IS NULL AND entity_id IS NULL AND request_id IS NULL
			                          AND before IS NULL AND after IS NULL AND changed_fields = '{}'),
			       count(*) FILTER (WHERE ip = '2001:db8::1'::inet)
			FROM identity.audit_logs`).Scan(&nulls, &v6)
	}); err != nil {
		t.Fatal(err)
	}
	if nulls != 5 || v6 != 5 {
		t.Fatalf("mong 5 dòng có cột tùy chọn NULL và IP 2001:db8::1, có %d, %d", nulls, v6)
	}
}

// Bảng audit có RLS nên phải qua mọi quy tắc schema như bảng nghiệp vụ (không nằm trong danh sách miễn).
func TestAuditLogsRLSForced(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	var enabled, forced bool
	if err := s.Owner.Pool.QueryRow(t.Context(),
		"SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'identity.audit_logs'::regclass").
		Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Fatalf("audit_logs: RLS bật %v, ép %v", enabled, forced)
	}
	if slices.Contains(platformTables(), "identity.audit_logs") {
		t.Fatal("audit_logs có RLS, không được nằm trong danh sách bảng nền tảng miễn kiểm")
	}
}

// Before nil (tạo mới) → mọi khóa của After; After nil (xóa) → mọi khóa của Before; số bằng nhau theo JSON.
func TestAuditChangedFieldsEdges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		before, after map[string]any
		want          []string
	}{
		{"tạo mới", nil, map[string]any{"b": 1, "a": "x"}, []string{"a", "b"}},
		{"xóa", map[string]any{"z": true}, nil, []string{"z"}},
		{"không đổi", map[string]any{"n": 1, "m": map[string]any{"k": []any{1}}}, map[string]any{"n": 1.0, "m": map[string]any{"k": []int{1}}}, []string{}},
		{"khóa chỉ một bên", map[string]any{"a": nil}, map[string]any{"b": nil}, []string{"a", "b"}},
		{"lồng nhau đổi", map[string]any{"addr": map[string]any{"city": "HN"}}, map[string]any{"addr": map[string]any{"city": "HCM"}}, []string{"addr"}},
		{"cả hai nil", nil, nil, []string{}},
	}
	for _, c := range cases {
		changed, _, _, err := identity.AuditPayload(c.before, c.after)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !slices.Equal(changed, c.want) {
			t.Errorf("%s: changed = %v, mong %v", c.name, changed, c.want)
		}
	}
	_, b, a, err := identity.AuditPayload(nil, nil)
	if err != nil || b != nil || a != nil {
		t.Fatalf("nil → cột NULL, có %q %q %v", b, a, err)
	}
	// Kiểu tự che (logging.Phone) dưới khóa không nhạy cảm cũng không lộ.
	_, _, a, err = identity.AuditPayload(nil, map[string]any{"contact": logging.Phone("0933")})
	if err != nil || strings.Contains(string(a), "0933") {
		t.Fatalf("logging.Phone lộ giá trị: %s, %v", a, err)
	}
}

// auditGen sinh Before/After cho TestAuditPayloadProperty và nhớ mọi giá trị chuỗi nằm dưới một khóa nhạy cảm (ở bất
// kỳ tổ tiên nào); các giá trị đó mang dấu SECRET-<n>- duy nhất.
type auditGen struct {
	keys    []string
	secrets map[string]bool
}

func (g *auditGen) value(t *rapid.T, depth int, sensitive bool) any {
	kind := rapid.IntRange(0, 3).Draw(t, "kind")
	if depth >= 3 {
		kind = rapid.IntRange(0, 1).Draw(t, "leaf")
	}
	switch kind {
	case 0:
		if sensitive {
			s := fmt.Sprintf("SECRET-%d-", len(g.secrets)+1)
			g.secrets[s] = true
			return s
		}
		return rapid.SampledFrom([]string{"new", "confirmed", "a"}).Draw(t, "str")
	case 1:
		return rapid.IntRange(0, 3).Draw(t, "int")
	case 2:
		m := map[string]any{}
		for _, k := range rapid.SliceOfNDistinct(rapid.SampledFrom(g.keys), 0, 3, rapid.ID).Draw(t, "keys") {
			m[k] = g.value(t, depth+1, sensitive || logging.SensitiveKey(k))
		}
		return m
	default:
		return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) any { return g.value(t, depth+1, sensitive) }), 0, 3).Draw(t, "slice")
	}
}

func (g *auditGen) top(t *rapid.T, label string) map[string]any {
	if rapid.IntRange(0, 5).Draw(t, label+"-nil") == 0 {
		return nil
	}
	m := map[string]any{}
	for _, k := range rapid.SliceOfNDistinct(rapid.SampledFrom(g.keys), 0, len(g.keys), rapid.ID).Draw(t, label) {
		m[k] = g.value(t, 1, logging.SensitiveKey(k))
	}
	return m
}

// Quy luật (rapid): changed_fields đúng bằng tập khóa có giá trị khác; không giá trị nào dưới khóa nhạy cảm (ở mọi
// cấp, cả trong slice) lọt vào JSON lưu; giá trị không nhạy cảm ở cấp đầu được giữ nguyên.
func TestAuditPayloadProperty(t *testing.T) {
	t.Parallel()
	keys := []string{"status", "total", "note_id", "phone", "customer_address", "ship_text", "access_token", "BankAccount", "tags"}
	rapid.Check(t, func(t *rapid.T) {
		g := &auditGen{keys: keys, secrets: map[string]bool{}}
		before := g.top(t, "before")
		after := g.top(t, "after")
		// Đôi khi chép giá trị của before sang after để có khóa không đổi.
		for _, k := range slices.Sorted(maps.Keys(before)) {
			if after != nil && rapid.Bool().Draw(t, "same-"+k) {
				after[k] = before[k]
			}
		}
		changed, bJSON, aJSON, err := identity.AuditPayload(before, after)
		if err != nil {
			t.Fatal(err)
		}
		if want := changedModel(t, before, after); !slices.Equal(changed, want) {
			t.Fatalf("changed = %v, mong %v", changed, want)
		}
		for s := range g.secrets {
			if strings.Contains(string(bJSON), s) || strings.Contains(string(aJSON), s) {
				t.Fatalf("giá trị nhạy cảm %s lọt vào JSON: %s | %s", s, bJSON, aJSON)
			}
		}
		checkStored(t, bJSON, before)
		checkStored(t, aJSON, after)
	})
}

// changedModel là định nghĩa changed_fields viết thẳng từ kế hoạch: khóa có ở một bên, hoặc JSON khác nhau.
func changedModel(t *rapid.T, before, after map[string]any) []string {
	want := []string{}
	for _, k := range slices.Sorted(maps.Keys(union(before, after))) {
		bv, bok := before[k]
		av, aok := after[k]
		if bok != aok || !jsonEqual(t, bv, av) {
			want = append(want, k)
		}
	}
	return want
}

// checkStored: map nil lưu NULL; khóa nhạy cảm cấp đầu lưu "[Redacted]"; chuỗi dưới khóa thường giữ nguyên.
func checkStored(t *rapid.T, raw []byte, m map[string]any) {
	if m == nil {
		if raw != nil {
			t.Fatalf("map nil phải lưu NULL, có %s", raw)
		}
		return
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	for k, v := range m {
		if logging.SensitiveKey(k) {
			if stored[k] != "[Redacted]" {
				t.Fatalf("khóa nhạy cảm %s lưu %v", k, stored[k])
			}
			continue
		}
		if s, ok := v.(string); ok && stored[k] != s {
			t.Fatalf("khóa %s: lưu %v, mong %q", k, stored[k], s)
		}
	}
}

func union(a, b map[string]any) map[string]any {
	out := maps.Clone(a)
	if out == nil {
		out = map[string]any{}
	}
	maps.Copy(out, b)
	return out
}

func jsonEqual(t *rapid.T, a, b any) bool {
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(ja) == string(jb)
}
