package deploy_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// scripts/agent/affected.py chọn gói cần test cho cổng kiểm của task (ci-task.sh) và hook Stop (Task 473, 23/11, chủ
// shop: máy nóng, "chỉ test các case ảnh hưởng"): migration mới chỉ thêm → gói dùng tới đối tượng nó chạm (nối qua view,
// hàm, trigger); migration thu hẹp, sửa tệp cũ hay câu không chắc → thoát 5 (toàn bộ); --split tách gói sửa trực tiếp
// (-race) khỏi gói chỉ dùng tới (test thường). Bỏ sót ở đây nghĩa là một migration làm hỏng gói mà không test nào chạy.

// emptyTree là cây rỗng git luôn biết: mọi migration đưa vào đều là "mới" (không cần lịch sử, chạy được trên bản clone nông).
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

const mod = "elimaz.com.vn/crm/"

type affectedRun struct {
	files  []string
	args   []string
	migDir string // khác rỗng: AFFECTED_MIGRATIONS_DIR (migration giả cạnh bản thật)
}

// affected chạy affected.py ở gốc module; trả từng dòng stdout và mã thoát (0, 4, 5; mã khác là lỗi của test).
func affected(t *testing.T, r affectedRun) (lines []string, code int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "python3", append([]string{"scripts/agent/affected.py"}, r.args...)...)
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader(strings.Join(r.files, "\n") + "\n")
	cmd.Env = append(cmd.Environ(), "HIGH_RISK_DIRS=internal/modules/order internal/modules/cash")
	if r.migDir != "" {
		cmd.Env = append(cmd.Env, "AFFECTED_MIGRATIONS_DIR="+r.migDir)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		t.Fatalf("chạy affected.py: %v", err)
	}
	if code != 0 && code != 4 && code != 5 {
		t.Fatalf("affected.py thoát %d: %s", code, errb.String())
	}
	for l := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, code
}

// mark đổi các dòng "race <gói>"/"plain <gói>" thành gói (bỏ tiền tố module) → loại.
func mark(t *testing.T, lines []string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, l := range lines {
		kind, pkg, ok := strings.Cut(l, " ")
		if !ok || (kind != "race" && kind != "plain") {
			t.Fatalf("dòng không đúng dạng --split: %q", l)
		}
		m[strings.TrimPrefix(pkg, mod)] = kind
	}
	return m
}

