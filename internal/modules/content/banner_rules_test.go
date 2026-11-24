package content_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/files"
)

// ruleRefs là dữ liệu tham chiếu cố định cho test luật: một chiến dịch đang chạy (theo danh mục catA), một đã dừng,
// danh mục catA ⊃ catA1, catB; mẫu pA (catA1, hiện được), pB (catB), pNoPhoto (catA, không hiện được); ảnh ready,
// đang xử lý, bị từ chối, đã xóa, sai mục đích.
type ruleRefs struct {
	now                                                        time.Time
	running, paused, catA, catA1, catB, pA, pB, pNoPhoto       uuid.UUID
	imgReady, imgProcessing, imgRejected, imgDeleted, imgWrong uuid.UUID
	ref                                                        content.RefData
}

func newRuleRefs() ruleRefs {
	r := ruleRefs{now: time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)}
	for _, p := range []*uuid.UUID{
		&r.running, &r.paused, &r.catA, &r.catA1, &r.catB, &r.pA, &r.pB, &r.pNoPhoto,
		&r.imgReady, &r.imgProcessing, &r.imgRejected, &r.imgDeleted, &r.imgWrong,
	} {
		*p = uuid.New()
	}
	day := 24 * time.Hour
	r.ref = content.RefData{
		Campaigns: map[uuid.UUID]content.CampaignRef{
			r.running: {
				ID: r.running, Name: "Sale 11.11", Percent: 20, Scope: "categories", State: "enabled",
				StartsAt: r.now.Add(-day), EndsAt: r.now.Add(day), TargetIDs: []uuid.UUID{r.catA},
			},
			r.paused: {
				ID: r.paused, Name: "Tạm dừng", Percent: 10, Scope: "all", State: "paused",
				StartsAt: r.now.Add(-day), EndsAt: r.now.Add(day),
			},
		},
		Categories: map[uuid.UUID]content.CategoryRef{
			r.catA: {ID: r.catA, Name: "Áo nữ"}, r.catA1: {ID: r.catA1, ParentID: r.catA, Name: "Áo sơ mi"},
			r.catB: {ID: r.catB, Name: "Váy"},
		},
		Products: map[uuid.UUID]content.ProductRef{
			r.pA:       {ID: r.pA, CategoryID: r.catA1, Showable: true},
			r.pB:       {ID: r.pB, CategoryID: r.catB, Showable: true},
			r.pNoPhoto: {ID: r.pNoPhoto, CategoryID: r.catA},
		},
		Images: map[uuid.UUID]files.FileStatus{
			r.imgReady:      {Purpose: content.BannerPurposeKey, Status: files.StatusReady},
			r.imgProcessing: {Purpose: content.BannerPurposeKey, Status: files.StatusProcessing},
			r.imgRejected:   {Purpose: content.BannerPurposeKey, Status: files.StatusRejected},
			r.imgDeleted:    {Purpose: content.BannerPurposeKey, Status: files.StatusReady, Deleted: true},
			r.imgWrong:      {Purpose: "product_image", Status: files.StatusReady},
		},
		Pool: map[uuid.UUID]bool{},
	}
	return r
}

func (r ruleRefs) banner(kind string, mod func(*content.Banner)) content.Banner {
	b := content.Banner{ID: uuid.New(), Enabled: true, Kind: kind}
	switch kind {
	case content.KindCampaign:
		b.CampaignID = r.running
	case content.KindCategory:
		b.CategoryID = r.catA
	case content.KindImage:
		b.ImageDesktop, b.Alt, b.LinkKind = r.imgReady, "Flash sale 11.11", content.LinkOffers
	}
	if mod != nil {
		mod(&b)
	}
	return b
}

func at(t time.Time) *time.Time { return &t }

// Luật lưu: tối đa 8, mỗi loại thiếu trường bắt buộc, khung giờ ngược, chữ quá dài, ghim > 3, ghim ngoài nguồn, ảnh
// bị từ chối, ảnh không dùng được; mỗi ca một mã lỗi.
type validateCase struct {
	name string
	list []content.Banner
	want error
}

func TestValidateBanners(t *testing.T) {
	t.Parallel()
	r := newRuleRefs()
	for _, c := range append(append([]validateCase{validCase(r)}, shapeCases(r)...), referenceCases(r)...) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := content.ValidateBanners(content.NormalizeBanners(c.list), r.ref)
			if (c.want == nil) != (err == nil) || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("lỗi %v, muốn %v", err, c.want)
			}
		})
	}
}

