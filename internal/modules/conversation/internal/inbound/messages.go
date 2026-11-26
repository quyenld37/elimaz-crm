package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const (
	// placeholderName là tên tạm của khách mới từ kênh (CH08); tên thật từ hồ sơ kênh đè sau (Task 155).
	placeholderName = "Khách Facebook"
	// referralBody là tin hệ thống khi khách mở hội thoại từ quảng cáo hay liên kết (Q34: không nới cửa sổ 24 giờ).
	referralBody = "Khách đến từ quảng cáo hoặc liên kết"
)

// Giá trị cột của conversation.messages.
const (
	dirIn, dirOut           = "in", "out"
	kindMessage, kindSystem = "message", "system"
	originCustomer          = "customer"
	originPageExternal      = "page_external"
	originOwnEchoUnmatched  = "own_echo_unmatched"
	originSystem            = "system"
)

// pendingAttachment là một mục đính kèm chờ tải. URL (đường dẫn có chữ ký của kênh, dữ liệu cá nhân) chỉ nằm trong mục
// tới khi lệnh tải xong (stored hay lỗi thì xóa; tệp task-155, Quyết định 4); không bao giờ trả ra API.
type pendingAttachment struct {
	Index     int     `json:"index"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	FileID    *string `json:"fileId"`
	StickerID string  `json:"stickerId,omitempty"`
	URL       string  `json:"url,omitempty"`
}

// messageReceived ghi tin khách gửi theo thứ tự khóa của đường vào: Page (nhóm 3) → khách (nhóm 5) → hội thoại →
// tin (nhóm 6) → cập nhật hội thoại đã khóa → hook → báo hiệu. Tin trùng (cùng mã ngoài) → dừng, không đổi bộ đếm.
func (h *handler) messageReceived(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.MessagePayload) error {
	acc, convID, inserted, ok, err := h.inbox(ctx, tx, e, p)
	if err != nil || !ok {
		return err
	}
	at := p.SentAt.UTC()
	extID := p.MID
	if extID == "" { // nút bấm (postback) không có mid
		extID = syntheticID("postback", p.UserID, at)
	}
	payload := p.QuickReplyPayload
	if payload == "" {
		payload = p.PostbackPayload
	}
	atts, err := pendingAttachments(p.Attachments)
	if err != nil {
		return err
	}
	q := store.New(tx)
	msgID, err := q.InsertInboundMessage(ctx, store.InsertInboundMessageParams{
		ConversationID: convID, ChannelAccountID: acc.ID, ExternalID: text(extID), Direction: dirIn, Kind: kindMessage,
		Origin: originCustomer, Body: text(p.Text), Attachments: atts, QuickReplyPayload: text(payload), SentAt: at,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // tin trùng
	}
	if err != nil {
		return fmt.Errorf("ghi tin vào: %w", err)
	}
	if _, err := q.BumpConversationInbound(ctx, store.BumpConversationInboundParams{
		At: at, Snippet: text(deps.Snippet(p.Text, len(p.Attachments) > 0)), ID: convID,
	}); err != nil {
		return fmt.Errorf("cập nhật hội thoại sau tin vào: %w", err)
	}
	if len(p.Attachments) > 0 {
		if err := OnInboundAttachments(ctx, tx, h.d.Queue, acc.TenantID, msgID, p.Attachments); err != nil {
			return err
		}
	}
	if err := onInboxMessage(ctx, tx, convID, inserted); err != nil {
		return err
	}
	return h.publish(ctx, tx, e, acc, convID)
}

// referral ghi tin hệ thống "khách đến từ quảng cáo hoặc liên kết"; không đổi mốc, chưa đọc của hội thoại.
func (h *handler) referral(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.MessagePayload) error {
	acc, convID, _, ok, err := h.inbox(ctx, tx, e, p)
	if err != nil || !ok {
		return err
	}
	at := p.SentAt.UTC()
	_, err = store.New(tx).InsertInboundMessage(ctx, store.InsertInboundMessageParams{
		ConversationID: convID, ChannelAccountID: acc.ID, ExternalID: text(syntheticID("referral", p.UserID, at)),
		Direction: dirOut, Kind: kindSystem, Origin: originSystem, Body: text(referralBody), Attachments: []byte("[]"),
		SentAt: at,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ghi tin hệ thống đến từ quảng cáo: %w", err)
	}
	return h.publish(ctx, tx, e, acc, convID)
}

// inbox là account rồi thread cho sự kiện p của khách (mốc p.SentAt); ok = false khi Page không có trong shop.
func (h *handler) inbox(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, p channel.MessagePayload,
) (acc channel.ChannelAccount, convID uuid.UUID, inserted, ok bool, err error) {
	if acc, ok, err = h.account(ctx, tx, e, p.PageID); err != nil || !ok {
		return acc, uuid.Nil, false, ok, err
	}
	convID, inserted, err = h.thread(ctx, tx, acc, p.UserID, p.SentAt.UTC())
	return acc, convID, inserted, err == nil, err
}

// syntheticID là mã ngoài của sự kiện kênh không gửi mid (nút bấm, đến từ quảng cáo): cùng người, cùng mốc → cùng mã,
// nên giao lại vẫn chống trùng bằng messages_external_key.
func syntheticID(kind, user string, at time.Time) string {
	return kind + ":" + user + ":" + strconv.FormatInt(at.UnixMilli(), 10)
}

// pendingAttachments là danh sách đính kèm chờ tải của tin (JSON mảng, rỗng → []).
func pendingAttachments(refs []channel.AttachmentRef) ([]byte, error) {
	out := make([]pendingAttachment, 0, len(refs))
	for i, r := range refs {
		out = append(out, pendingAttachment{Index: i, Type: r.Type, Status: attPending, StickerID: r.StickerID, URL: r.URL})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("mã hóa danh sách đính kèm: %w", err)
	}
	return b, nil
}

// text là pgtype.Text, chuỗi rỗng → NULL.
func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
