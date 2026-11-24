package deploy_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	got := pkgsExceptSelf(t, affectedMigration(t, name, sql))
	if strings.Join(got, ",") != "internal/platform/db,internal/platform/tenancy,internal/testkit,migrations" {
		t.Fatalf("muốn chỉ các gói luôn chạy, có %v", got)
	}
}

// affectedMigration: chạy affected.py --split với đúng một migration giả name (so với cây rỗng), đòi thoát 0, trả các dòng.
func affectedMigration(t *testing.T, name, sql string) []string {
	t.Helper()
	dir, f := withMigration(t, name, sql)
	lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--base", emptyTree, "--split"}, migDir: dir})
	if code != 0 {
		t.Fatalf("thoát %d", code)
	}
	return lines
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
		wantRunsAll(t, tc.name, "9003_x.sql", tc.sql)
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

// Thư mục có tệp Go đổi mà không khớp gói nào vì lý do khác nhãn build (ở đây: thư mục ẩn ".…" mà go list ./... bỏ qua)
// → thoát 4, bên gọi chạy toàn bộ. Thư mục tạm tạo ở gốc module, xóa khi test xong; thư mục ẩn nên go list ./...,
// git ls-files của các test song song và walkGoFiles của tests/arch (bỏ thư mục ẩn) không thấy nó.
func TestAffected_UnmatchedGoDirExits4(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("../..", ".zz481")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := affected(t, affectedRun{files: []string{filepath.Base(dir) + "/x.go"}, args: []string{"--split"}}); code != 4 {
		t.Fatalf("muốn thoát 4, có %d", code)
	}
}

// Task 481 (24/11): thư mục mà mọi tệp Go mang nhãn build riêng (tests/drills: drill, tests/probe: probe) không thuộc
// bản dựng thường — không gói nào nhập nó, make test không chạy nó — nên đổi tệp ở đó không chọn gói nào và không thoát
// 4 (trước: ci-task chạy toàn bộ test vô ích). Thư mục lẫn tệp có nhãn và không nhãn (internal/imager) vẫn là gói thường.
func TestAffected_BuildTaggedOnlyDirSelectsNothing(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"tests/drills/drill_test.go", "tests/probe/seaweedfs_test.go", "tests/drills/zz481_moi_test.go"} {
		lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--split"}})
		if code != 0 || len(lines) != 0 {
			t.Errorf("%s: muốn thoát 0, không gói nào; có %d %v", f, code, lines)
		}
	}
	lines, code := affected(t, affectedRun{files: []string{"tests/drills/drill_test.go", "internal/imager/kill_perf_test.go"}, args: []string{"--split"}})
	if code != 0 {
		t.Fatalf("thoát %d", code)
	}
	if m := mark(t, lines); len(m) != 1 || m["internal/imager"] != "race" {
		t.Fatalf("muốn đúng internal/imager (race), có %v", m)
	}
}

// Task 481 (24/11): CREATE CONSTRAINT TRIGGER … ON s.t là câu nhận ra được (loại trigger: mọi đối tượng trong câu bị
// chạm). Lỗ đã gặp: migration 0119 (cash COD) làm affected.py thoát 5, ci-task chạy toàn bộ test.
func TestAffected_ConstraintTriggerIsNarrow(t *testing.T) {
	t.Parallel()
	lines, code := affected(t, affectedRun{files: []string{"migrations/0119_cash_cod.sql"}, args: []string{"--base", emptyTree, "--split"}})
	if code != 0 {
		t.Fatalf("0119 có CONSTRAINT TRIGGER trên bảng mới phải được thu hẹp, thoát %d", code)
	}
	wantKinds(t, mark(t, lines), map[string]string{"internal/modules/cash": "plain"})
	wantOnlyAlwaysRun(t, "9007_zz481.sql", `CREATE SCHEMA zz481;
CREATE TABLE zz481.transfers (id uuid PRIMARY KEY);
CREATE TABLE zz481.lines (transfer_id uuid NOT NULL);
CREATE FUNCTION zz481.balanced() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    RETURN NULL;
END
$fn$;
CREATE CONSTRAINT TRIGGER transfers_balanced
    AFTER INSERT OR UPDATE ON zz481.transfers
    FROM zz481.lines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION zz481.balanced();`)
}

