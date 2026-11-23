package web_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
	"elimaz.com.vn/crm/internal/testkit"
)

// hangGuard là chốt chống treo khi chờ một sự kiện; không phải tiêu chí đúng/sai về thời gian.
const hangGuard = 30 * time.Second

type availabilityBody struct {
	Items []struct {
		VariantID string `json:"variantId"`
		Available bool   `json:"available"`
		Low       bool   `json:"low"`
	} `json:"items"`
	TTLS int `json:"ttlS"`
}

func (e *env) availability(ids ...uuid.UUID) availabilityBody {
	e.t.Helper()
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	rec := e.get(e.key, "/api/v1/web/availability?variantIds="+strings.Join(s, ","))
	var out availabilityBody
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		e.t.Fatalf("availability: %d %s", rec.Code, rec.Body.String())
	}
	return out
}

func (e *env) warehouse() uuid.UUID {
	e.t.Helper()
	var wh uuid.UUID
	e.app(func(tx pgx.Tx) error {
		var err error
		wh, err = inventory.DefaultWarehouse(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	return wh
}

func (e *env) stock(v uuid.UUID, onHand int32) {
	e.t.Helper()
	wh := e.warehouse()
	e.app(func(tx pgx.Tx) error { invtest.SeedStock(e.t, tx, v, wh, onHand); return nil })
}

// export ghi sổ phiếu xuất qty của v ở kho mặc định (chứng từ của M1), không có worker hay bộ xóa khóa nào chạy.
func (e *env) export(v uuid.UUID, qty int32) {
	e.t.Helper()
	wh := e.warehouse()
	w := inventory.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	e.app(func(tx pgx.Tx) error { invtest.Export(e.t, tx, w, wh, v, qty); return nil })
}

// [WB02], [FS09]: tồn 2 → còn, sắp hết; 7 trừ 5 (đã giữ cho đợt, phiếu xuất của M1) → còn, sắp hết; 0 → hết; ttlS 5,
// bộ đệm SpaceAvailability 5 s; mã lạ không có trong items; theo mã tăng dần.
func TestWebAvailability(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	p := e.product("AV1", "[]")
	two, flash, none := p.Variants[0].ID, p.Variants[1].ID, p.Variants[2].ID
	e.stock(two, 2)
	e.stock(flash, 7)
	e.export(flash, 5)

	got := e.availability(none, two, uuid.New(), flash, two)
	if got.TTLS != 5 || e.rc.ttl(cache.SpaceAvailability) != 5*time.Second {
		t.Fatalf("ttlS %d, hạn sống trong bộ đệm %s", got.TTLS, e.rc.ttl(cache.SpaceAvailability))
	}
	want := map[string][2]bool{two.String(): {true, true}, flash.String(): {true, true}, none.String(): {false, false}}
	if len(got.Items) != len(want) {
		t.Fatalf("mong %d dòng, có %+v", len(want), got.Items)
	}
	for i, it := range got.Items {
		if w, ok := want[it.VariantID]; !ok || w != [2]bool{it.Available, it.Low} {
			t.Errorf("%s: mong %v, có %+v", it.VariantID, w, it)
		}
		if i > 0 && it.VariantID < got.Items[i-1].VariantID {
			t.Errorf("không theo mã tăng dần: %+v", got.Items)
		}
	}
}

// 100 mã → 200; 101 → 400; thiếu variantIds, mã sai dạng → 400.
func TestWebAvailability_Params(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	ids := make([]string, webcatalog.MaxAvailabilityIDs+1)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	if rec := e.get(e.key, "/api/v1/web/availability?variantIds="+strings.Join(ids[:100], ",")); rec.Code != http.StatusOK {
		t.Errorf("100 mã: mong 200, có %d %s", rec.Code, rec.Body.String())
	}
	for name, q := range map[string]string{
		"101 mã": "?variantIds=" + strings.Join(ids, ","), "thiếu": "", "sai dạng": "?variantIds=abc",
	} {
		if rec := e.get(e.key, "/api/v1/web/availability"+q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: mong 400, có %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

// Lưới an toàn: Valkey thật, không worker, không bộ xóa khóa → bán hết sau khi đã đệm "còn" thì khi hạn sống 5 s qua,
// đọc lại thấy hết (chờ theo kết quả, chốt chống treo hangGuard).
func TestWebAvailability_TTLWithoutWorker(t *testing.T) {
	_, conn := valkeyCache(t)
	e := newEnv(t, conn)
	v := e.product("AV2", "[]").Variants[0].ID
	e.stock(v, 2)
	if it := e.availability(v).Items; len(it) != 1 || !it[0].Available {
		t.Fatalf("trước khi bán: %+v", it)
	}
	e.export(v, 2)
	start := time.Now()
	for {
		it := e.availability(v).Items
		if len(it) == 1 && !it[0].Available {
			break
		}
		if time.Since(start) > hangGuard {
			t.Fatal("còn/hết không đổi sau hạn sống của bộ đệm")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, src := e.rc.take(); !slices.Contains(src, cache.SourceCache) {
		t.Errorf("mong có lần đọc từ bộ đệm trước khi hết hạn, nguồn %v", src)
	}
	t.Logf("hết hàng hiện sau %s (không worker)", time.Since(start))
}

// Đã bán 30 ngày qua HTTP: chưa có đơn → items rỗng, asOf dạng RFC 3339; bộ đệm SpaceSold30 600 s. Cách đếm thử ở
// internal/queries/webcatalog (TestSold30).
func TestWebSold30d(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	rec := e.get(e.key, "/api/v1/web/catalog/sold-30d")
	var out struct {
		Items []any  `json:"items"`
		AsOf  string `json:"asOf"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Items == nil || len(out.Items) != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if _, err := time.Parse(time.RFC3339Nano, out.AsOf); err != nil {
		t.Fatalf("asOf %q: %v", out.AsOf, err)
	}
	if ttl := e.rc.ttl(cache.SpaceSold30); ttl != 600*time.Second {
		t.Fatalf("hạn sống %s", ttl)
	}
}

type province struct {
	Code  string           `json:"code"`
	Name  string           `json:"name"`
	Wards []map[string]any `json:"wards"`
}

// Địa giới: tỉnh mẫu có xã mẫu (không có mã của hãng); ETag; If-None-Match → 304 thân rỗng; bộ đệm 1 ngày.
func TestWebLocations(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	e.staff.SeedAdminUnits(t)
	rec := e.get(e.key, "/api/v1/web/locations")
	tag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || !etagRe.MatchString(tag) {
		t.Fatalf("%d, ETag %q, %s", rec.Code, tag, rec.Body.String())
	}
	var out struct {
		Provinces []province `json:"provinces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(out.Provinces, func(p province) bool { return p.Code == testkit.SampleProvinceCode })
	if i < 0 || out.Provinces[i].Name != "Thành phố Thử Một" || len(out.Provinces[i].Wards) != 1 {
		t.Fatalf("tỉnh mẫu: %s", rec.Body.String())
	}
	if w := out.Provinces[i].Wards[0]; len(w) != 2 || w["code"] != testkit.SampleWardCode || w["name"] != "Phường Bến Thử" {
		t.Fatalf("xã mẫu: %v", w)
	}
	if e.rc.ttl(cache.SpaceLocations) != 24*time.Hour {
		t.Fatalf("hạn sống %s", e.rc.ttl(cache.SpaceLocations))
	}
	if again := e.get(e.key, "/api/v1/web/locations", "If-None-Match", tag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("If-None-Match: mong 304 rỗng, có %d %q", again.Code, again.Body.String())
	}
}

// Ba route mới: không khóa → 401; khóa thiếu web.catalog.read → 403; khóa đúng → 200.
func TestWebAvailability_Auth(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	for _, path := range []string{
		"/api/v1/web/availability?variantIds=" + uuid.NewString(), "/api/v1/web/catalog/sold-30d", "/api/v1/web/locations",
	} {
		for key, want := range map[string]int{"": http.StatusUnauthorized, e.otherKey: http.StatusForbidden, e.key: http.StatusOK} {
			if rec := e.get(key, path); rec.Code != want {
				t.Errorf("%s khóa %.6q: mong %d, có %d %s", path, key, want, rec.Code, rec.Body.String())
			}
		}
	}
}

// Trạng thái không tính "đã bán" của webcatalog trùng hằng của order.
func TestNotSoldStatuses(t *testing.T) {
	t.Parallel()
	want := []string{order.StatusDraft, order.StatusCancelled, order.StatusReturned}
	if got := webcatalog.NotSoldStatuses(); !slices.Equal(got, want) {
		t.Fatalf("webcatalog %v, order %v", got, want)
	}
}
