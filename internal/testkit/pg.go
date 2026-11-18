// Package testkit dựng hạ tầng thật cho test: mỗi test một database PostgreSQL 18 riêng trên container dev của
// phiên (docker/compose.dev.yml), đã chạy db.Migrate (nhân bản từ database mẫu, migrate một lần). Chỉ dùng trong tệp _test.go.
package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/migrations"
)

// Mật khẩu cố định của role dev trong docker/compose.dev.yml và docker/postgres/init/01-roles.sql (chỉ database dev).
const (
	devAdminUser = "postgres"
	devAdminPass = "postgres_dev"  //nolint:gosec // mật khẩu dev công khai trong compose, không dùng ở máy chủ
	devOwnerPass = "crm_owner_dev" //nolint:gosec // như trên
	devAppPass   = "crm_app_dev"   //nolint:gosec // như trên
)

// createFilesyncRole tạo role crm_filesync của container chép tệp (migration 0126 cấp quyền cho nó; crm_owner không có
// CREATEROLE nên người quản trị tạo trước, như docker/postgres/init/01-roles.sql). Cụm dev dựng trước Task M1-45 chưa
// có role; nhiều tiến trình test cùng tạo thì lần đến sau bỏ qua.
const createFilesyncRole = `DO $$
BEGIN
    CREATE ROLE crm_filesync LOGIN NOBYPASSRLS CONNECTION LIMIT 1 PASSWORD 'crm_filesync_dev';
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL;
END $$`

// StartPostgres tạo database crm_test_<ngẫu nhiên> (chủ crm_owner) đã chạy db.Migrate, xóa khi test xong. Trả chuỗi
// kết nối của người quản trị, chủ bảng và crm_app. Database được nhân bản từ database mẫu crm_tpl_<băm bộ migration>
// (migrate một lần mỗi giờ cho mỗi bộ migration trên máy chủ thử, xem pgtemplate.go): giá trị now() mà migration ghi
// vào dữ liệu là của lúc dựng mẫu, cũ tối đa một giờ.
func StartPostgres(t testing.TB) (adminURL, ownerURL, appURL string) {
	t.Helper()
	return cloneFromTemplate(t, migrations.FS, migrateDB, time.Now())
}

// migrateDB là migrateFunc của database thật: db.Migrate luôn dùng migrations.FS.
func migrateDB(ctx context.Context, ownerURL string, _ fs.FS) error {
	if _, err := db.Migrate(ctx, ownerURL); err != nil {
		return fmt.Errorf("db.Migrate: %w", err)
	}
	return nil
}

