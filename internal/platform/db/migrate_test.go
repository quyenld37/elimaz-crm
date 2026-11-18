package db_test

import (
	"errors"
	"io/fs"
	"maps"
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
	all := migrateTwiceConcurrently(t, func() ([]string, error) { return db.Migrate(t.Context(), owner) })
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

// Tệp đến lệch số (Task 318): database đã chạy 9011 và 9013, rồi 9012 được gộp sau. Ba tệp dùng chung ở các test dưới.
const (
	oooLow  = "9011_ooo_low.sql"
	oooMid  = "9012_ooo_mid.sql"
	oooHigh = "9013_ooo_high.sql"
)

func oooBase() map[string]string {
	return map[string]string{
		oooLow:  "-- +goose Up\nCREATE TABLE platform.ooo_low (id int);\n",
		oooHigh: "-- +goose Up\nCREATE TABLE platform.ooo_high (id int);\n",
	}
}

// oooApplied dựng database mới đã chạy mọi migration thật cùng 9011, 9013 (chưa có 9012).
func oooApplied(t *testing.T) string {
	t.Helper()
	_, owner, _ := testkit.CreateDatabase(t)
	if _, err := db.MigrateFS(t.Context(), owner, realPlus(t, oooBase())); err != nil {
		t.Fatalf("chạy 9011, 9013: %v", err)
	}
	return owner
}

func oooWith(t *testing.T, extra map[string]string) fstest.MapFS {
	t.Helper()
	m := oooBase()
	maps.Copy(m, extra)
	return realPlus(t, m)
}

func TestMigrateAppliesOutOfOrderFile(t *testing.T) {
	owner := oooApplied(t)
	v2 := oooWith(t, map[string]string{oooMid: "-- +goose Up\nCREATE TABLE platform.ooo_mid (id int);\n"})
	applied, err := db.MigrateFS(t.Context(), owner, v2)
	if err != nil {
		t.Fatalf("tệp số nhỏ đến sau phải chạy được: %v", err)
	}
	if !slices.Equal(applied, []string{oooMid}) {
		t.Fatalf("áp dụng %v, muốn đúng [%s]", applied, oooMid)
	}
	if !regclassExists(t, owner, "platform.ooo_mid") {
		t.Fatal("thiếu bảng của tệp lệch số")
	}
	again, err := db.MigrateFS(t.Context(), owner, v2)
	if err != nil || len(again) != 0 {
		t.Fatalf("lần sau: %v %v, muốn []", again, err)
	}
	// Checksum của tệp lệch số cũng được ghi: sửa nó sau khi áp dụng thì bị từ chối.
	v3 := oooWith(t, map[string]string{oooMid: "-- +goose Up\nCREATE TABLE platform.ooo_mid (id bigint);\n"})
	_, err = db.MigrateFS(t.Context(), owner, v3)
	if !errors.Is(err, db.ErrMigration) || !strings.Contains(err.Error(), oooMid) || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("sửa tệp lệch số đã áp dụng phải bị từ chối, nêu tên tệp và checksum; nhận %v", err)
	}
}

func TestMigrateOutOfOrderStillRejectsChangedAppliedFile(t *testing.T) {
	owner := oooApplied(t)
	v2 := oooWith(t, map[string]string{
		oooLow: "-- +goose Up\nCREATE TABLE platform.ooo_low (id bigint);\n",
		oooMid: "-- +goose Up\nCREATE TABLE platform.ooo_mid (id int);\n",
	})
	_, err := db.MigrateFS(t.Context(), owner, v2)
	if !errors.Is(err, db.ErrMigration) || !strings.Contains(err.Error(), oooLow) || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tệp đã áp dụng bị sửa phải bị từ chối, nêu tên tệp và checksum; nhận %v", err)
	}
	if regclassExists(t, owner, "platform.ooo_mid") {
		t.Fatal("bị từ chối thì tệp lệch số không được chạy")
	}
}

func TestMigrateOutOfOrderRejectsContractWithoutMarker(t *testing.T) {
	owner := oooApplied(t)
	v2 := oooWith(t, map[string]string{oooMid: "-- +goose Up\nDROP TABLE platform.ooo_low;\n"})
	_, err := db.MigrateFS(t.Context(), owner, v2)
	if !errors.Is(err, db.ErrMigration) || !strings.Contains(err.Error(), oooMid) {
		t.Fatalf("tệp lệch số có lệnh thu hẹp thiếu -- contract: phải bị từ chối, nêu tên tệp; nhận %v", err)
	}
	if !regclassExists(t, owner, "platform.ooo_low") {
		t.Fatal("lệnh DROP không được chạy")
	}
}

func TestMigrateOutOfOrderFailedFileLeavesNoTable(t *testing.T) {
	owner := oooApplied(t)
	v2 := oooWith(t, map[string]string{oooMid: "-- +goose Up\n" +
		"CREATE TABLE platform.ooo_mid (id int);\n" +
		"CREATE TABLE platform.ooo_mid_b (id no_such_type);\n"})
	_, err := db.MigrateFS(t.Context(), owner, v2)
	if err == nil || !strings.Contains(err.Error(), oooMid) {
		t.Fatalf("tệp lệch số có lỗi SQL phải trả lỗi nêu tên tệp; nhận %v", err)
	}
	if regclassExists(t, owner, "platform.ooo_mid") {
		t.Fatal("bảng của tệp lỗi không được còn lại")
	}
}

func TestMigrateOutOfOrderConcurrentRunsApplyOnce(t *testing.T) {
	owner := oooApplied(t)
	v2 := oooWith(t, map[string]string{oooMid: "-- +goose Up\nCREATE TABLE platform.ooo_mid (id int);\n"})
	all := migrateTwiceConcurrently(t, func() ([]string, error) { return db.MigrateFS(t.Context(), owner, v2) })
	if !slices.Equal(all, []string{oooMid}) {
		t.Fatalf("hai lần chạy cộng lại áp dụng %v, muốn đúng một lần [%s]", all, oooMid)
	}
}

// migrateTwiceConcurrently chạy migrate hai lần song song, đòi cả hai không lỗi, trả các tệp hai lần áp dụng cộng lại
// (theo thứ tự lần chạy, chưa sắp xếp).
func migrateTwiceConcurrently(t *testing.T, migrate func() ([]string, error)) []string {
	t.Helper()
	var wg sync.WaitGroup
	results := make([][]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { results[i], errs[i] = migrate() })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("hai lần chạy song song: %v / %v", errs[0], errs[1])
	}
	return slices.Concat(results[0], results[1])
}
