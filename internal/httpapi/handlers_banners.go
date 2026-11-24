package httpapi

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Banner trang chủ của nhân viên (kế hoạch M5, Task 453): đọc, xem trước content.read, lưu content.manage (bảng route).
// Audit content.banners.save ghi ở đây, cùng tx với lần lưu (QĐ38).

// GetBanners (GET /api/v1/content/banners).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetBanners(ctx context.Context) (gen.GetBannersRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var out *gen.HomeBannerList
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		set, err := content.GetBanners(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // content đã bọc
		}
		out, err = h.bannerList(ctx, tx, set)
		return err
	}, db.TxAPI)
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của fn trả nguyên
	}
	return out, nil
}

// PreviewBanners (POST /api/v1/content/banners/preview): kiểm như lưu rồi tính hiện/ẩn lúc này; không ghi gì.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PreviewBanners(ctx context.Context, req *gen.PreviewBannersRequest) (gen.PreviewBannersRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	bs := content.NormalizeBanners(bannersIn(req.Banners))
	now := h.now()
	var out *gen.BannerPreview
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		ref, err := web.LoadBannerRefs(ctx, tx, bs, now)
		if err != nil {
			return err //nolint:wrapcheck // web, content đã bọc
		}
		if err := content.ValidateBanners(bs, ref); err != nil {
			return err //nolint:wrapcheck // sentinel của content
		}
		out = &gen.BannerPreview{Statuses: statusesOut(content.Statuses(bs, ref, now))}
		return nil
	}, db.TxAPI)
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của fn trả nguyên
	}
	return out, nil
}

// SaveBanners (PUT /api/v1/content/banners): thay cả danh sách khi revision khớp; lỗi luật không lưu vào khóa chống
// trùng (gửi lại sau khi sửa).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SaveBanners(ctx context.Context, req *gen.SaveBannersRequest, _ gen.SaveBannersParams) (gen.SaveBannersRes, error) {
	list := bannersIn(req.Banners)
	out, err := h.refusableWrite(ctx, http.StatusOK, isContentRefusal, "lưu banner", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		w := content.Writer{Realtime: h.d.Realtime, Events: h.d.Events, Queue: queue.NewQueue()}
		ch, err := w.SaveBanners(ctx, tx, list, int(req.Revision), content.Actor{TenantID: u.TenantID, UserID: u.UserID}, h.now())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của content
		}
		if err := h.audit(ctx, tx, u, "content.banners.save", "content.home_banners", u.TenantID,
			ch.Before.AuditFields(), ch.After.AuditFields()); err != nil {
			return nil, err
		}
		return h.bannerList(ctx, tx, ch.After)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.HomeBannerList), nil //nolint:forcetypeassert // fn trên trả *gen.HomeBannerList
}

// WebHomeBanners (GET /api/v1/web/content/banners, Task 453) → 200 kèm ETag, hoặc 304.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebHomeBanners(ctx context.Context, params gen.WebHomeBannersParams) (gen.WebHomeBannersRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.HomeBanners(ctx, u.TenantID, params) //nolint:wrapcheck // lỗi đổi mã ở errors.go
}

func isContentRefusal(err error) bool {
	_, _, ok := contentError(err)
	return ok
}

// bannerList dựng câu trả lời của nhân viên: danh sách, hiện/ẩn lúc này, ảnh, chiến dịch, danh mục, mẫu được nhắc.
func (h *handler) bannerList(ctx context.Context, tx pgx.Tx, set content.BannerSet) (*gen.HomeBannerList, error) {
	now := h.now()
	ref, err := web.LoadBannerRefs(ctx, tx, set.Banners, now)
	if err != nil {
		return nil, err //nolint:wrapcheck // web, content đã bọc
	}
	var ids []uuid.UUID
	for _, b := range set.Banners {
		ids = append(ids, b.ImageDesktop, b.ImageMobile)
	}
	pics, err := content.BannerPictures(ctx, tx, ids, h.d.Config.FilesPublicBaseURL)
	if err != nil {
		return nil, err //nolint:wrapcheck // content đã bọc
	}
	out := &gen.HomeBannerList{
		Revision: int32(set.Revision), //nolint:gosec // G115: revision int4
		Banners:  make([]gen.HomeBanner, 0, len(set.Banners)), Statuses: statusesOut(content.Statuses(set.Banners, ref, now)),
		Images: []gen.HomeBannerImage{}, Campaigns: []gen.HomeBannerCampaign{}, Categories: []gen.HomeBannerCategory{},
		Products: []gen.HomeBannerProduct{},
	}
	if set.UpdatedBy != nil {
		out.UpdatedBy = gen.NewOptUUID(*set.UpdatedBy)
	}
	if set.UpdatedAt != nil {
		out.UpdatedAt = gen.NewOptDateTime(*set.UpdatedAt)
	}
	for _, b := range set.Banners {
		out.Banners = append(out.Banners, bannerOut(b))
		for _, id := range []uuid.UUID{b.ImageDesktop, b.ImageMobile} {
			if id != uuid.Nil {
				out.Images = append(out.Images, imageOut(id, ref.Images, pics))
			}
		}
	}
	for _, c := range sortedValues(ref.Campaigns, func(a, b content.CampaignRef) bool { return a.StartsAt.After(b.StartsAt) }) {
		out.Campaigns = append(out.Campaigns, gen.HomeBannerCampaign{
			ID: c.ID, Name: c.Name, Percent: int32(c.Percent), Scope: gen.HomeBannerCampaignScope(c.Scope), //nolint:gosec // G115: 1–99
			State: gen.HomeBannerCampaignState(c.State), StartsAt: c.StartsAt, EndsAt: c.EndsAt,
			TargetIds: append([]uuid.UUID{}, c.TargetIDs...), Running: c.Running(now),
		})
	}
	for _, c := range sortedValues(ref.Categories, func(a, b content.CategoryRef) bool { return a.Name < b.Name }) {
		g := gen.HomeBannerCategory{ID: c.ID, Name: c.Name}
		if c.ParentID != uuid.Nil {
			g.ParentId = gen.NewOptUUID(c.ParentID)
		}
		out.Categories = append(out.Categories, g)
	}
	for _, p := range sortedValues(ref.Products, func(a, b content.ProductRef) bool { return a.Code < b.Code }) {
		g := gen.HomeBannerProduct{ID: p.ID, Code: p.Code, Name: p.Name, Showable: p.Showable}
		if p.CategoryID != uuid.Nil {
			g.CategoryId = gen.NewOptUUID(p.CategoryID)
		}
		out.Products = append(out.Products, g)
	}
	return out, nil
}

