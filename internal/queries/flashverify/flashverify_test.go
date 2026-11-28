package flashverify_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/flashverify"
	"elimaz.com.vn/crm/internal/testkit"
)

// Lệnh kiểm đợt flash sale (kế hoạch M5, Task 219 / M5-36): đợt 5 suất, giới hạn 2 mỗi khách; khách 1 mua 1, khách 2
// mua 2 (held 3, available 2, claims 3). Mỗi ca sửa dữ liệu sai cố ý bằng chủ bảng trong một transaction rồi lùi.

type env struct {
	t              *testing.T
	s              *testkit.Staff
	tenant         uuid.UUID
	sale           flashtest.Sale
	c1, c2         uuid.UUID
	order1, order2 uuid.UUID
	ref1           string
}

var errRollback = errors.New("lùi transaction của ca thử")

func newEnv(t *testing.T) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &env{t: t, s: s, tenant: s.Tenant(t, "A")}
	user := s.User(t, e.tenant, "chu@elimaz.test", "active")
	start := time.Now().UTC().Add(-30 * time.Minute)
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		shop := invtest.NewShop(t, tx, e.tenant)
		v := shop.Variant(t, tx, shop.Seller, 350000, 20)
		if err := order.EnsureDefaults(t.Context(), tx, e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		e.sale = flashtest.Schedule(t, tx, flashtest.Item{
			Variant: v, Price: 199000, Slots: 5, Limit: 2, HoldMinutes: 10, StartsAt: start,
			TenantID: e.tenant, UserID: user,
		})
		var err error
		if e.c1, err = customer.Create(t.Context(), tx, "Khách 1"); err != nil {
			return err //nolint:wrapcheck // test
		}
		e.c2, err = customer.Create(t.Context(), tx, "Khách 2")
		return err //nolint:wrapcheck // test
	})
	e.ref1 = "FS-1"
	e.order1 = e.buy(e.c1, 1, e.ref1)
	e.order2 = e.buy(e.c2, 2, "FS-2")
	return e
}

func (e *env) buy(c uuid.UUID, qty int, ref string) uuid.UUID {
	e.t.Helper()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	a := order.Actor{TenantID: e.tenant, UserID: uuid.New(), Type: order.ActorService}
	var res order.FlashBuyResult
	err := tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		res, err = w.BuyFlash(e.t.Context(), tx, order.FlashBuyInput{
			ItemID: e.sale.Item, CustomerID: c, Qty: qty, ExternalRef: ref, Now: time.Now().UTC(),
		}, a)
		return err //nolint:wrapcheck // test
	}, db.TxFlashBuy)
	if err != nil || res.Result != order.FlashBought {
		e.t.Fatalf("mua %d: %v %+v", qty, err, res)
	}
	return res.OrderID
}

