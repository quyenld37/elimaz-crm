package content

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/files"
)

// Luật banner trang chủ chép từ website (server/banners/rules.ts) và admin (lib/banners.mjs) — Task 453.

// Giới hạn của danh sách banner (BANNER_LIMITS của admin).
const (
	MaxBanners = 8
	MaxKicker  = 40
	MaxTitle   = 60
	MaxCTA     = 30
	MaxAlt     = 120
	MaxPins    = 3
)

// Loại banner.
const (
	KindCampaign = "campaign"
	KindTrending = "trending"
	KindNew      = "new"
	KindCategory = "category"
	KindImage    = "image"
)

// Màu nền: auto = đỏ cho giảm giá, đen cho xu hướng, sáng cho loại khác (website tự chọn).
const (
	ThemeAuto  = "auto"
	ThemeRed   = "red"
	ThemeBlack = "black"
	ThemeLight = "light"
)

// Nơi đến khi bấm banner ảnh.
const (
	LinkCampaign = "campaign"
	LinkCategory = "category"
	LinkProduct  = "product"
	LinkOffers   = "offers"
)

// Lý do banner không hiện trên trang chủ (đúng tên BannerHidden của website, hiện cho nhân viên).
const (
	HiddenDisabled           = "disabled"
	HiddenScheduled          = "scheduled"
	HiddenExpired            = "expired"
	HiddenCampaignMissing    = "campaign-missing"
	HiddenCampaignNotRunning = "campaign-not-running"
	HiddenCategoryMissing    = "category-missing"
	HiddenNoProducts         = "no-products"
	HiddenImageMissing       = "image-missing"
	HiddenProductMissing     = "product-missing"
)

// HiddenReasons trả 9 lý do ẩn theo thứ tự của website.
func HiddenReasons() []string {
	reasons := [...]string{
		HiddenDisabled, HiddenScheduled, HiddenExpired, HiddenCampaignMissing, HiddenCampaignNotRunning,
		HiddenCategoryMissing, HiddenNoProducts, HiddenImageMissing, HiddenProductMissing,
	}
	return reasons[:]
}

// Banner là một banner trang chủ. Mã rỗng (uuid.Nil), chuỗi rỗng, con trỏ nil là "không có". Trường không thuộc loại
// bị NormalizeBanners bỏ: chỉ campaign có CampaignID; chỉ category có CategoryID; ảnh (ImageDesktop, ImageMobile, Alt,
// LinkKind, LinkID) chỉ ở loại image; loại image không có chữ, màu, ghim.
type Banner struct {
	ID         uuid.UUID
	Enabled    bool
	Kind       string
	CampaignID uuid.UUID
	CategoryID uuid.UUID
	// Kicker, Title, CTA rỗng = website tự điền ("Ưu đãi có hạn", tên chiến dịch, "Mua ngay"…).
	Kicker, Title, CTA string
	Theme              string
	// StartsAt, EndsAt: khung giờ hiện tùy chọn.
	StartsAt, EndsAt *time.Time
	// ProductIDs: tối đa 3 mẫu hiện trước, theo thứ tự; phải thuộc nguồn của banner.
	ProductIDs                []uuid.UUID
	ImageDesktop, ImageMobile uuid.UUID
	Alt                       string
	LinkKind                  string
	LinkID                    uuid.UUID
}

// CampaignRef là chiến dịch giảm giá web chưa xóa (view promotion.web_campaigns_v) kèm đích.
type CampaignRef struct {
	ID               uuid.UUID
	Name             string
	Percent          int
	Scope            string // all | products | categories
	State            string // draft | enabled | paused
	StartsAt, EndsAt time.Time
	TargetIDs        []uuid.UUID
}

// Running cho biết chiến dịch đang áp giá ở now — đã bật và StartsAt ≤ now < EndsAt (cùng luật promotion.Running).
func (c CampaignRef) Running(now time.Time) bool {
	return c.State == "enabled" && !now.Before(c.StartsAt) && now.Before(c.EndsAt)
}

// covers: mẫu p thuộc phạm vi của chiến dịch (danh mục so đúng mã, như giá chiến dịch của promotion).
func (c CampaignRef) covers(p ProductRef) bool {
	switch c.Scope {
	case "all":
		return true
	case "products":
		return slices.Contains(c.TargetIDs, p.ID)
	case "categories":
		return p.CategoryID != uuid.Nil && slices.Contains(c.TargetIDs, p.CategoryID)
	}
	return false
}

// CategoryRef là danh mục đang dùng (catalog.categories_v status active).
type CategoryRef struct {
	ID, ParentID uuid.UUID
	Name         string
}

// ProductRef là mẫu đang bán (chưa xóa, status active, người bán hoạt động). Showable: có biến thể bán được giá > 0 và
// có ảnh đã xử lý (website chỉ đưa mẫu như vậy lên banner).
type ProductRef struct {
	ID         uuid.UUID
	Code, Name string
	CategoryID uuid.UUID
	Showable   bool
}

