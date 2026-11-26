package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Danh sách "đã chốt, thiếu địa chỉ" và nhắc một chạm của phiên live (Task 173; LV04, LV05, LV06). Quyền theo Page kiểm
// ở module (không có → 404). Hoàn tất đơn dùng updateOrder, confirmOrder của M2 (không route mới).

// ListPendingDrafts (GET /api/v1/live-sessions/{id}/pending-drafts, conversation.read trên Page): đơn còn nháp có
// ghi nhận trong phiên, số đơn đã xác nhận, đã hủy vì quá hạn nháp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListPendingDrafts(ctx context.Context, params gen.ListPendingDraftsParams) (gen.ListPendingDraftsRes, error) {
	env := conversation.SendEnv{Registry: h.d.Channels}
	var l conversation.PendingDraftList
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		l, err = conversation.ListPendingDrafts(ctx, tx, env, u, params.ID, h.now().UTC())
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc đơn nháp của phiên live: %w", err)
	}
	out := &gen.PendingDraftList{
		Items: make([]gen.PendingDraft, 0, len(l.Items)), ExpiredCount: int32(l.ExpiredCount), //nolint:gosec // tối đa 500 đơn mỗi trang, đếm theo phiên
		ConfirmedCount: int32(l.ConfirmedCount), //nolint:gosec // như trên
	}
	for _, r := range l.Items {
		d := gen.PendingDraft{
			OrderId: r.OrderID, OrderCode: r.OrderCode, Customer: gen.ChatCustomerRef{ID: r.CustomerID, Name: r.CustomerName},
			Items: make([]gen.LiveDraftItem, 0, len(r.Items)), GrandTotal: gen.Money(r.GrandTotal),
			ClaimedBy: gen.Actor{ID: r.ClaimedBy, Name: r.ClaimedByName}, ClaimedAt: r.ClaimedAt,
			Ask:                     gen.AskAddressResult{Status: gen.AskAddressResultStatus(r.Ask.Status)},
			CustomerRepliedAfterAsk: r.CustomerRepliedAfterAsk, CanRemind: gen.RemindAllowed{Allowed: r.CanRemind.Allowed},
		}
		if r.ExpiresAt != nil {
			d.ExpiresAt = gen.NewOptDateTime(*r.ExpiresAt)
		}
		if r.Ask.Reason != "" {
			d.Ask.Reason = gen.NewOptString(r.Ask.Reason)
		}
		if r.CanRemind.Reason != "" {
			d.CanRemind.Reason = gen.NewOptString(r.CanRemind.Reason)
		}
		for _, it := range r.Items {
			d.Items = append(d.Items, gen.LiveDraftItem{VariantId: it.VariantID, Name: it.Name, Quantity: it.Qty})
		}
		out.Items = append(out.Items, d)
	}
	return out, nil
}

// RemindPendingDrafts (POST /api/v1/live-sessions/{id}/pending-drafts/remind, live.operate trên Page): nhắc khách còn
// đơn nháp gửi địa chỉ, mỗi đơn một kết quả và một lệnh gửi riêng → 200. Phiên, mẫu không có → 404 (không lưu theo
// khóa).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RemindPendingDrafts(ctx context.Context, req *gen.RemindPendingDraftsRequest,
	params gen.RemindPendingDraftsParams,
) (gen.RemindPendingDraftsRes, error) {
	env := conversation.SendEnv{Registry: h.d.Channels, Queue: queue.NewQueue(), Bus: h.d.Realtime}
	in := conversation.RemindInput{OrderIDs: req.OrderIds, TemplateID: req.TemplateId}
	out, err := h.refusableWrite(ctx, http.StatusOK, isConversationRefusal, "nhắc khách gửi địa chỉ", func(tx pgx.Tx,
		u identity.AuthUser,
	) (any, error) {
		rs, err := conversation.RemindPendingDrafts(ctx, tx, env, u, params.ID, in, h.now().UTC())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của conversation; đổi sang HTTP ở errors*.go
		}
		res := &gen.RemindPendingDraftsResult{Results: make([]gen.RemindOutcome, 0, len(rs))}
		for _, r := range rs {
			o := gen.RemindOutcome{OrderId: r.OrderID, Outcome: gen.RemindOutcomeOutcome(r.Outcome)}
			if r.Reason != "" {
				o.Reason = gen.NewOptString(r.Reason)
			}
			res.Results = append(res.Results, o)
		}
		return res, nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.RemindPendingDraftsResult), nil //nolint:forcetypeassert // change trên trả *gen.RemindPendingDraftsResult
}
