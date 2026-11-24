package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

const bannersPath = "/api/v1/content/banners"

// bannerEnv: couponEnv (quản lý có content.*, sale không có) cộng hai ảnh banner đã xử lý (bộ banner-v1), một ảnh bị
// từ chối của tenant A và một ảnh của tenant B.
type bannerEnv struct {
	*couponEnv
	img, img2, rejected, foreign uuid.UUID
	other                        uuid.UUID // tenant B
}

func newBannerEnv(t *testing.T) *bannerEnv {
	t.Helper()
	content.RegisterFilePurposes()
	e := &bannerEnv{couponEnv: newCouponEnv(t)}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.img = filestest.Image(t, tx, e.tenant, content.BannerPurposeKey)
		e.img2 = filestest.Image(t, tx, e.tenant, content.BannerPurposeKey)
		e.rejected = filestest.Rejected(t, tx, e.tenant, content.BannerPurposeKey)
		return nil
	})
	e.other = e.staff.Tenant(t, "B")
	e.staff.Exec(t, e.other, func(tx pgx.Tx) error {
		e.foreign = filestest.Image(t, tx, e.other, content.BannerPurposeKey)
		return nil
	})
	return e
}

func imageBannerJSON(id, img uuid.UUID, extra string) string {
	return fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"image","imageDesktop":%q,"alt":"Flash sale 11.11","linkKind":"offers"%s}`,
		id, img, extra)
}

func saveBody(rev int, banners ...string) string {
	return fmt.Sprintf(`{"revision":%d,"banners":[%s]}`, rev, strings.Join(banners, ","))
}

// [KM04] Banner qua HTTP: sale đọc → 403; mỗi luật sai → 422 đúng mã, không lưu; lưu hợp lệ → revision 1, trạng thái
// hiện/ẩn có lý do, ảnh có đường dẫn bộ banner-v1; revision cũ → 409; xem trước không ghi; một audit.
func TestBannersAPI_SaveRulesAndRead(t *testing.T) {
	t.Parallel()
	e := newBannerEnv(t)
	expect(t, "sale đọc", e.do(http.MethodGet, bannersPath, couponSale, "", ""), http.StatusForbidden, "forbidden")
	g := e.ok("đọc rỗng", e.do(http.MethodGet, bannersPath, couponManager, "", ""), http.StatusOK)
	if g["revision"] != float64(0) || len(g["banners"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("chưa lưu: %v", g)
	}
	nine := checkBannerRules(e)
	imgID, newID := uuid.New(), uuid.New()
	body := saveBody(0, imageBannerJSON(imgID, e.img, fmt.Sprintf(`,"imageMobile":%q`, e.img2)),
		fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"new","title":"  Hàng mới  "}`, newID))
	s := e.ok("lưu", e.do(http.MethodPut, bannersPath, couponManager, "s1", body), http.StatusOK)
	if s["revision"] != float64(1) {
		t.Fatalf("sau lưu: %v", s)
	}
	banners, _ := s["banners"].([]any)
	if len(banners) != 2 || banners[1].(map[string]any)["title"] != "Hàng mới" { //nolint:forcetypeassert // test
		t.Fatalf("banner: %v", banners)
	}
	statuses, _ := s["statuses"].([]any)
	st0, _ := statuses[0].(map[string]any)
	st1, _ := statuses[1].(map[string]any)
	if st0["shown"] != true || st1["shown"] != false || st1["reason"] != "no-products" {
		t.Fatalf("trạng thái: %v", statuses)
	}
	images, _ := s["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("ảnh: %v", images)
	}
	for _, x := range images {
		im, _ := x.(map[string]any)
		d, _ := im["desktop"].(string)
		small, _ := im["desktopSmall"].(string)
		if im["status"] != "ready" || !strings.Contains(d, "/v1001/2400.webp") || !strings.Contains(small, "/v1001/1200.webp") {
			t.Fatalf("ảnh: %v", im)
		}
	}
	expect(t, "revision cũ", e.do(http.MethodPut, bannersPath, couponManager, "s2", saveBody(0)), http.StatusConflict,
		"version_conflict")
	p := e.ok("xem trước", e.do(http.MethodPost, bannersPath+"/preview", couponManager, "",
		fmt.Sprintf(`{"banners":[%s]}`, imageBannerJSON(imgID, e.img, `,"enabled":false`))), http.StatusOK)
	ps, _ := p["statuses"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["reason"] != "disabled" { //nolint:forcetypeassert // test
		t.Fatalf("xem trước: %v", p)
	}
	expect(t, "xem trước luật sai", e.do(http.MethodPost, bannersPath+"/preview", couponManager, "", `{"banners":[`+
		strings.Join(nine, ",")+`]}`), http.StatusUnprocessableEntity, "too_many_banners")
	if g := e.ok("đọc lại", e.do(http.MethodGet, bannersPath, couponManager, "", ""), http.StatusOK); g["revision"] != float64(1) {
		t.Fatalf("xem trước đã ghi: %v", g)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'content.banners.save'
		AND entity_id = $1 AND (before->>'revision')::int = 0 AND (after->>'revision')::int = 1`, e.tenant.String()); n != 1 {
		t.Fatalf("audit: %d dòng, mong 1", n)
	}
}

// 50 lần lưu song song cùng Idempotency-Key → một lần đổi (revision 1), một audit, một sự kiện, mọi 200 cùng thân.
func TestBannersAPI_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	e := newBannerEnv(t)
	body := saveBody(0, imageBannerJSON(uuid.New(), e.img, ""))
	okBodies := sameKeyBodies(t, 50, func() *httptest.ResponseRecorder {
		return e.do(http.MethodPut, bannersPath, couponManager, "same-key", body)
	})
	if len(okBodies) != 1 {
		t.Fatalf("câu trả lời 200 khác nhau hoặc không có: %d loại", len(okBodies))
	}
	if g := e.ok("đọc", e.do(http.MethodGet, bannersPath, couponManager, "", ""), http.StatusOK); g["revision"] != float64(1) {
		t.Fatalf("sau 50 lần: %v", g)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'content.banners.save'`); n != 1 {
		t.Fatalf("audit: %d dòng, mong 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'content.banners.changed'`); n != 1 {
		t.Fatalf("sự kiện: %d, mong 1", n)
	}
}

// Tenant khác: danh sách riêng từng shop; quản lý shop B thấy rỗng, lưu của B không đổi A.
func TestBannersAPI_OtherTenant(t *testing.T) {
	t.Parallel()
	e := newBannerEnv(t)
	e.ok("A lưu", e.do(http.MethodPut, bannersPath, couponManager, "a1", saveBody(0, imageBannerJSON(uuid.New(), e.img, ""))),
		http.StatusOK)
	const managerB = "quan-ly-b@elimaz.test"
	e.staff.Grant(t, e.other, e.staff.User(t, e.other, managerB, "active"), rolePerms(t, "manager"), "all", "")
	if g := e.ok("B đọc", e.do(http.MethodGet, bannersPath, managerB, "", ""), http.StatusOK); g["revision"] != float64(0) ||
		len(g["banners"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("B thấy: %v", g)
	}
	expect(t, "B dùng ảnh của A", e.do(http.MethodPut, bannersPath, managerB, "b1",
		saveBody(0, imageBannerJSON(uuid.New(), e.img, ""))), http.StatusUnprocessableEntity, "banner_image_invalid")
	e.ok("B lưu", e.do(http.MethodPut, bannersPath, managerB, "b2", saveBody(0)), http.StatusOK)
	if g := e.ok("A đọc", e.do(http.MethodGet, bannersPath, couponManager, "", ""), http.StatusOK); g["revision"] != float64(1) ||
		len(g["banners"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Fatalf("A bị đổi: %v", g)
	}
}

// checkBannerRules: mỗi luật sai → 422 đúng mã (không lưu); trả thân 9 banner cho ca xem trước.
func checkBannerRules(e *bannerEnv) []string {
	e.t.Helper()
	t := e.t
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"new"}`, uuid.New())
	}
	pins := fmt.Sprintf(`[%q,%q,%q,%q]`, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	for i, c := range []struct {
		name, body, code string
	}{
		{"9 banner", saveBody(0, nine...), "too_many_banners"},
		{
			"chiến dịch thiếu", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"campaign"}`, uuid.New())),
			"banner_campaign_required",
		},
		{
			"danh mục thiếu", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"category"}`, uuid.New())),
			"banner_category_required",
		},
		{"ảnh thiếu mô tả", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"image","imageDesktop":%q,"linkKind":"offers"}`,
			uuid.New(), e.img)), "banner_image_required"},
		{"khung giờ ngược", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"new","startsAt":"2026-12-02T00:00:00Z","endsAt":"2026-12-01T00:00:00Z"}`,
			uuid.New())), "banner_window_invalid"},
		{"ghim 4", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"new","productIds":%s}`, uuid.New(), pins)), "too_many_pins"},
		{
			"ghim mẫu không có", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"new","productIds":[%q]}`, uuid.New(), uuid.New())),
			"pin_not_in_source",
		},
		{
			"tiêu đề 61", saveBody(0, fmt.Sprintf(`{"id":%q,"enabled":true,"kind":"new","title":%q}`, uuid.New(), strings.Repeat("a", 61))),
			"invalid_banner",
		},
		{"ảnh bị từ chối", saveBody(0, imageBannerJSON(uuid.New(), e.rejected, "")), "file_rejected"},
		{"ảnh tenant khác", saveBody(0, imageBannerJSON(uuid.New(), e.foreign, "")), "banner_image_invalid"},
	} {
		expect(t, c.name, e.do(http.MethodPut, bannersPath, couponManager, fmt.Sprintf("bad-%d", i), c.body),
			http.StatusUnprocessableEntity, c.code)
	}
	return nine
}

// sameKeyBodies gửi n yêu cầu song song (cùng khóa chống trùng) bằng send; mỗi câu trả lời phải là 200 hoặc 409
// request_in_progress; trả số lần gặp của từng thân 200.
func sameKeyBodies(t *testing.T, n int, send func() *httptest.ResponseRecorder) map[string]int {
	t.Helper()
	bodies := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			rec := send()
			mu.Lock()
			defer mu.Unlock()
			if rec.Code == http.StatusOK {
				bodies[rec.Body.String()]++
				return
			}
			expect(t, "song song", rec, http.StatusConflict, "request_in_progress")
		})
	}
	wg.Wait()
	return bodies
}

// Task 488 (mockup HV19, HV20): tên người lưu cuối; ảnh đã xử lý có rộng, cao; ảnh đang chờ không có; ảnh bị từ chối
// sau khi lưu có lý do.
func TestBannersAPI_MockupFields(t *testing.T) {
	t.Parallel()
	e := newBannerEnv(t)
	var pending uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		pending = filestest.Uploaded(t, tx, e.tenant, content.BannerPurposeKey)
		return nil
	})
	body := saveBody(0, imageBannerJSON(uuid.New(), e.img, fmt.Sprintf(`,"imageMobile":%q`, pending)))
	s := e.ok("lưu", e.do(http.MethodPut, bannersPath, couponManager, "m1", body), http.StatusOK)
	if s["updatedByName"] != couponManager {
		t.Fatalf("tên người lưu: %v", s["updatedByName"])
	}
	img := bannerImages(s)
	if r := img[e.img.String()]; r["width"] != float64(2000) || r["height"] != float64(3000) || r["rejectReason"] != nil {
		t.Fatalf("ảnh đã xử lý: %v", r)
	}
	if r := img[pending.String()]; r["status"] != "uploaded" || r["width"] != nil || r["rejectReason"] != nil {
		t.Fatalf("ảnh đang chờ: %v", r)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		filestest.MarkRejected(t, tx, pending, "too_small")
		return nil
	})
	g := e.ok("đọc", e.do(http.MethodGet, bannersPath, couponManager, "", ""), http.StatusOK)
	if r := bannerImages(g)[pending.String()]; r["status"] != "rejected" || r["rejectReason"] != "too_small" || r["width"] != nil {
		t.Fatalf("ảnh bị từ chối: %v", r)
	}
	if g["updatedByName"] != couponManager {
		t.Fatalf("đọc: tên người lưu %v", g["updatedByName"])
	}
}

// bannerImages: ảnh của câu trả lời theo mã tệp.
func bannerImages(m map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	imgs, _ := m["images"].([]any)
	for _, x := range imgs {
		im, _ := x.(map[string]any)
		id, _ := im["fileId"].(string)
		out[id] = im
	}
	return out
}
