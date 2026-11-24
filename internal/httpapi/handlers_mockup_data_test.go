package httpapi

// Dữ liệu theo mockup M5 qua API nhân viên (Task 487): chiến dịch (liên kết đầy đủ, tên đích, banner đang dùng, SKU mẫu,
// trần phần trăm) và quà sinh nhật (sắp gửi 7 ngày, mã đã dùng năm nay, mã đơn đã dùng, SĐT gắn mã đã che, số của
// từng đợt trong danh sách đợt).

import (
	"bytes"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/config"
)

// withOrigin dựng lại máy chủ của e với WEBSITE_ORIGIN origin.
func (e *couponEnv) withOrigin(origin string) {
	authE := &authEnv{t: e.t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}, WebsiteOrigin: origin}, DB: e.staff.App,
		Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: &bytes.Buffer{}}, nil)), Access: authE.verifier(),
		Now: func() time.Time { return e.now },
	})
}

func numbers(v any) []float64 {
	xs, _ := v.([]any)
	out := make([]float64, 0, len(xs))
	for _, x := range xs {
		f, _ := x.(float64)
		out = append(out, f)
	}
	return out
}

// Chiến dịch theo danh mục: getWebCampaign có url = WEBSITE_ORIGIN + path (cấu hình rỗng → tên miền mặc định), targets
// (tên, slug, số mẫu; danh mục đã bỏ không có), bannerPositions (banner chiến dịch số 2, banner ảnh liên kết chiến dịch
// số 4; banner khác, chiến dịch khác không tính); danh sách có url, không có targets; xem trước có sku; trần 50 (mặc
// định), cài 70 → 70; bán hàng (promotion.read) đọc được.
func TestWebCampaignAPI_MockupData(t *testing.T) {
	t.Parallel()
	e := newCampaignEnv(t)
	var cat uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO catalog.categories (tenant_id, name, slug, depth)
			VALUES (current_setting('app.tenant_id')::uuid, 'Đầm công sở', 'dam-cong-so', 1) RETURNING id`).Scan(&cat); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `UPDATE catalog.products SET category_id = $1 WHERE id = $2`, cat, e.product)
		return err //nolint:wrapcheck // test
	})
	body := strings.Replace(e.campaignBody("Đầm công sở", 20), `"scope":"all"`,
		`"scope":"categories","targetIds":["`+cat.String()+`"]`, 1)
	c := e.ok("tạo", e.post("/api/v1/web-campaigns", "m1", body), http.StatusCreated)
	id, _ := c["id"].(string)
	other := e.ok("tạo khác", e.post("/api/v1/web-campaigns", "m2", e.campaignBody("Khác", 10)), http.StatusCreated)
	otherID, _ := other["id"].(string)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO content.home_banner_sets (tenant_id) VALUES ($1)`, e.tenant)
		if err == nil {
			_, err = tx.Exec(t.Context(), `INSERT INTO content.home_banners (id, tenant_id, position, enabled, kind,
				campaign_id, image_desktop, alt, link_kind, link_id) VALUES
				(gen_random_uuid(), $1, 0, true, 'new', NULL, NULL, '', NULL, NULL),
				(gen_random_uuid(), $1, 1, true, 'campaign', $2, NULL, '', NULL, NULL),
				(gen_random_uuid(), $1, 2, true, 'campaign', $3, NULL, '', NULL, NULL),
				(gen_random_uuid(), $1, 3, false, 'image', NULL, gen_random_uuid(), 'Ảnh', 'campaign', $2),
				(gen_random_uuid(), $1, 4, true, 'image', NULL, gen_random_uuid(), 'Ảnh', 'category', $4)`,
				e.tenant, id, otherID, cat)
		}
		return err //nolint:wrapcheck // test
	})

	got := e.ok("đọc", e.do(http.MethodGet, "/api/v1/web-campaigns/"+id, couponSale, "", ""), http.StatusOK)
	path, _ := got["path"].(string)
	targets, _ := got["targets"].([]any)
	if got["url"] != "https://elimaz.com.vn"+path || len(targets) != 1 ||
		!slices.Equal(numbers(got["bannerPositions"]), []float64{2, 4}) {
		t.Fatalf("getWebCampaign: %v", got)
	}
	if tg, _ := targets[0].(map[string]any); tg["id"] != cat.String() || tg["name"] != "Đầm công sở" ||
		tg["code"] != "dam-cong-so" || tg["modelCount"] != float64(1) {
		t.Fatalf("đích: %v", tg)
	}
	if o := e.ok("đọc khác", e.do(http.MethodGet, "/api/v1/web-campaigns/"+otherID, couponSale, "", ""), http.StatusOK); !slices.Equal(numbers(o["bannerPositions"]), []float64{3}) ||
		len(o["targets"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("chiến dịch cả shop: %v", o)
	}

	e.withOrigin("https://staging.elimaz.test")
	page := e.ok("danh sách", e.do(http.MethodGet, "/api/v1/web-campaigns", couponSale, "", ""), http.StatusOK)
	items, _ := page["items"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		p, _ := m["path"].(string)
		if m["url"] != "https://staging.elimaz.test"+p || m["targets"] != nil || m["bannerPositions"] != nil {
			t.Fatalf("mục danh sách: %v", m)
		}
	}
	p := e.ok("xem trước", e.post("/api/v1/web-campaigns/"+id+"/preview", "m3", `{"revision":1}`), http.StatusOK)
	samples, _ := p["samples"].([]any)
	if s, _ := samples[0].(map[string]any); s["sku"] != "SKU-1" {
		t.Fatalf("mẫu có sku: %v", p)
	}

	if s := e.ok("trần", e.do(http.MethodGet, "/api/v1/web-campaign-settings", couponSale, "", ""), http.StatusOK); s["maxPercent"] != float64(50) {
		t.Fatalf("trần mặc định: %v", s)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO platform.settings (tenant_id, key, value)
			VALUES ($1, 'promotion.web_campaigns', '{"maxPercent":70}')`, e.tenant)
		return err //nolint:wrapcheck // test
	})
	if s := e.ok("trần 70", e.do(http.MethodGet, "/api/v1/web-campaign-settings", couponSale, "", ""), http.StatusOK); s["maxPercent"] != float64(70) {
		t.Fatalf("trần đã cài: %v", s)
	}
}

// Quà sinh nhật: hai khách sinh nhật 18/12 (hôm nay 15/12) nhận mã; một mã dùng cho đơn thật → Tất cả khách có
// upcomingWeek, yearGifts {2026, 2, 1}, usedOrderCode của khách đã dùng; CSV có cột "Đơn đã dùng"; listCoupons của đợt
// có boundPhoneMasked "0912 *** 001", usedOrderCode và usedOrderId ở mã đã dùng; listCouponBatches có givenCount, usedCount,
// expiredCount.
func TestBirthdayAPI_MockupData(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	e.ok("bật quà", e.do(http.MethodPut, bdSettingsPath, couponManager, "s1",
		strings.Replace(bdSettingsBody, "%d", "1", 1)), http.StatusOK)
	e.seedBirthdays([]string{"0912000001", "0912000002"}, []bool{true, false})
	e.deliverAll()
	e.ok("tạo mã ngay", e.do(http.MethodPost, bdRunsPath, couponManager, "r1", ""), http.StatusOK)
	var usedCode string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT k.code::text FROM promotion.coupons k
			WHERE k.bound_phone = '+84912000001'`).Scan(&usedCode)
	})
	o := e.redeemOnNewOrder(usedCode, "web", "+84912000001", e.now)

	all := e.ok("tất cả khách", e.do(http.MethodGet, bdCustomersPath+"?limit=10", couponSale, "", ""), http.StatusOK)
	year, _ := all["yearGifts"].(map[string]any)
	if all["upcomingWeek"] != float64(0) || year["year"] != float64(2026) || year["issued"] != float64(2) ||
		year["used"] != float64(1) {
		t.Fatalf("số đầu trang: %v", all)
	}
	used := 0
	for _, it := range all["items"].([]any) { //nolint:forcetypeassert // test
		c, _ := it.(map[string]any)
		if c["state"] == "used" {
			used++
			if c["usedOrderCode"] != o.Code || c["code"] != usedCode {
				t.Errorf("khách đã dùng: %v, muốn đơn %s", c, o.Code)
			}
		} else if c["usedOrderCode"] != nil {
			t.Errorf("khách chưa dùng có mã đơn: %v", c)
		}
	}
	if used != 1 {
		t.Fatalf("một khách đã dùng: %v", all)
	}
	x := e.ok("xuất", e.do(http.MethodPost, bdExportPath, couponManager, "x1", `{}`), http.StatusOK)
	if csv, _ := x["csv"].(string); !strings.Contains(csv, "Đơn đã dùng") || !strings.Contains(csv, o.Code) {
		t.Fatalf("CSV: %v", x)
	}

	e.checkBirthdayBatch(usedCode, o)
}

