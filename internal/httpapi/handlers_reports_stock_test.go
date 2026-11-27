package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// GET /api/v1/reports/inventory/stock và /movements (Task 234): dạng JSON đúng hợp đồng (nguồn live, không
// firstSnapshotDay khi chưa chụp; nextCursor khi còn trang); người chỉ có kho W1 không thấy W2, lọc W2 → 403 forbidden;
// ngày đã qua chưa chụp → 404 no_snapshot; ngày mai → 400 bad_date; xuất nhập tồn kỳ 15/11 đúng số.
func TestReportsStock_Report(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	seed := reportseed.New(t, staff.Owner, tenant)
	w1, w2 := seed.Warehouse("Kho 1"), seed.Warehouse("Kho 2")
	a, b := seed.Variant(seed.Own(), "Áo A", "AO-A-S"), seed.Variant(seed.Own(), "Áo A", "AO-A-M")
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, reporting.VN)
	seed.Ledger(a, w1, "on_hand", 5, "import", at)
	seed.Ledger(b, w1, "on_hand", 3, "import", at)
	seed.Ledger(a, w2, "on_hand", 7, "import", at)
	seed.Ledger(a, w1, "on_hand", -1, "order", at.Add(time.Hour))
	staff.Grant(t, tenant, staff.User(t, tenant, "chu@elimaz.test", "active"), []string{"report.inventory.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "k1@elimaz.test", "active"), []string{"report.inventory.read"}, "warehouse", w1.String())
	srv := testServer(t, staff, keys, 0)
	get := func(path, email string) *httptest.ResponseRecorder {
		return serveAs(srv, keys, http.MethodGet, path, email, "", "")
	}

	type stockBody struct {
		Status, Date, Source string
		FirstSnapshotDay     *string `json:"firstSnapshotDay"`
		NextCursor           *string `json:"nextCursor"`
		Rows                 []map[string]any
	}
	var stock stockBody
	decodeOK(t, get("/api/v1/reports/inventory/stock?limit=2", "chu@elimaz.test"), &stock)
	if stock.Status != "ready" || stock.Source != "live" || stock.Date != reporting.Today(time.Now()) || stock.FirstSnapshotDay != nil ||
		len(stock.Rows) != 2 || stock.NextCursor == nil {
		t.Fatalf("trang 1: %+v", stock)
	}
	first := stock.Rows[0]
	for k, v := range map[string]any{
		"sku": "AO-A-M", "productName": "Áo A", "warehouseName": "Kho 1", "onHand": 3.0, "held": 0.0,
		"inTransit": 0.0, "damaged": 0.0, "available": 3.0, "variantId": b.String(), "warehouseId": w1.String(), "color": "Mặc định", "size": "AO-A-M",
	} {
		if first[k] != v {
			t.Errorf("dòng đầu %s = %v, mong %v", k, first[k], v)
		}
	}
	cursor := *stock.NextCursor
	stock = stockBody{}
	decodeOK(t, get("/api/v1/reports/inventory/stock?limit=2&cursor="+cursor, "chu@elimaz.test"), &stock)
	if len(stock.Rows) != 1 || stock.NextCursor != nil || stock.Rows[0]["onHand"] != 7.0 {
		t.Errorf("trang 2: %+v", stock)
	}
	stock = stockBody{}
	decodeOK(t, get("/api/v1/reports/inventory/stock?q=ao-a-s", "k1@elimaz.test"), &stock)
	if len(stock.Rows) != 1 || stock.Rows[0]["onHand"] != 4.0 || stock.Rows[0]["warehouseId"] != w1.String() {
		t.Errorf("kho 1, tiền tố: %+v", stock.Rows)
	}

	expect(t, "kho ngoài phạm vi", get("/api/v1/reports/inventory/stock?warehouseId="+w2.String(), "k1@elimaz.test"), http.StatusForbidden, "forbidden")
	expect(t, "chưa chụp", get("/api/v1/reports/inventory/stock?date=2026-11-16", "chu@elimaz.test"), http.StatusNotFound, "no_snapshot")
	expect(t, "ngày mai", get("/api/v1/reports/inventory/stock?date="+reporting.Today(time.Now().Add(24*time.Hour)), "chu@elimaz.test"),
		http.StatusBadRequest, "bad_date")
	expect(t, "con trỏ hỏng", get("/api/v1/reports/inventory/stock?cursor=x", "chu@elimaz.test"), http.StatusBadRequest, "bad_params")

	checkMovementsHTTP(t, get, w1.String(), w2.String())
}

// checkMovementsHTTP: GET xuất nhập tồn kỳ 15/11 của người chỉ có kho 1 (áo A-S: nhập 5, bán 1) đúng số; kho 2 ngoài
// phạm vi → 403; from sau to → 400 bad_date.
func checkMovementsHTTP(t *testing.T, get func(path, email string) *httptest.ResponseRecorder, w1, w2 string) {
	t.Helper()
	var mv struct {
		Status     string
		Period     map[string]string
		NextCursor *string `json:"nextCursor"`
		Rows       []map[string]any
	}
	decodeOK(t, get("/api/v1/reports/inventory/movements?from=2026-11-15&to=2026-11-15&warehouseId="+w1+"&q=AO-A-S", "k1@elimaz.test"), &mv)
	if mv.Status != "ready" || mv.Period["from"] != "2026-11-15" || mv.Period["to"] != "2026-11-15" || len(mv.Rows) != 1 || mv.NextCursor != nil {
		t.Fatalf("xuất nhập tồn: %+v", mv)
	}
	for k, v := range map[string]any{
		"opening": 0.0, "imported": 5.0, "returnedIn": 0.0, "sold": 1.0, "exportedOther": 0.0,
		"adjusted": 0.0, "closing": 4.0, "damagedClosing": 0.0, "sku": "AO-A-S", "warehouseId": w1,
	} {
		if mv.Rows[0][k] != v {
			t.Errorf("xuất nhập tồn %s = %v, mong %v", k, mv.Rows[0][k], v)
		}
	}
	expect(t, "xuất nhập tồn kho ngoài phạm vi", get("/api/v1/reports/inventory/movements?from=2026-11-15&to=2026-11-15&warehouseId="+w2,
		"k1@elimaz.test"), http.StatusForbidden, "forbidden")
	expect(t, "xuất nhập tồn from sau to", get("/api/v1/reports/inventory/movements?from=2026-11-16&to=2026-11-15", "chu@elimaz.test"),
		http.StatusBadRequest, "bad_date")
}

// decodeOK đọc thân 200 vào v.
func decodeOK(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("mã %d (%s)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatal(err)
	}
}
