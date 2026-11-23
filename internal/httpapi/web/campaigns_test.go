package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

func campaignWriter() promotion.Writer {
	return promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
}

// campaign tạo chiến dịch name (percent %, cả shop trừ khi có targets — mẫu) chạy [start, end); state enabled thì xem
// trước rồi bật, paused thì bật rồi tạm dừng, deleted thì tạm dừng rồi xóa.
func (e *env) campaign(name string, start, end time.Time, state string, targets ...uuid.UUID) promotion.WebCampaign {
	e.t.Helper()
	a := promotion.Actor{TenantID: e.tenant, UserID: e.owner.UserID}
	in := promotion.WebCampaignInput{Name: name, Percent: 20, Scope: promotion.ScopeAll, StartsAt: start, EndsAt: end, Clearance: true}
	if len(targets) > 0 {
		in.Scope, in.TargetIDs, in.Clearance = promotion.ScopeProducts, targets, false
	}
	var c promotion.WebCampaign
	e.app(func(tx pgx.Tx) error {
		w := campaignWriter()
		var err error
		if c, err = w.CreateCampaign(e.t.Context(), tx, a, e.seller, in); err != nil || state == promotion.StateDraft {
			return err //nolint:wrapcheck // test
		}
		p, err := w.PreviewCampaign(e.t.Context(), tx, e.seller, c.ID, c.Revision)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		ch, err := w.SetCampaignState(e.t.Context(), tx, a, e.seller, c.ID, c.Revision, promotion.StateEnabled, "bật", p.PreviewCounts)
		if err != nil || state == promotion.StateEnabled {
			if ch.After != nil {
				c = *ch.After
			}
			return err //nolint:wrapcheck // test
		}
		ch, err = w.SetCampaignState(e.t.Context(), tx, a, e.seller, c.ID, ch.After.Revision, promotion.StatePaused, "dừng",
			promotion.PreviewCounts{})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		c = *ch.After
		if state == "deleted" {
			_, err = w.DeleteCampaign(e.t.Context(), tx, a, e.seller, c.ID, c.Revision, "bỏ")
		}
		return err //nolint:wrapcheck // test
	})
	return c
}

type campaignList struct {
	Items []map[string]any `json:"items"`
}

// [WB06], [KM04] Danh sách cho website: chỉ chiến dịch đã bật chưa hết giờ (đang chạy, đã hẹn giờ) theo id, kèm
// slug, linkKey, đích, clearance, status; không có nháp, tạm dừng, đã xóa, đã hết; ETag, If-None-Match → 304; bộ
// đệm SpaceCampaigns 30 s.
func TestWebCampaigns(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	p := e.product("CD01", "[]")
	now := time.Now()
	running := e.campaign("Sale cuối năm", now.Add(-time.Hour), now.Add(24*time.Hour), promotion.StateEnabled)
	later := e.campaign("Đầm mới", now.Add(time.Hour), now.Add(48*time.Hour), promotion.StateEnabled, p.ID)
	e.campaign("Nháp", now.Add(-time.Hour), now.Add(time.Hour), promotion.StateDraft)
	e.campaign("Tạm dừng", now.Add(-time.Hour), now.Add(time.Hour), promotion.StatePaused)
	e.campaign("Đã xóa", now.Add(-time.Hour), now.Add(time.Hour), "deleted")
	e.campaign("Đã hết", now.Add(-2*time.Hour), now.Add(-time.Hour), promotion.StateEnabled)

	rec := e.get(e.key, "/api/v1/web/campaigns")
	if rec.Code != http.StatusOK {
		t.Fatalf("mong 200, có %d %s", rec.Code, rec.Body.String())
	}
	var l campaignList
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 2 {
		t.Fatalf("mong 2 chiến dịch, có %d: %s", len(l.Items), rec.Body.String())
	}
	byID := map[string]map[string]any{}
	for _, it := range l.Items {
		byID[it["id"].(string)] = it //nolint:forcetypeassert // test
	}
	r, s := byID[running.ID.String()], byID[later.ID.String()]
	if r["status"] != "active" || r["slug"] != "sale-cuoi-nam" || r["linkKey"] != running.LinkKey || r["percent"] != float64(20) ||
		r["clearance"] != true || r["allowCoupon"] != false || r["scope"] != "all" || r["sellerId"] != e.seller.String() {
		t.Fatalf("chiến dịch đang chạy: %v", r)
	}
	if ts, _ := s["targetIds"].([]any); s["status"] != "scheduled" || len(ts) != 1 || ts[0] != p.ID.String() {
		t.Fatalf("chiến dịch hẹn giờ: %v", s)
	}
	if ttl := e.rc.ttl(cache.SpaceCampaigns); ttl != 30*time.Second {
		t.Fatalf("hạn sống SpaceCampaigns: %s", ttl)
	}
	tag := rec.Header().Get("ETag")
	if !etagRe.MatchString(tag) {
		t.Fatalf("ETag: %q", tag)
	}
	if rec := e.get(e.key, "/api/v1/web/campaigns", "If-None-Match", tag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: %d %q", rec.Code, rec.Body.String())
	}
}

