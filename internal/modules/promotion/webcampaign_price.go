package promotion

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Luật giá, phạm vi, trạng thái và liên kết của chiến dịch giảm giá web (Task 452), chép đúng website (apps/storefront
// src/server/promotions/engine.ts campaignPrice, cheapestProgram; src/server/campaigns/rules.ts campaignCovers;
// src/features/promotions/campaign-link.ts) và admin (src/lib/campaigns.mjs campaignStatus). Bảng mẫu
// testdata/contracts/web-campaign-{rounding,links}.json do hàm của website sinh; test của gói đọc cùng tệp.

// Trạng thái lưu (state) và trạng thái hiển thị (CampaignStatus) của chiến dịch.
const (
	StateDraft   = "draft"
	StateEnabled = "enabled"
	StatePaused  = "paused"

	CampaignDraft     = "draft"
	CampaignPaused    = "paused"
	CampaignScheduled = "scheduled"
	CampaignActive    = "active"
	CampaignEnded     = "ended"
)

// Phạm vi của chiến dịch.
const (
	ScopeAll        = "all"
	ScopeProducts   = "products"
	ScopeCategories = "categories"
)

// maxSafeInteger là Number.MAX_SAFE_INTEGER: ngoài vùng này phép nhân số thực của website không còn chính xác, nên
// chiến dịch không áp (giá thật của shop nhỏ hơn rất nhiều).
const maxSafeInteger = 1<<53 - 1

// linkKeyLen, slugMax: liên kết /uu-dai/<slug ≤ 60>-<8 hex đầu của id> (CAMPAIGN_KEY_LENGTH, slugify(name, 60)).
const (
	linkKeyLen = 8
	slugMax    = 60
)

// CampaignPrice trả giá sau giảm percent % của giá niêm yết list, làm tròn như Math.round của website
// (round(list × (100 − percent) / 100), nửa đơn vị làm tròn lên), và true khi giá đó áp được: 0 < giá < list.
func CampaignPrice(list int64, percent int) (int64, bool) {
	if list <= 0 || percent < 1 || percent > 99 || list > maxSafeInteger/(100-int64(percent)) {
		return 0, false
	}
	price := (list*(100-int64(percent)) + 50) / 100
	return price, price > 0 && price < list
}

// CampaignStatus trả trạng thái hiển thị của c ở now: draft, paused theo state; còn lại scheduled (trước giờ bắt đầu),
// active (từ giờ bắt đầu), ended (từ đúng giờ kết thúc).
func CampaignStatus(c WebCampaign, now time.Time) string {
	switch {
	case c.State == StateDraft || c.State == StatePaused:
		return c.State
	case now.Before(c.StartsAt):
		return CampaignScheduled
	case !now.Before(c.EndsAt):
		return CampaignEnded
	}
	return CampaignActive
}

// Running cho biết c đang áp giá ở now: đã bật và startsAt ≤ now < endsAt.
func Running(c WebCampaign, now time.Time) bool {
	return c.State == StateEnabled && CampaignStatus(c, now) == CampaignActive
}

// CampaignCovers cho biết phạm vi của c phủ biến thể v: cả shop, mẫu của v (products) hay đúng danh mục của mẫu
// (categories; không tính danh mục cha), như campaignCovers của website.
func CampaignCovers(c WebCampaign, v VariantRef) bool {
	switch c.Scope {
	case ScopeAll:
		return true
	case ScopeProducts:
		return contains(c.TargetIDs, v.ProductID)
	case ScopeCategories:
		return v.CategoryID != uuid.Nil && contains(c.TargetIDs, v.CategoryID)
	}
	return false
}

func contains(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// BestCampaign chọn trong cands (bên gọi đã lọc: đang chạy, phủ biến thể) chiến dịch cho giá thấp nhất của giá niêm
// yết list; hòa giá thì id nhỏ hơn (cheapestProgram so chuỗi "campaign:<id>"). Không chiến dịch nào áp được → false.
func BestCampaign(cands []WebCampaign, list int64) (Applied, bool) {
	var best Applied
	found := false
	for _, c := range cands {
		price, ok := CampaignPrice(list, c.Percent)
		if !ok {
			continue
		}
		if !found || price < best.UnitPrice || (price == best.UnitPrice && c.ID.String() < best.CampaignID.String()) {
			best = Applied{
				CampaignID: c.ID, Revision: c.Revision, Percent: c.Percent, AllowCoupon: c.AllowCoupon,
				UnitPrice: price, Discount: list - price,
			}
			found = true
		}
	}
	return best, found
}

// CampaignSlug là phần tên của liên kết chiến dịch: textnorm.Slug(name, 60), đúng slugify(name, 60) của website.
func CampaignSlug(name string) string { return textnorm.Slug(name, slugMax) }

// CampaignLinkKey là 8 hex đầu của id (chữ thường): phần tìm chiến dịch của liên kết.
func CampaignLinkKey(id uuid.UUID) string {
	return strings.ReplaceAll(id.String(), "-", "")[:linkKeyLen]
}

// CampaignPath là đường dẫn trang chiến dịch trên website: /uu-dai/<slug>-<link key> (tên không còn chữ thì chỉ có mã).
func CampaignPath(id uuid.UUID, name string) string {
	if s := CampaignSlug(name); s != "" {
		return "/uu-dai/" + s + "-" + CampaignLinkKey(id)
	}
	return "/uu-dai/" + CampaignLinkKey(id)
}