// validCase: 8 banner đủ loại, chữ đúng giới hạn, ghim đúng nguồn, ảnh đang xử lý, chiến dịch đã xóa → lưu được.
func validCase(r ruleRefs) validateCase {
	return validateCase{"8 banner đủ loại", []content.Banner{
		r.banner(content.KindCampaign, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{r.pNoPhoto} }),
		r.banner(content.KindTrending, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{r.pA, r.pB, r.pNoPhoto} }),
		r.banner(content.KindNew, nil),
		r.banner(content.KindCategory, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{r.pA, r.pNoPhoto} }),
		r.banner(content.KindImage, nil),
		r.banner(content.KindImage, func(b *content.Banner) {
			b.ImageMobile, b.LinkKind, b.LinkID = r.imgProcessing, content.LinkProduct, r.pA
		}),
		r.banner(content.KindNew, func(b *content.Banner) {
			b.Kicker, b.Title, b.CTA = strings.Repeat("ữ", 40), strings.Repeat("ữ", 60), strings.Repeat("ữ", 30)
			b.StartsAt, b.EndsAt = at(r.now), at(r.now.Add(time.Minute))
		}),
		r.banner(content.KindCampaign, func(b *content.Banner) { b.CampaignID = uuid.New() }), // đã xóa: ẩn, vẫn lưu
	}, nil}
}

// shapeCases: số banner, mã, loại, màu, trường bắt buộc, khung giờ, độ dài chữ.
func shapeCases(r ruleRefs) []validateCase {
	nine := make([]content.Banner, 9)
	for i := range nine {
		nine[i] = r.banner(content.KindNew, nil)
	}
	dup := r.banner(content.KindNew, nil)
	return []validateCase{
		{"9 banner", nine, content.ErrTooManyBanners},
		{"trùng mã", []content.Banner{dup, dup}, content.ErrInvalidBanner},
		{"loại lạ", []content.Banner{r.banner("video", nil)}, content.ErrInvalidBanner},
		{"màu lạ", []content.Banner{r.banner(content.KindNew, func(b *content.Banner) { b.Theme = "blue" })}, content.ErrInvalidBanner},
		{
			"chiến dịch thiếu",
			[]content.Banner{r.banner(content.KindCampaign, func(b *content.Banner) { b.CampaignID = uuid.Nil })},
			content.ErrCampaignRequired,
		},
		{
			"danh mục thiếu",
			[]content.Banner{r.banner(content.KindCategory, func(b *content.Banner) { b.CategoryID = uuid.Nil })},
			content.ErrCategoryRequired,
		},
		{
			"ảnh thiếu ảnh máy tính",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.ImageDesktop = uuid.Nil })},
			content.ErrImageRequired,
		},
		{
			"ảnh thiếu mô tả",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.Alt = "   " })},
			content.ErrImageRequired,
		},
		{
			"ảnh thiếu đích",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.LinkKind = "" })},
			content.ErrImageRequired,
		},
		{
			"ảnh đích sản phẩm thiếu mã",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.LinkKind = content.LinkProduct })},
			content.ErrImageRequired,
		},
		{
			"ảnh đích lạ",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.LinkKind = "home" })},
			content.ErrInvalidBanner,
		},
		{"khung giờ ngược", []content.Banner{r.banner(content.KindNew, func(b *content.Banner) {
			b.StartsAt, b.EndsAt = at(r.now), at(r.now.Add(-time.Minute))
		})}, content.ErrWindowInvalid},
		{"khung giờ bằng nhau", []content.Banner{r.banner(content.KindNew, func(b *content.Banner) {
			b.StartsAt, b.EndsAt = at(r.now), at(r.now)
		})}, content.ErrWindowInvalid},
		{
			"tiêu đề 61",
			[]content.Banner{r.banner(content.KindNew, func(b *content.Banner) { b.Title = strings.Repeat("a", 61) })},
			content.ErrInvalidBanner,
		},
		{
			"kicker 41",
			[]content.Banner{r.banner(content.KindNew, func(b *content.Banner) { b.Kicker = strings.Repeat("a", 41) })},
			content.ErrInvalidBanner,
		},
		{
			"nút 31",
			[]content.Banner{r.banner(content.KindNew, func(b *content.Banner) { b.CTA = strings.Repeat("a", 31) })},
			content.ErrInvalidBanner,
		},
		{
			"mô tả ảnh 121",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.Alt = strings.Repeat("a", 121) })},
			content.ErrInvalidBanner,
		},
	}
}