// RefData là dữ liệu module khác mà luật banner cần, đọc một lần cho cả danh sách (LoadRefData). Products, Images chỉ
// gồm mã mà danh sách nhắc; mã không có trong map là "không có" (đã xóa, ngừng bán, tenant khác). Pool: banner id →
// nguồn của banner (chiến dịch, bán chạy, hàng mới, danh mục) có ít nhất một mẫu hiện được.
type RefData struct {
	Campaigns  map[uuid.UUID]CampaignRef
	Categories map[uuid.UUID]CategoryRef
	Products   map[uuid.UUID]ProductRef
	Images     map[uuid.UUID]files.FileStatus
	Pool       map[uuid.UUID]bool
}

// subtree trả mã danh mục root và mọi danh mục con cháu của nó (website: danh mục cha hiện mẫu của danh mục con).
func (r RefData) subtree(root uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{root}
	for i := 0; i < len(out); i++ {
		for _, c := range r.Categories {
			if c.ParentID == out[i] && !slices.Contains(out, c.ID) {
				out = append(out, c.ID)
			}
		}
	}
	return out
}

// imageReady: tệp là ảnh banner đã xử lý xong, chưa xóa.
func (r RefData) imageReady(id uuid.UUID) bool {
	s, ok := r.Images[id]
	return ok && s.Purpose == BannerPurposeKey && !s.Deleted && s.Status == files.StatusReady
}

// NormalizeBanners trả bản sao của bs đã chuẩn hóa như admin cũ (bannerPayloadOne): cắt khoảng trắng hai đầu, màu rỗng
// = auto, bỏ trường không thuộc loại. Không sửa bs.
func NormalizeBanners(bs []Banner) []Banner {
	out := make([]Banner, len(bs))
	for i, b := range bs {
		b.Kicker, b.Title, b.CTA, b.Alt = strings.TrimSpace(b.Kicker), strings.TrimSpace(b.Title),
			strings.TrimSpace(b.CTA), strings.TrimSpace(b.Alt)
		if b.Theme == "" {
			b.Theme = ThemeAuto
		}
		b.ProductIDs = slices.Clone(b.ProductIDs)
		if b.Kind != KindCampaign {
			b.CampaignID = uuid.Nil
		}
		if b.Kind != KindCategory {
			b.CategoryID = uuid.Nil
		}
		if b.Kind == KindImage {
			b.Kicker, b.Title, b.CTA, b.Theme, b.ProductIDs = "", "", "", ThemeAuto, nil
			if b.LinkKind == LinkOffers {
				b.LinkID = uuid.Nil
			}
		} else {
			b.ImageDesktop, b.ImageMobile, b.Alt, b.LinkKind, b.LinkID = uuid.Nil, uuid.Nil, "", "", uuid.Nil
		}
		out[i] = b
	}
	return out
}

// ValidateBanners kiểm danh sách đã chuẩn hóa (NormalizeBanners) theo luật của website: tối đa MaxBanners, mã khác
// nhau; từng banner đúng loại, đủ trường bắt buộc, chữ không quá dài, khung giờ đúng chiều, ghim ≤ MaxPins thuộc nguồn
// của banner, ảnh là ảnh banner chưa xóa, không bị từ chối (đang xử lý vẫn lưu được: banner ẩn tới khi ảnh xong).
// Chiến dịch, danh mục, mẫu đích không còn thì vẫn lưu được (banner ẩn với lý do tương ứng). Lỗi đầu tiên được trả,
// bọc "banner <n>".
func ValidateBanners(bs []Banner, ref RefData) error {
	if len(bs) > MaxBanners {
		return fmt.Errorf("%d banner: %w", len(bs), ErrTooManyBanners)
	}
	seen := map[uuid.UUID]bool{}
	for i, b := range bs {
		if b.ID == uuid.Nil || seen[b.ID] {
			return fmt.Errorf("banner %d: mã rỗng hay trùng: %w", i+1, ErrInvalidBanner)
		}
		seen[b.ID] = true
		if err := validateBanner(b, ref); err != nil {
			return fmt.Errorf("banner %d: %w", i+1, err)
		}
	}
	return nil
}

func validateBanner(b Banner, ref RefData) error {
	if err := validateShape(b); err != nil {
		return err
	}
	if b.StartsAt != nil && b.EndsAt != nil && !b.EndsAt.After(*b.StartsAt) {
		return ErrWindowInvalid
	}
	if err := validatePins(b, ref); err != nil {
		return err
	}
	for _, id := range []uuid.UUID{b.ImageDesktop, b.ImageMobile} {
		if id == uuid.Nil {
			continue
		}
		s, ok := ref.Images[id]
		switch {
		case !ok || s.Deleted || s.Purpose != BannerPurposeKey:
			return ErrImageInvalid
		case s.Status == files.StatusRejected:
			return ErrFileRejected
		}
	}
	return nil
}

