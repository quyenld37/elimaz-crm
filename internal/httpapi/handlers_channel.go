package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng channel (kế hoạch M4, Task 145): danh sách tài khoản kênh (Page) theo phạm vi conversation.read, đổi chế độ
// gửi và thẻ nhân viên (channel.manage, toàn shop; guard kiểm trước handler). Không bao giờ có token trong câu trả lời.

const permConversationRead = identity.Permission("conversation.read")

// ListChannelAccounts (GET /api/v1/channels): Page người gọi có conversation.read (grant all → mọi Page; grant theo
// page:<id> → đúng các Page đó).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListChannelAccounts(ctx context.Context) (gen.ListChannelAccountsRes, error) {
	var items []channel.ChannelAccount
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		all, ids := readableWarehouses(u, permConversationRead)
		if all {
			ids = nil
		} else if ids == nil {
			ids = []uuid.UUID{}
		}
		items, err = channel.ListAccounts(ctx, tx, ids)
		return err //nolint:wrapcheck // channel đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc tài khoản kênh: %w", err)
	}
	out := &gen.ChannelAccountList{Items: make([]gen.ChannelAccount, 0, len(items))}
	for _, a := range items {
		out.Items = append(out.Items, *channelAccountOut(a))
	}
	return out, nil
}

// SetChannelSendMode (PUT /api/v1/channels/{id}/send-mode).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetChannelSendMode(ctx context.Context, req *gen.SetChannelSendModeRequest, params gen.SetChannelSendModeParams) (gen.SetChannelSendModeRes, error) {
	return h.channelChange(ctx, "đổi chế độ gửi", func(tx pgx.Tx, w channel.Writer, u identity.AuthUser) (channel.ChannelAccount, error) {
		return w.SetSendMode(ctx, tx, params.ID, string(req.Mode), req.ExpectedVersion, u)
	})
}

// SetChannelHumanAgent (PUT /api/v1/channels/{id}/human-agent).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetChannelHumanAgent(ctx context.Context, req *gen.SetChannelHumanAgentRequest, params gen.SetChannelHumanAgentParams) (gen.SetChannelHumanAgentRes, error) {
	return h.channelChange(ctx, "đổi thẻ nhân viên", func(tx pgx.Tx, w channel.Writer, u identity.AuthUser) (channel.ChannelAccount, error) {
		return w.SetHumanAgent(ctx, tx, params.ID, req.Enabled, req.ExpectedVersion, u)
	})
}

// channelChange chạy op trong refusableWrite với channel.Writer của yêu cầu (báo hiệu, RequestID của audit); lỗi
// nghiệp vụ của channel là câu trả lời không đổi gì.
func (h *handler) channelChange(ctx context.Context, what string,
	op func(tx pgx.Tx, w channel.Writer, u identity.AuthUser) (channel.ChannelAccount, error),
) (*gen.ChannelAccount, error) {
	w := channel.Writer{Realtime: h.d.Realtime, RequestID: RequestID(ctx)}
	out, err := h.refusableWrite(ctx, http.StatusOK, isChannelRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		a, err := op(tx, w, u)
		if err != nil {
			return nil, err
		}
		return channelAccountOut(a), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ChannelAccount), nil //nolint:forcetypeassert // fn trên trả *gen.ChannelAccount
}

func isChannelRefusal(err error) bool {
	_, _, ok := channelError(err)
	return ok
}

func channelAccountOut(a channel.ChannelAccount) *gen.ChannelAccount {
	o := &gen.ChannelAccount{
		ID: a.ID, Channel: a.Channel, ExternalId: a.ExternalID, Name: a.Name, Status: gen.ChannelAccountStatus(a.Status),
		SendMode: gen.ChannelSendMode(a.SendMode), HumanAgentEnabled: a.HumanAgentEnabled,
		SubscribedFields: append([]string{}, a.SubscribedFields...), Version: a.Version,
	}
	if a.StatusReason != "" {
		o.StatusReason = gen.NewOptString(a.StatusReason)
	}
	if a.TokenCheckedAt != nil {
		o.TokenCheckedAt = gen.NewOptDateTime(*a.TokenCheckedAt)
	}
	return o
}
