package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
)

// Banner trang chủ cho website (kế hoạch M5, Task 453). Bộ đệm SpaceBanners 60 s: hiện/ẩn đổi theo giờ (khung giờ của
// banner, chiến dịch), theo mẫu, ảnh mà không có sự kiện riêng; content.banners.changed và
// promotion.web_campaign.changed làm mới sớm hơn (webcache).
const (
	bannersTTL = 60 * time.Second
	bannersKey = "live"
)

// HomeBanners trả banner đang hiện theo thứ tự (khóa "live" của SpaceBanners), có ETag; If-None-Match khớp → 304.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) HomeBanners(ctx context.Context, tenant uuid.UUID, p gen.WebHomeBannersParams) (gen.WebHomeBannersRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceBanners, bannersKey, bannersTTL, func(ctx context.Context) ([]byte, error) {
		var out gen.WebHomeBannerList
		now := c.clock()
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			var err error
			out, err = c.loadHomeBanners(ctx, tx, now)
			return err
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc banner cho website: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	tag := etag(raw)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebHomeBannersNotModified{}, nil
	}
	var out gen.WebHomeBannerList
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc banner trong bộ đệm: %w", err)
	}
	return &gen.WebHomeBannerListHeaders{ETag: gen.NewOptString(tag), Response: out}, nil
}

// loadHomeBanners: banner hiện ở now (content.Visibility) kèm đường dẫn ảnh; gán lại kết quả mỗi lần (RetryTx).
func (c *Catalog) loadHomeBanners(ctx context.Context, tx pgx.Tx, now time.Time) (gen.WebHomeBannerList, error) {
	set, err := content.GetBanners(ctx, tx)
	if err != nil {
		return gen.WebHomeBannerList{}, err //nolint:wrapcheck // content đã bọc
	}
	ref, err := LoadBannerRefs(ctx, tx, set.Banners, now)
	if err != nil {
		return gen.WebHomeBannerList{}, err
	}
	var ids []uuid.UUID
	for _, b := range set.Banners {
		ids = append(ids, b.ImageDesktop, b.ImageMobile)
	}
	pics, err := content.BannerPictures(ctx, tx, ids, c.view.BaseURL)
	if err != nil {
		return gen.WebHomeBannerList{}, err //nolint:wrapcheck // content đã bọc
	}
	out := gen.WebHomeBannerList{Items: []gen.WebHomeBanner{}}
	for _, b := range set.Banners {
		if shown, _ := content.Visibility(b, ref, now); !shown {
			continue
		}
		item, ok := webBanner(b, pics)
		if ok {
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
}

// LoadBannerRefs là content.LoadRefData kèm danh sách mẫu bán trong 30 ngày (chỉ đọc khi có banner bán chạy) — dùng
// chung cho website và màn nhân viên (content không đọc view của order).
func LoadBannerRefs(ctx context.Context, tx pgx.Tx, bs []content.Banner, now time.Time) (content.RefData, error) {
	var sold []uuid.UUID
	if content.NeedsSold30(bs) {
		rows, err := webcatalog.Sold30(ctx, tx, now)
		if err != nil {
			return content.RefData{}, err //nolint:wrapcheck // webcatalog đã bọc
		}
		seen := map[uuid.UUID]bool{}
		for _, r := range rows {
			if r.Sold > 0 && !seen[r.ProductID] {
				seen[r.ProductID] = true
				sold = append(sold, r.ProductID)
			}
		}
	}
	ref, err := content.LoadRefData(ctx, tx, bs, sold)
	if err != nil {
		return content.RefData{}, err //nolint:wrapcheck // content đã bọc
	}
	return ref, nil
}

// webBanner dựng banner của hợp đồng website; banner ảnh mà ảnh chưa có đường dẫn (thiếu biến thể) → ok = false.
func webBanner(b content.Banner, pics map[uuid.UUID]content.Picture) (gen.WebHomeBanner, bool) {
	out := gen.WebHomeBanner{
		ID: b.ID, Kind: gen.HomeBannerKind(b.Kind), Kicker: b.Kicker, Title: b.Title, Cta: b.CTA,
		Theme: gen.HomeBannerTheme(b.Theme), ProductIds: append([]uuid.UUID{}, b.ProductIDs...),
	}
	if b.CampaignID != uuid.Nil {
		out.CampaignId = gen.NewOptUUID(b.CampaignID)
	}
	if b.CategoryID != uuid.Nil {
		out.CategoryId = gen.NewOptUUID(b.CategoryID)
	}
	if b.StartsAt != nil {
		out.StartsAt = gen.NewOptDateTime(*b.StartsAt)
	}
	if b.EndsAt != nil {
		out.EndsAt = gen.NewOptDateTime(*b.EndsAt)
	}
	if b.Kind != content.KindImage {
		return out, true
	}
	desk, ok := pics[b.ImageDesktop]
	if !ok {
		return out, false
	}
	mobile := desk.Small
	if b.ImageMobile != uuid.Nil {
		m, ok := pics[b.ImageMobile]
		if !ok {
			return out, false
		}
		mobile = m.Small
	}
	out.Picture = gen.NewOptWebHomeBannerPicture(gen.WebHomeBannerPicture{
		Desktop: desk.Large, DesktopSmall: desk.Small, Mobile: mobile, Alt: b.Alt,
	})
	link := gen.WebHomeBannerLink{Kind: gen.HomeBannerLinkKind(b.LinkKind)}
	if b.LinkID != uuid.Nil {
		link.ID = gen.NewOptUUID(b.LinkID)
	}
	out.Link = gen.NewOptWebHomeBannerLink(link)
	return out, true
}
