package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Chiến dịch giảm giá web cho website (kế hoạch M5, Task 452; QĐ33). Bộ đệm SpaceCampaigns 30 s: trạng thái
// (scheduled → active → ended) đổi theo giờ mà không có sự kiện, nên hạn sống ngắn; website quyết giá theo
// startsAt/endsAt, không theo status. Sự kiện promotion.web_campaign.changed làm mới cả vùng (webcache).
const (
	campaignTTL     = 30 * time.Second
	campaignListKey = "live"
	campaignKeyPref = "k_"
)

// Campaigns trả chiến dịch đã bật, chưa hết giờ (khóa "live" của SpaceCampaigns), có ETag; If-None-Match khớp → 304.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Campaigns(ctx context.Context, tenant uuid.UUID, p gen.WebCampaignsParams) (gen.WebCampaignsRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceCampaigns, campaignListKey, campaignTTL, func(ctx context.Context) ([]byte, error) {
		var out gen.WebCampaignList
		now := c.clock()
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			cs, err := promotion.WebCampaigns(ctx, tx, now)
			if err != nil {
				return err //nolint:wrapcheck // promotion đã bọc
			}
			out = gen.WebCampaignList{Items: make([]gen.WebCampaignPublic, 0, len(cs))}
			for _, wc := range cs {
				out.Items = append(out.Items, publicCampaign(wc, now))
			}
			return nil
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc chiến dịch cho website: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	tag := etag(raw)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebCampaignsNotModified{}, nil
	}
	var out gen.WebCampaignList
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc chiến dịch trong bộ đệm: %w", err)
	}
	return &gen.WebCampaignListHeaders{ETag: gen.NewOptString(tag), Response: out}, nil
}

// Campaign trả chiến dịch theo link key (trang /uu-dai, khóa "k_<link key>" của SpaceCampaigns); nháp, đã xóa →
// promotion.ErrNotFound (404).
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Campaign(ctx context.Context, tenant uuid.UUID, p gen.WebCampaignParams) (gen.WebCampaignRes, error) {
	key := strings.ToLower(p.LinkKey)
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceCampaigns, campaignKeyPref+key, campaignTTL, func(ctx context.Context) ([]byte, error) {
		var out gen.WebCampaignPublic
		now := c.clock()
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			wc, err := promotion.WebCampaignByLinkKey(ctx, tx, key)
			if err != nil {
				return err //nolint:wrapcheck // sentinel của promotion
			}
			out = publicCampaign(wc, now)
			return nil
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc chiến dịch theo liên kết: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; promotion.ErrNotFound → 404, ErrBusy → 503
	}
	var out gen.WebCampaignPublic
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc chiến dịch trong bộ đệm: %w", err)
	}
	return &out, nil
}

func publicCampaign(c promotion.WebCampaign, now time.Time) gen.WebCampaignPublic {
	return gen.WebCampaignPublic{
		ID: c.ID, SellerId: c.SellerID, Name: c.Name, Percent: int32(c.Percent), //nolint:gosec // G115: 1–99
		Scope: gen.WebCampaignScope(c.Scope), TargetIds: c.TargetIDs, AllowCoupon: c.AllowCoupon, Clearance: c.Clearance,
		StartsAt: c.StartsAt, EndsAt: c.EndsAt, Status: gen.WebCampaignStatus(promotion.CampaignStatus(c, now)),
		Revision: int32(c.Revision), Slug: promotion.CampaignSlug(c.Name), LinkKey: c.LinkKey, //nolint:gosec // G115: int4
	}
}
