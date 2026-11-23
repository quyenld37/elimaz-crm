package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// campaignEnv: couponEnv (quản lý có promotion.manage, sale chỉ promotion.read) cộng người bán ELIMAZ và một mẫu hai
// biến thể (100.000, 250.000).
type campaignEnv struct {
	*couponEnv
	seller, product uuid.UUID
}

func newCampaignEnv(t *testing.T) *campaignEnv {
	t.Helper()
	e := &campaignEnv{couponEnv: newCouponEnv(t)}
	e.seller, _ = shoptest.SeedSellers(t, e.staff.Owner, e.tenant)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.product = catalogtest.Product(t, tx, e.tenant, e.seller, "CD01")
		catalogtest.Variant(t, tx, e.tenant, e.seller, e.product, 1, 100000)
		catalogtest.Variant(t, tx, e.tenant, e.seller, e.product, 2, 250000)
		return nil
	})
	return e
}

// campaignBody là chiến dịch name percent % cả shop, chạy quanh e.now (từ 1 giờ trước tới 7 ngày sau).
func (e *campaignEnv) campaignBody(name string, percent int) string {
	return fmt.Sprintf(`{"name":%q,"percent":%d,"scope":"all","startsAt":%q,"endsAt":%q}`, name, percent,
		e.now.Add(-time.Hour).Format(time.RFC3339), e.now.Add(7*24*time.Hour).Format(time.RFC3339))
}

func (e *campaignEnv) post(path, key, body string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, path, couponManager, key, body)
}