// referenceCases: ghim (số, trùng, nguồn) và ảnh (từ chối, xóa, sai mục đích, không có).
func referenceCases(r ruleRefs) []validateCase {
	return []validateCase{
		{"ghim 4", []content.Banner{r.banner(content.KindNew, func(b *content.Banner) {
			b.ProductIDs = []uuid.UUID{r.pA, r.pB, r.pNoPhoto, uuid.New()}
		})}, content.ErrTooManyPins},
		{
			"ghim trùng",
			[]content.Banner{r.banner(content.KindNew, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{r.pA, r.pA} })},
			content.ErrInvalidBanner,
		},
		{
			"ghim mẫu không bán",
			[]content.Banner{r.banner(content.KindNew, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{uuid.New()} })},
			content.ErrPinNotInSource,
		},
		{
			"ghim ngoài chiến dịch",
			[]content.Banner{r.banner(content.KindCampaign, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{r.pB} })},
			content.ErrPinNotInSource,
		},
		{
			"ghim ngoài danh mục",
			[]content.Banner{r.banner(content.KindCategory, func(b *content.Banner) { b.ProductIDs = []uuid.UUID{r.pB} })},
			content.ErrPinNotInSource,
		},
		{
			"ảnh bị từ chối",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.ImageDesktop = r.imgRejected })},
			content.ErrFileRejected,
		},
		{
			"ảnh điện thoại bị từ chối",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.ImageMobile = r.imgRejected })},
			content.ErrFileRejected,
		},
		{
			"ảnh đã xóa",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.ImageDesktop = r.imgDeleted })},
			content.ErrImageInvalid,
		},
		{
			"ảnh sai mục đích",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.ImageDesktop = r.imgWrong })},
			content.ErrImageInvalid,
		},
		{
			"ảnh không có",
			[]content.Banner{r.banner(content.KindImage, func(b *content.Banner) { b.ImageDesktop = uuid.New() })},
			content.ErrImageInvalid,
		},
	}
}

// Chuẩn hóa như admin cũ (bannerPayloadOne): cắt khoảng trắng, màu rỗng = auto, bỏ trường không thuộc loại (ảnh không
// có chữ, ghim, màu; loại khác không có ảnh, đích; chỉ campaign có chiến dịch, chỉ category có danh mục).
func TestNormalizeBanners(t *testing.T) {
	t.Parallel()
	r := newRuleRefs()
	in := []content.Banner{
		r.banner(content.KindImage, func(b *content.Banner) {
			b.Kicker, b.Title, b.CTA, b.Theme, b.ProductIDs = "x", "y", "z", "red", []uuid.UUID{r.pA}
			b.CampaignID, b.CategoryID, b.Alt = r.running, r.catA, "  Flash sale  "
			b.LinkKind, b.LinkID = content.LinkOffers, r.pA
		}),
		r.banner(content.KindNew, func(b *content.Banner) {
			b.Kicker, b.Theme, b.ImageDesktop, b.Alt, b.LinkKind, b.LinkID = "  Mới  ", "", r.imgReady, "a", content.LinkProduct, r.pA
			b.CampaignID, b.CategoryID = r.running, r.catA
		}),
	}
	out := content.NormalizeBanners(in)
	img, nw := out[0], out[1]
	if img.Kicker != "" || img.Title != "" || img.CTA != "" || img.Theme != content.ThemeAuto || len(img.ProductIDs) != 0 ||
		img.CampaignID != uuid.Nil || img.CategoryID != uuid.Nil || img.Alt != "Flash sale" || img.LinkID != uuid.Nil {
		t.Fatalf("banner ảnh: %+v", img)
	}
	if nw.Kicker != "Mới" || nw.Theme != content.ThemeAuto || nw.ImageDesktop != uuid.Nil || nw.Alt != "" ||
		nw.LinkKind != "" || nw.LinkID != uuid.Nil || nw.CampaignID != uuid.Nil || nw.CategoryID != uuid.Nil {
		t.Fatalf("banner hàng mới: %+v", nw)
	}
	if in[0].Kicker != "x" {
		t.Fatal("NormalizeBanners không được sửa danh sách gốc")
	}
}

// Mỗi lý do ẩn của website (9 lý do, server/banners/rules.ts) một ca, và các ca hiện.
type visibilityCase struct {
	name   string
	b      content.Banner
	shown  bool
	reason string
}

func TestVisibility(t *testing.T) {
	t.Parallel()
	r := newRuleRefs()
	cases := visibilityCases(r)
	reasons := map[string]bool{}
	for _, c := range cases {
		shown, reason := content.Visibility(c.b, r.ref, r.now)
		if shown != c.shown || reason != c.reason {
			t.Errorf("%s: (%v, %q), muốn (%v, %q)", c.name, shown, reason, c.shown, c.reason)
		}
		if reason != "" {
			reasons[reason] = true
		}
	}
	if len(reasons) != len(content.HiddenReasons()) {
		t.Fatalf("test phủ %d lý do ẩn, có %d", len(reasons), len(content.HiddenReasons()))
	}
}