// CONSTRAINT TRIGGER mới trên bảng đã có (catalog.tags): người ghi bảng đó chạy lại trigger → được chọn, như trigger thường.
func TestAffected_ConstraintTriggerOnExistingTableTouchesUsers(t *testing.T) {
	t.Parallel()
	lines := affectedMigration(t, "9008_zz481.sql", `CREATE FUNCTION catalog.zz481_check() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    RETURN NULL;
END
$fn$;
CREATE CONSTRAINT TRIGGER tags_zz481
    AFTER INSERT ON catalog.tags
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION catalog.zz481_check();`)
	wantKinds(t, mark(t, lines), map[string]string{"internal/modules/catalog": "plain"})
}

// Task 478 (24/11): tệp dữ liệu (không phải .go, hay trong testdata/) chọn test đọc nó. Lỗ đã gặp: Task 192 đổi
// openapi/openapi.yaml, test của tools/routegen đọc tệp đó bằng đường dẫn (không qua import Go) nên không được chọn,
// main đỏ. Tên tệp giả "zz478…" không có thật: affected.py không đòi tệp đổi còn tồn tại (tệp bị xóa cũng là đổi).
// Đường dẫn tệp thật viết tách làm hai chuỗi (dataPath): luật (b) quét chuỗi của mọi _test.go, viết liền thì đổi
// openapi.yaml, tài liệu… sẽ kéo cả gói này vào mọi lần chạy.

// dataPath nối các mẩu thành đường dẫn tệp dữ liệu mà không để nguyên đường dẫn trong một chuỗi Go.
func dataPath(parts ...string) string { return strings.Join(parts, "") }

// affectedSplit chạy affected.py --split với các tệp đổi, đòi thoát 0, trả dòng ra và bảng gói → race|plain.
func affectedSplit(t *testing.T, files ...string) (lines []string, m map[string]string) {
	t.Helper()
	lines, code := affected(t, affectedRun{files: files, args: []string{"--split"}})
	if code != 0 {
		t.Fatalf("thoát %d: %v", code, lines)
	}
	return lines, mark(t, lines)
}

// wantNoRace: tệp dữ liệu không gây race — không gói nào được chạy -race.
func wantNoRace(t *testing.T, m map[string]string) {
	t.Helper()
	for p, k := range m {
		if k == "race" {
			t.Errorf("%s: chỉ tệp dữ liệu đổi, không được chạy -race", p)
		}
	}
}

// (b) Test đọc tệp theo đường dẫn: chuỗi "openapi.yaml" trong test của tools/routegen → gói đó chạy (thường).
// Có và không --split.
func TestAffected_DataFileSelectsTestsReadingIt(t *testing.T) {
	t.Parallel()
	spec := dataPath("openapi/openapi", ".yaml")
	_, m := affectedSplit(t, spec)
	wantKinds(t, m, map[string]string{"tools/routegen": "plain"})
	wantNoRace(t, m)
	plain, code := affected(t, affectedRun{files: []string{spec}})
	if code != 0 || !slices.Contains(plain, mod+"tools/routegen") {
		t.Fatalf("không --split: muốn có tools/routegen, có %v (thoát %d)", plain, code)
	}
}

// (b) Chỉ trùng tên không đủ: chuỗi "testdata/ts/index.ts" trong test của tools/catalog không trỏ tới
// console/src/api/index.ts (phần đường dẫn đi kèm phải khớp đuôi đường dẫn tệp đổi). Chuỗi có phần đường dẫn khớp
// đuôi thì được chọn: "../../../docs/quyen.md" trong test của identity (tài liệu ngoài gói chỉ theo luật này).
func TestAffected_DataFileNameMatchNeedsPathSuffix(t *testing.T) {
	t.Parallel()
	lines, m := affectedSplit(t, dataPath("console/src/api/index", ".ts"))
	if _, ok := m["tools/catalog"]; ok {
		t.Errorf("tools/catalog chỉ đọc tệp testdata của chính nó, không được chạy khi đổi console: %v", lines)
	}
	lines, m = affectedSplit(t, dataPath("docs/quyen", ".md"))
	wantKinds(t, m, map[string]string{"internal/modules/identity": "plain"})
	if _, ok := m["internal/httpapi"]; ok {
		t.Errorf("luật (b) chỉ chạy test của gói chứa tệp test, không lan lên gói nhập identity: %v", lines)
	}
	wantNoRace(t, m)
}

