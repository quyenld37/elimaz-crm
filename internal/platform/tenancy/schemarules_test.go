package tenancy_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// platformTables: bảng nền tảng không RLS có cột tenant_id mà M0 hiện có.
func platformTables() []string { return []string{"platform.tenant_routes"} }

func checkRules(t *testing.T, owner *pgxpool.Pool) []string {
	t.Helper()
	got, err := tenancy.CheckSchemaRules(t.Context(), owner, platformTables())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// goodTable trả câu tạo bảng đúng luật tên name trong schema probe (gọi enable_tenant_rls riêng).
func goodTable(name string) string {
	return "CREATE TABLE probe." + name + ` (
		id        uuid PRIMARY KEY DEFAULT uuidv7(),
		tenant_id uuid NOT NULL,
		code      text NOT NULL,
		UNIQUE (tenant_id, id),
		UNIQUE (tenant_id, code)
	)`
}

func TestCheckSchemaRulesCleanSchema(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	if got := checkRules(t, e.owner); len(got) != 0 {
		t.Fatalf("schema sau migrate vi phạm: %v", got)
	}
	probeRLS(t, e.owner)
	if got := checkRules(t, e.owner); len(got) != 0 {
		t.Fatalf("bảng thử đúng luật mà bị báo: %v", got)
	}
	if got, err := tenancy.CheckSchemaRules(t.Context(), e.owner, nil); err != nil || len(got) != 1 ||
		!strings.Contains(got[0], "platform.tenant_routes") {
		t.Fatalf("bỏ platform.tenant_routes khỏi danh sách nền tảng phải báo đúng bảng đó: %v, %v", got, err)
	}
}

func TestCheckSchemaRulesFourViolationsThenPublic(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	ownerExec(t, e.owner,
		"CREATE SCHEMA probe",
		goodTable("good_parent"),
		"SELECT platform.enable_tenant_rls('probe.good_parent')",
		// (c) unique không bắt đầu bằng tenant_id.
		goodTable("bad_unique"),
		"ALTER TABLE probe.bad_unique ADD CONSTRAINT bad_unique_code_only UNIQUE (code)",
		"SELECT platform.enable_tenant_rls('probe.bad_unique')",
		// (d) khóa ngoại một cột giữa hai bảng có tenant_id.
		goodTable("bad_fk"),
		"ALTER TABLE probe.bad_fk ADD COLUMN parent_id uuid REFERENCES probe.good_parent (id)",
		"SELECT platform.enable_tenant_rls('probe.bad_fk')",
		// (a) RLS bật nhưng chưa FORCE.
		goodTable("bad_noforce"),
		"ALTER TABLE probe.bad_noforce ENABLE ROW LEVEL SECURITY",
		`CREATE POLICY tenant_isolation ON probe.bad_noforce
			USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)`,
		// (a) policy không NULLIF.
		goodTable("bad_nonullif"),
		"ALTER TABLE probe.bad_nonullif ENABLE ROW LEVEL SECURITY",
		"ALTER TABLE probe.bad_nonullif FORCE ROW LEVEL SECURITY",
		"CREATE POLICY tenant_isolation ON probe.bad_nonullif USING (tenant_id = current_setting('app.tenant_id')::uuid)",
	)
	got := checkRules(t, e.owner)
	requireOneEach(t, got, "probe.bad_unique", "probe.bad_fk", "probe.bad_noforce", "probe.bad_nonullif")

	ownerExec(t, e.owner, "CREATE TABLE public.stray (x int)")
	got = checkRules(t, e.owner)
	requireOneEach(t, got, "probe.bad_unique", "probe.bad_fk", "probe.bad_noforce", "probe.bad_nonullif", "public.stray")
}

// requireOneEach: got có đúng len(tables) dòng, mỗi dòng nêu đúng một bảng của tables.
func requireOneEach(t *testing.T, got []string, tables ...string) {
	t.Helper()
	if len(got) != len(tables) {
		t.Fatalf("muốn %d dòng, nhận %d: %q", len(tables), len(got), got)
	}
	for _, tbl := range tables {
		n := 0
		for _, line := range got {
			if strings.Contains(line, tbl+":") {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("bảng %s xuất hiện %d lần, muốn 1: %q", tbl, n, got)
		}
	}
}

// Từng luật riêng lẻ, mỗi ca một bảng vi phạm đúng một luật.
func TestCheckSchemaRulesEachRule(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	ownerExec(t, e.owner, "CREATE SCHEMA probe", goodTable("good_parent"),
		"SELECT platform.enable_tenant_rls('probe.good_parent')")
	cases := []struct {
		name  string
		setup []string
		want  string // đoạn phải có trong dòng báo
	}{
		{"rls tắt", []string{goodTable("t")}, "chưa bật"},
		{"không policy", []string{
			goodTable("t"), "ALTER TABLE probe.t ENABLE ROW LEVEL SECURITY", "ALTER TABLE probe.t FORCE ROW LEVEL SECURITY",
		}, "policy"},
		{"thêm policy mở", []string{
			goodTable("t"), "SELECT platform.enable_tenant_rls('probe.t')", "CREATE POLICY open ON probe.t USING (true)",
		}, "NULLIF"},
		{"with check không NULLIF", []string{
			goodTable("t"), "SELECT platform.enable_tenant_rls('probe.t')",
			"CREATE POLICY w ON probe.t FOR INSERT WITH CHECK (tenant_id IS NOT NULL)",
		}, "NULLIF"},
		{"thiếu unique (tenant_id, id)", []string{
			"CREATE TABLE probe.t (id uuid PRIMARY KEY, tenant_id uuid NOT NULL)", "SELECT platform.enable_tenant_rls('probe.t')",
		}, "UNIQUE (tenant_id, id)"},
		{"unique index không bắt đầu bằng tenant_id", []string{
			goodTable("t"), "CREATE UNIQUE INDEX t_code_tenant ON probe.t (code, tenant_id)",
			"SELECT platform.enable_tenant_rls('probe.t')",
		}, "t_code_tenant"},
		{"khóa ngoại ghép nhưng không nối tenant_id với tenant_id", []string{
			goodTable("t"), "ALTER TABLE probe.t ADD COLUMN other uuid, ADD COLUMN parent_id uuid",
			"ALTER TABLE probe.t ADD CONSTRAINT t_fk FOREIGN KEY (other, parent_id) REFERENCES probe.good_parent (tenant_id, id)",
			"SELECT platform.enable_tenant_rls('probe.t')",
		}, "t_fk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Cleanup(func() { // t.Context() đã hủy khi dọn
				if _, err := e.owner.Exec(context.Background(), "DROP TABLE IF EXISTS probe.t"); err != nil {
					t.Error(err)
				}
			})
			ownerExec(t, e.owner, c.setup...)
			got := checkRules(t, e.owner)
			if len(got) != 1 || !strings.Contains(got[0], "probe.t:") || !strings.Contains(got[0], c.want) {
				t.Fatalf("muốn đúng 1 dòng về probe.t chứa %q, nhận %q", c.want, got)
			}
		})
	}
}

