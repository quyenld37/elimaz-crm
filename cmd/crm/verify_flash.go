package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/flashverify"
)

// verifyFlashCommand là lệnh `crm verify-flash --sale <mã đợt>` (kế hoạch M5, Task 219 / M5-36; thử tải và RUNBOOK mục
// flash sale): kiểm a–h của một đợt, in số đếm từng món và từng mục sai, thoát 1 khi có mục sai. Chạy khi tải đã
// dừng; mọi vai trò, bằng DATABASE_URL (crm_app, RLS vẫn áp).
const verifyFlashCommand = "verify-flash"

var errSaleUnknown = errors.New("không có đợt flash sale với mã này ở tenant nào")

// isVerifyFlashCommand: đúng dạng `verify-flash --sale <mã>` (mã không phải UUID: runVerifyFlash báo cách gọi).
func isVerifyFlashCommand(args []string) bool {
	return len(args) == 3 && args[0] == verifyFlashCommand && args[1] == "--sale"
}

// verifyResult là kết quả kiểm: tenant của đợt và báo cáo (gồm cả mục CheckHeld của inventory.Reconcile).
type verifyResult struct {
	TenantID uuid.UUID
	Report   flashverify.Report
}

// verifyFlash tìm tenant của saleID (thử lần lượt mọi tenant như recompute-customer-stats), chạy flashverify.Verify
// trong một transaction REPEATABLE READ chỉ đọc, rồi (g) inventory.Reconcile của tenant đó ở transaction riêng (ghi một
// dòng inventory.reconcile_runs như lần đối chiếu tay) và thêm các mục lệch held, sổ, đơn vị suất của biến thể, kho,
// món thuộc đợt thành CheckHeld. Không có ở tenant nào → errSaleUnknown.
func verifyFlash(ctx context.Context, d *db.DB, saleID uuid.UUID) (verifyResult, error) {
	tenants, err := tenancy.ListTenantIDs(ctx, d)
	if err != nil {
		return verifyResult{}, err //nolint:wrapcheck // đã bọc trong tenancy
	}
	snapshot := db.TxOptions{IsoLevel: pgx.RepeatableRead}
	for _, tenantID := range tenants {
		var rep flashverify.Report
		err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
			var err error
			rep, err = flashverify.Verify(ctx, tx, saleID)
			return err //nolint:wrapcheck // đã bọc trong flashverify
		}, snapshot)
		if errors.Is(err, flashverify.ErrSaleNotFound) {
			continue
		}
		if err != nil {
			return verifyResult{}, fmt.Errorf("kiểm đợt ở tenant %s: %w", tenantID, err)
		}
		var run inventory.Run
		if err := tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
			var err error
			run, err = inventory.Reconcile(ctx, tx, 0)
			return err //nolint:wrapcheck // đã bọc trong inventory
		}, db.TxWorker); err != nil {
			return verifyResult{}, fmt.Errorf("đối chiếu tồn ở tenant %s: %w", tenantID, err)
		}
		rep.Failures = append(rep.Failures, heldFailures(rep, run)...)
		return verifyResult{TenantID: tenantID, Report: rep}, nil
	}
	return verifyResult{}, fmt.Errorf("%s: %w", saleID, errSaleUnknown)
}

// heldFailures đổi các mục lệch của run thuộc đợt (biến thể, kho của một món; hay mục đơn vị suất của một món) thành
// CheckHeld.
func heldFailures(rep flashverify.Report, run inventory.Run) []flashverify.Failure {
	type vw struct{ v, w uuid.UUID }
	byStock := map[vw]uuid.UUID{}
	items := map[uuid.UUID]bool{}
	for _, it := range rep.Items {
		byStock[vw{it.VariantID, it.WarehouseID}] = it.ItemID
		items[it.ItemID] = true
	}
	var out []flashverify.Failure
	for _, m := range run.Mismatches {
		item, ok := byStock[vw{m.VariantID, m.WarehouseID}]
		if m.FlashItemID != uuid.Nil {
			item, ok = m.FlashItemID, items[m.FlashItemID]
		}
		if !ok {
			continue
		}
		detail := "đối chiếu tồn: " + m.Check
		if m.Bucket != "" {
			detail += " (" + m.Bucket + ")"
		}
		out = append(out, flashverify.Failure{
			Check: flashverify.CheckHeld, ItemID: item, Expected: m.Expected, Actual: m.Actual, Detail: detail,
		})
	}
	return out
}

// printVerify in kết quả (chỉ mã và số, không dữ liệu cá nhân) và dòng cuối "VERIFY-FLASH ĐẠT|SAI".
func printVerify(w io.Writer, res verifyResult) {
	r := res.Report
	_, _ = fmt.Fprintf(w, "đợt %s (tenant %s, %s): %d món\n", r.SaleID, res.TenantID, r.Status, len(r.Items))
	for _, it := range r.Items {
		_, _ = fmt.Fprintf(w, "món %s: suất %d, available %d, held %d, sold %d, released %d, đã lấy %d, "+
			"dòng đơn còn hiệu lực %d\n", it.ItemID, it.Quantity, it.Available, it.Held, it.Sold, it.Released,
			it.Claimed, it.ActiveQty)
	}
	for _, f := range r.Failures {
		_, _ = fmt.Fprintf(w, "SAI %s món %s: %s (đúng %d, thấy %d)\n", f.Check, f.ItemID, f.Detail, f.Expected, f.Actual)
	}
	if r.OK() {
		_, _ = fmt.Fprintln(w, "VERIFY-FLASH ĐẠT")
		return
	}
	_, _ = fmt.Fprintf(w, "VERIFY-FLASH SAI: %d mục\n", len(r.Failures))
}

// runVerifyFlash mở pool nhỏ bằng DATABASE_URL, kiểm đợt rawSaleID và in kết quả; mã thoát 0 khi đạt, 1 khi có mục
// sai hay lỗi (lỗi in ra stderr), exitConfig khi mã đợt không phải UUID (in cách gọi).
func runVerifyFlash(ctx context.Context, cfg config.Config, rawSaleID string, stdout, stderr io.Writer) int {
	saleID, err := uuid.Parse(rawSaleID)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mã đợt %q không phải UUID; cách gọi: `crm %s --sale <mã đợt>`\n", rawSaleID,
			verifyFlashCommand)
		return exitConfig
	}
	d, err := openCommandDB(ctx, cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, verifyFlashCommand+":", err)
		return exitFail
	}
	defer d.Pool.Close()
	res, err := verifyFlash(ctx, d, saleID)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, verifyFlashCommand+":", err)
		return exitFail
	}
	printVerify(stdout, res)
	if !res.Report.OK() {
		return exitFail
	}
	return exitOK
}