// (a) Tệp trong testdata/ của gói: chỉ test của gói đó, không lan lên gói nhập nó (httpapi nhập catalog).
// Tệp testdata mà mã chạy nhúng (go:embed testdata của carriertest; kể cả thư mục con mới): như mã, lan lên vtptest.
func TestAffected_TestdataOnlyOwnTestsUnlessEmbedded(t *testing.T) {
	t.Parallel()
	lines, m := affectedSplit(t, "internal/modules/catalog/testdata/zz478.json")
	wantKinds(t, m, map[string]string{"internal/modules/catalog": "plain"})
	if _, ok := m["internal/httpapi"]; ok {
		t.Errorf("testdata chỉ test của catalog dùng, không được lan lên httpapi: %v", lines)
	}
	wantNoRace(t, m)
	_, m = affectedSplit(t, "internal/adapters/carrier/carriertest/testdata/zz478moi/a.json")
	wantKinds(t, m, map[string]string{
		"internal/adapters/carrier/carriertest": "plain", "internal/adapters/carrier/vtp/vtptest": "plain",
	})
	wantNoRace(t, m)
}

// (a) Tệp dữ liệu ngay trong thư mục gói (go:embed, tệp gói đọc lúc chạy): như mã của gói, lan lên gói nhập nó.
// Tài liệu .md ngoài thư mục gói: không chọn gói nào.
func TestAffected_DataFileInPackageDirSpreads(t *testing.T) {
	t.Parallel()
	_, m := affectedSplit(t, "internal/modules/catalog/zz478.tmpl")
	wantKinds(t, m, map[string]string{"internal/modules/catalog": "plain", "internal/httpapi": "plain"})
	wantNoRace(t, m)
	lines, code := affected(t, affectedRun{files: []string{dataPath("docs/zz478", ".md")}, args: []string{"--split"}})
	if code != 0 || len(lines) != 0 {
		t.Fatalf("tài liệu ngoài gói: muốn thoát 0, không gói nào; có %d, %v", code, lines)
	}
}

// (c) Tệp dữ liệu trong vùng rủi ro cao: vẫn chạy cả vùng nhưng test thường; thêm tệp .go viết tay trong vùng → -race.
func TestAffected_DataFileInHighRiskIsPlainUnlessCodeChanges(t *testing.T) {
	t.Parallel()
	data := "internal/modules/order/testdata/zz478.json"
	_, m := affectedSplit(t, data)
	wantKinds(t, m, map[string]string{"internal/modules/order": "plain", "internal/modules/order/internal/state": "plain"})
	wantNoRace(t, m)
	_, m = affectedSplit(t, data, "internal/modules/order/create_m2.go")
	wantKinds(t, m, map[string]string{"internal/modules/order": "race", "internal/modules/order/internal/state": "race"})
}

// Task 479 (24/11): TRUNCATE, DELETE chỉ tính là lệnh thu hẹp khi đứng đầu một câu (ngoài thân hàm). Lỗ đã gặp:
// 0504 (Task 195) chỉ thêm bảng và trigger chỉ-ghi-thêm "BEFORE UPDATE OR DELETE OR TRUNCATE ON <bảng mới>", chữ
// TRUNCATE trong danh sách sự kiện làm ci-task chạy toàn bộ test (~15 phút) thay vì phần ord.

// Migration thật có trigger chỉ-ghi-thêm (sự kiện TRUNCATE) và REVOKE … TRUNCATE: được thu hẹp; 0504 chọn gói order.
func TestAffected_AppendOnlyTriggerMigrationsAreNarrow(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"migrations/0004_audit.sql", "migrations/0115_inventory.sql", "migrations/0504_order_requests.sql"} {
		lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--base", emptyTree, "--split"}})
		if code != 0 {
			t.Errorf("%s: trigger chặn TRUNCATE trên bảng mới phải được thu hẹp, thoát %d", f, code)
			continue
		}
		if f == "migrations/0504_order_requests.sql" {
			wantKinds(t, mark(t, lines), map[string]string{"internal/modules/order": "plain"})
		}
	}
}

// Bảng mới + hàm chỉ-ghi-thêm + trigger BEFORE UPDATE OR DELETE OR TRUNCATE + REVOKE … DELETE, TRUNCATE; hàm mới có
// DELETE FROM, TRUNCATE trên bảng ghi rõ schema trong thân (việc lúc chạy, như INSERT): chỉ các gói luôn chạy.
func TestAffected_TruncateInTriggerOrBodyIsNarrow(t *testing.T) {
	t.Parallel()
	wantOnlyAlwaysRun(t, "9005_zz479.sql", `CREATE SCHEMA zz479;
CREATE TABLE zz479.msgs (id uuid PRIMARY KEY);
CREATE TABLE zz479.tmp (id uuid PRIMARY KEY);
CREATE FUNCTION zz479.msgs_append_only() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    RAISE EXCEPTION 'msgs is append-only (% bị chặn)', TG_OP;
END
$fn$;
CREATE TRIGGER msgs_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON zz479.msgs
    FOR EACH STATEMENT EXECUTE FUNCTION zz479.msgs_append_only();
REVOKE UPDATE, DELETE, TRUNCATE ON zz479.msgs FROM crm_app;
CREATE FUNCTION zz479.clean() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM zz479.tmp WHERE id IS NULL;
    TRUNCATE TABLE ONLY zz479.tmp, zz479.msgs RESTART IDENTITY CASCADE;
END
$$;`)
}

