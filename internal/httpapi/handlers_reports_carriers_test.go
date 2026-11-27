package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// GET /api/v1/reports/sales/carriers trên kịch bản chín đơn (Task 231): dạng JSON đúng hợp đồng (dòng tổng không có
// carrierAccountId, accountName; tỷ lệ, trung bình null khi mẫu số 0); người chỉ có Page P2 chỉ thấy vận đơn của O3;
// from sau to → 400 bad_date.
func TestReportsCarriers_Report(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	sc := reportseed.NineOrders(reportseed.New(t, staff.Owner, tenant), "2026-11-02")
	staff.Grant(t, tenant, staff.User(t, tenant, "chu@elimaz.test", "active"), []string{"report.sales.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "p2@elimaz.test", "active"), []string{"report.sales.read"}, "page", sc.P2.String())
	srv := testServer(t, staff, keys, 0)
	const path = "/api/v1/reports/sales/carriers?from=2026-11-02&to=2026-11-02"

	read := func(email string) (rows []carrierJSON, totals carrierJSON) {
		t.Helper()
		return readCarriers(t, serveAs(srv, keys, http.MethodGet, path, email, "", ""))
	}

	rows, totals := read("chu@elimaz.test")
	m := carriersByKey(rows)
	if len(rows) != 3 || len(m) != 3 {
		t.Fatalf("số dòng %d: %v", len(rows), rows)
	}
	checkCarrierJSON(t, "vtp", m["vtp"], map[string]any{
		"accountName": "vtp", "created": 3.0, "cancelled": 0.0, "inProgress": 0.0, "returning": 0.0, "deliveryFailed": 0.0,
		"delivered": 2.0, "returned": 1.0, "successRateBp": 6666.0, "feeTotal": 66000.0, "feeAvg": 22000.0, "codTotal": 1090000.0,
	})
	if _, ok := m["vtp"]["carrierAccountId"].(string); !ok {
		t.Errorf("dòng vtp thiếu carrierAccountId: %v", m["vtp"])
	}
	checkCarrierJSON(t, "spx", m["spx"], map[string]any{"created": 1.0, "cancelled": 1.0, "inProgress": 1.0, "successRateBp": nil, "codTotal": 180000.0})
	checkCarrierJSON(t, "tổng", totals, map[string]any{"carrierKey": "", "created": 5.0, "cancelled": 1.0, "successRateBp": 6666.0, "feeTotal": 110000.0})
	for _, k := range []string{"carrierAccountId", "accountName"} {
		if _, ok := totals[k]; ok {
			t.Errorf("dòng tổng có %s: %v", k, totals)
		}
	}

	rows, totals = read("p2@elimaz.test")
	m = carriersByKey(rows)
	if len(rows) != 2 {
		t.Fatalf("P2: số dòng %d: %v", len(rows), rows)
	}
	checkCarrierJSON(t, "P2 spx", m["spx"], map[string]any{"created": 0.0, "cancelled": 1.0, "feeAvg": nil, "successRateBp": nil, "feeTotal": 0.0})
	checkCarrierJSON(t, "P2 best", m["best"], map[string]any{"created": 1.0, "inProgress": 1.0, "codTotal": 150000.0})
	checkCarrierJSON(t, "P2 tổng", totals, map[string]any{"created": 1.0, "cancelled": 1.0, "feeAvg": 22000.0})

	expect(t, "from sau to", serveAs(srv, keys, http.MethodGet, "/api/v1/reports/sales/carriers?from=2026-11-03&to=2026-11-02",
		"chu@elimaz.test", "", ""), http.StatusBadRequest, "bad_date")
}

// carrierJSON là một dòng của báo cáo hãng dạng JSON.
type carrierJSON map[string]any

// readCarriers đọc thân 200 của báo cáo hãng kỳ 02/11 – 02/11.
func readCarriers(t *testing.T, rec *httptest.ResponseRecorder) (rows []carrierJSON, totals carrierJSON) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("mã %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string         `json:"status"`
		Period map[string]any `json:"period"`
		Rows   []carrierJSON  `json:"rows"`
		Totals carrierJSON    `json:"totals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ready" || body.Period["from"] != "2026-11-02" || body.Period["to"] != "2026-11-02" {
		t.Errorf("status %q, kỳ %v", body.Status, body.Period)
	}
	return body.Rows, body.Totals
}

func carriersByKey(rows []carrierJSON) map[string]carrierJSON {
	m := map[string]carrierJSON{}
	for _, r := range rows {
		m[r["carrierKey"].(string)] = r //nolint:forcetypeassert // test: sai kiểu thì panic là đủ
	}
	return m
}

func checkCarrierJSON(t *testing.T, name string, r carrierJSON, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got, ok := r[k]; !ok || got != v {
			t.Errorf("%s.%s = %v (có: %v), mong %v", name, k, got, ok, v)
		}
	}
}
