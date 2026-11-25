package channel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
)

// Chế độ gửi của tài khoản kênh (cột send_mode): Page mới luôn read_only (chỉ xem, MO03); enabled = gửi thật.
const (
	SendModeReadOnly = "read_only"
	SendModeEnabled  = "enabled"
)

// Trạng thái tài khoản kênh (cột status).
const (
	StatusActive       = "active"
	StatusDegraded     = "degraded"
	StatusDisconnected = "disconnected"
)

// TopicStatus là chủ đề báo hiệu realtime khi cài đặt hay trạng thái tài khoản kênh đổi (conversation.TopicChannelStatus
// là bí danh); đòi conversation.read trên page:<id>, thực thể {EntityAccount, id}.
const (
	TopicStatus   = "channel.status"
	EntityAccount = "channel_account"
)

// inboundThrottle: NoteInbound ghi mỗi mốc tối đa mỗi chừng này một lần.
const inboundThrottle = time.Minute

var (
	// ErrAccountNotFound báo không có tài khoản kênh này trong shop của ngữ cảnh.
	ErrAccountNotFound = errors.New("channel: không có tài khoản kênh")
	// ErrVersionConflict báo version gửi lên khác version hiện tại (người khác vừa sửa).
	ErrVersionConflict = errors.New("channel: tài khoản kênh đã đổi, tải lại rồi thử lại")
	// ErrInvalidSendMode báo chế độ gửi không phải read_only, enabled.
	ErrInvalidSendMode = errors.New("channel: chế độ gửi không hợp lệ")
	// ErrInvalidStatus báo trạng thái không phải active, degraded, disconnected.
	ErrInvalidStatus = errors.New("channel: trạng thái tài khoản kênh không hợp lệ")
)

// accountRow là dạng chung của các câu đọc tài khoản (sqlc sinh một kiểu mỗi câu, cùng cột).
type accountRow = store.GetAccountRow

func accountFrom(r accountRow) ChannelAccount {
	return ChannelAccount{
		ID: r.ID, TenantID: r.TenantID, SellerID: r.SellerID, Channel: r.Channel, ExternalID: r.ExternalID, Name: r.Name,
		Status: r.Status, SendMode: r.SendMode, HumanAgentEnabled: r.HumanAgentEnabled, BlockedUntil: r.BlockedUntil,
		Version: r.Version, StatusReason: r.StatusReason.String, TokenKind: r.TokenKind.String,
		SubscribedFields: slices.Clone(r.SubscribedFields), TokenCheckedAt: r.TokenCheckedAt, LastWebhookAt: r.LastWebhookAt,
		LastFeedAt: r.LastFeedAt,
	}
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// Account đọc tài khoản kênh id trong tx tenant của bên gọi; không có → ErrAccountNotFound.
func Account(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ChannelAccount, error) {
	r, err := store.New(tx).GetAccount(ctx, id)
	if err != nil {
		return ChannelAccount{}, notFound(err, "đọc tài khoản kênh")
	}
	return accountFrom(r), nil
}

// AccountByExternal đọc tài khoản theo khóa kênh (facebook, fakechat) và mã Page trên kênh; không có →
// ErrAccountNotFound.
func AccountByExternal(ctx context.Context, tx pgx.Tx, channel, externalID string) (ChannelAccount, error) {
	r, err := store.New(tx).GetAccountByExternal(ctx, store.GetAccountByExternalParams{Channel: channel, ExternalID: externalID})
	if err != nil {
		return ChannelAccount{}, notFound(err, "đọc tài khoản kênh theo mã Page")
	}
	return accountFrom(accountRow(r)), nil
}

// ListAccounts trả tài khoản kênh của shop theo tên: pageIDs nil = mọi tài khoản; khác nil = chỉ các mã đó (rỗng →
// không tài khoản nào: người không có Page nào không thấy gì).
func ListAccounts(ctx context.Context, tx pgx.Tx, pageIDs []uuid.UUID) ([]ChannelAccount, error) {
	rows, err := store.New(tx).ListAccounts(ctx, store.ListAccountsParams{AllAccounts: pageIDs == nil, Ids: append([]uuid.UUID{}, pageIDs...)})
	if err != nil {
		return nil, fmt.Errorf("liệt kê tài khoản kênh: %w", err)
	}
	out := make([]ChannelAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFrom(accountRow(r)))
	}
	return out, nil
}

// NoteInbound ghi mốc có sự kiện vào của tài khoản id lúc at (feed: cả mốc feed), mỗi mốc tối đa mỗi phút một lần,
// bằng một câu UPDATE có điều kiện (nhiều worker cùng lúc không tranh nhau đọc-ghi); mốc cũ hơn không đè mốc mới.
// Tài khoản không có thì không làm gì.
func NoteInbound(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time, feed bool) error {
	at = at.UTC()
	if _, err := store.New(tx).NoteInbound(ctx, store.NoteInboundParams{
		Cutoff: at.Add(-inboundThrottle), At: at, Feed: feed, ID: id,
	}); err != nil {
		return fmt.Errorf("ghi mốc sự kiện vào của tài khoản kênh: %w", err)
	}
	return nil
}

// MarkStatus đặt trạng thái status (active, degraded, disconnected) và lý do reason (rỗng = không có) của tài khoản
// id; changed = true khi thật sự đổi (để bên gọi chỉ báo hiệu, cảnh báo một lần). Không tăng version (version chỉ cho
// cài đặt nhân viên sửa). Không có tài khoản → ErrAccountNotFound.
func MarkStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status, reason string) (changed bool, err error) {
	if status != StatusActive && status != StatusDegraded && status != StatusDisconnected {
		return false, ErrInvalidStatus
	}
	q := store.New(tx)
	n, err := q.MarkStatus(ctx, store.MarkStatusParams{
		Status: status, StatusReason: pgtype.Text{String: reason, Valid: reason != ""}, ID: id,
	})
	if err != nil {
		return false, fmt.Errorf("đổi trạng thái tài khoản kênh: %w", err)
	}
	if n == 0 {
		if _, err := q.GetAccount(ctx, id); err != nil {
			return false, notFound(err, "đọc tài khoản kênh")
		}
	}
	return n > 0, nil
}

// NoteBlockedUntil ghi kênh chặn gửi của tài khoản id tới until (chỉ dời về sau, không rút ngắn); AcquireSendToken từ
// chối tới mốc đó. Không có tài khoản → ErrAccountNotFound.
func NoteBlockedUntil(ctx context.Context, tx pgx.Tx, id uuid.UUID, until time.Time) error {
	n, err := store.New(tx).NoteBlockedUntil(ctx, store.NoteBlockedUntilParams{Until: until.UTC(), ID: id})
	if err != nil {
		return fmt.Errorf("ghi mốc chặn gửi: %w", err)
	}
	if n == 0 {
		return ErrAccountNotFound
	}
	return nil
}