// [KM04] Vòng đời qua HTTP: sale tạo → 403; tạo 60 % (trần 50) → 422 percent_too_high; tạo nháp → 201 (path
// /uu-dai/<tên>-<linkKey>); bật khi chưa xem trước → 422 preview_required; xem trước → số mẫu, biến thể, giá mẫu; đổi
// tên (revision tăng) → bật bằng bản xem cũ 422; xem lại, bật → 200 active; sửa, xóa khi đang bật → 409
// campaign_running; revision cũ → 409 version_conflict; tạm dừng rồi xóa có lý do → 200; đọc lại → 404; mỗi lần
// ghi một audit (bật, tạm dừng, xóa kèm lý do).
func TestWebCampaignAPI_Lifecycle(t *testing.T) {
	t.Parallel()
	e := newCampaignEnv(t)
	expect(t, "sale tạo", e.do(http.MethodPost, "/api/v1/web-campaigns", couponSale, "w0", e.campaignBody("Sale", 20)),
		http.StatusForbidden, "forbidden")
	expect(t, "60 %", e.post("/api/v1/web-campaigns", "w1", e.campaignBody("Xả kho", 60)),
		http.StatusUnprocessableEntity, "percent_too_high")
	c := e.ok("tạo", e.post("/api/v1/web-campaigns", "w2", e.campaignBody("Sale cuối năm", 20)), http.StatusCreated)
	id, _ := c["id"].(string)
	key, _ := c["linkKey"].(string)
	if c["state"] != "draft" || c["status"] != "draft" || c["revision"] != float64(1) || c["path"] != "/uu-dai/sale-cuoi-nam-"+key {
		t.Fatalf("chiến dịch mới: %v", c)
	}
	base := "/api/v1/web-campaigns/" + id
	expect(t, "bật khi chưa xem trước", e.post(base+"/state", "w3",
		`{"revision":1,"state":"enabled","reason":"duyệt","preview":{"revision":1,"modelCount":1,"variantCount":2}}`),
		http.StatusUnprocessableEntity, "preview_required")
	p := e.ok("xem trước", e.post(base+"/preview", "w4", `{"revision":1}`), http.StatusOK)
	samples, _ := p["samples"].([]any)
	if p["modelCount"] != float64(1) || p["variantCount"] != float64(2) || len(samples) != 2 ||
		samples[0].(map[string]any)["salePrice"] != float64(80000) { //nolint:forcetypeassert // test
		t.Fatalf("xem trước: %v", p)
	}
	saved := e.ok("đổi tên", e.do(http.MethodPut, base, couponManager, "w5",
		`{"revision":1,"campaign":`+e.campaignBody("Sale cuối năm 2", 20)+`}`), http.StatusOK)
	if saved["revision"] != float64(2) {
		t.Fatalf("sau khi lưu: %v", saved)
	}
	expect(t, "bật bằng bản xem cũ", e.post(base+"/state", "w6",
		`{"revision":2,"state":"enabled","reason":"duyệt","preview":{"revision":1,"modelCount":1,"variantCount":2}}`),
		http.StatusUnprocessableEntity, "preview_required")
	e.ok("xem lại", e.post(base+"/preview", "w7", `{"revision":2}`), http.StatusOK)
	on := e.ok("bật", e.post(base+"/state", "w8",
		`{"revision":2,"state":"enabled","reason":"duyệt","preview":{"revision":2,"modelCount":1,"variantCount":2}}`), http.StatusOK)
	if on["state"] != "enabled" || on["status"] != "active" || on["revision"] != float64(3) {
		t.Fatalf("sau khi bật: %v", on)
	}
	expect(t, "sửa khi đang bật", e.do(http.MethodPut, base, couponManager, "w9",
		`{"revision":3,"campaign":`+e.campaignBody("Đổi", 20)+`}`), http.StatusConflict, "campaign_running")
	expect(t, "xóa khi đang bật", e.post(base+"/delete", "w10", `{"revision":3,"reason":"hết"}`),
		http.StatusConflict, "campaign_running")
	expect(t, "revision cũ", e.post(base+"/state", "w11", `{"revision":2,"state":"paused","reason":"dừng"}`),
		http.StatusConflict, "version_conflict")
	expect(t, "lý do rỗng", e.post(base+"/state", "w12", `{"revision":3,"state":"paused","reason":""}`),
		http.StatusBadRequest, "")
	e.ok("tạm dừng", e.post(base+"/state", "w13", `{"revision":3,"state":"paused","reason":"hết hàng"}`), http.StatusOK)
	del := e.ok("xóa", e.post(base+"/delete", "w14", `{"revision":4,"reason":"bỏ đợt"}`), http.StatusOK)
	if del["revision"] != float64(5) {
		t.Fatalf("xóa: %v", del)
	}
	expect(t, "đọc sau khi xóa", e.do(http.MethodGet, base, couponSale, "", ""), http.StatusNotFound, "not_found")
	for action, n := range map[string]int{
		"promotion.web_campaign.create": 1, "promotion.web_campaign.save": 1, "promotion.web_campaign.state": 2,
		"promotion.web_campaign.delete": 1,
	} {
		if got := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = $1
			AND entity_id = $2`, action, id); got != n {
			t.Errorf("audit %s: %d, mong %d", action, got, n)
		}
	}
	if got := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND ((action = 'promotion.web_campaign.state' AND after->>'reason' IN ('duyệt', 'hết hàng'))
		  OR (action = 'promotion.web_campaign.delete' AND after->>'reason' = 'bỏ đợt'
		      AND before->>'name' = 'Sale cuối năm 2'))`, id); got != 3 {
		t.Errorf("audit có lý do: %d, mong 3", got)
	}
}