// OpenAppDB tạo database thử như StartPostgres và mở pool bằng crm_app với tùy chọn o; pool đóng khi test xong.
func OpenAppDB(t testing.TB, o db.PoolOptions) *db.DB {
	t.Helper()
	_, _, appURL := StartPostgres(t)
	d, err := db.Open(t.Context(), appURL, o)
	if err != nil {
		t.Fatalf("testkit: mở database thử bằng crm_app: %v", err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

// CreateDatabase như StartPostgres nhưng chưa migrate (test của chính Migrate và vai trò migrate). Người quản trị tạo
// sẵn extension pg_stat_statements (không phải extension "trusted" nên crm_owner không tự tạo được) và cho crm_owner
// quyền cấp pg_monitor (migration 0010).
//
// Máy chủ PostgreSQL: CRM_TEST_ADMIN_URL nếu có; không thì lấy máy và cổng từ CRM_TEST_DATABASE_URL (Makefile dựng
// từ CRM_PG_PORT) với role dev. Thiếu cả hai thì test đỏ, không bỏ qua.
func CreateDatabase(t testing.TB) (adminURL, ownerURL, appURL string) {
	t.Helper()
	base := serverURL(t)
	name, adminURL, ownerURL, appURL := newTestDB(t, base)
	dropAtCleanup(t, base, name)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	withAdmin(ctx, t, base, func(admin *pgx.Conn) {
		exec(ctx, t, admin, "CREATE DATABASE "+name+" OWNER crm_owner")
		prepareEmpty(ctx, t, admin, adminURL)
	})
	return adminURL, ownerURL, appURL
}

// newTestDB đặt tên database test crm_test_<ngẫu nhiên> trên máy chủ base và trả chuỗi kết nối của người quản trị,
// chủ bảng và crm_app tới nó (chưa tạo database).
func newTestDB(t testing.TB, base *url.URL) (name, adminURL, ownerURL, appURL string) {
	t.Helper()
	name = testDBPrefix + randomSuffix(t)
	return name, withDB(base, nil, name), withDB(base, url.UserPassword("crm_owner", devOwnerPass), name),
		withDB(base, url.UserPassword("crm_app", devAppPass), name)
}

// withAdmin mở kết nối người quản trị tới database mặc định của máy chủ base, chạy fn, rồi đóng kết nối.
func withAdmin(ctx context.Context, t testing.TB, base *url.URL, fn func(admin *pgx.Conn)) {
	t.Helper()
	admin := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	fn(admin)
}

// prepareEmpty làm phần của người quản trị trên database vừa tạo (adminURL), trước khi migrate: quyền cấp pg_monitor
// cho crm_owner, role crm_filesync và extension pg_stat_statements. admin nối vào database mặc định của máy chủ. Đổi hàm này thì đổi
// templateRecipe (pgtemplate.go).
func prepareEmpty(ctx context.Context, t testing.TB, admin *pgx.Conn, adminURL string) {
	t.Helper()
	// Migration 0010 cấp pg_monitor cho crm_app; cụm dev dựng trước Task 19 chưa có dòng này trong 01-roles.sql.
	exec(ctx, t, admin, "GRANT pg_monitor TO crm_owner WITH ADMIN OPTION")
	exec(ctx, t, admin, createFilesyncRole)
	inNew := connect(ctx, t, adminURL)
	defer func() { _ = inNew.Close(context.WithoutCancel(ctx)) }()
	exec(ctx, t, inNew, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
}

// dropAtCleanup xóa database name (kể cả khi còn kết nối; IF EXISTS: gọi được trước khi tạo) khi test xong.
func dropAtCleanup(t testing.TB, base *url.URL, name string) {
	t.Helper()
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		withAdmin(cctx, t, base, func(c *pgx.Conn) { exec(cctx, t, c, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
	})
}

// serverURL trả chuỗi kết nối quản trị tới database mặc định của máy chủ thử.
func serverURL(t testing.TB) *url.URL {
	t.Helper()
	raw := os.Getenv("CRM_TEST_ADMIN_URL") //nolint:forbidigo // testkit chỉ chạy trong test, đọc biến của môi trường test
	if raw == "" {
		app := os.Getenv("CRM_TEST_DATABASE_URL") //nolint:forbidigo // như trên
		if app == "" {
			t.Fatal("testkit: thiếu CRM_TEST_DATABASE_URL (hoặc CRM_TEST_ADMIN_URL): chạy `make db-up` rồi chạy test qua make, hoặc đặt biến này")
		}
		u, err := url.Parse(app)
		if err != nil {
			t.Fatalf("testkit: CRM_TEST_DATABASE_URL không đọc được: %v", err)
		}
		u.User = url.UserPassword(devAdminUser, devAdminPass)
		raw = u.String()
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("testkit: CRM_TEST_ADMIN_URL không đọc được: %v", err)
	}
	return u
}

// withDB trả bản sao của base với database dbName và người dùng user (nil = giữ người dùng của base).
func withDB(base *url.URL, user *url.Userinfo, dbName string) string {
	u := *base
	u.Path = "/" + strings.TrimPrefix(dbName, "/")
	if user != nil {
		u.User = user
	}
	return u.String()
}

func randomSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func connect(ctx context.Context, t testing.TB, u string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(ctx, u)
	if err != nil {
		t.Fatalf("testkit: nối PostgreSQL thử: %v", err)
	}
	return c
}

func exec(ctx context.Context, t testing.TB, c *pgx.Conn, sql string) {
	t.Helper()
	if _, err := c.Exec(ctx, sql); err != nil {
		t.Fatalf("testkit: %s: %v", sql, err)
	}
}
