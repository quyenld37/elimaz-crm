package inbound

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// echo ghi tin Page gửi (Meta báo lại). EchoAppID là app của CRM và Metadata là client_ref của tin đang chờ (queued,
// checking, in_doubt) → đúng tin đó thành sent có mã ngoài, không thêm dòng. Còn lại: app của CRM → tin
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
		if ref, perr := uuid.Parse(p.Metadata); perr == nil && p.MID != "" {
			row, err := q.ConfirmOwnEcho(ctx, store.ConfirmOwnEchoParams{
				ExternalID: text(p.MID), SentAt: at, ClientRef: pgtype.UUID{Bytes: ref, Valid: true}, ChannelAccountID: acc.ID,
			})
			switch {
			case err == nil:
				return h.publish(ctx, tx, e, acc, row.ConversationID)
			case !errors.Is(err, pgx.ErrNoRows):
				return fmt.Errorf("khớp echo với tin của CRM: %w", err)
			}
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
	atts, err := deps.PendingAttachments(p.Attachments)
	if err != nil {
		return err //nolint:wrapcheck // deps đã bọc việc đang làm
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
		if err := OnInboundAttachments(ctx, tx, msgID, p.Attachments); err != nil {
			return err
		}
	}
	return h.publish(ctx, tx, e, acc, convID)
}
