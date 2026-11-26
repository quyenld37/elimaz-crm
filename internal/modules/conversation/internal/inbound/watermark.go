package inbound

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// watermarkWindow: "đã nhận", "đã xem" chỉ đánh dấu tin ra gửi trong chừng này (index messages_conversation, không quét
// cả lịch sử).
const watermarkWindow = 7 * 24 * time.Hour

// watermark ghi "đã nhận" (read = false) hay "đã xem" tới mốc p.Watermark cho tin ra của hội thoại inbox (Page, PSID);
// chưa có hội thoại → bỏ qua.
func (h *handler) watermark(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.WatermarkPayload, read bool) error {
	acc, ok, err := h.account(ctx, tx, e, p.PageID)
	if err != nil || !ok {
		return err
	}
	q := store.New(tx)
	convID, err := q.FindInboxConversation(ctx, store.FindInboxConversationParams{ChannelAccountID: acc.ID, ExternalThreadRef: p.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		h.ignore("watermark_unknown_conversation")
		return nil
	}
	if err != nil {
		return fmt.Errorf("tìm hội thoại của mốc đã nhận/đã xem: %w", err)
	}
	wm := pgtype.Timestamptz{Time: p.Watermark.UTC(), Valid: true}
	since := h.now().UTC().Add(-watermarkWindow)
	var n int64
	if read {
		n, err = q.MarkRead(ctx, store.MarkReadParams{Wm: wm, ConversationID: convID, Since: since})
	} else {
		n, err = q.MarkDelivered(ctx, store.MarkDeliveredParams{Wm: wm, ConversationID: convID, Since: since})
	}
	if err != nil {
		return fmt.Errorf("ghi mốc đã nhận/đã xem: %w", err)
	}
	if n == 0 {
		return nil
	}
	return h.publish(ctx, tx, e, acc, convID)
}

// reaction ghi khách thả (react) hay bỏ (unreact) cảm xúc trên tin mid; tin chưa có → bỏ qua.
func (h *handler) reaction(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.ReactionPayload) error {
	acc, ok, err := h.account(ctx, tx, e, p.PageID)
	if err != nil || !ok {
		return err
	}
	convID, err := store.New(tx).SetCustomerReaction(ctx, store.SetCustomerReactionParams{
		React: p.Action == "react", Reaction: p.Reaction, ChannelAccountID: acc.ID, ExternalID: text(p.MID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		h.ignore("reaction_unknown_message")
		return nil
	}
	if err != nil {
		return fmt.Errorf("ghi cảm xúc trên tin: %w", err)
	}
	return h.publish(ctx, tx, e, acc, convID)
}

// edit ghi lần khách sửa tin mid: khóa dòng tin, ghi nội dung cũ vào message_revisions rồi đổi body, edited_at. Lần sửa
// không mới hơn lần đã ghi (giao lại, đến muộn) → không làm gì; tin chưa có → bỏ qua.
func (h *handler) edit(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.EditPayload) error {
	acc, ok, err := h.account(ctx, tx, e, p.PageID)
	if err != nil || !ok {
		return err
	}
	q := store.New(tx)
	m, err := q.LockMessageByExternal(ctx, store.LockMessageByExternalParams{ChannelAccountID: acc.ID, ExternalID: text(p.MID)})
	if errors.Is(err, pgx.ErrNoRows) {
		h.ignore("edit_unknown_message")
		return nil
	}
	if err != nil {
		return fmt.Errorf("khóa tin để sửa: %w", err)
	}
	editedAt := p.EditedAt.UTC()
	if m.EditedAt.Valid && !editedAt.After(m.EditedAt.Time) {
		return nil
	}
	source := originCustomer
	if m.Direction != dirIn {
		source = originPageExternal
	}
	if err := q.InsertMessageRevision(ctx, store.InsertMessageRevisionParams{MessageID: m.ID, Body: m.Body, Source: source}); err != nil {
		return fmt.Errorf("ghi lịch sử sửa tin: %w", err)
	}
	if err := q.ApplyMessageEdit(ctx, store.ApplyMessageEditParams{
		Body: text(p.Text), EditedAt: pgtype.Timestamptz{Time: editedAt, Valid: true}, ID: m.ID,
	}); err != nil {
		return fmt.Errorf("đổi nội dung tin đã sửa: %w", err)
	}
	return h.publish(ctx, tx, e, acc, m.ConversationID)
}
