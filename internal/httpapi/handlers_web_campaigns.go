package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Chiến dịch giảm giá web của nhân viên (kế hoạch M5, Task 452): đọc promotion.read, ghi promotion.manage (bảng
// route). Chiến dịch thuộc người bán của shop (catalog.DefaultSeller, như các đường của catalog). Audit ghi ở đây,
// cùng tx, kèm lý do của lần bật, tạm dừng, xóa.

// ListWebCampaigns (GET /api/v1/web-campaigns).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListWebCampaigns(ctx context.Context, params gen.ListWebCampaignsParams) (gen.ListWebCampaignsRes, error) {
	after, err := couponCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	now := h.now()
	f := promotion.WebCampaignFilter{
		Q: params.Q.Value, Status: string(params.Status.Value), Now: now, After: after,
		Limit: int(params.Limit.Or(defaultDocLimit)),
	}
	var cs []promotion.WebCampaign
	var next *uuid.UUID
	if err := h.campaignRead(ctx, func(tx pgx.Tx, seller uuid.UUID) (err error) {
		cs, next, err = promotion.ListWebCampaigns(ctx, tx, seller, f)
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh sách chiến dịch: %w", err)
	}
	page := &gen.WebCampaignPage{Items: make([]gen.WebCampaign, 0, len(cs))}
	for _, c := range cs {
		page.Items = append(page.Items, *campaignOut(c, f.Now))
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}

// GetWebCampaign (GET /api/v1/web-campaigns/{campaignId}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetWebCampaign(ctx context.Context, params gen.GetWebCampaignParams) (gen.GetWebCampaignRes, error) {
	var c promotion.WebCampaign
	if err := h.campaignRead(ctx, func(tx pgx.Tx, seller uuid.UUID) (err error) {
		c, err = promotion.GetWebCampaign(ctx, tx, seller, params.CampaignId)
		return err //nolint:wrapcheck // sentinel của promotion
	}); err != nil {
		return nil, fmt.Errorf("đọc chiến dịch: %w", err)
	}
	return campaignOut(c, h.now()), nil
}

// ListCampaignTargets (GET /api/v1/web-campaign-targets).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCampaignTargets(ctx context.Context, params gen.ListCampaignTargetsParams) (gen.ListCampaignTargetsRes, error) {
	after, err := couponCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	var ts []promotion.CampaignTarget
	var next *uuid.UUID
	if err := h.campaignRead(ctx, func(tx pgx.Tx, seller uuid.UUID) (err error) {
		ts, next, err = promotion.ListCampaignTargets(ctx, tx, seller, string(params.Kind), params.Q.Value, after,
			int(params.Limit.Or(defaultDocLimit)))
		return err //nolint:wrapcheck // promotion đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc đích chiến dịch: %w", err)
	}
	page := &gen.CampaignTargetPage{Items: make([]gen.CampaignTarget, 0, len(ts))}
	for _, t := range ts {
		page.Items = append(page.Items, gen.CampaignTarget{ID: t.ID, Code: t.Code, Name: t.Name})
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}

// CreateWebCampaign (POST /api/v1/web-campaigns) → 201 nháp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateWebCampaign(ctx context.Context, req *gen.WebCampaignInput, _ gen.CreateWebCampaignParams) (gen.CreateWebCampaignRes, error) {
	in := campaignIn(req)
	out, err := h.campaignWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		c, err := w.CreateCampaign(ctx, tx, promotionActor(u), seller, in)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return campaignOut(c, h.now()), h.audit(ctx, tx, u, "promotion.web_campaign.create", "promotion.web_campaign",
			c.ID, nil, c.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCampaign), nil //nolint:forcetypeassert // fn trên trả *gen.WebCampaign
}

// SaveWebCampaign (PUT /api/v1/web-campaigns/{campaignId}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SaveWebCampaign(ctx context.Context, req *gen.SaveWebCampaignRequest, params gen.SaveWebCampaignParams) (gen.SaveWebCampaignRes, error) {
	in := campaignIn(&req.Campaign)
	out, err := h.campaignWrite(ctx, http.StatusOK, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		ch, err := w.SaveCampaign(ctx, tx, promotionActor(u), seller, params.CampaignId, int(req.Revision), in)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return campaignOut(*ch.After, h.now()), h.audit(ctx, tx, u, "promotion.web_campaign.save",
			"promotion.web_campaign", ch.After.ID, ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCampaign), nil //nolint:forcetypeassert // fn trên trả *gen.WebCampaign
}

// PreviewWebCampaign (POST /api/v1/web-campaigns/{campaignId}/preview): tính và ghi bản xem trước; không audit (không
// đổi chiến dịch).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PreviewWebCampaign(ctx context.Context, req *gen.WebCampaignRevision, params gen.PreviewWebCampaignParams) (gen.PreviewWebCampaignRes, error) {
	out, err := h.campaignWrite(ctx, http.StatusOK, func(tx pgx.Tx, w promotion.Writer, _ identity.AuthUser, seller uuid.UUID) (any, error) {
		p, err := w.PreviewCampaign(ctx, tx, seller, params.CampaignId, int(req.Revision))
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		g := &gen.WebCampaignPreview{
			Revision: int32(p.Revision), ModelCount: int32(p.Models), VariantCount: int32(p.Variants), //nolint:gosec // G115: số đếm của shop
			Samples: make([]gen.WebCampaignSample, 0, len(p.Samples)),
		}
		for _, s := range p.Samples {
			g.Samples = append(g.Samples, gen.WebCampaignSample{
				ProductId: s.ProductID, VariantId: s.VariantID, ProductName: s.ProductName,
				ListPrice: gen.Money(s.ListPrice), SalePrice: gen.Money(s.SalePrice),
			})
		}
		return g, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCampaignPreview), nil //nolint:forcetypeassert // fn trên trả *gen.WebCampaignPreview
}

// SetWebCampaignState (POST /api/v1/web-campaigns/{campaignId}/state): bật, tạm dừng; audit kèm lý do.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetWebCampaignState(ctx context.Context, req *gen.SetWebCampaignStateRequest, params gen.SetWebCampaignStateParams) (gen.SetWebCampaignStateRes, error) {
	var seen promotion.PreviewCounts
	if p, ok := req.Preview.Get(); ok {
		seen = promotion.PreviewCounts{Revision: int(p.Revision), Models: int(p.ModelCount), Variants: int(p.VariantCount)}
	}
	reason := strings.TrimSpace(req.Reason)
	out, err := h.campaignWrite(ctx, http.StatusOK, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		ch, err := w.SetCampaignState(ctx, tx, promotionActor(u), seller, params.CampaignId, int(req.Revision),
			string(req.State), reason, seen)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		after := ch.After.AuditFields()
		after["reason"] = reason
		return campaignOut(*ch.After, h.now()), h.audit(ctx, tx, u, "promotion.web_campaign.state",
			"promotion.web_campaign", ch.After.ID, ch.Before.AuditFields(), after)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCampaign), nil //nolint:forcetypeassert // fn trên trả *gen.WebCampaign
}

