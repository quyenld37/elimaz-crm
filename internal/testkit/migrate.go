package testkit

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/migrations"
)

// MigrateBefore chạy bằng goose (như db.Migrate) mọi migration có tên nhỏ hơn before (ví dụ "0201") trên database
// ownerURL, để test dựng dữ liệu ở trạng thái của mốc trước rồi chạy db.Migrate thật (test mở rộng bảng của mốc trước).
//
// Khi nào không dùng: test thường dùng OpenStaff hay CreateDatabase rồi db.Migrate (đủ mọi migration).
func MigrateBefore(t testing.TB, ownerURL, before string) {
	t.Helper()
	ctx := t.Context()
	cc, err := pgx.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	cc.DefaultQueryExecMode = pgx.QueryExecModeExec
	conn, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS platform"); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)

	old := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < before {
			b, err := fs.ReadFile(migrations.FS, e.Name())
			if err != nil {
				t.Fatal(err)
			}
			old[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	sqlDB := stdlib.OpenDB(*cc)
	defer func() { _ = sqlDB.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, old,
		goose.WithTableName("platform.goose_db_version"), goose.WithDisableGlobalRegistry(true),
		goose.WithAllowOutofOrder(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate tới trước %s: %v", before, err)
	}
}

// OpenDB mở pool tới url (owner hay crm_app của database thử), đóng khi test xong; lỗi làm test dừng.
func OpenDB(t testing.TB, url string) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), url, db.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

// InTenant chạy fn trong tenancy.WithTenant của tenant trên d; lỗi làm test dừng.
func InTenant(t testing.TB, d *db.DB, tenant uuid.UUID, fn func(tx pgx.Tx) error) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), d, tenant, fn); err != nil {
		t.Fatal(err)
	}
}