// 50 lần setWebCampaignState song song cùng khóa chống trùng → mọi câu trả lời 200 giống nhau, chiến dịch chỉ đổi một
// lần (revision 2), một dòng audit có lý do.
func TestWebCampaignAPI_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	e := newCampaignEnv(t)
	c := e.ok("tạo", e.post("/api/v1/web-campaigns", "c1", e.campaignBody("Song song", 20)), http.StatusCreated)
	id, _ := c["id"].(string)
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 50)
	for i := range recs {
		wg.Go(func() {
			recs[i] = e.post("/api/v1/web-campaigns/"+id+"/state", "same-key", `{"revision":1,"state":"paused","reason":"tạm nghỉ"}`)
		})
	}
	wg.Wait()
	for i, r := range recs {
		if r.Code != http.StatusOK && r.Code != http.StatusConflict {
			t.Fatalf("yêu cầu %d: %d %s", i, r.Code, r.Body.String())
		}
	}
	got := e.ok("đọc", e.do(http.MethodGet, "/api/v1/web-campaigns/"+id, couponSale, "", ""), http.StatusOK)
	if got["revision"] != float64(2) || got["state"] != "paused" {
		t.Fatalf("sau 50 lần: %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'promotion.web_campaign.state'
		AND entity_id = $1 AND after->>'reason' = 'tạm nghỉ'`, id); n != 1 {
		t.Fatalf("audit: %d dòng, mong 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events
		WHERE topic = 'promotion.web_campaign.changed' AND aggregate_id = $1`, id); n != 2 {
		t.Fatalf("sự kiện: %d, mong 2 (tạo, tạm dừng)", n)
	}
}

// Danh sách lọc theo trạng thái hiển thị (giờ máy chủ Deps.Now), tìm theo tên, trang; đích chọn được (mẫu theo tên
// không dấu, danh mục); chiến dịch của tenant khác → 404.
func TestWebCampaignAPI_ListTargetsTenant(t *testing.T) {
	t.Parallel()
	e := newCampaignEnv(t)
	for i, name := range []string{"Đầm hè", "Áo thu", "Đầm đông"} {
		e.ok("tạo", e.post("/api/v1/web-campaigns", fmt.Sprintf("l%d", i), e.campaignBody(name, 10)), http.StatusCreated)
	}
	list := func(q string) map[string]any {
		return e.ok("danh sách "+q, e.do(http.MethodGet, "/api/v1/web-campaigns?"+q, couponSale, "", ""), http.StatusOK)
	}
	if items, _ := list("q=%C4%90%E1%BA%A7m&status=draft")["items"].([]any); len(items) != 2 {
		t.Fatalf("lọc nháp theo tên Đầm: %d", len(items))
	}
	if items, _ := list("status=active")["items"].([]any); len(items) != 0 {
		t.Fatalf("lọc đang chạy: %d", len(items))
	}
	first := list("limit=2")
	if items, _ := first["items"].([]any); len(items) != 2 || first["nextCursor"] == nil {
		t.Fatalf("trang 1: %v", first)
	}
	next, _ := first["nextCursor"].(string)
	if items, _ := list("limit=2&cursor=" + next)["items"].([]any); len(items) != 1 {
		t.Fatalf("trang 2: %d", len(items))
	}
	tg := e.ok("đích mẫu", e.do(http.MethodGet, "/api/v1/web-campaign-targets?kind=product&q=ao%20thu", couponSale, "", ""), http.StatusOK)
	if items, _ := tg["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != e.product.String() { //nolint:forcetypeassert // test
		t.Fatalf("đích mẫu: %v", tg)
	}
	expect(t, "loại đích lạ", e.do(http.MethodGet, "/api/v1/web-campaign-targets?kind=brand", couponSale, "", ""),
		http.StatusBadRequest, "")

	c := e.ok("tạo", e.post("/api/v1/web-campaigns", "t1", e.campaignBody("Riêng shop A", 10)), http.StatusCreated)
	tb := e.staff.Tenant(t, "B")
	mb := e.staff.User(t, tb, "quan-ly-b@elimaz.test", "active")
	e.staff.Grant(t, tb, mb, rolePerms(t, "manager"), "all", "")
	shoptest.SeedSellers(t, e.staff.Owner, tb)
	id, _ := c["id"].(string)
	expect(t, "tenant khác đọc", e.do(http.MethodGet, "/api/v1/web-campaigns/"+id, "quan-ly-b@elimaz.test", "", ""),
		http.StatusNotFound, "not_found")
	expect(t, "tenant khác tạm dừng", e.do(http.MethodPost, "/api/v1/web-campaigns/"+id+"/state", "quan-ly-b@elimaz.test",
		"tb1", `{"revision":1,"state":"paused","reason":"x"}`), http.StatusNotFound, "not_found")
}
