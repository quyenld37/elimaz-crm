package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	agedAll = "giu-hang-ca-shop@elimaz.test"
	agedW2  = "giu-hang-kho-phu@elimaz.test"
)

// agedAPIEnv: shop có kho MAIN và W2 (orderScopeEnv), mỗi kho một đơn giữ 1 cái (held) đổi trạng thái 21 ngày trước.
// agedAll có inventory.aged.read toàn shop, agedW2 chỉ ở kho W2; orderViewer không có quyền.
type agedAPIEnv struct {
	*orderScopeEnv
	codeMain, codeW2 string
}

func newAgedAPIEnv(t *testing.T) *agedAPIEnv {
	t.Helper()
	e := &agedAPIEnv{orderScopeEnv: newOrderScopeEnv(t)}
	e.codeMain = fmt.Sprint(e.mustCreate("aged-main", e.bodyAt(e.shop.Warehouse))["code"])
	e.codeW2 = fmt.Sprint(e.mustCreate("aged-w2", e.bodyAt(e.w2))["code"])
	tenant := e.shop.Tenant
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_reservations SET state_changed_at = $1 WHERE state = 'held'`,
			time.Now().Add(-21*24*time.Hour))
		return err //nolint:wrapcheck // test
	})
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, agedAll, "active"), []string{"inventory.aged.read"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, agedW2, "active"), []string{"inventory.aged.read"}, "warehouse", e.w2.String())
	return e
}

// agedCodes đọc trang giữ hàng quá hạn và trả mã đơn theo thứ tự; mọi dòng phải là held 21 ngày, 1 cái, có SKU.
func agedCodes(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var page struct {
		Items []struct {
			OrderCode, Sku, State string
			AgeDays, Quantity     int
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("đọc trang: %v (%s)", err, rec.Body.String())
	}
	codes := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		if it.Sku == "" || it.State != "held" || it.AgeDays != 21 || it.Quantity != 1 {
			t.Errorf("dòng sai: %+v", it)
		}
		codes = append(codes, it.OrderCode)
	}
	return codes
}

// TestAgedAPI_Scope (KH09, AT53, Task M3-22): toàn shop thấy cả hai kho; chỉ quyền kho W2 → không thấy dòng của kho
// chính, gửi warehouseId kho chính → 403; không có quyền → 403; ba lần đọc không đổi giữ hàng, sổ, việc.
func TestAgedAPI_Scope(t *testing.T) {
	t.Parallel()
	e := newAgedAPIEnv(t)
	snapshot := func() string {
		return fmt.Sprint(e.count(`SELECT count(*) FROM inventory.stock_reservations WHERE state = 'held'`),
			e.count(`SELECT count(*) FROM inventory.stock_ledger`), e.count(`SELECT count(*) FROM platform.jobs`))
	}
	before := snapshot()
	const path = "/api/v1/inventory/aged-reservations"
	for range 3 {
		rec := e.get(agedAll, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("toàn shop: %d %s", rec.Code, rec.Body.String())
		}
		if got := strings.Join(agedCodes(t, rec), ","); got != e.codeMain+","+e.codeW2 &&
			got != e.codeW2+","+e.codeMain {
			t.Errorf("toàn shop: %s, muốn %s và %s", got, e.codeMain, e.codeW2)
		}
	}
	if after := snapshot(); after != before {
		t.Errorf("đọc danh sách mà dữ liệu đổi: %s → %s (giữ hàng, sổ, việc)", before, after)
	}
	rec := e.get(agedW2, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("kho W2: %d %s", rec.Code, rec.Body.String())
	}
	if got := agedCodes(t, rec); len(got) != 1 || got[0] != e.codeW2 {
		t.Errorf("kho W2: %v, muốn chỉ %s", got, e.codeW2)
	}
	rec = e.get(agedW2, path+"?warehouseId="+e.w2.String())
	if got := agedCodes(t, rec); rec.Code != http.StatusOK || len(got) != 1 || got[0] != e.codeW2 {
		t.Errorf("kho W2 lọc W2: %d %v", rec.Code, got)
	}
	expect(t, "kho W2 xin kho chính", e.get(agedW2, path+"?warehouseId="+e.shop.Warehouse.String()), http.StatusForbidden, "forbidden")
	expect(t, "không quyền", e.get(orderViewer, path), http.StatusForbidden, "forbidden")
	expect(t, "con trỏ hỏng", e.get(agedAll, path+"?cursor=!!"), http.StatusBadRequest, "invalid_input")
	rec = e.get(agedAll, path+"?state=in_transit")
	if got := agedCodes(t, rec); rec.Code != http.StatusOK || len(got) != 0 {
		t.Errorf("lọc in_transit: %d %v", rec.Code, got)
	}
}

// TestAgedAPI_Settings: cài đặt vận chuyển agedDays.held = 25 → dòng 21 ngày biến khỏi danh sách; trang 1 dòng có
// nextCursor, trang sau lấy dòng còn lại.
func TestAgedAPI_Settings(t *testing.T) {
	t.Parallel()
	e := newAgedAPIEnv(t)
	const path = "/api/v1/inventory/aged-reservations"
	rec := e.get(agedAll, path+"?limit=1")
	first, cur := agedCodes(t, rec), decodeJSON(t, rec)["nextCursor"]
	if len(first) != 1 || cur == nil {
		t.Fatalf("trang 1 dòng: %s", rec.Body.String())
	}
	rec = e.get(agedAll, path+"?limit=1&cursor="+fmt.Sprint(cur))
	if got := agedCodes(t, rec); len(got) != 1 || got[0] == first[0] || decodeJSON(t, rec)["nextCursor"] != nil {
		t.Errorf("trang sau: %s", rec.Body.String())
	}
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO platform.settings (tenant_id, key, value)
			VALUES ($1, 'fulfillment', '{"agedDays":{"held":25,"inTransit":180}}')`, e.shop.Tenant)
		return err //nolint:wrapcheck // test
	})
	rec = e.get(agedAll, path)
	if got := agedCodes(t, rec); rec.Code != http.StatusOK || len(got) != 0 {
		t.Errorf("Held = 25: %d %v, muốn rỗng", rec.Code, got)
	}
}
