package promotion_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/promotion"
)

// readContract đọc tệp mẫu do website sinh (testdata/contracts, web-campaign.gen.mjs chép nguyên hàm của website).
func readContract(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile("../../../testdata/contracts/" + name)
	if err != nil {
		t.Fatalf("đọc %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("giải %s: %v", name, err)
	}
}

// [WB06] Bảng mẫu làm tròn 200 dòng (giá 1 đồng, giá lẻ, 1 %, 99 %, giá lớn) khớp campaignPrice của website; áp khi
// 0 < giá < niêm yết.
func TestCampaignPrice_MatchesWebsiteTable(t *testing.T) {
	t.Parallel()
	var rows []struct {
		ListPrice int64 `json:"listPrice"`
		Percent   int   `json:"percent"`
		SalePrice int64 `json:"salePrice"`
		Applies   bool  `json:"applies"`
	}
	readContract(t, "web-campaign-rounding.json", &rows)
	if len(rows) != 200 {
		t.Fatalf("bảng mẫu có %d dòng, cần 200", len(rows))
	}
	for _, r := range rows {
		got, ok := promotion.CampaignPrice(r.ListPrice, r.Percent)
		if ok != r.Applies || (ok && got != r.SalePrice) {
			t.Errorf("giá %d giảm %d%%: (%d, %v), website (%d, %v)", r.ListPrice, r.Percent, got, ok, r.SalePrice, r.Applies)
		}
	}
	for _, c := range []struct {
		list    int64
		percent int
	}{{0, 10}, {-5, 10}, {100, 0}, {100, 100}} {
		if _, ok := promotion.CampaignPrice(c.list, c.percent); ok {
			t.Errorf("giá %d giảm %d%% không được áp", c.list, c.percent)
		}
	}
}

// Liên kết /uu-dai/<slug>-<8 hex đầu>: khớp bảng mẫu của website (bỏ dấu, cắt 60, giữ "--" khi cắt rơi vào "-", tên
// không còn chữ thì chỉ có mã).
func TestCampaignLink_MatchesWebsiteTable(t *testing.T) {
	t.Parallel()
	var rows []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Slug    string `json:"slug"`
		LinkKey string `json:"linkKey"`
		Path    string `json:"path"`
	}
	readContract(t, "web-campaign-links.json", &rows)
	if len(rows) < 12 {
		t.Fatalf("bảng liên kết có %d dòng", len(rows))
	}
	for _, r := range rows {
		id := uuid.MustParse(r.ID)
		if got := promotion.CampaignSlug(r.Name); got != r.Slug {
			t.Errorf("slug %q: %q, website %q", r.Name, got, r.Slug)
		}
		if got := promotion.CampaignLinkKey(id); got != r.LinkKey {
			t.Errorf("link key %s: %q, website %q", r.ID, got, r.LinkKey)
		}
		if got := promotion.CampaignPath(id, r.Name); got != r.Path {
			t.Errorf("đường dẫn %q: %q, website %q", r.Name, got, r.Path)
		}
	}
}

// Trạng thái hiển thị đúng campaignStatus của admin: draft, paused theo state; scheduled trước giờ bắt đầu; active từ
// đúng giờ bắt đầu; ended từ đúng giờ kết thúc.
func TestCampaignStatus(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	c := func(state string) promotion.WebCampaign {
		return promotion.WebCampaign{State: state, StartsAt: start, EndsAt: end}
	}
	for _, tc := range []struct {
		state string
		now   time.Time
		want  string
	}{
		{promotion.StateDraft, start.Add(time.Hour), promotion.CampaignDraft},
		{promotion.StatePaused, start.Add(time.Hour), promotion.CampaignPaused},
		{promotion.StatePaused, end.Add(time.Hour), promotion.CampaignPaused},
		{promotion.StateEnabled, start.Add(-time.Nanosecond), promotion.CampaignScheduled},
		{promotion.StateEnabled, start, promotion.CampaignActive},
		{promotion.StateEnabled, end.Add(-time.Nanosecond), promotion.CampaignActive},
		{promotion.StateEnabled, end, promotion.CampaignEnded},
	} {
		if got := promotion.CampaignStatus(c(tc.state), tc.now); got != tc.want {
			t.Errorf("%s lúc %s: %s, cần %s", tc.state, tc.now, got, tc.want)
		}
	}
}

// Hai chiến dịch cùng biến thể: giá thấp hơn thắng; hòa giá thì id nhỏ hơn (cheapestProgram); chiến dịch không áp
// được (giá sau giảm ≥ niêm yết) bị bỏ.
func TestBestCampaign(t *testing.T) {
	t.Parallel()
	low := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	high := uuid.MustParse("ffffffff-0000-4000-8000-000000000001")
	cands := []promotion.WebCampaign{{ID: high, Percent: 20, Revision: 3}, {ID: low, Percent: 10, Revision: 1}}
	got, ok := promotion.BestCampaign(cands, 100000)
	if !ok || got.CampaignID != high || got.UnitPrice != 80000 || got.Discount != 20000 || got.Revision != 3 {
		t.Fatalf("giá thấp hơn thắng: %+v %v", got, ok)
	}
	// 1 % và 2 % của giá 3 đồng đều làm tròn về 3 (không áp) → không chiến dịch nào.
	if _, ok := promotion.BestCampaign(cands[:0], 3); ok {
		t.Fatal("không có chiến dịch mà vẫn áp")
	}
	if _, ok := promotion.BestCampaign([]promotion.WebCampaign{{ID: low, Percent: 1}, {ID: high, Percent: 2}}, 3); ok {
		t.Fatal("giá sau giảm bằng niêm yết vẫn áp")
	}
	// 33 % và 34 % của 2 đồng đều ra 1 đồng: hòa → id nhỏ hơn, bất kể thứ tự đầu vào.
	tie := []promotion.WebCampaign{{ID: high, Percent: 34}, {ID: low, Percent: 33}}
	if got, ok := promotion.BestCampaign(tie, 2); !ok || got.CampaignID != low || got.UnitPrice != 1 {
		t.Fatalf("hòa giá: %+v %v", got, ok)
	}
}

// Phạm vi: all phủ mọi mẫu; products theo mã mẫu; categories theo đúng danh mục của mẫu (không tính danh mục cha).
func TestCampaignCovers(t *testing.T) {
	t.Parallel()
	p, other, cat, parent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ref := promotion.VariantRef{VariantID: uuid.New(), ProductID: p, CategoryID: cat}
	for _, tc := range []struct {
		c    promotion.WebCampaign
		want bool
	}{
		{promotion.WebCampaign{Scope: promotion.ScopeAll}, true},
		{promotion.WebCampaign{Scope: promotion.ScopeProducts, TargetIDs: []uuid.UUID{other, p}}, true},
		{promotion.WebCampaign{Scope: promotion.ScopeProducts, TargetIDs: []uuid.UUID{other}}, false},
		{promotion.WebCampaign{Scope: promotion.ScopeProducts, TargetIDs: []uuid.UUID{cat}}, false},
		{promotion.WebCampaign{Scope: promotion.ScopeCategories, TargetIDs: []uuid.UUID{cat}}, true},
		{promotion.WebCampaign{Scope: promotion.ScopeCategories, TargetIDs: []uuid.UUID{parent}}, false},
		{promotion.WebCampaign{Scope: promotion.ScopeCategories, TargetIDs: []uuid.UUID{p}}, false},
	} {
		if got := promotion.CampaignCovers(tc.c, ref); got != tc.want {
			t.Errorf("%s %v: %v, cần %v", tc.c.Scope, tc.c.TargetIDs, got, tc.want)
		}
	}
}