// DeleteWebCampaign (POST /api/v1/web-campaigns/{campaignId}/delete): xóa mềm; audit giữ bản trước và lý do.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DeleteWebCampaign(ctx context.Context, req *gen.DeleteWebCampaignRequest, params gen.DeleteWebCampaignParams) (gen.DeleteWebCampaignRes, error) {
	reason := strings.TrimSpace(req.Reason)
	out, err := h.campaignWrite(ctx, http.StatusOK, func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		before, err := w.DeleteCampaign(ctx, tx, promotionActor(u), seller, params.CampaignId, int(req.Revision), reason)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của promotion
		}
		return &gen.WebCampaignRevision{Revision: int32(before.Revision + 1)}, //nolint:gosec // G115: revision int4
			h.audit(ctx, tx, u, "promotion.web_campaign.delete", "promotion.web_campaign", before.ID,
				before.AuditFields(), map[string]any{"deleted": true, "reason": reason})
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCampaignRevision), nil //nolint:forcetypeassert // fn trên trả *gen.WebCampaignRevision
}

// campaignRead chạy fn trong tx đọc của tenant với người bán của shop.
func (h *handler) campaignRead(ctx context.Context, fn func(tx pgx.Tx, seller uuid.UUID) error) error {
	u, ok := authUser(ctx)
	if !ok {
		return errNoAuthUser
	}
	return h.readAsDefaultSeller(ctx, u, fn)
}

// campaignWrite chạy change trong refusableWrite (lỗi nghiệp vụ của promotion không lưu vào khóa chống trùng), với
// Writer có sổ sự kiện (promotion.web_campaign.changed) và người bán của shop.
func (h *handler) campaignWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w promotion.Writer, u identity.AuthUser, seller uuid.UUID) (any, error),
) (any, error) {
	return h.refusableWrite(ctx, status, isPromotionRefusal, "ghi chiến dịch", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return change(tx, promotion.Writer{Realtime: h.d.Realtime, Events: h.d.Events, Queue: queue.NewQueue()}, u, seller)
	})
}

func campaignIn(r *gen.WebCampaignInput) promotion.WebCampaignInput {
	return promotion.WebCampaignInput{
		Name: r.Name, Percent: int(r.Percent), Scope: string(r.Scope), TargetIDs: r.TargetIds,
		AllowCoupon: r.AllowCoupon.Or(false), Clearance: r.Clearance.Or(false), StartsAt: r.StartsAt, EndsAt: r.EndsAt,
	}
}

// campaignOut là chiến dịch của hợp đồng nhân viên; status tính ở now (đồng hồ của máy chủ, Deps.Now).
func campaignOut(c promotion.WebCampaign, now time.Time) *gen.WebCampaign {
	out := &gen.WebCampaign{
		ID: c.ID, Name: c.Name, Percent: int32(c.Percent), Scope: gen.WebCampaignScope(c.Scope), //nolint:gosec // G115: 1–99
		TargetIds: c.TargetIDs, AllowCoupon: c.AllowCoupon, Clearance: c.Clearance, StartsAt: c.StartsAt,
		EndsAt: c.EndsAt, State: gen.WebCampaignState(c.State),
		Status:   gen.WebCampaignStatus(promotion.CampaignStatus(c, now)),
		Revision: int32(c.Revision), LinkKey: c.LinkKey, Path: promotion.CampaignPath(c.ID, c.Name), //nolint:gosec // G115: revision int4
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if p := c.Preview; p != nil {
		out.Preview = gen.NewOptWebCampaignPreviewCounts(gen.WebCampaignPreviewCounts{
			Revision: int32(p.Revision), ModelCount: int32(p.Models), VariantCount: int32(p.Variants), //nolint:gosec // G115: int4 của bảng
		})
	}
	return out
}