// verify chạy các câu sửa (chủ bảng) rồi Verify trong cùng transaction tenant, rồi lùi.
func (e *env) verify(t *testing.T, corrupt ...string) flashverify.Report {
	t.Helper()
	var rep flashverify.Report
	err := tenancy.WithTenant(t.Context(), e.s.Owner, e.tenant, func(tx pgx.Tx) error {
		for _, q := range corrupt {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		var err error
		if rep, err = flashverify.Verify(t.Context(), tx, e.sale.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
	return rep
}

func checks(r flashverify.Report) []flashverify.Check {
	var out []flashverify.Check
	for _, f := range r.Failures {
		out = append(out, f.Check)
	}
	return out
}

func TestVerify(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	t.Run("đúng", func(t *testing.T) { checkCorrect(t, e) })
	line1 := fmt.Sprintf(`(SELECT id FROM ord.order_items WHERE order_id = '%s')`, e.order1)
	verifyCorrupted(t, e, line1)
	verifyCancelledSold(t, e, line1)
}

// checkCorrect: dữ liệu đúng của newEnv đạt mọi phép kiểm, số đếm của món khớp.
func checkCorrect(t *testing.T, e *env) {
	t.Helper()
	rep := e.verify(t)
	if !rep.OK() || len(rep.Failures) != 0 {
		t.Fatalf("dữ liệu đúng phải đạt, có %+v", rep.Failures)
	}
	if rep.Status != "scheduled" || len(rep.Items) != 1 {
		t.Fatalf("đợt %q, %d món", rep.Status, len(rep.Items))
	}
	it := rep.Items[0]
	if it.Quantity != 5 || it.Available != 2 || it.Held != 3 || it.Sold != 0 || it.Claimed != 3 || it.ActiveQty != 3 {
		t.Fatalf("số đếm của món: %+v", it)
	}
}

// verifyCorrupted: mỗi phép kiểm a–f, h một ca dữ liệu sai cố ý (g ở cmd/crm qua inventory.Reconcile).
func verifyCorrupted(t *testing.T, e *env, line1 string) {
	t.Helper()
	unit := fmt.Sprintf(`(SELECT id FROM inventory.flash_sale_units WHERE flash_sale_item_id = '%s'
		AND status = 'available' ORDER BY id LIMIT 1)`, e.sale.Item)
	cases := []struct {
		name    string
		want    flashverify.Check
		corrupt []string
	}{
		{"a: đơn đã hủy còn giữ đơn vị", flashverify.CheckActiveLines, []string{
			fmt.Sprintf(`UPDATE ord.orders SET status = 'cancelled', cancelled_at = now(), cancel_reason = 'other', draft_expires_at = NULL
				WHERE id = '%s'`, e.order1),
		}},
		{"b: dòng đơn giữ quá số lượng", flashverify.CheckLineUnits, []string{
			fmt.Sprintf(`UPDATE inventory.flash_sale_units SET status = 'held', order_item_id = %s WHERE id = %s`, line1, unit),
		}},
		{"c: số đã lấy khác held + sold", flashverify.CheckClaims, []string{
			fmt.Sprintf(`UPDATE promotion.flash_sale_claims SET quantity = 1 WHERE customer_id = '%s'`, e.c2),
		}},
		{"d: khách vượt giới hạn", flashverify.CheckCustomerLimit, []string{
			fmt.Sprintf(`UPDATE promotion.flash_sale_claims SET quantity = 3 WHERE customer_id = '%s'`, e.c2),
		}},
		{"e: số dư âm", flashverify.CheckNegative, []string{
			`UPDATE inventory.stock_balances SET held = on_hand + 5`,
		}},
		{"f: đợt đã đóng còn đơn vị trống", flashverify.CheckClosedAvailable, []string{
			fmt.Sprintf(`UPDATE promotion.flash_sales SET status = 'closed' WHERE id = '%s'`, e.sale.ID),
		}},
		{"h: hai đơn cùng mã website", flashverify.CheckExternalRef, []string{
			`ALTER TABLE ord.orders DROP CONSTRAINT orders_external_ref_key`,
			fmt.Sprintf(`UPDATE ord.orders SET external_ref = '%s' WHERE id = '%s'`, e.ref1, e.order2),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := e.verify(t, c.corrupt...)
			if rep.OK() || !slices.Contains(checks(rep), c.want) {
				t.Fatalf("phải báo %s, có %v", c.want, checks(rep))
			}
			for _, f := range rep.Failures {
				if f.Detail == "" {
					t.Fatalf("mục lệch thiếu mô tả: %+v", f)
				}
			}
		})
	}
}

// verifyCancelledSold: đơn hủy giữ đơn vị sold hợp lệ khi đợt đã đóng (05 mục 7.4), sai khi đợt còn mở.
func verifyCancelledSold(t *testing.T, e *env, line1 string) {
	t.Helper()
	t.Run("đợt đã đóng, đơn hủy giữ sold: hợp lệ", func(t *testing.T) {
		rep := e.verify(t,
			fmt.Sprintf(`UPDATE inventory.flash_sale_units SET status = 'released'
				WHERE flash_sale_item_id = '%s' AND status = 'available'`, e.sale.Item),
			fmt.Sprintf(`UPDATE inventory.flash_sale_units SET status = 'sold' WHERE order_item_id = %s`, line1),
			fmt.Sprintf(`UPDATE ord.orders SET status = 'cancelled', cancelled_at = now(), cancel_reason = 'other', draft_expires_at = NULL
				WHERE id = '%s'`, e.order1),
			fmt.Sprintf(`UPDATE promotion.flash_sales SET status = 'closed' WHERE id = '%s'`, e.sale.ID),
		)
		if !rep.OK() {
			t.Fatalf("đơn vị sold của đơn hủy sau khi đóng đợt là đúng (05 mục 7.4), có %+v", rep.Failures)
		}
	})
	t.Run("đợt còn mở, đơn hủy giữ sold: sai", func(t *testing.T) {
		rep := e.verify(t,
			fmt.Sprintf(`UPDATE inventory.flash_sale_units SET status = 'sold' WHERE order_item_id = %s`, line1),
			fmt.Sprintf(`UPDATE ord.orders SET status = 'cancelled', cancelled_at = now(), cancel_reason = 'other', draft_expires_at = NULL
				WHERE id = '%s'`, e.order1),
		)
		if !slices.Contains(checks(rep), flashverify.CheckActiveLines) {
			t.Fatalf("phải báo %s, có %v", flashverify.CheckActiveLines, checks(rep))
		}
	})
}

func TestVerify_UnknownSale(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	err := tenancy.WithTenant(t.Context(), s.App, tenant, func(tx pgx.Tx) error {
		_, err := flashverify.Verify(t.Context(), tx, uuid.New())
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, flashverify.ErrSaleNotFound) {
		t.Fatalf("đợt không có phải trả ErrSaleNotFound, có %v", err)
	}
}