// withMigration chép mọi migration thật vào thư mục tạm và thêm một migration giả; trả thư mục và đường dẫn tệp đổi.
func withMigration(t *testing.T, name, sql string) (dir, changed string) {
	t.Helper()
	dir = t.TempDir()
	migs, err := filepath.Glob("../../migrations/*.sql")
	if err != nil || len(migs) == 0 {
		t.Fatalf("không thấy migration thật: %v", err)
	}
	for _, f := range migs {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f)), b, 0o600); err != nil { //nolint:gosec // G703: bản chép migration trong repo vào thư mục tạm của test
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("-- +goose Up\n"+sql+"\n-- +goose Down\nDROP TABLE x;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, "migrations/" + name
}

// pkgsExceptSelf trả danh sách gói đã sắp, bỏ chính tests/deploy: tệp này chứa tên đối tượng giả (zz473.things…) nên
// affected.py chọn tests/deploy chỉ khi tệp đã được git theo dõi; kết quả test không được phụ thuộc trạng thái đó.
func pkgsExceptSelf(t *testing.T, lines []string) []string {
	t.Helper()
	got := make([]string, 0, len(lines))
	for p := range mark(t, lines) {
		if p != "tests/deploy" {
			got = append(got, p)
		}
	}
	sort.Strings(got)
	return got
}

func wantKinds(t *testing.T, m map[string]string, want map[string]string) {
	t.Helper()
	for p, k := range want {
		if m[p] != k {
			t.Errorf("%s: muốn %q, có %q", p, k, m[p])
		}
	}
}

// Migration chỉ thêm cột vào view fulfillment.shipments_v: test mọi gói đọc view (kể cả SQL dựng lúc chạy ở
// internal/queries, gói cash đọc view), chạy test thường, không -race; bộ chạy migration và testkit luôn có.
func TestAffected_AdditiveMigrationTestsViewUsers(t *testing.T) {
	t.Parallel()
	lines, code := affected(t, affectedRun{
		files: []string{"migrations/0331_fulfillment_shipments_v_province.sql"},
		args:  []string{"--base", emptyTree, "--split"},
	})
	if code != 0 {
		t.Fatalf("migration chỉ thêm phải được thu hẹp, thoát %d", code)
	}
	m := mark(t, lines)
	wantKinds(t, m, map[string]string{
		"internal/modules/fulfillment": "plain", "internal/modules/cash": "plain",
		"internal/modules/cash/internal/reconcile": "plain", "internal/queries": "plain", "internal/httpapi": "plain",
		"internal/platform/db": "plain", "internal/testkit": "plain", "migrations": "plain", "internal/platform/tenancy": "plain",
	})
	for p, k := range m {
		if k == "race" {
			t.Errorf("%s: migration không sửa mã viết tay nào, không được chạy -race", p)
		}
	}
	if _, ok := m["internal/modules/promotion"]; ok {
		t.Errorf("promotion không đọc shipments_v, không được chạy: %v", lines)
	}
}

// Bảng mới (0501): gói dùng bảng mới chạy; bảng cha của khóa ngoại (platform.tenants, identity.users) KHÔNG bị kéo
// vào khi tạo bảng mới (chưa ai ghi bảng mới thì không đổi gì ở bảng cha).
func TestAffected_NewTableTestsItsUsersOnly(t *testing.T) {
	t.Parallel()
	lines, code := affected(t, affectedRun{
		files: []string{"migrations/0501_service_keys.sql"},
		args:  []string{"--base", emptyTree, "--split"},
	})
	if code != 0 {
		t.Fatalf("thoát %d", code)
	}
	m := mark(t, lines)
	wantKinds(t, m, map[string]string{"internal/modules/identity": "plain", "internal/httpapi": "plain", "cmd/crm": "plain"})
	for _, p := range []string{"internal/platform/xlsx", "internal/imager", "internal/platform/textnorm"} {
		if _, ok := m[p]; ok {
			t.Errorf("%s không dùng identity.service_keys (cũng không nhập gói dùng nó), không được chạy", p)
		}
	}
}

// Schema mới + bảng mới trong đó + quyền dùng schema đó: chưa gói nào dùng → chỉ bộ chạy migration, testkit, tenancy
// (soát luật RLS của mọi bảng). Chính gói test này có thể có hoặc không (xem pkgsExceptSelf).
func TestAffected_NewSchemaNewTable(t *testing.T) {
	t.Parallel()
	wantOnlyAlwaysRun(t, "9001_zz473.sql", `CREATE SCHEMA zz473;
GRANT USAGE ON SCHEMA zz473 TO crm_app, crm_search;
CREATE TABLE zz473.things (id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES platform.tenants (id));
SELECT platform.enable_tenant_rls('zz473.things');
GRANT SELECT, INSERT ON zz473.things TO crm_app;
CREATE INDEX things_tenant ON zz473.things (tenant_id);`)
}

// Migration chỉ có chú thích: không đối tượng nào bị chạm → chỉ các gói luôn chạy khi có migration (không phải mọi gói).
func TestAffected_CommentOnlyMigration(t *testing.T) {
	t.Parallel()
	wantOnlyAlwaysRun(t, "9004_x.sql", "COMMENT ON TABLE catalog.tags IS 'nhãn';")
}

// wantOnlyAlwaysRun: migration giả name chỉ được chọn các gói luôn chạy khi có migration (bộ chạy migration, db,
// testkit, tenancy) — ngoài chính tests/deploy (xem pkgsExceptSelf).
func wantOnlyAlwaysRun(t *testing.T, name, sql string) {
	t.Helper()
	dir, f := withMigration(t, name, sql)
	lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--base", emptyTree, "--split"}, migDir: dir})
	if code != 0 {
		t.Fatalf("thoát %d", code)
	}
	got := pkgsExceptSelf(t, lines)
	if strings.Join(got, ",") != "internal/platform/db,internal/platform/tenancy,internal/testkit,migrations" {
		t.Fatalf("muốn chỉ các gói luôn chạy, có %v", got)
	}
}

// Câu sửa bảng đã có kéo theo người dùng bảng đó; khóa ngoại mới kéo cả bảng cha (xóa ở bảng cha có thể bị chặn);
// REVOKE trên một bảng kéo người dùng bảng đó.
func TestAffected_AlterAndRevokeTouchUsers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, sql, wantPkg string }{
		{"khóa ngoại tới bảng cha", "ALTER TABLE catalog.tags ADD COLUMN owner_id uuid REFERENCES identity.users (id);", "internal/modules/identity"},
		{"REVOKE trên bảng", "REVOKE UPDATE ON catalog.products FROM crm_app;", "internal/modules/catalog"},
		{"policy dùng bảng khác", "CREATE POLICY p473 ON catalog.tags USING (EXISTS (SELECT 1 FROM identity.users));", "internal/modules/identity"},
	} {
		dir, f := withMigration(t, "9002_x.sql", tc.sql)
		lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--base", emptyTree, "--split"}, migDir: dir})
		if code != 0 {
			t.Errorf("%s: thoát %d", tc.name, code)
			continue
		}
		if k := mark(t, lines)[tc.wantPkg]; k != "plain" {
			t.Errorf("%s: %s muốn plain, có %q", tc.name, tc.wantPkg, k)
		}
	}
}

