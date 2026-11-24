package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

const webCarrierPath = "/api/v1/web-carrier-settings"

// newWebCarrierEnv: newCAEnv với Service có sổ sự kiện, ba tài khoản nhập tay active của ELIMAZ ở kho MAIN: a (hiện
// trên web, web_sort 1), b (hiện, web_sort 2), hidden (không hiện).
func newWebCarrierEnv(t *testing.T) (e *caEnv, a, b, hidden uuid.UUID) {
	t.Helper()
	e = newCAEnv(t)
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
	})
	authE := &authEnv{t: t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App, Logger: slog.New(slog.DiscardHandler),
		Access: authE.verifier(), Realtime: &realtimetest.RecordingBus{}, Fulfillment: m.Service,
	})
	ids := make([]uuid.UUID, 3)
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		for i, ref := range []string{"a", "b", "hidden"} {
			id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
				SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: "self", Name: "Hãng " + ref,
				ExternalAccountRef: "web-" + ref,
			})
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if _, err := tx.Exec(t.Context(), `UPDATE fulfillment.carrier_accounts SET mode = 'manual', status = 'active',
				show_on_web = $2, web_sort = $3 WHERE id = $1`, id, i < 2, i+1); err != nil {
				return err //nolint:wrapcheck // test
			}
			ids[i] = id
		}
		return nil
	})
	return e, ids[0], ids[1], ids[2]
}