func imageOut(id uuid.UUID, st map[uuid.UUID]files.FileStatus, pics map[uuid.UUID]content.Picture) gen.HomeBannerImage {
	img := gen.HomeBannerImage{FileId: id, Status: "missing"}
	if s, ok := st[id]; ok {
		img.Status, img.Deleted = s.Status, s.Deleted
	}
	if p, ok := pics[id]; ok {
		img.Desktop, img.DesktopSmall = gen.NewOptString(p.Large), gen.NewOptString(p.Small)
	}
	return img
}

func statusesOut(ss []content.Status) []gen.HomeBannerStatus {
	out := make([]gen.HomeBannerStatus, 0, len(ss))
	for _, s := range ss {
		g := gen.HomeBannerStatus{ID: s.ID, Shown: s.Shown}
		if s.Reason != "" {
			g.Reason = gen.NewOptHomeBannerHidden(gen.HomeBannerHidden(s.Reason))
		}
		out = append(out, g)
	}
	return out
}

func bannersIn(in []gen.HomeBanner) []content.Banner {
	out := make([]content.Banner, 0, len(in))
	for _, g := range in {
		b := content.Banner{
			ID: g.ID, Enabled: g.Enabled, Kind: string(g.Kind), CampaignID: g.CampaignId.Or(uuid.Nil),
			CategoryID: g.CategoryId.Or(uuid.Nil), Kicker: g.Kicker.Or(""), Title: g.Title.Or(""), CTA: g.Cta.Or(""),
			Theme: string(g.Theme.Or("")), ProductIDs: g.ProductIds, ImageDesktop: g.ImageDesktop.Or(uuid.Nil),
			ImageMobile: g.ImageMobile.Or(uuid.Nil), Alt: g.Alt.Or(""), LinkKind: string(g.LinkKind.Or("")),
			LinkID: g.LinkId.Or(uuid.Nil),
		}
		if v, ok := g.StartsAt.Get(); ok {
			b.StartsAt = timePtr(v)
		}
		if v, ok := g.EndsAt.Get(); ok {
			b.EndsAt = timePtr(v)
		}
		out = append(out, b)
	}
	return out
}

func timePtr(t time.Time) *time.Time { return &t }

func bannerOut(b content.Banner) gen.HomeBanner {
	g := gen.HomeBanner{
		ID: b.ID, Enabled: b.Enabled, Kind: gen.HomeBannerKind(b.Kind), Theme: gen.NewOptHomeBannerTheme(gen.HomeBannerTheme(b.Theme)),
		Kicker: gen.NewOptString(b.Kicker), Title: gen.NewOptString(b.Title), Cta: gen.NewOptString(b.CTA),
		ProductIds: append([]uuid.UUID{}, b.ProductIDs...),
	}
	opt := func(id uuid.UUID) gen.OptUUID {
		if id == uuid.Nil {
			return gen.OptUUID{}
		}
		return gen.NewOptUUID(id)
	}
	g.CampaignId, g.CategoryId, g.ImageDesktop, g.ImageMobile, g.LinkId = opt(b.CampaignID), opt(b.CategoryID),
		opt(b.ImageDesktop), opt(b.ImageMobile), opt(b.LinkID)
	if b.Alt != "" {
		g.Alt = gen.NewOptString(b.Alt)
	}
	if b.LinkKind != "" {
		g.LinkKind = gen.NewOptHomeBannerLinkKind(gen.HomeBannerLinkKind(b.LinkKind))
	}
	if b.StartsAt != nil {
		g.StartsAt = gen.NewOptDateTime(*b.StartsAt)
	}
	if b.EndsAt != nil {
		g.EndsAt = gen.NewOptDateTime(*b.EndsAt)
	}
	return g
}

// sortedValues trả giá trị của m sắp theo less (bằng nhau: theo khóa) để câu trả lời ổn định.
func sortedValues[V any](m map[uuid.UUID]V, less func(a, b V) bool) []V {
	keys := slices.SortedFunc(maps.Keys(m), func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	out := make([]V, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	slices.SortStableFunc(out, func(a, b V) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	})
	return out
}
