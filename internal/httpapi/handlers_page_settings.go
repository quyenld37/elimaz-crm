package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng cài đặt hộp chat theo Page (kế hoạch M4, Task 160): đọc cần conversation.read trên Page, lưu cần
// conversation.settings trên Page (module kiểm; không có → 404).

// GetPageSettings (GET /api/v1/channels/{id}/page-settings).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetPageSettings(ctx context.Context, params gen.GetPageSettingsParams) (gen.GetPageSettingsRes, error) {
	var s conversation.PageSettings
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		s, err = conversation.GetPageSettings(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc cài đặt Page: %w", err)
	}
	return pageSettingsOut(s), nil
}

// UpdatePageSettings (PUT /api/v1/channels/{id}/page-settings).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdatePageSettings(ctx context.Context, req *gen.UpdatePageSettingsRequest,
	params gen.UpdatePageSettingsParams,
) (gen.UpdatePageSettingsRes, error) {
	in := conversation.PageSettingsInput{
		RoundRobinEnabled: req.RoundRobinEnabled, RoundRobinUserIDs: req.RoundRobinUserIds,
		AskAddressTemplateID: optNilUUID(req.AskAddressTemplateId), InboxTrafficSourceID: optNilUUID(req.InboxTrafficSourceId),
		CommentTrafficSourceID: optNilUUID(req.CommentTrafficSourceId), CommentPollMode: string(req.CommentPollMode),
		AutomationMaxAgeMinutes: req.AutomationMaxAgeMinutes, LiveSlots: make([]conversation.LiveSlot, 0, len(req.LiveSlots)),
	}
	for _, l := range req.LiveSlots {
		in.LiveSlots = append(in.LiveSlots, conversation.LiveSlot{From: l.From, To: l.To, TrafficSourceID: l.TrafficSourceId})
	}
	out, err := h.conversationChange(ctx, http.StatusOK, "lưu cài đặt Page", func(tx pgx.Tx, w conversation.Writer, u identity.AuthUser) (any, error) {
		s, err := w.UpdatePageSettings(ctx, tx, u, params.ID, in, req.ExpectedVersion)
		if err != nil {
			return nil, err //nolint:wrapcheck // conversation đã bọc
		}
		return pageSettingsOut(s), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.PageSettings), nil //nolint:forcetypeassert // fn trên trả *gen.PageSettings
}

func optNilUUID(v gen.OptNilUUID) *uuid.UUID {
	if id, ok := v.Get(); ok && !v.Null {
		return &id
	}
	return nil
}

func optUUIDOut(id *uuid.UUID) gen.OptUUID {
	if id == nil {
		return gen.OptUUID{}
	}
	return gen.NewOptUUID(*id)
}

func pageSettingsOut(s conversation.PageSettings) *gen.PageSettings {
	o := &gen.PageSettings{
		ChannelAccountId: s.PageID, RoundRobinEnabled: s.RoundRobinEnabled, RoundRobinUserIds: s.RoundRobinUserIDs,
		AskAddressTemplateId: optUUIDOut(s.AskAddressTemplateID), InboxTrafficSourceId: optUUIDOut(s.InboxTrafficSourceID),
		CommentTrafficSourceId: optUUIDOut(s.CommentTrafficSourceID), CommentPollMode: gen.CommentPollMode(s.CommentPollMode),
		AutomationMaxAgeMinutes: s.AutomationMaxAgeMinutes, Version: s.Version,
		LiveSlots: make([]gen.LiveSlot, 0, len(s.LiveSlots)),
	}
	for _, l := range s.LiveSlots {
		o.LiveSlots = append(o.LiveSlots, gen.LiveSlot{From: l.From, To: l.To, TrafficSourceId: l.TrafficSourceID})
	}
	return o
}
