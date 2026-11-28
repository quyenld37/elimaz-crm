package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
	"elimaz.com.vn/crm/internal/queries/flashverify"
	"elimaz.com.vn/crm/internal/testkit"
)

// `crm verify-flash --sale <mã>` qua run (Task 219): tenant A trống, tenant B có đợt 5 suất đã lên lịch (held của số
// dư 5 = 5 đơn vị trống). Đúng → thoát 0, "VERIFY-FLASH ĐẠT"; held của số dư bị sửa (phép kiểm g, inventory.Reconcile)
// → thoát 1, dòng SAI g_held; đợt không có → thoát 1; mã sai dạng → thoát 2.
func TestVerifyFlash(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	s.Tenant(t, "A")
	b := s.Tenant(t, "B")
	user := s.User(t, b, "chu@elimaz.test", "active")
	var sale flashtest.Sale
	s.Exec(t, b, func(tx pgx.Tx) error {
		shop := invtest.NewShop(t, tx, b)
		v := shop.Variant(t, tx, shop.Seller, 350000, 20)
		sale = flashtest.Schedule(t, tx, flashtest.Item{
			Variant: v, Price: 199000, Slots: 5, Limit: 1, HoldMinutes: 10,
			StartsAt: time.Now().UTC().Add(time.Hour), TenantID: b, UserID: user,
		})
		return nil
	})
	getenv := recomputeEnvVars(s.App.Pool.Config().ConnString())
	call := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := run(t.Context(), args, getenv, &out, &errOut)
		return code, out.String() + errOut.String()
	}

	if code, out := call(verifyFlashCommand, "--sale", sale.ID.String()); code != exitOK ||
		!strings.Contains(out, "VERIFY-FLASH ĐẠT") || !strings.Contains(out, "available 5, held 0") {
		t.Fatalf("đợt đúng: mã %d, %s", code, out)
	}
	s.Exec(t, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_balances SET held = held + 1`)
		return err //nolint:wrapcheck // test
	})
	if code, out := call(verifyFlashCommand, "--sale", sale.ID.String()); code != exitFail ||
		!strings.Contains(out, "SAI "+string(flashverify.CheckHeld)) || !strings.Contains(out, "VERIFY-FLASH SAI") {
		t.Fatalf("held lệch: mã %d, %s", code, out)
	}
	if code, out := call(verifyFlashCommand, "--sale", uuid.NewString()); code != exitFail ||
		!strings.Contains(out, "không có đợt") {
		t.Fatalf("đợt không có: mã %d, %s", code, out)
	}
	if code, _ := call(verifyFlashCommand, "--sale", "khong-phai-uuid"); code != exitConfig {
		t.Fatalf("mã sai dạng: mã %d", code)
	}
	if code, _ := call(verifyFlashCommand, sale.ID.String()); code != exitConfig {
		t.Fatalf("thiếu --sale: mã %d", code)
	}
}
