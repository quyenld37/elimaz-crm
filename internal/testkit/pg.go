// Package testkit dựng hạ tầng thật cho test: mỗi test một database PostgreSQL 18 riêng trên container dev của
// phiên (docker/compose.dev.yml), đã chạy db.Migrate. Chỉ dùng trong tệp _test.go.
package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
)

// Mật khẩu cố định của role dev trong docker/compose.dev.yml và docker/postgres/init/01-roles.sql (chỉ database dev).
const (
	devAdminUser = "postgres"
	devAdminPass = "postgres_dev"  //nolint:gosec // mật khẩu dev công khai trong compose, không dùng ở máy chủ
	devOwnerPass = "crm_owner_dev" //nolint:gosec // như trên
	devAppPass   = "crm_app_dev"   //nolint:gosec // như trên
)

// StartPostgres tạo database crm_test_<ngẫu nhiên> (chủ crm_owner) đã chạy db.Migrate, xóa khi test xong. Trả chuỗi
// kết nối của người quản trị, chủ bảng và crm_app.
func StartPostgres(t testing.TB) (adminURL, ownerURL, appURL string) {
	t.Helper()
	adminURL, ownerURL, appURL = CreateDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatalf("testkit: migrate database thử: %v", err)
	}
	return adminURL, ownerURL, appURL
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
	name := "crm_test_" + randomSuffix(t)
	adminURL = withDB(base, nil, name)
	ownerURL = withDB(base, url.UserPassword("crm_owner", devOwnerPass), name)
	appURL = withDB(base, url.UserPassword("crm_app", devAppPass), name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin := connect(ctx, t, withDB(base, nil, base.Path))
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	exec(ctx, t, admin, "CREATE DATABASE "+name+" OWNER crm_owner")
	dropAtCleanup(t, base, name)
	prepareEmpty(ctx, t, admin, adminURL)
	return adminURL, ownerURL, appURL
}

// prepareEmpty làm phần của người quản trị trên database vừa tạo (adminURL), trước khi migrate: quyền cấp pg_monitor
// cho crm_owner và extension pg_stat_statements. admin nối vào database mặc định của máy chủ.
func prepareEmpty(ctx context.Context, t testing.TB, admin *pgx.Conn, adminURL string) {
	t.Helper()
	// Migration 0010 cấp pg_monitor cho crm_app; cụm dev dựng trước Task 19 chưa có dòng này trong 01-roles.sql.
	exec(ctx, t, admin, "GRANT pg_monitor TO crm_owner WITH ADMIN OPTION")
	inNew := connect(ctx, t, adminURL)
	defer func() { _ = inNew.Close(context.WithoutCancel(ctx)) }()
	exec(ctx, t, inNew, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
}

// dropAtCleanup xóa database name (kể cả khi còn kết nối) khi test xong.
func dropAtCleanup(t testing.TB, base *url.URL, name string) {
	t.Helper()
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		c := connect(cctx, t, withDB(base, nil, base.Path))
		defer func() { _ = c.Close(cctx) }()
		exec(cctx, t, c, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
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