// validateShape kiểm loại, màu, đích, độ dài chữ và trường bắt buộc theo loại.
func validateShape(b Banner) error {
	switch {
	case !slices.Contains([]string{KindCampaign, KindTrending, KindNew, KindCategory, KindImage}, b.Kind),
		!slices.Contains([]string{ThemeAuto, ThemeRed, ThemeBlack, ThemeLight}, b.Theme),
		utf8.RuneCountInString(b.Kicker) > MaxKicker, utf8.RuneCountInString(b.Title) > MaxTitle,
		utf8.RuneCountInString(b.CTA) > MaxCTA, utf8.RuneCountInString(b.Alt) > MaxAlt:
		return ErrInvalidBanner
	case b.Kind == KindCampaign && b.CampaignID == uuid.Nil:
		return ErrCampaignRequired
	case b.Kind == KindCategory && b.CategoryID == uuid.Nil:
		return ErrCategoryRequired
	case b.Kind != KindImage:
		return nil
	case b.LinkKind != "" && !slices.Contains([]string{LinkCampaign, LinkCategory, LinkProduct, LinkOffers}, b.LinkKind):
		return ErrInvalidBanner
	case b.ImageDesktop == uuid.Nil || b.Alt == "" || b.LinkKind == "" || (b.LinkKind != LinkOffers && b.LinkID == uuid.Nil):
		return ErrImageRequired
	}
	return nil
}

// validatePins: ≤ MaxPins, không trùng, mỗi mẫu đang bán và thuộc nguồn (chiến dịch, danh mục và con cháu; bán chạy,
// hàng mới: mọi mẫu đang bán). Nguồn không còn (chiến dịch đã xóa, danh mục ẩn) thì chỉ kiểm mẫu đang bán.
func validatePins(b Banner, ref RefData) error {
	if len(b.ProductIDs) > MaxPins {
		return ErrTooManyPins
	}
	var scope []uuid.UUID
	if _, ok := ref.Categories[b.CategoryID]; ok && b.Kind == KindCategory {
		scope = ref.subtree(b.CategoryID)
	}
	c, hasCampaign := ref.Campaigns[b.CampaignID]
	for i, id := range b.ProductIDs {
		if slices.Contains(b.ProductIDs[:i], id) {
			return ErrInvalidBanner
		}
		p, ok := ref.Products[id]
		switch {
		case !ok:
			return ErrPinNotInSource
		case b.Kind == KindCampaign && hasCampaign && !c.covers(p):
			return ErrPinNotInSource
		case scope != nil && !slices.Contains(scope, p.CategoryID):
			return ErrPinNotInSource
		}
	}
	return nil
}

// Visibility cho biết banner b có hiện trên trang chủ ở now không, và lý do khi không (đúng thứ tự kiểm của
// explainBanners trên website). b đã chuẩn hóa; ref của LoadRefData.
func Visibility(b Banner, ref RefData, now time.Time) (shown bool, reason string) {
	if b.Kind == KindImage {
		return imageVisibility(b, ref, now)
	}
	switch b.Kind {
	case KindCampaign:
		c, ok := ref.Campaigns[b.CampaignID]
		switch {
		case !ok:
			return false, HiddenCampaignMissing
		case !c.Running(now) && b.Enabled:
			return false, HiddenCampaignNotRunning
		case !c.Running(now):
			return false, HiddenDisabled
		}
	case KindCategory:
		if _, ok := ref.Categories[b.CategoryID]; !ok {
			return false, HiddenCategoryMissing
		}
	}
	if r := windowReason(b, now); r != "" {
		return false, r
	}
	if !ref.Pool[b.ID] {
		return false, HiddenNoProducts
	}
	return true, ""
}

// windowReason: tắt, chưa tới giờ, hết giờ (EndsAt đúng giờ là hết).
func windowReason(b Banner, now time.Time) string {
	switch {
	case !b.Enabled:
		return HiddenDisabled
	case b.StartsAt != nil && now.Before(*b.StartsAt):
		return HiddenScheduled
	case b.EndsAt != nil && !now.Before(*b.EndsAt):
		return HiddenExpired
	}
	return ""
}

// imageVisibility: banner ảnh hiện khi bật, trong giờ, ảnh đã xử lý xong, và đích mua được ngay (chiến dịch đang chạy,
// danh mục còn, mẫu hiện được).
func imageVisibility(b Banner, ref RefData, now time.Time) (bool, string) {
	if b.ImageDesktop == uuid.Nil || b.LinkKind == "" {
		return false, HiddenImageMissing
	}
	if r := windowReason(b, now); r != "" {
		return false, r
	}
	if !ref.imageReady(b.ImageDesktop) || (b.ImageMobile != uuid.Nil && !ref.imageReady(b.ImageMobile)) {
		return false, HiddenImageMissing
	}
	switch b.LinkKind {
	case LinkCampaign:
		c, ok := ref.Campaigns[b.LinkID]
		if !ok {
			return false, HiddenCampaignMissing
		}
		if !c.Running(now) {
			return false, HiddenCampaignNotRunning
		}
	case LinkCategory:
		if _, ok := ref.Categories[b.LinkID]; !ok {
			return false, HiddenCategoryMissing
		}
	case LinkProduct:
		if p, ok := ref.Products[b.LinkID]; !ok || !p.Showable {
			return false, HiddenProductMissing
		}
	}
	return true, ""
}
