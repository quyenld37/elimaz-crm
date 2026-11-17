package e2e

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/testkit"
)

// CRM_ROLE=migrate chạy db.Migrate bằng chuỗi chủ bảng rồi thoát 0; lần hai không áp dụng gì và vẫn thoát 0.
func TestMigrateRoleAppliesThenExits(t *testing.T) {
	bin := buildBinary(t)
	_, owner, app := testkit.CreateDatabase(t)
	env := []string{
		"CRM_ROLE=migrate", "CRM_ENV=test", "DATABASE_URL=" + app, "DATABASE_OWNER_URL=" + owner,
		"OTEL_ENABLED=false", "PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
	}
	runMigrate := func() string {
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
	if out := runMigrate(); !strings.Contains(out, "0001_platform.sql") {
		t.Fatalf("lần đầu phải nêu 0001_platform.sql đã áp dụng:\n%s", out)
	}
	if out := runMigrate(); strings.Contains(out, "0001_platform.sql") {
		t.Fatalf("lần hai không được áp dụng lại 0001_platform.sql:\n%s", out)
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
