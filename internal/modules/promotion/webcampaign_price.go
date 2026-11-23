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

// CampaignSlug là phần tên của liên kết: bỏ dấu, chữ thường, mỗi chuỗi ký tự ngoài [a-z0-9] thành một "-", bỏ "-" ở
// hai đầu, cắt 60 ký tự (cắt sau khi bỏ "-", như slugify của website). Bỏ dấu bằng textnorm.Fold (bảng tiếng Việt):
// chữ Latin có dấu ngoài tiếng Việt (ñ, ç…) thành "-" thay vì chữ gốc như NFD của website; chỉ làm liên kết khác
// tên, mã ở cuối vẫn tìm đúng chiến dịch (trang chuyển hướng về tên hiện tại).
func CampaignSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range textnorm.Fold(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimPrefix(b.String(), "-")
	s = strings.TrimSuffix(s, "-")
	if len(s) > slugMax {
		s = s[:slugMax]
	}
	return s
}

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
