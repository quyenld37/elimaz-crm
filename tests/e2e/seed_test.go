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

// `crm seed-dev <email>` (CRM_ROLE=migrate): tenant ELIMAZ, vai trò owner mọi quyền phạm vi all, user theo email,
// định tuyến staff_email, tài khoản hãng giả fake-1 và tuyến của nó. Chạy hai lần không lỗi, không thêm dòng; crm_app nạp được user với đủ quyền.
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
	// Kho MAIN "Kho chính" do ELIMAZ vận hành, đúng một dòng sau hai lần chạy (kế hoạch M1, Task M1-28).
	var warehouses int
	if err := tenancy.WithTenant(t.Context(), d, u.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM inventory.warehouses_v AS w
			JOIN catalog.sellers_v AS s ON s.seller_id = w.operated_by_seller_id
			WHERE w.code = 'MAIN' AND w.name = 'Kho chính' AND w.status = 'active' AND s.code = 'ELIMAZ'`).Scan(&warehouses)
	}); err != nil || warehouses != 1 {
		t.Fatalf("mong đúng 1 kho MAIN của ELIMAZ, có %d, %v", warehouses, err)
	}
	checkSeedCarrier(t, d, u.TenantID)

	// Sai cách gọi: thiếu email, hoặc vai trò khác migrate → thoát khác 0.
	if out, err := run(env, "seed-dev"); err == nil {
		t.Fatalf("thiếu email phải lỗi:\n%s", out)
	}
	apiEnv := append(roleEnv("api", app, 0), "PORT=1")
	if out, err := run(apiEnv, "seed-dev", "a@elimaz.test"); err == nil || !strings.Contains(out, "migrate") {
		t.Fatalf("seed-dev ngoài vai trò migrate phải lỗi và nêu migrate: %v\n%s", err, out)
	}
}

// checkSeedCarrier: tài khoản hãng giả fake-1 ở kho MAIN và tuyến carrier_account:<route_key của fake-1> → tenant,
// đúng một dòng sau hai lần chạy (Task M1-31; tuyến theo route_key từ Task M3-10, không còn carrier_account:fake-1).
func checkSeedCarrier(t *testing.T, d *db.DB, tenant uuid.UUID) {
	t.Helper()
	var accounts, routes, old int
	var rk string
	if err := tenancy.WithTenant(t.Context(), d, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*), max(a.route_key) FROM fulfillment.carrier_accounts AS a
			JOIN inventory.warehouses_v AS w ON w.warehouse_id = a.warehouse_id
			WHERE a.carrier_key = 'fake' AND a.external_account_ref = 'fake-1' AND w.code = 'MAIN'`).Scan(&accounts, &rk)
	}); err != nil || accounts != 1 {
		t.Fatalf("mong đúng 1 tài khoản hãng giả fake-1, có %d, %v", accounts, err)
	}
	if err := d.Pool.QueryRow(t.Context(), `SELECT count(*) FILTER (WHERE external_key = $2 AND tenant_id = $1),
		count(*) FILTER (WHERE external_key = 'fake-1')
		FROM platform.tenant_routes WHERE kind = 'carrier_account'`, tenant, rk).Scan(&routes, &old); err != nil || routes != 1 || old != 0 {
		t.Fatalf("mong đúng 1 tuyến carrier_account:<route_key>, 0 tuyến fake-1; có %d, %d, %v", routes, old, err)
	}
}