// Trang /uu-dai: theo link key (hoa thường đều được) → chiến dịch, cả khi tạm dừng (status paused) để website báo
// hết; nháp, đã xóa, mã lạ, chiến dịch của tenant khác → 404; link key sai dạng → 400; khóa thiếu quyền → 403.
func TestWebCampaign_ByLinkKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	now := time.Now()
	paused := e.campaign("Ưu đãi tháng 12/2026 (chỉ online)", now.Add(-time.Hour), now.Add(time.Hour), promotion.StatePaused)
	draft := e.campaign("Nháp", now.Add(-time.Hour), now.Add(time.Hour), promotion.StateDraft)
	gone := e.campaign("Đã xóa", now.Add(-time.Hour), now.Add(time.Hour), "deleted")

	rec := e.get(e.key, "/api/v1/web/campaigns/"+strings.ToUpper(paused.LinkKey))
	if rec.Code != http.StatusOK {
		t.Fatalf("tạm dừng: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != paused.ID.String() || got["status"] != "paused" || got["slug"] != "uu-dai-thang-12-2026-chi-online" {
		t.Fatalf("chiến dịch theo liên kết: %v", got)
	}

	foreign := e.foreignCampaign()
	for name, key := range map[string]string{
		"nháp": draft.LinkKey, "đã xóa": gone.LinkKey, "mã lạ": "0000abcd", "tenant khác": foreign,
	} {
		if rec := e.get(e.key, "/api/v1/web/campaigns/"+key); rec.Code != http.StatusNotFound ||
			!strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Errorf("%s: mong 404 not_found, có %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := e.get(e.key, "/api/v1/web/campaigns/xyz"); rec.Code != http.StatusBadRequest {
		t.Errorf("link key sai dạng: %d", rec.Code)
	}
	for _, path := range []string{"/api/v1/web/campaigns", "/api/v1/web/campaigns/" + paused.LinkKey} {
		if rec := e.get(e.otherKey, path); rec.Code != http.StatusForbidden {
			t.Errorf("%s thiếu quyền: %d", path, rec.Code)
		}
		if rec := e.get("", path); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s không khóa: %d", path, rec.Code)
		}
	}
}

// foreignCampaign tạo ở tenant B (cùng database) một chiến dịch đang chạy (xem trước rồi bật) và trả link key của nó;
// kiểm bằng khóa dịch vụ của tenant B rằng chiến dịch đó đọc được, để 404 từ tenant A chỉ có thể do tách tenant.
func (e *env) foreignCampaign() string {
	e.t.Helper()
	tb := e.staff.Tenant(e.t, "B")
	sellerB, _ := shoptest.SeedSellers(e.t, e.staff.Owner, tb)
	now := time.Now()
	a := promotion.Actor{TenantID: tb, UserID: uuid.New()}
	var key string
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tb, func(tx pgx.Tx) error {
		w := campaignWriter()
		c, err := w.CreateCampaign(e.t.Context(), tx, a, sellerB, promotion.WebCampaignInput{
			Name: "Shop khác", Percent: 10, Scope: promotion.ScopeAll, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
		})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		p, err := w.PreviewCampaign(e.t.Context(), tx, sellerB, c.ID, c.Revision)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := w.SetCampaignState(e.t.Context(), tx, a, sellerB, c.ID, c.Revision, promotion.StateEnabled, "bật",
			p.PreviewCounts); err != nil {
			return err //nolint:wrapcheck // test
		}
		live, err := promotion.WebCampaignByLinkKey(e.t.Context(), tx, c.LinkKey)
		if err != nil || live.State != promotion.StateEnabled {
			e.t.Fatalf("chiến dịch của tenant B phải đang bật và đọc được trong tenant B: %+v %v", live, err)
		}
		key = c.LinkKey
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return key
}
