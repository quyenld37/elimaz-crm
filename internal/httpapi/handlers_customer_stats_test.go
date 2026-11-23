package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const (
	deliveryNoRead = "khong-xem-khach-giao@elimaz.test" // không có customer.read
	deliveryMerger = "gop-khach-giao@elimaz.test"       // customer.read, customer.merge
)

// setStats đặt thẳng số nhận/hoàn của hồ sơ id (dữ liệu dựng cho test HTTP; đường tính thật do consumer M2 làm, test ở
// customer.TestDeliveryStatsFor).
func (e *customerAPIEnv) setStats(id string, delivered, returned int) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE customer.customers SET delivered_count = $2, returned_count = $3,
			order_count = $4 WHERE id = $1`, id, delivered, returned, delivered+returned)
		return err //nolint:wrapcheck // test
	})
}

// TestCustomerDeliveryStatsAPI (kế hoạch M3-21): GET delivery-stats trả {delivered, returned, returnRateBp, risk, hint}
// của gốc theo ngưỡng của cài đặt fulfillment (mặc định khi chưa lưu); đổi HighRateBP 5000 → watch; hồ sơ con đọc số
// của gốc; không có customer.read → 403; khách không có hay của tenant khác → 404.
func TestCustomerDeliveryStatsAPI(t *testing.T) {
	t.Parallel()
	e := newCustomerAPIEnv(t)
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, deliveryNoRead, "active"), []string{"order.read"}, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, deliveryMerger, "active"), []string{"customer.read", "customer.merge"},
		"all", "")
	ids := map[string]string{}
	for _, k := range []struct{ key, body string }{
		{"K1", `{"fullName":"Khách Một","phone":"0912000001"}`},
		{"K2", `{"fullName":"Khách Hai","phone":"0912000002"}`},
		{"K3", `{"fullName":"Khách Ba","phone":"0912000003"}`},
	} {
		got := mustOK(t, "createCustomer", e.send(http.MethodPost, "/api/v1/customers", customerWriter, k.body), http.StatusCreated)
		ids[k.key] = got["id"].(string) //nolint:forcetypeassert // test
	}
	e.setStats(ids["K1"], 3, 2)
	e.setStats(ids["K2"], 9, 1)

	check := func(label, id string, delivered, returned, rate int, risk, hint string) {
		t.Helper()
		body := mustOK(t, label, e.send(http.MethodGet, "/api/v1/customers/"+id+"/delivery-stats", customerReader, ""),
			http.StatusOK)
		gotHint, _ := body["hint"].(string)
		if body["delivered"] != float64(delivered) || body["returned"] != float64(returned) ||
			body["returnRateBp"] != float64(rate) || body["risk"] != risk || gotHint != hint {
			t.Errorf("%s: %v; muốn %d/%d/%d %s %q", label, body, delivered, returned, rate, risk, hint)
		}
	}
	check("K1 mặc định", ids["K1"], 3, 2, 4000, customer.RiskHigh, customer.HintHigh)
	check("K2 mặc định", ids["K2"], 9, 1, 1000, customer.RiskWatch, customer.HintWatch)
	check("K3 chưa có đơn", ids["K3"], 0, 0, 0, customer.RiskNone, "")

	s := fulfillment.DefaultSettings()
	s.ReturnRisk.HighRateBP = 5000
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := fulfillment.SaveSettings(t.Context(), tx, s, 0, fulfillment.Actor{TenantID: e.tenant, UserID: uuid.New()})
		return err //nolint:wrapcheck // test
	})
	check("K1 sau HighRateBP 5000", ids["K1"], 3, 2, 4000, customer.RiskWatch, customer.HintWatch)

	mustOK(t, "mergeCustomers", e.send(http.MethodPost, "/api/v1/customers/"+ids["K1"]+"/merge", deliveryMerger,
		`{"mergedId":"`+ids["K3"]+`","matchedBy":"staff"}`), http.StatusOK)
	check("K3 là hồ sơ con của K1", ids["K3"], 3, 2, 4000, customer.RiskWatch, customer.HintWatch)

	expect(t, "không có customer.read", e.send(http.MethodGet, "/api/v1/customers/"+ids["K1"]+"/delivery-stats",
		deliveryNoRead, ""), http.StatusForbidden, "forbidden")
	expect(t, "khách không có", e.send(http.MethodGet, "/api/v1/customers/"+uuid.NewString()+"/delivery-stats",
		customerReader, ""), http.StatusNotFound, "not_found")
	e.staff.Grant(t, e.other, e.staff.User(t, e.other, "xem-khach-b@elimaz.test", "active"), []string{"customer.read"}, "all", "")
	expect(t, "khách của tenant khác", e.send(http.MethodGet, "/api/v1/customers/"+ids["K1"]+"/delivery-stats",
		"xem-khach-b@elimaz.test", ""), http.StatusNotFound, "not_found")
}
