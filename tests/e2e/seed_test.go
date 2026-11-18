package e2e

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// `crm seed-dev <email>` (CRM_ROLE=migrate): tenant ELIMAZ, vai trò owner mọi quyền phạm vi all, user theo email,
// định tuyến staff_email. Chạy hai lần không lỗi, không thêm dòng; crm_app nạp được user với đủ quyền.
func TestSeedDev(t *testing.T) {
	bin := buildBinary(t)
	_, owner, app := testkit.StartPostgres(t)
	env := []string{
		"CRM_ROLE=migrate", "CRM_ENV=test", "DATABASE_URL=" + app, "DATABASE_OWNER_URL=" + owner,
		"OTEL_ENABLED=false", "PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
	}
	run := func(env []string, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for i := range 2 {
		if out, err := run(env, "seed-dev", "Chu@Elimaz.test"); err != nil {
			t.Fatalf("seed-dev lần %d: %v\n%s", i+1, err, out)
		}
	}
	d, err := db.Open(t.Context(), app, db.PoolOptions{MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Pool.Close()
	u, err := identity.LoadUser(t.Context(), d, "chu@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range identity.Permissions {
		if !identity.Can(u, p.Key, nil) {
			t.Errorf("owner thiếu quyền %s", p.Key)
		}
	}
	var tenants int
	if err := d.Pool.QueryRow(t.Context(), "SELECT count(*) FROM platform.tenants WHERE name = 'ELIMAZ'").Scan(&tenants); err != nil || tenants != 1 {
		t.Fatalf("mong đúng 1 tenant ELIMAZ, có %d, %v", tenants, err)
	}
	// Người bán ELIMAZ (kind own) đúng một dòng sau hai lần chạy (kế hoạch M1, Task M1-27).
	var sellers int
	if err := tenancy.WithTenant(t.Context(), d, u.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM catalog.sellers_v WHERE code = 'ELIMAZ' AND kind = 'own' AND status = 'active'").Scan(&sellers)
	}); err != nil || sellers != 1 {
		t.Fatalf("mong đúng 1 người bán ELIMAZ, có %d, %v", sellers, err)
	}

	// Sai cách gọi: thiếu email, hoặc vai trò khác migrate → thoát khác 0.
	if out, err := run(env, "seed-dev"); err == nil {
		t.Fatalf("thiếu email phải lỗi:\n%s", out)
	}
	apiEnv := append(roleEnv("api", app, 0), "PORT=1")
	if out, err := run(apiEnv, "seed-dev", "a@elimaz.test"); err == nil || !strings.Contains(out, "migrate") {
		t.Fatalf("seed-dev ngoài vai trò migrate phải lỗi và nêu migrate: %v\n%s", err, out)
	}
}
