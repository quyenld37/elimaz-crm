package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
	"elimaz.com.vn/crm/internal/testkit/flashbuytest"
)

// Số "Đã bán / suất" của đợt và "Có thể bán lúc này" của món (Task 528, mockup 514 trạng thái A, C) qua HTTP, người
// chỉ có promotion.flash.read: tồn 20. Đợt nháp 7 suất → quantity 7, sold 0, sellable 20. Đợt khác cùng biến thể lên
// lịch 5 suất → sellable của cả hai đợt 15. Khách 1 lấy 2 suất, khách 2 lấy 1 suất (đơn nháp, đang giữ) → sold vẫn 0;
// đơn khách 1 hoàn tất (đơn vị → sold) → sold 2; nhả suất khách 2 (mở lại trong đợt) → sold 2, live còn trống 3,
// sellable 15 (suất nhả về đợt, không về bán thường). Danh sách và một đợt khớp nhau; tenant khác: một đợt 404, danh
// sách không có đợt nào.
func TestFlashSaleAPI_Counts(t *testing.T) {
	t.Parallel()
	e := newFlashEnv(t)
	var sale flashtest.Sale
	var c1, c2 uuid.UUID
	e.seedStock(t, 20)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := order.EnsureDefaults(t.Context(), tx, e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		if c1, err = customer.Create(t.Context(), tx, "Khách 1"); err != nil {
			return err //nolint:wrapcheck // test
		}
		c2, err = customer.Create(t.Context(), tx, "Khách 2")
		return err //nolint:wrapcheck // test
	})
	d := e.ok("tạo nháp", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "n1", futureFlashBody),
		http.StatusCreated)
	ds, _ := d["sale"].(map[string]any)
	draftID, _ := ds["id"].(string)
	e.ok("thêm món nháp", e.do(http.MethodPost, "/api/v1/flash-sales/"+draftID+"/items", couponManager, "n2",
		e.item(199000, 7)), http.StatusCreated)
	e.checkCounts(t, draftID, 7, 0, 20)

	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		sale = flashtest.Schedule(t, tx, flashtest.Item{
			Variant: e.variant, Price: 199000, Slots: 5, Limit: 2, HoldMinutes: 10,
			StartsAt: time.Now().UTC().Add(-30 * time.Minute), TenantID: e.tenant, UserID: uuid.New(),
		})
		return nil
	})
	openID := sale.ID.String()
	e.checkCounts(t, openID, 5, 0, 15)
	e.checkCounts(t, draftID, 7, 0, 15)

	o1 := flashbuytest.Buy(t, e.staff.App, e.tenant, sale.Item, c1, 2, "FS-528-1")
	o2 := flashbuytest.Buy(t, e.staff.App, e.tenant, sale.Item, c2, 1, "FS-528-2")
	e.checkCounts(t, openID, 5, 0, 15)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return inventory.MarkFlashSold(t.Context(), tx, orderItemOf(t, tx, o1))
	})
	e.checkCounts(t, openID, 5, 2, 15)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := inventory.ReleaseFlashUnits(t.Context(), tx, orderItemOf(t, tx, o2), "draft_expired", true)
		return err //nolint:wrapcheck // test
	})
	e.checkCounts(t, openID, 5, 2, 15)
	e.checkLive(t, "/api/v1/flash-sales/"+openID, map[string]float64{"quantity": 5, "sold": 2, "held": 0, "available": 3})

	tb := e.staff.Tenant(t, "B")
	mb := e.staff.User(t, tb, "xem-flash-b@elimaz.test", "active")
	e.staff.Grant(t, tb, mb, []string{"promotion.flash.read"}, "all", "")
	expect(t, "tenant khác đọc đợt", e.do(http.MethodGet, "/api/v1/flash-sales/"+openID, "xem-flash-b@elimaz.test", "", ""),
		http.StatusNotFound, "not_found")
	lb := e.ok("tenant khác đọc danh sách", e.do(http.MethodGet, "/api/v1/flash-sales", "xem-flash-b@elimaz.test", "", ""),
		http.StatusOK)
	if items, _ := lb["items"].([]any); len(items) != 0 {
		t.Fatalf("tenant khác thấy đợt: %v", lb)
	}
}

// checkCounts đọc đợt id (một đợt và dòng của nó trong danh sách) bằng người chỉ có promotion.flash.read và so tổng
// suất, đã bán của đợt, có thể bán của món đầu.
func (e *flashEnv) checkCounts(t *testing.T, id string, quantity, sold, sellable float64) {
	t.Helper()
	got := e.ok("một đợt", e.do(http.MethodGet, "/api/v1/flash-sales/"+id, flashReader, "", ""), http.StatusOK)
	s, _ := got["sale"].(map[string]any)
	items, _ := got["items"].([]any)
	if s["quantity"] != quantity || s["sold"] != sold || len(items) != 1 {
		t.Fatalf("đợt %s: quantity %v sold %v (mong %v, %v): %v", id, s["quantity"], s["sold"], quantity, sold, got)
	}
	if it, _ := items[0].(map[string]any); it["sellable"] != sellable {
		t.Fatalf("đợt %s: sellable %v, mong %v", id, it["sellable"], sellable)
	}
	list := e.ok("danh sách", e.do(http.MethodGet, "/api/v1/flash-sales", flashReader, "", ""), http.StatusOK)
	rows, _ := list["items"].([]any)
	for _, r := range rows {
		if m, _ := r.(map[string]any); m["id"] == id {
			if m["quantity"] != quantity || m["sold"] != sold {
				t.Fatalf("danh sách đợt %s: quantity %v sold %v, mong %v, %v", id, m["quantity"], m["sold"], quantity, sold)
			}
			return
		}
	}
	t.Fatalf("danh sách không có đợt %s: %v", id, list)
}

func orderItemOf(t *testing.T, tx pgx.Tx, orderID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(t.Context(), `SELECT id FROM ord.order_items WHERE order_id = $1`, orderID).Scan(&id); err != nil {
		t.Fatalf("dòng đơn của %s: %v", orderID, err)
	}
	return id
}
