package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Chốt đơn từ comment live (Task 172; LV03, LV07, LV08, LV09). Cả ba route kiểm quyền theo Page ở module (không có →
// 404); route chốt cần thêm order.create ở kho chính (mode new) hay order.update (add_to_draft), thiếu → 403.

// permOrderUpdate là quyền sửa đơn (thêm vào đơn nháp đang có).
const permOrderUpdate = identity.Permission("order.update")

// ClaimLiveComment (POST /api/v1/live-comments/{id}/claim, live.operate trên Page): một chạm tạo đơn nháp có giữ hàng
// (hay thêm vào nháp đang có), ghi nhận một comment một đơn, xin địa chỉ → 201. Từ chối nghiệp vụ (đã chốt, khách có
// nháp, hết hàng, khác người bán, version cũ…) lùi cả transaction và không lưu theo khóa. Audit order.create /
// order.update như đường tạo, sửa đơn.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ClaimLiveComment(ctx context.Context, req *gen.ClaimLiveCommentRequest, params gen.ClaimLiveCommentParams,
) (gen.ClaimLiveCommentRes, error) {
	in := claimInput(req)
	env := conversation.SendEnv{Registry: h.d.Channels, Queue: queue.NewQueue(), Bus: h.d.Realtime}
	w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusCreated, isConversationRefusal, "chốt comment live", func(tx pgx.Tx,
		u identity.AuthUser,
	) (any, error) {
		if err := claimAllowed(ctx, tx, u, in.Mode); err != nil {
			return nil, err
		}
		now := h.now().UTC()
		res, err := conversation.ClaimLiveComment(ctx, tx, env, w, orderActor(u), u, params.ID, in, now)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của conversation, order; đổi sang HTTP ở errors*.go
		}
		if err := claimAudit(ctx, tx, u, res, now); err != nil {
			return nil, err
		}
		return claimOut(res), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ClaimLiveCommentResult), nil //nolint:forcetypeassert // change trên trả *gen.ClaimLiveCommentResult
}

// claimAllowed: mode new cần order.create ở kho chính (orderWarehouseAt, như tạo đơn); add_to_draft cần order.update.
func claimAllowed(ctx context.Context, tx pgx.Tx, u identity.AuthUser, mode string) error {
	if mode == conversation.LiveClaimAddToDraft {
		if !identity.Can(u, permOrderUpdate, nil) {
			return errForbiddenTarget
		}
		return nil
	}
	_, err := orderWarehouseAt(ctx, tx, u, uuid.Nil)
	return err
}

// claimAudit ghi audit đơn như đường tạo đơn (order.create) hay sửa đơn (order.update, chỉ khi có trường đổi); đơn do
// khóa nghiệp vụ trả lại không ghi.
func claimAudit(ctx context.Context, tx pgx.Tx, u identity.AuthUser, res conversation.LiveClaimResult, now time.Time) error {
	e := identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), EntityType: "order",
		EntityID: res.Order.ID.String(), RequestID: RequestID(ctx), At: now,
	}
	switch {
	case res.Created && !res.Replayed:
		e.Action, e.After = "order.create", res.Order.AuditFields()
	case !res.Created && len(res.Fields) > 0:
		after := res.Order.AuditFields()
		after["fields"] = res.Fields
		e.Action, e.Before, e.After = "order.update", res.Before.AuditFields(), after
	default:
		return nil
	}
	if err := identity.RecordAudit(ctx, tx, e); err != nil {
		return fmt.Errorf("ghi audit %s: %w", e.Action, err)
	}
	return nil
}

func claimInput(req *gen.ClaimLiveCommentRequest) conversation.LiveClaimInput {
	in := conversation.LiveClaimInput{
		Mode: string(req.Mode), OrderID: optUUID(req.OrderId), ExpectedVersion: req.ExpectedVersion.Or(0),
		ForceNew: req.ForceNew.Or(false), AskAddress: req.AskAddress.Or(true), TemplateID: optUUID(req.TemplateId),
		Items: make([]conversation.LiveClaimItem, 0, len(req.Items)),
	}
	for _, it := range req.Items {
		in.Items = append(in.Items, conversation.LiveClaimItem{VariantID: it.VariantId, Qty: it.Qty})
	}
	return in
}

