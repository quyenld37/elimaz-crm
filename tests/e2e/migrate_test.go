package e2e

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// CRM_ROLE=migrate chạy db.Migrate bằng chuỗi chủ bảng rồi thoát 0; lần hai không áp dụng gì và vẫn thoát 0.
func TestMigrateRoleAppliesThenExits(t *testing.T) {
	_, owner, app := testkit.CreateDatabase(t)
	runMigrate := migrateRunner(t, owner, app)
	if out := runMigrate(); !strings.Contains(out, "0001_platform.sql") {
		t.Fatalf("lần đầu phải nêu 0001_platform.sql đã áp dụng:\n%s", out)
	}
	if out := runMigrate(); strings.Contains(out, "0001_platform.sql") {
		t.Fatalf("lần hai không được áp dụng lại 0001_platform.sql:\n%s", out)
	}
}

// migrateRunner trả hàm chạy tệp crm với CRM_ROLE=migrate trên database (owner, app); thoát khác 0 thì test dừng; trả
// stdout (mỗi dòng phải là JSON).
func migrateRunner(t *testing.T, owner, app string) func() string {
	t.Helper()
	bin := buildBinary(t)
	env := []string{
		"CRM_ROLE=migrate", "CRM_ENV=test", "DATABASE_URL=" + app, "DATABASE_OWNER_URL=" + owner,
		"OTEL_ENABLED=false", "PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
	}
	return func() string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin)
		cmd.Env = env
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("migrate thoát lỗi: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
		assertJSONLines(t, stdout.String())
		return stdout.String()
	}
}

// Vai trò migrate chạy một lần rồi thoát nên không có healthcheck.
func TestMigrateRoleHasNoHealthcheck(t *testing.T) {
	bin := buildBinary(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "healthcheck")
	cmd.Env = []string{"CRM_ROLE=migrate", "CRM_ENV=test", "DATABASE_URL=postgres://x@127.0.0.1:1/x", "DATABASE_OWNER_URL=postgres://x@127.0.0.1:1/x", "OTEL_ENABLED=false"}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "migrate") {
		t.Fatalf("healthcheck của migrate phải khác 0 và nêu migrate: %v %s", err, stderr.String())
	}
}

// Task 315: sau khi áp dụng migration, vai trò migrate đồng bộ vai trò mẫu cho mọi shop: shop chưa có vai trò nhận đủ
// vai trò mẫu; quyền mới (giả lập bằng cách xóa dòng role_permissions) vào vai trò mẫu chưa sửa; vai trò đã sửa không
// đổi; lần chạy sau không đổi thêm.
func TestMigrateSyncsDefaultRoles(t *testing.T) {
	_, ownerURL, app := testkit.CreateDatabase(t)
	runMigrate := migrateRunner(t, ownerURL, app)
	runMigrate()
	owner, err := db.Open(t.Context(), ownerURL, db.PoolOptions{MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Pool.Close()
	var shop uuid.UUID
	if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('shop') RETURNING id").Scan(&shop); err != nil {
		t.Fatal(err)
	}
	execSQL := func(q string) {
		t.Helper()
		if err := tenancy.WithTenant(t.Context(), owner, shop, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := tenancy.WithTenant(t.Context(), owner, shop, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), q).Scan(&n)
		}); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if out := runMigrate(); !strings.Contains(out, "đồng bộ vai trò mẫu") {
		t.Fatalf("migrate phải báo đã đồng bộ vai trò mẫu:\n%s", out)
	}
	if n := count("SELECT count(*) FROM identity.roles"); n != len(identity.DefaultRoles) {
		t.Fatalf("shop mới có %d vai trò, mong %d", n, len(identity.DefaultRoles))
	}
	execSQL("UPDATE identity.roles SET customized = true WHERE key = 'accountant'")
	execSQL("DELETE FROM identity.role_permissions WHERE permission = 'order.read' AND role_id IN (SELECT id FROM identity.roles WHERE key IN ('sale', 'accountant'))")
	runMigrate()
	has := func(role string) int {
		return count("SELECT count(*) FROM identity.role_permissions rp JOIN identity.roles r ON r.id = rp.role_id WHERE r.key = '" +
			role + "' AND rp.permission = 'order.read'")
	}
	if has("sale") != 1 || has("accountant") != 0 {
		t.Fatalf("sau đồng bộ: sale order.read=%d (mong 1), accountant=%d (mong 0)", has("sale"), has("accountant"))
	}
	total := count("SELECT count(*) FROM identity.role_permissions")
	runMigrate()
	if n := count("SELECT count(*) FROM identity.role_permissions"); n != total {
		t.Fatalf("lần chạy sau đổi số quyền: %d → %d", total, n)
	}
}