// Xóa dữ liệu thật: câu TRUNCATE/DELETE (cả sau câu khác, chữ thường, giữa hai chuỗi có "$$" — kể cả DEFAULT, thân
// hàm nháy đơn — làm việc tách câu gộp nhiều câu), DELETE lồng ngoài thân hàm, xóa động hay bảng không schema trong
// thân hàm → toàn bộ test (thoát 5).
func TestAffected_DeleteOrTruncateStatementRunsAll(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, sql string }{
		{"TRUNCATE", "TRUNCATE ord.x;"},
		{"DELETE FROM … WHERE", "DELETE FROM ord.x WHERE id IS NULL;"},
		{"truncate sau câu khác", "CREATE TABLE catalog.zz479 (id int);\ntruncate table catalog.tags;"},
		{"DELETE sau thân hàm", "CREATE FUNCTION catalog.f479() RETURNS void LANGUAGE sql AS $$ SELECT 1 $$;\nDELETE FROM catalog.tags;"},
		{"DELETE lồng trong WITH", "WITH d AS (DELETE FROM catalog.tags RETURNING id) SELECT count(*) FROM d;"},
		{"INSERT … DELETE lồng", "INSERT INTO catalog.tags (id) SELECT id FROM (DELETE FROM catalog.tags RETURNING id) d;"},
		{"xóa động trong thân hàm", "CREATE FUNCTION catalog.f479(t text) RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'DELETE FROM ' || t; END $$;"},
		{"xóa bảng không schema trong thân hàm", "CREATE FUNCTION catalog.f479() RETURNS void LANGUAGE plpgsql AS $$ BEGIN DELETE FROM tags; END $$;"},
		{"TRUNCATE bảng không schema trong thân hàm", "CREATE FUNCTION catalog.f479() RETURNS void LANGUAGE plpgsql AS $$ BEGIN TRUNCATE tags; END $$;"},
		{"TRUNCATE danh sách có bảng không schema", "CREATE FUNCTION catalog.f479() RETURNS void LANGUAGE plpgsql AS $$ BEGIN TRUNCATE catalog.tags, tags CASCADE; END $$;"},
		{"$$ trong chuỗi nháy đơn bọc câu DELETE", "COMMENT ON TABLE catalog.tags IS 'a $$';\nDELETE FROM catalog.tags;\nCOMMENT ON TABLE catalog.products IS 'b $$';"},
		{"$$ trong chuỗi nháy đơn bọc câu TRUNCATE", "COMMENT ON TABLE catalog.tags IS 'a $$';\nTRUNCATE catalog.tags;\nCOMMENT ON TABLE catalog.products IS 'b $$';"},
		{"$$ trong DEFAULT bọc câu TRUNCATE", "CREATE TABLE catalog.z479 (n text DEFAULT 'a $$');\nTRUNCATE catalog.tags;\nCREATE TABLE catalog.w479 (n text DEFAULT 'b $$');"},
		{"$$ trong thân hàm nháy đơn bọc câu TRUNCATE", "CREATE FUNCTION catalog.f479() RETURNS text LANGUAGE sql AS 'SELECT ''a $$''';\nTRUNCATE catalog.tags;\nCOMMENT ON TABLE catalog.products IS 'b $$';"},
		{"chữ TRUNCATE trong thông báo của thân hàm", "CREATE FUNCTION catalog.f479() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'cấm TRUNCATE bảng này'; END $$;"},
	} {
		wantRunsAll(t, tc.name, "9006_x.sql", tc.sql)
	}
}

// wantRunsAll: migration giả name (nội dung sql) phải làm affected.py thoát 5 (chạy toàn bộ test).
func wantRunsAll(t *testing.T, caseName, name, sql string) {
	t.Helper()
	dir, f := withMigration(t, name, sql)
	if lines, code := affected(t, affectedRun{files: []string{f}, args: []string{"--base", emptyTree, "--split"}, migDir: dir}); code != 5 {
		t.Errorf("%s: muốn thoát 5 (toàn bộ), có %d %v", caseName, code, lines)
	}
}