// Đọc, lưu cài đặt hãng trên web: người chỉ có fulfillment.read → 403; người đọc (carrier_accounts.read) đọc được,
// lưu → 403; mặc định version 1, hai hãng bật showOnWeb, 0 vận đơn; lưu ưu tiên b, phí 30.000 → 200 version 2, một
// audit; ưu tiên tài khoản không hiện trên web → 422 carrier_not_on_web; version cũ → 409; phí quá 500.000 → 422 setting_out_of_range;
// tắt showOnWeb của b (đang ưu tiên) → bỏ ưu tiên; tắt a (hãng cuối) → 422 last_web_carrier.
func TestWebCarrierSettingsAPI(t *testing.T) {
	t.Parallel()
	e, a, b, hidden := newWebCarrierEnv(t)
	if code, _, _ := e.send(http.MethodGet, webCarrierPath, caNobody, ""); code != http.StatusForbidden {
		t.Fatalf("không có quyền đọc tài khoản hãng: %d, muốn 403", code)
	}
	code, got, raw := e.send(http.MethodGet, webCarrierPath, caReader, "")
	cars, _ := got["carriers"].([]any)
	if code != http.StatusOK || got["version"] != float64(1) || got["preferredAccountId"] != nil || got["fallbackFee"] != nil ||
		len(cars) != 2 || cars[0].(map[string]any)["accountId"] != a.String() || //nolint:forcetypeassert // test
		cars[0].(map[string]any)["shipments30d"] != float64(0) { //nolint:forcetypeassert // test
		t.Fatalf("đọc mặc định: %d %s", code, raw)
	}
	put := func(email, body string) (int, map[string]any, string) {
		return e.send(http.MethodPut, webCarrierPath, email, body)
	}
	if code, _, _ := put(caReader, `{"version":1}`); code != http.StatusForbidden {
		t.Fatalf("người đọc lưu: %d, muốn 403", code)
	}
	code, got, raw = put(caManager, fmt.Sprintf(`{"preferredAccountId":%q,"fallbackFee":30000,"version":1}`, b))
	if code != http.StatusOK || got["version"] != float64(2) || got["preferredAccountId"] != b.String() || got["fallbackFee"] != float64(30000) {
		t.Fatalf("lưu: %d %s", code, raw)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.web_carrier_settings.update'
		AND after->>'preferredAccountId' = $1 AND (after->>'fallbackFee')::bigint = 30000`, b.String()); n != 1 {
		t.Fatalf("audit: %d dòng, muốn 1", n)
	}
	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"không hiện trên web", fmt.Sprintf(`{"preferredAccountId":%q,"version":2}`, hidden), http.StatusUnprocessableEntity, "carrier_not_on_web"},
		{"version cũ", `{"version":1}`, http.StatusConflict, "version_conflict"},
		{"phí quá trần", `{"fallbackFee":500001,"version":2}`, http.StatusUnprocessableEntity, "setting_out_of_range"},
	} {
		if code, got, raw := put(caManager, c.body); code != c.status || got["error"] != c.code {
			t.Errorf("%s: %d %s, muốn %d %s", c.name, code, raw, c.status, c.code)
		}
	}
	patch := func(id uuid.UUID) (int, map[string]any, string) {
		v := e.count(`SELECT version FROM fulfillment.carrier_accounts WHERE id = $1`, id)
		return e.send(http.MethodPatch, "/api/v1/carrier-accounts/"+id.String(), caManager,
			fmt.Sprintf(`{"version":%d,"showOnWeb":false}`, v))
	}
	if code, _, raw := patch(b); code != http.StatusOK {
		t.Fatalf("tắt b: %d %s", code, raw)
	}
	if _, got, raw := e.send(http.MethodGet, webCarrierPath, caReader, ""); got["preferredAccountId"] != nil ||
		got["fallbackFee"] != float64(30000) || got["version"] != float64(3) {
		t.Fatalf("sau tắt hãng ưu tiên: %s", raw)
	}
	if code, got, raw := patch(a); code != http.StatusUnprocessableEntity || got["error"] != "last_web_carrier" {
		t.Fatalf("tắt hãng cuối: %d %s, muốn 422 last_web_carrier", code, raw)
	}
}

// Task 488: đọc trả hiddenCarriers (tài khoản chưa hiện, trừ revoked) và người, giờ lưu (chưa lưu: không có); đổi thứ tự
// cả danh sách: người đọc → 403; đúng tập, đúng version → 200 thứ tự mới, version + 1, có tên người lưu, một audit;
// version cũ, thiếu tài khoản, kèm tài khoản ẩn → 409 version_conflict; mã trùng → 400 bad_request (uniqueItems).
func TestReorderWebCarriersAPI(t *testing.T) {
	t.Parallel()
	e, a, b, hidden := newWebCarrierEnv(t)
	_, got, raw := e.send(http.MethodGet, webCarrierPath, caReader, "")
	hid, _ := got["hiddenCarriers"].([]any)
	if len(hid) != 1 || hid[0].(map[string]any)["accountId"] != hidden.String() || //nolint:forcetypeassert // test
		got["updatedBy"] != nil || got["updatedAt"] != nil || got["updatedByName"] != nil {
		t.Fatalf("đọc mặc định: %s", raw)
	}
	path := webCarrierPath + "/order"
	body := func(v int, ids ...uuid.UUID) string {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("%q", id)
		}
		return fmt.Sprintf(`{"version":%d,"accountIds":[%s]}`, v, strings.Join(parts, ","))
	}
	if code, _, _ := e.send(http.MethodPut, path, caReader, body(1, b, a)); code != http.StatusForbidden {
		t.Fatalf("người đọc đổi thứ tự: %d, muốn 403", code)
	}
	code, got, raw := e.send(http.MethodPut, path, caManager, body(1, b, a))
	cars, _ := got["carriers"].([]any)
	if code != http.StatusOK || got["version"] != float64(2) || len(cars) != 2 ||
		cars[0].(map[string]any)["accountId"] != b.String() || cars[1].(map[string]any)["accountId"] != a.String() || //nolint:forcetypeassert // test
		got["updatedByName"] == nil || got["updatedByName"] == "" || got["updatedAt"] == nil {
		t.Fatalf("đổi thứ tự: %d %s", code, raw)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.web_carrier_order.update'
		AND after->'accountIds'->>0 = $1`, b.String()); n != 1 {
		t.Fatalf("audit: %d dòng, muốn 1", n)
	}
	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"version cũ", body(1, a, b), http.StatusConflict, "version_conflict"},
		{"thiếu tài khoản", body(2, a), http.StatusConflict, "version_conflict"},
		{"kèm tài khoản ẩn", body(2, a, b, hidden), http.StatusConflict, "version_conflict"},
		{"mã trùng", body(2, a, a), http.StatusBadRequest, "bad_request"},
	} {
		if code, got, raw := e.send(http.MethodPut, path, caManager, c.body); code != c.status || got["error"] != c.code {
			t.Errorf("%s: %d %s, muốn %d %s", c.name, code, raw, c.status, c.code)
		}
	}
}
