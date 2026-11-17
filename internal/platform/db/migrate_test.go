package db_test

import (
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/migrations"
)

func migrationNames(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(names) == 0 {
		t.Fatalf("migrations.FS: %v %v", names, err)
	}
	slices.Sort(names)
	return names
}

// realPlus trả FS gồm mọi migration thật cộng các tệp thêm. Tệp thêm dùng số 9xxx, ngoài mọi dải migration thật
// (Q-X7), để không trùng số khi mốc thêm migration.
func realPlus(t *testing.T, extra map[string]string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, n := range migrationNames(t) {
		b, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatal(err)
		}
		m[n] = &fstest.MapFile{Data: b}
	}
	for n, body := range extra {
		m[n] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func regclassExists(t *testing.T, ownerURL, name string) bool {
	t.Helper()
	var ok bool
	if err := ownerConn(t, ownerURL).QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestMigrateAppliesAllInOrderThenNothing(t *testing.T) {
	_, owner, _ := testkit.CreateDatabase(t)
	applied, err := db.Migrate(t.Context(), owner)
	if err != nil {
		t.Fatalf("Migrate lần đầu: %v", err)
	}
	if want := migrationNames(t); !slices.Equal(applied, want) {
		t.Fatalf("lần đầu áp dụng %v, muốn %v", applied, want)
	}
	again, err := db.Migrate(t.Context(), owner)
	if err != nil || len(again) != 0 {
		t.Fatalf("lần hai: %v %v, muốn []", again, err)
	}
	for _, tbl := range []string{"platform.tenants", "platform.tenant_routes", "platform.migration_checksums", "platform.goose_db_version"} {
		if !regclassExists(t, owner, tbl) {
			t.Errorf("thiếu bảng %s", tbl)
		}
	}
	if regclassExists(t, owner, "public.goose_db_version") {
		t.Error("bảng phiên bản của goose không được nằm trong public")
	}
}

func TestMigrateConcurrentRunsApplyOnce(t *testing.T) {
	_, owner, _ := testkit.CreateDatabase(t)
	var wg sync.WaitGroup
	results := make([][]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { results[i], errs[i] = db.Migrate(t.Context(), owner) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("hai lần chạy song song: %v / %v", errs[0], errs[1])
	}
	all := slices.Concat(results[0], results[1])
	slices.Sort(all)
	if want := migrationNames(t); !slices.Equal(all, want) {
		t.Fatalf("hai lần chạy cộng lại áp dụng %v, muốn đúng một lần mỗi tệp %v", all, want)
	}
}

func TestMigrateRejectsChangedAppliedFile(t *testing.T) {
	_, owner, _ := testkit.CreateDatabase(t)
	const name = "9001_probe.sql"
	v1 := realPlus(t, map[string]string{name: "-- +goose Up\nCREATE TABLE platform.probe_a (id int);\n"})
	if _, err := db.MigrateFS(t.Context(), owner, v1); err != nil {
		t.Fatal(err)
	}
	v2 := realPlus(t, map[string]string{name: "-- +goose Up\nCREATE TABLE platform.probe_a (id bigint);\n"})
	_, err := db.MigrateFS(t.Context(), owner, v2)
	if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("sửa tệp đã áp dụng phải bị từ chối, nêu tên tệp và checksum; nhận %v", err)
	}
}

func TestMigrateFailedFileLeavesNoTable(t *testing.T) {
	_, owner, _ := testkit.CreateDatabase(t)
	bad := realPlus(t, map[string]string{"9002_bad.sql": "-- +goose Up\n" +
		"CREATE TABLE platform.probe_a (id int);\n" +
		"CREATE TABLE platform.probe_b (id no_such_type);\n"})
	if _, err := db.MigrateFS(t.Context(), owner, bad); err == nil {
		t.Fatal("tệp có lỗi SQL phải trả lỗi")
	}
	if regclassExists(t, owner, "platform.probe_a") {
		t.Fatal("bảng của tệp lỗi không được còn lại")
	}
	if !regclassExists(t, owner, "platform.tenants") {
		t.Fatal("tệp trước tệp lỗi vẫn phải được áp dụng")
	}
}

func TestMigrateRejectsContractWithoutMarker(t *testing.T) {
	_, owner, _ := testkit.StartPostgres(t)
	const name = "9003_drop.sql"
	bad := realPlus(t, map[string]string{name: "-- +goose Up\nDROP TABLE platform.tenant_routes;\n"})
	_, err := db.MigrateFS(t.Context(), owner, bad)
	if err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("tệp thu hẹp thiếu -- contract: phải bị từ chối và nêu tên tệp; nhận %v", err)
	}
	if !regclassExists(t, owner, "platform.tenant_routes") {
		t.Fatal("lệnh DROP không được chạy")
	}
}
