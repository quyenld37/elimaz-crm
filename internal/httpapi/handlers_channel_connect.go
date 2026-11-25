package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Kết nối Page Facebook (kế hoạch M4, Task 149): đăng nhập Facebook for Business mở trong cùng tab (start → Meta →
// exchange), hoặc mã system user; ngắt kết nối. Gọi Meta (đổi mã, liệt kê Page, lấy token Page) ngoài transaction,
// trước WriteTx; lỗi tạm của Meta → 503 retry_later, chưa lưu gì. Token không bao giờ có trong câu trả lời.

// facebookKey là khóa adapter của kênh Facebook trong channel.Registry.
const facebookKey = "facebook"

// errConnectSource: yêu cầu nối Page không có đúng một trong sessionId, systemUserToken (422 invalid_input).
var errConnectSource = errors.New("httpapi: cần đúng một trong sessionId, systemUserToken")

// connectReady: tiến trình có adapter Facebook có Connector và khóa mã hóa token; không thì channel.ErrNoConnector
// (503 retry_later: tính năng chưa bật ở tiến trình này).
func (h *handler) connectReady() error {
	if h.d.ChannelKeys == nil || !channel.CanConnect(h.d.Channels, facebookKey) {
		return channel.ErrNoConnector
	}
	return nil
}

// StartFacebookOAuth (POST /api/v1/channels/facebook/oauth/start): tạo phiên đăng nhập (state hạn 15 phút, gắn người
// gọi) và trả địa chỉ hộp thoại đăng nhập; màn hình mở trong cùng tab.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) StartFacebookOAuth(ctx context.Context, _ gen.StartFacebookOAuthParams) (gen.StartFacebookOAuthRes, error) {
	if err := h.connectReady(); err != nil {
		return nil, err
	}
	if _, err := channel.AuthorizeURL(h.d.Config.Meta, "x"); err != nil {
		return nil, err //nolint:wrapcheck // sentinel của channel, đổi sang HTTP ở errors_channel.go
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isChannelRefusal, "bắt đầu đăng nhập Facebook",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			state, err := channel.StartOAuth(ctx, tx, u.UserID, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // channel đã bọc
			}
			raw, err := channel.AuthorizeURL(h.d.Config.Meta, state)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của channel
			}
			u2, err := url.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("đọc địa chỉ đăng nhập: %w", err)
			}
			return &gen.FacebookOAuthStart{AuthorizeUrl: *u2}, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FacebookOAuthStart), nil //nolint:forcetypeassert // fn trên trả *gen.FacebookOAuthStart
}

// ExchangeFacebookOAuth (POST /api/v1/channels/facebook/oauth/exchange): kiểm state (cùng người gọi, còn hạn, chưa
// dùng), đổi mã lấy token người dùng và danh sách Page ngoài transaction, rồi đánh dấu state đã dùng (một lần thắng)
// và lưu phiên Page (token mã hóa, hạn 30 phút).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ExchangeFacebookOAuth(ctx context.Context, req *gen.ExchangeFacebookOAuthRequest, _ gen.ExchangeFacebookOAuthParams) (gen.ExchangeFacebookOAuthRes, error) {
	if err := h.connectReady(); err != nil {
		return nil, err
	}
	now := h.now()
	var startID uuid.UUID
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		startID, err = channel.CheckOAuthState(ctx, tx, u.UserID, req.State, now)
		return err //nolint:wrapcheck // channel đã bọc
	}); err != nil {
		return nil, fmt.Errorf("kiểm phiên đăng nhập Facebook: %w", err)
	}
	tok, pages, err := channel.ExchangeCode(ctx, h.d.Channels, facebookKey, req.Code)
	if err != nil {
		return nil, fmt.Errorf("đổi mã đăng nhập Facebook: %w", err)
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isChannelRefusal, "lưu phiên đăng nhập Facebook",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			id, err := channel.SaveOAuthPages(ctx, tx, h.d.ChannelKeys, startID, u.UserID, tok, pages, now)
			if err != nil {
				return nil, err //nolint:wrapcheck // channel đã bọc
			}
			body := &gen.FacebookOAuthPages{SessionId: id, Pages: make([]gen.FacebookPageOption, 0, len(pages))}
			for _, p := range pages {
				body.Pages = append(body.Pages, gen.FacebookPageOption{
					PageId: p.PageID, Name: p.Name, CanMessage: p.CanMessage, CanModerate: p.CanModerate,
				})
			}
			return body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FacebookOAuthPages), nil //nolint:forcetypeassert // fn trên trả *gen.FacebookOAuthPages
}

// ConnectFacebookPage (POST /api/v1/channels/facebook/pages): lấy token Page (từ phiên đăng nhập của người gọi hoặc mã
// system user chỉ sống trong yêu cầu này) và kiểm quyền MESSAGING, MODERATE ngoài transaction; rồi nối Page (người bán
// own của shop, tuyến webhook, token mã hóa, audit, lệnh đăng ký webhook) trong một WriteTx.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ConnectFacebookPage(ctx context.Context, req *gen.ConnectFacebookPageRequest, _ gen.ConnectFacebookPageParams) (gen.ConnectFacebookPageRes, error) {
	if req.SessionId.Set == req.SystemUserToken.Set {
		return nil, errConnectSource
	}
	if err := h.connectReady(); err != nil {
		return nil, err
	}
	token, kind := logging.Secret(req.SystemUserToken.Value), channel.TokenSystemUser
	if req.SessionId.Set {
		kind = channel.TokenPageUser
		if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
			token, err = channel.OAuthUserToken(ctx, tx, h.d.ChannelKeys, req.SessionId.Value, u.UserID, req.PageId, h.now())
			return err //nolint:wrapcheck // channel đã bọc
		}); err != nil {
			return nil, fmt.Errorf("đọc phiên Page: %w", err)
		}
	}
	pageToken, info, err := channel.FetchPageToken(ctx, h.d.Channels, facebookKey, token, req.PageId)
	token = ""
	if err != nil {
		return nil, fmt.Errorf("lấy token Page: %w", err)
	}
	return h.channelChange(ctx, "nối Page", func(tx pgx.Tx, w channel.Writer, u identity.AuthUser) (channel.ChannelAccount, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return channel.ChannelAccount{}, fmt.Errorf("đọc người bán của shop: %w", err)
		}
		return w.Connect(ctx, tx, h.d.ChannelKeys, channel.ConnectInput{
			Channel: facebookKey, PageID: req.PageId, Name: info.Name, SellerID: seller, Token: pageToken, TokenKind: kind,
		}, u)
	})
}

// DisconnectChannel (POST /api/v1/channels/{id}/disconnect): ngắt Page (tin cũ giữ nguyên, tuyến giữ), ghi lệnh hủy
// đăng ký webhook; token bị xóa khi lệnh đó kết thúc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) DisconnectChannel(ctx context.Context, params gen.DisconnectChannelParams) (gen.DisconnectChannelRes, error) {
	return h.channelChange(ctx, "ngắt Page", func(tx pgx.Tx, w channel.Writer, u identity.AuthUser) (channel.ChannelAccount, error) {
		return w.Disconnect(ctx, tx, params.ID, u)
	})
}