// platform.idempotency_keys có RLS (kế hoạch M0, Global Constraints; ADR-41) nên không nằm trong danh sách miễn: bảng
// có mặt sau migrate, qua mọi luật, và bỏ FORCE thì CheckSchemaRules báo đúng bảng này.
func TestCheckSchemaRulesCoversIdempotencyKeys(t *testing.T) {
	e := newEnv(t, db.PoolOptions{})
	var n int
	if err := e.owner.QueryRow(t.Context(),
		"SELECT count(*) FROM pg_catalog.pg_class WHERE oid = to_regclass('platform.idempotency_keys')").Scan(&n); err != nil || n != 1 {
		t.Fatalf("platform.idempotency_keys phải có sau migrate: %d, %v", n, err)
	}
	if slices.Contains(platformTables(), "platform.idempotency_keys") {
		t.Fatal("platform.idempotency_keys có RLS, không được nằm trong danh sách miễn")
	}
	if got := checkRules(t, e.owner); len(got) != 0 {
		t.Fatalf("schema sau migrate vi phạm: %v", got)
	}
	ownerExec(t, e.owner, "ALTER TABLE platform.idempotency_keys NO FORCE ROW LEVEL SECURITY")
	requireOneEach(t, checkRules(t, e.owner), "platform.idempotency_keys")
}
