package inbound

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Echo muộn cho tin nhân viên đã bỏ (Task 157): trạng thái cũ và tên chỉ số (nhãn là khóa kênh).
const (
	statusDiscarded            = "discarded"
	metricLateEchoAfterDiscard = "late_echo_after_discard"
)

// echo ghi tin Page gửi (Meta báo lại). EchoAppID là app của CRM và Metadata là client_ref của tin chưa sent (queued,
// checking, in_doubt; cả failed, expired, discarded: sự thật thắng, Task 157) → đúng tin đó thành sent có mã ngoài,
// không thêm dòng; tin đã bị nhân viên bỏ thì đếm chỉ số late_echo_after_discard. Còn lại: app của CRM → tin
// own_echo_unmatched (Task 156 gộp); app khác hay không có (Business Suite, ứng dụng Meta) → tin page_external (CH14).
// Hai loại chèn cập nhật mốc tin cuối, mốc tin nhân viên; không tăng chưa đọc. Mã ngoài đã có → không làm gì.
func (h *handler) echo(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.MessagePayload) error {
	acc, ok, err := h.account(ctx, tx, e, p.PageID)
	if err != nil || !ok {
		return err
	}
	at := p.SentAt.UTC()
	q := store.New(tx)
	origin := originPageExternal
	if h.ownAppID != "" && p.EchoAppID == h.ownAppID {
		origin = originOwnEchoUnmatched
		if done, err := h.confirmOwn(ctx, tx, e, acc, p, at); done || err != nil {
			return err
		}
	}
	if p.MID == "" {
		h.ignore("echo_without_mid")
		return nil
	}
	convID, _, err := h.thread(ctx, tx, acc, p.UserID, at)
	if err != nil {
		return err
	}
	refs := p.Attachments
	if origin == originOwnEchoUnmatched {
		refs = withoutURLs(refs) // ảnh CRM vừa gửi: đã có tệp, Task 156 gộp rồi xóa dòng này (tệp task-155, Quyết định 5)
	}
	atts, err := pendingAttachments(refs)
	if err != nil {
		return err
	}
	msgID, err := q.InsertInboundMessage(ctx, store.InsertInboundMessageParams{
		ConversationID: convID, ChannelAccountID: acc.ID, ExternalID: text(p.MID), Direction: dirOut, Kind: kindMessage,
		Origin: origin, Body: text(p.Text), Attachments: atts, SentAt: at,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ghi tin Page gửi: %w", err)
	}
	if err := q.BumpConversationOutbound(ctx, store.BumpConversationOutboundParams{
		At: at, Snippet: text(deps.Snippet(p.Text, len(p.Attachments) > 0)), ID: convID,
	}); err != nil {
		return fmt.Errorf("cập nhật hội thoại sau tin Page gửi: %w", err)
	}
	if len(p.Attachments) > 0 {
		if err := OnInboundAttachments(ctx, tx, h.d.Queue, acc.TenantID, msgID, refs); err != nil {
			return err
		}
	}
	return h.publish(ctx, tx, e, acc, convID)
}

// withoutURLs là bản sao refs không có URL (mục giữ pending, không lệnh tải).
func withoutURLs(refs []channel.AttachmentRef) []channel.AttachmentRef {
	out := make([]channel.AttachmentRef, len(refs))
	for i, r := range refs {
		r.URL = ""
		out[i] = r
	}
	return out
}

// confirmOwn khớp echo của CRM có metadata = client_ref với tin chưa sent (ConfirmOwnEcho) và báo hiệu; tin cũ đã bị
// nhân viên bỏ → chỉ số late_echo_after_discard. done = false: không khớp (thiếu mid, metadata không phải mã tin, không
// có tin chờ), bên gọi chèn dòng own_echo_unmatched.
func (h *handler) confirmOwn(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.MessagePayload, at time.Time,
) (bool, error) {
	ref, perr := uuid.Parse(p.Metadata)
	if perr != nil || p.MID == "" {
		return false, nil
	}
	row, err := store.New(tx).ConfirmOwnEcho(ctx, store.ConfirmOwnEchoParams{
		ExternalID: text(p.MID), SentAt: at, ClientRef: pgtype.UUID{Bytes: ref, Valid: true}, ChannelAccountID: acc.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("khớp echo với tin của CRM: %w", err)
	}
	if row.PrevStatus == statusDiscarded && h.d.Metrics != nil {
		h.d.Metrics.Inc(metricLateEchoAfterDiscard, h.source, 1)
	}
	return true, h.publish(ctx, tx, e, acc, row.ConversationID)
}