// Lệnh thu hẹp, pha contract, sửa migration đã có ở bản so, câu không chắc → toàn bộ test (thoát 5).
func TestAffected_NarrowingOrUnsureMigrationRunsAll(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, file, base string }{
		{"DROP", "migrations/0302_ord_return_items_qty.sql", emptyTree},
		{"contract", "migrations/0132_inventory_insert_columns.sql", emptyTree},
		{"sửa tệp cũ", "migrations/0331_fulfillment_shipments_v_province.sql", "HEAD"},
		{"migration bị xóa", "migrations/9999_khong_co.sql", emptyTree},
	} {
		if _, code := affected(t, affectedRun{files: []string{tc.file}, args: []string{"--base", tc.base, "--split"}}); code != 5 {
			t.Errorf("%s: muốn thoát 5 (toàn bộ), có %d", tc.name, code)
		}
	}
	for _, tc := range []struct{ name, sql string }{
		{"DO $$", "DO $$ BEGIN EXECUTE 'ALTER TABLE catalog.tags ADD COLUMN x int'; END $$;"},
		{"DO $tag$", "DO $body$ BEGIN PERFORM 1; END $body$;"},
		{"quyền cả schema", "GRANT SELECT ON ALL TABLES IN SCHEMA catalog TO crm_search;"},
		{"quyền dùng schema đã có", "GRANT USAGE ON SCHEMA catalog TO crm_search;"},
		{"bảng không ghi schema", "CREATE TABLE things (id int);"},
		{"INSERT không schema trong thân hàm", "CREATE FUNCTION catalog.f473() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO audit_rows VALUES (1); RETURN NEW; END $$;"},
		{"CREATE RULE", "CREATE RULE r473 AS ON INSERT TO catalog.tags DO ALSO INSERT INTO catalog.products (id) VALUES (NULL);"},
		{"ALTER COLUMN", "ALTER TABLE catalog.tags ALTER COLUMN name TYPE varchar(10);"},
		{"đổi kiểu enum", "ALTER TYPE catalog.kind ADD VALUE 'x';"},
		{"gọi hàm không rõ đối tượng", "SELECT platform.rebuild_everything();"},
		{"tạo lại schema đã có", "CREATE SCHEMA catalog;"},
	} {
		dir, f := withMigration(t, "9003_x.sql", tc.sql)
		if lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--base", emptyTree, "--split"}, migDir: dir}); code != 5 {
			t.Errorf("%s: muốn thoát 5 (toàn bộ), có %d %v", tc.name, code, lines)
		}
	}
}

// Không có --base (merge-task, lệnh cũ): tệp .sql bị bỏ qua như trước, không thoát 5.
func TestAffected_MigrationIgnoredWithoutBase(t *testing.T) {
	t.Parallel()
	lines, code := affected(t, affectedRun{files: []string{"migrations/0302_ord_return_items_qty.sql"}})
	if code != 0 || len(lines) != 0 {
		t.Fatalf("muốn thoát 0, không gói nào; có %d, %v", code, lines)
	}
}

// Mã viết tay đổi → gói đó và vùng rủi ro cao của nó chạy -race; gói chỉ dùng tới chạy thường. Mã sinh (sqlc store)
// đổi theo nguồn: lan lên gói dùng nó nhưng không tính là sửa trực tiếp, không kéo cả vùng rủi ro cao vào -race.
// Không có --split: mỗi dòng một gói như cũ.
func TestAffected_SplitRaceOnlyForHandEditedPackages(t *testing.T) {
	t.Parallel()
	files := []string{"internal/modules/order/create_m2.go", "internal/modules/cash/internal/store/models.go"}
	lines, code := affected(t, affectedRun{files: files, args: []string{"--split"}})
	if code != 0 {
		t.Fatalf("thoát %d", code)
	}
	m := mark(t, lines)
	wantKinds(t, m, map[string]string{
		"internal/modules/order":                "race",
		"internal/modules/order/internal/state": "race",
		"internal/httpapi":                      "plain",
		"internal/modules/cash/internal/store":  "plain",
		"internal/modules/cash":                 "plain",
	})
	plain, code := affected(t, affectedRun{files: files})
	if code != 0 || len(plain) != len(lines) {
		t.Fatalf("không --split: muốn %d gói, có %d (thoát %d)", len(lines), len(plain), code)
	}
	for _, l := range plain {
		if !strings.HasPrefix(l, mod) {
			t.Errorf("không --split phải in import path trần, có %q", l)
		}
	}
}

// Thư mục có tệp Go đổi mà không khớp gói nào (chỉ có tệp nhãn build riêng) → thoát 4, bên gọi chạy toàn bộ.
func TestAffected_UnmatchedGoDirExits4(t *testing.T) {
	t.Parallel()
	if _, code := affected(t, affectedRun{files: []string{"tests/probe/seaweedfs_test.go"}, args: []string{"--split"}}); code != 4 {
		t.Fatalf("muốn thoát 4, có %d", code)
	}
}