// checkBirthdayBatch: đợt sinh nhật duy nhất có số đã tặng/dùng/hết hạn trong danh sách; mã của đợt có SĐT đã che, mã
// usedCode có mã và id đơn o, mã khác không có.
func (e *couponEnv) checkBirthdayBatch(usedCode string, o order.Order) {
	t := e.t
	t.Helper()
	batches := e.ok("đợt", e.do(http.MethodGet, "/api/v1/coupon-batches?purpose=birthday", couponSale, "", ""), http.StatusOK)
	bs, _ := batches["items"].([]any)
	if len(bs) != 1 {
		t.Fatalf("đợt sinh nhật: %v", batches)
	}
	b, _ := bs[0].(map[string]any)
	if b["givenCount"] != float64(0) || b["usedCount"] != float64(1) || b["expiredCount"] != float64(0) {
		t.Fatalf("số của đợt trong danh sách: %v", b)
	}
	bid, _ := b["id"].(string)
	page := e.ok("mã của đợt", e.do(http.MethodGet, "/api/v1/coupons?batchId="+bid, couponSale, "", ""), http.StatusOK)
	masks := []string{}
	for _, it := range page["items"].([]any) { //nolint:forcetypeassert // test
		c, _ := it.(map[string]any)
		m, _ := c["boundPhoneMasked"].(string)
		masks = append(masks, m)
		if want := map[bool]any{true: o.Code, false: nil}[c["code"] == usedCode]; c["usedOrderCode"] != want {
			t.Errorf("mã %v: usedOrderCode %v, muốn %v", c["code"], c["usedOrderCode"], want)
		}
		if want := map[bool]any{true: o.ID.String(), false: nil}[c["code"] == usedCode]; c["usedOrderId"] != want {
			t.Errorf("mã %v: usedOrderId %v, muốn %v", c["code"], c["usedOrderId"], want)
		}
	}
	slices.Sort(masks)
	if !slices.Equal(masks, []string{"0912 *** 001", "0912 *** 002"}) {
		t.Fatalf("SĐT gắn mã đã che: %v", masks)
	}
}