func claimOut(res conversation.LiveClaimResult) *gen.ClaimLiveCommentResult {
	out := &gen.ClaimLiveCommentResult{
		OrderId: res.Order.ID, OrderCode: res.Order.Code, Claim: claimRefOut(res.Claim),
		Ask: gen.AskAddressResult{Status: gen.AskAddressResultStatus(res.Ask.Status)},
	}
	if res.Ask.Reason != "" {
		out.Ask.Reason = gen.NewOptString(res.Ask.Reason)
	}
	return out
}

func claimRefOut(c conversation.LiveClaimRef) gen.LiveCommentClaimRef {
	return gen.LiveCommentClaimRef{By: gen.Actor{ID: c.By, Name: c.ByName}, OrderId: c.OrderID, OrderCode: c.OrderCode}
}

// GetLiveCommentContext (GET /api/v1/live-comments/{id}/context, conversation.read trên Page): thẻ khách, đơn nháp
// của khách trong phiên, SĐT tách từ comment, người bán của Page.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetLiveCommentContext(ctx context.Context, params gen.GetLiveCommentContextParams,
) (gen.GetLiveCommentContextRes, error) {
	var c conversation.LiveCommentContext
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		c, err = conversation.GetLiveCommentContext(ctx, tx, u, params.ID)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc bối cảnh chốt comment: %w", err)
	}
	out := &gen.LiveCommentContext{
		Customer: cardInfoOut(c.Card), OpenDrafts: make([]gen.LiveDraftRef, 0, len(c.OpenDrafts)), Phones: c.Phones,
		SellerId: c.SellerID,
	}
	for _, d := range c.OpenDrafts {
		r := gen.LiveDraftRef{
			OrderId: d.OrderID, Code: d.Code, GrandTotal: gen.Money(d.GrandTotal), Version: d.Version,
			Items: make([]gen.LiveDraftItem, 0, len(d.Items)),
		}
		if d.ExpiresAt != nil {
			r.ExpiresAt = gen.NewOptDateTime(*d.ExpiresAt)
		}
		for _, it := range d.Items {
			r.Items = append(r.Items, gen.LiveDraftItem{VariantId: it.VariantID, Name: it.Name, Quantity: it.Qty})
		}
		out.OpenDrafts = append(out.OpenDrafts, r)
	}
	return out, nil
}

// ListLiveComments (GET /api/v1/live-sessions/{id}/comments, conversation.read trên Page): comment theo thứ tự đến,
// sau mốc after (lùi 30 giây; nextCursor của trang trước đúng vị trí), lọc có SĐT, chưa chốt.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListLiveComments(ctx context.Context, params gen.ListLiveCommentsParams) (gen.ListLiveCommentsRes, error) {
	q := conversation.LiveCommentQuery{
		After: params.After.Or(""), Limit: int(params.Limit.Or(0)), PhoneOnly: params.PhoneOnly.Or(false),
		UnhandledOnly: params.UnhandledOnly.Or(false),
	}
	var rows []conversation.LiveCommentRow
	var next string
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		rows, next, err = conversation.ListLiveComments(ctx, tx, u, params.ID, q)
		return err //nolint:wrapcheck // conversation đã bọc
	}); err != nil {
		return nil, fmt.Errorf("liệt kê comment live: %w", err)
	}
	out := &gen.LiveCommentPage{Items: make([]gen.LiveComment, 0, len(rows))}
	if next != "" {
		out.NextCursor = gen.NewOptString(next)
	}
	for _, r := range rows {
		c := gen.LiveComment{
			MessageId: r.MessageID, ConversationId: r.ConversationID, Customer: gen.ChatCustomerRef{ID: r.CustomerID, Name: r.CustomerName},
			Text: r.Text, SentAt: r.SentAt, HasPhone: r.HasPhone, IsHidden: r.IsHidden,
			PrivateReply:         gen.ChatPrivateReplyRef{State: r.PrivateState, MessageId: optUUIDOut(r.PrivateMessageID)},
			CustomerHasOpenDraft: r.CustomerHasOpenDraft,
		}
		if r.Claim != nil {
			c.Claim = gen.NewOptLiveCommentClaimRef(claimRefOut(*r.Claim))
		}
		out.Items = append(out.Items, c)
	}
	return out, nil
}