// visibilityCases: mỗi lý do ẩn ít nhất một ca và các ca hiện; ghi Pool của banner vào r.ref.
func visibilityCases(r ruleRefs) []visibilityCase {
	withPool := func(b content.Banner, has bool) content.Banner {
		r.ref.Pool[b.ID] = has
		return b
	}
	return []visibilityCase{
		{"chiến dịch đang chạy", withPool(r.banner(content.KindCampaign, nil), true), true, ""},
		{"tắt", withPool(r.banner(content.KindNew, func(b *content.Banner) { b.Enabled = false }), true), false, content.HiddenDisabled},
		{
			"chưa tới giờ", withPool(r.banner(content.KindNew, func(b *content.Banner) { b.StartsAt = at(r.now.Add(time.Second)) }), true),
			false, content.HiddenScheduled,
		},
		{"đúng giờ bắt đầu", withPool(r.banner(content.KindNew, func(b *content.Banner) { b.StartsAt = at(r.now) }), true), true, ""},
		{
			"hết giờ (đúng giờ kết thúc)", withPool(r.banner(content.KindNew, func(b *content.Banner) { b.EndsAt = at(r.now) }), true),
			false, content.HiddenExpired,
		},
		{
			"chiến dịch đã xóa", withPool(r.banner(content.KindCampaign, func(b *content.Banner) { b.CampaignID = uuid.New() }), true),
			false, content.HiddenCampaignMissing,
		},
		{
			"chiến dịch tạm dừng", withPool(r.banner(content.KindCampaign, func(b *content.Banner) { b.CampaignID = r.paused }), true),
			false, content.HiddenCampaignNotRunning,
		},
		{"chiến dịch tạm dừng, banner tắt", withPool(r.banner(content.KindCampaign, func(b *content.Banner) {
			b.CampaignID, b.Enabled = r.paused, false
		}), true), false, content.HiddenDisabled},
		{
			"danh mục không còn", withPool(r.banner(content.KindCategory, func(b *content.Banner) { b.CategoryID = uuid.New() }), true),
			false, content.HiddenCategoryMissing,
		},
		{"không có mẫu", withPool(r.banner(content.KindTrending, nil), false), false, content.HiddenNoProducts},
		{"ảnh tốt", r.banner(content.KindImage, nil), true, ""},
		{
			"ảnh đang xử lý", r.banner(content.KindImage, func(b *content.Banner) { b.ImageDesktop = r.imgProcessing }), false,
			content.HiddenImageMissing,
		},
		{
			"ảnh điện thoại đã xóa", r.banner(content.KindImage, func(b *content.Banner) { b.ImageMobile = r.imgDeleted }), false,
			content.HiddenImageMissing,
		},
		{"ảnh tắt", r.banner(content.KindImage, func(b *content.Banner) { b.Enabled = false }), false, content.HiddenDisabled},
		{
			"ảnh chưa tới giờ", r.banner(content.KindImage, func(b *content.Banner) { b.StartsAt = at(r.now.Add(time.Hour)) }), false,
			content.HiddenScheduled,
		},
		{"ảnh tới chiến dịch đã xóa", r.banner(content.KindImage, func(b *content.Banner) {
			b.LinkKind, b.LinkID = content.LinkCampaign, uuid.New()
		}), false, content.HiddenCampaignMissing},
		{"ảnh tới chiến dịch tạm dừng", r.banner(content.KindImage, func(b *content.Banner) {
			b.LinkKind, b.LinkID = content.LinkCampaign, r.paused
		}), false, content.HiddenCampaignNotRunning},
		{"ảnh tới chiến dịch chạy", r.banner(content.KindImage, func(b *content.Banner) {
			b.LinkKind, b.LinkID = content.LinkCampaign, r.running
		}), true, ""},
		{"ảnh tới danh mục không còn", r.banner(content.KindImage, func(b *content.Banner) {
			b.LinkKind, b.LinkID = content.LinkCategory, uuid.New()
		}), false, content.HiddenCategoryMissing},
		{"ảnh tới mẫu không hiện được", r.banner(content.KindImage, func(b *content.Banner) {
			b.LinkKind, b.LinkID = content.LinkProduct, r.pNoPhoto
		}), false, content.HiddenProductMissing},
		{"ảnh tới mẫu", r.banner(content.KindImage, func(b *content.Banner) {
			b.LinkKind, b.LinkID = content.LinkProduct, r.pA
		}), true, ""},
	}
}

// Chiến dịch "đang chạy": đã bật và startsAt ≤ now < endsAt (như promotion.Running).
func TestCampaignRunning(t *testing.T) {
	t.Parallel()
	now := time.Now()
	c := content.CampaignRef{State: "enabled", StartsAt: now, EndsAt: now.Add(time.Hour)}
	if !c.Running(now) || c.Running(now.Add(time.Hour)) || c.Running(now.Add(-time.Nanosecond)) {
		t.Fatal("khung giờ chạy sai")
	}
	c.State = "draft"
	if c.Running(now) {
		t.Fatal("nháp không chạy")
	}
}
