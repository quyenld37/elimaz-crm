package httpapi

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// newUnmatchedEnv dựng môi trường M3 với ba sự kiện ngoài hệ thống: VTP0000OLD1 lúc 2026-12-01 23:30 giờ Việt Nam
// (= 16:30 UTC, mốc trả về) và một phút sau; BEST-OLD-2 lúc 2026-12-02 01:00 giờ Việt Nam (= 2026-12-01 18:00 UTC).
func newUnmatchedEnv(t *testing.T) (*m3Env, time.Time) {
	t.Helper()
	e := newM3Env(t)
	unmatchedDay1 := time.Date(2026, 12, 1, 16, 30, 0, 0, time.UTC)
	d2 := time.Date(2026, 12, 1, 18, 0, 0, 0, time.UTC)
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		for _, c := range []struct {
			carrier, code string
			at            time.Time
		}{{"vtp", "VTP0000OLD1", unmatchedDay1}, {"vtp", "VTP0000OLD1", unmatchedDay1.Add(time.Minute)}, {"best", "BEST-OLD-2", d2}} {
			if err := fulfillment.RecordUnmatched(t.Context(), tx, c.carrier, nil, c.code, "", c.at); err != nil {
				return fmt.Errorf("ghi %s: %w", c.code, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return e, unmatchedDay1
}

// getUnmatched gọi GET /api/v1/fulfillment/unmatched-events<query> bằng email.
func getUnmatched(e *m3Env, query, email string) (int, map[string]any) {
	return e.send(http.MethodGet, "/api/v1/fulfillment/unmatched-events"+query, email, "")
}

// unmatchedTrackingCodes là trackingCode của từng dòng trong thân trả về.
func unmatchedTrackingCodes(b map[string]any) []string {
	items, _ := b["items"].([]any)
	out := make([]string, 0, len(items))
	for _, x := range items {
		out = append(out, x.(map[string]any)["trackingCode"].(string)) //nolint:forcetypeassert // test
	}
	return out
}

// listUnmatchedCarrierEvents (Task 265, MO07): kế toán đọc sự kiện hãng của vận đơn ngoài hệ thống — chỉ mã vận đơn,
// hãng, tài khoản, số lần, mốc giờ (không dữ liệu khách); mới thấy trước; con trỏ; Kho (không có
// fulfillment.unmatched.read) → 403.
func TestListUnmatchedCarrierEvents(t *testing.T) {
	t.Parallel()
	e, day1 := newUnmatchedEnv(t)
	code, b := getUnmatched(e, "", m3Accounter)
	if code != http.StatusOK || !slices.Equal(unmatchedTrackingCodes(b), []string{"BEST-OLD-2", "VTP0000OLD1"}) || b["nextCursor"] != nil {
		t.Fatalf("GET: %d %v", code, b)
	}
	vtp := b["items"].([]any)[1].(map[string]any) //nolint:forcetypeassert // test
	keys := slices.Sorted(maps.Keys(vtp))
	if want := []string{"carrierKey", "eventCount", "firstSeenAt", "id", "lastSeenAt", "trackingCode"}; !slices.Equal(keys, want) {
		t.Errorf("trường của dòng = %v, muốn %v (không tài khoản khi trống, không dữ liệu khách)", keys, want)
	}
	want := map[string]any{
		"carrierKey": "vtp", "eventCount": float64(2), "firstSeenAt": day1.Format(time.RFC3339),
		"lastSeenAt": day1.Add(time.Minute).Format(time.RFC3339),
	}
	for k, v := range want {
		if vtp[k] != v {
			t.Errorf("dòng VTP %s = %v, muốn %v", k, vtp[k], v)
		}
	}
	code, b = getUnmatched(e, "?limit=1", m3Owner)
	next, _ := b["nextCursor"].(string)
	if code != http.StatusOK || !slices.Equal(unmatchedTrackingCodes(b), []string{"BEST-OLD-2"}) || next == "" {
		t.Fatalf("trang 1: %d %v", code, b)
	}
	if _, b := getUnmatched(e, "?limit=1&cursor="+next, m3Owner); !slices.Equal(unmatchedTrackingCodes(b), []string{"VTP0000OLD1"}) || b["nextCursor"] != nil {
		t.Errorf("trang 2: %v", b)
	}
	if code, _ := getUnmatched(e, "", m3Warehouse); code != http.StatusForbidden {
		t.Errorf("Kho: %d, muốn 403", code)
	}
}

// Lọc hãng, from/to theo ngày giờ Việt Nam; con trỏ sai, from > to → 400 invalid_input.
func TestListUnmatchedCarrierEvents_Filters(t *testing.T) {
	t.Parallel()
	e, _ := newUnmatchedEnv(t)
	for name, c := range map[string]struct {
		query string
		want  []string
	}{
		"lọc hãng": {"?carrier=best", []string{"BEST-OLD-2"}},
		// Ngày 01/12 giờ Việt Nam chỉ có VTP (23:30); ngày 02/12 chỉ có Best (01:00).
		"ngày 01/12": {"?from=2026-12-01&to=2026-12-01", []string{"VTP0000OLD1"}},
		"từ 02/12":   {"?from=2026-12-02", []string{"BEST-OLD-2"}},
	} {
		if code, b := getUnmatched(e, c.query, m3Accounter); code != http.StatusOK || !slices.Equal(unmatchedTrackingCodes(b), c.want) {
			t.Errorf("%s: %d %v, muốn %v", name, code, b, c.want)
		}
	}
	for name, q := range map[string]string{
		"con trỏ sai dạng": "?cursor=abc",
		"con trỏ lạ":       "?cursor=" + uuid.NewString(),
		"from > to":        "?from=2026-12-02&to=2026-12-01",
	} {
		if code, b := getUnmatched(e, q, m3Accounter); code != http.StatusBadRequest || b["error"] != "invalid_input" {
			t.Errorf("%s: %d %v, muốn 400 invalid_input", name, code, b)
		}
	}
}
