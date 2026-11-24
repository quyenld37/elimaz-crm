package identity_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// auditPlanSeed: tenant A có 100.000 dòng audit rải 2 năm trước 2026-11-20 (30 hành động, 40 người làm, 8 loại thực thể,
// 5.000 thực thể), tenant B 20.000 dòng; ANALYZE (như shop chạy lâu).
func auditPlanSeed() testkit.Seed {
	a := testkit.FixedUUID("identity-audit-plan/A")
	b := testkit.FixedUUID("identity-audit-plan/B")
	rows := func(tenant uuid.UUID, n int) []string {
		return []string{
			fmt.Sprintf("SELECT set_config('app.tenant_id', '%s', false)", tenant),
			fmt.Sprintf(`INSERT INTO identity.audit_logs (tenant_id, actor_type, actor_id, action, entity_type, entity_id,
				changed_fields, created_at)
				SELECT current_setting('app.tenant_id')::uuid, 'staff', 'u-' || (g %% 40), 'act.' || (g %% 30),
				       'ent' || (g %% 8), 'e-' || (g %% 5000), '{status}',
				       timestamptz '2026-11-20 00:00Z' - (g %% 730) * interval '1 day' - (g %% 1440) * interval '1 minute'
				FROM generate_series(1, %d) AS g`, n),
		}
	}
	sql := []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
	}
	sql = append(sql, rows(a, 100000)...)
	sql = append(sql, rows(b, 20000)...)
	sql = append(sql, "ANALYZE identity.audit_logs")
	return testkit.Seed{Name: "identity-audit-plan", SQL: sql}
}

// queryCaptureTx ghi mọi câu Query (và tham số) đi qua tx, để EXPLAIN đúng câu sqlc mà ListAuditLogs chạy.
type queryCaptureTx struct {
	pgx.Tx
	sqls []string
	args [][]any
}

//nolint:ireturn // hiện thực pgx.Tx
func (c *queryCaptureTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.sqls, c.args = append(c.sqls, sql), append(c.args, args)
	return c.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

// [Task 457] 100.000 dòng audit: câu danh sách (không lọc, trang sau, theo hành động, người làm, loại thực thể, thực thể,
// khoảng giờ, kết hợp) bằng crm_app → kế hoạch không có Seq Scan; chạy lặp trên một kết nối, PostgreSQL không chuyển sang
// kế hoạch chung (assertCustomPlansOnly).
func TestListAuditLogs_Plan(t *testing.T) {
	t.Parallel()
	staff := testkit.OpenStaffSeeded(t, auditPlanSeed())
	tenant := testkit.FixedUUID("identity-audit-plan/A")
	if n := staff.CountIn(t, tenant, "SELECT count(*) FROM identity.audit_logs"); n != 100000 {
		t.Fatalf("audit: %d dòng, muốn 100.000", n)
	}
	mid := &identity.AuditCursor{CreatedAt: time.Date(2025, 11, 20, 0, 0, 0, 0, time.UTC), ID: uuid.Max}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		f    identity.AuditFilter
	}{
		{"không lọc", identity.AuditFilter{}},
		{"trang sau", identity.AuditFilter{After: mid}},
		{"hành động", identity.AuditFilter{Action: "act.7"}},
		{"người làm", identity.AuditFilter{ActorID: "u-3"}},
		{"loại thực thể", identity.AuditFilter{EntityType: "ent2"}},
		{"thực thể", identity.AuditFilter{EntityType: "ent2", EntityID: "e-42"}},
		{"khoảng giờ", identity.AuditFilter{From: from, To: from.AddDate(0, 0, 7)}},
		{"hành động, người làm, trang sau", identity.AuditFilter{Action: "act.7", ActorID: "u-7", After: mid}},
		{"không khớp", identity.AuditFilter{Action: "không.có"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ct := &queryCaptureTx{}
			if err := tenancy.WithTenant(t.Context(), staff.App, tenant, func(tx pgx.Tx) error {
				ct.Tx = tx
				_, _, err := identity.ListAuditLogs(t.Context(), ct, c.f)
				return err //nolint:wrapcheck // test
			}); err != nil {
				t.Fatal(err)
			}
			if len(ct.sqls) == 0 || !strings.Contains(ct.sqls[0], "identity.audit_logs") {
				t.Fatalf("không bắt được câu danh sách: %v", ct.sqls)
			}
			nodes, err := testkit.ExplainAsApp(t.Context(), staff.App, tenant, ct.sqls[0], ct.args[0]...)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("kế hoạch: %v", nodes)
			testkit.RequireNoSeqScan(t, nodes)
		})
	}
	assertCustomPlansOnly(t, staff, tenant, cases)
}

// assertCustomPlansOnly chạy câu danh sách 3 lượt mọi bộ lọc trên cùng một kết nối crm_app (pgx giữ câu đã chuẩn bị theo
// kết nối) rồi đọc pg_prepared_statements: PostgreSQL chỉ dùng kế hoạch theo giá trị (custom), không chuyển sang kế
// hoạch chung (generic: với bộ lọc "NULL hoặc bằng" kế hoạch chung quét cả bảng rồi xếp) vì kế hoạch chung đắt hơn hẳn.
func assertCustomPlansOnly(t *testing.T, staff *testkit.Staff, tenant uuid.UUID, cases []struct {
	name string
	f    identity.AuditFilter
},
) {
	t.Helper()
	conn, err := staff.App.Pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for range 3 {
		for _, c := range cases {
			if err := pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
				if _, err := tx.Exec(t.Context(), "SELECT set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
					return err //nolint:wrapcheck // test
				}
				_, _, err := identity.ListAuditLogs(t.Context(), tx, c.f)
				return err //nolint:wrapcheck // test
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	var generic, custom int64
	if err := conn.QueryRow(t.Context(), `SELECT generic_plans, custom_plans FROM pg_prepared_statements
		WHERE statement LIKE '%FROM identity.audit_logs%' AND statement LIKE '%LIMIT 51%'
		  AND statement NOT LIKE '%pg_prepared_statements%'`).Scan(&generic, &custom); err != nil {
		t.Fatal(err)
	}
	t.Logf("kế hoạch theo giá trị: %d, kế hoạch chung: %d", custom, generic)
	if generic != 0 || custom < int64(3*len(cases)) {
		t.Errorf("câu danh sách dùng kế hoạch chung %d lần (theo giá trị %d), muốn 0", generic, custom)
	}
}
