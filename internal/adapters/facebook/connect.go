package facebook

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// ErrNoToken báo Meta trả lời không có access_token khi đổi mã hay lấy token Page.
var ErrNoToken = errors.New("facebook: Meta không trả token")

const (
	// listPagesLimit, listPagesMax: số Page mỗi trang me/accounts và số trang đọc tối đa.
	listPagesLimit = 100
	listPagesMax   = 10
)

type tokenReply struct {
	AccessToken string `json:"access_token"`
}

// ExchangeCode đổi mã đăng nhập Facebook (redirect_uri là OAuthRedirectURL) lấy token người dùng, rồi đổi token đó lấy
// token dài hạn (grant_type=fb_exchange_token): token Page lấy từ token dài hạn không hết hạn.
func (c *Channel) ExchangeCode(ctx context.Context, code string) (logging.Secret, error) {
	q := url.Values{
		"client_id": {c.o.AppID}, "client_secret": {string(c.o.AppSecret)}, "redirect_uri": {c.o.OAuthRedirectURL}, "code": {code},
	}
	short, err := c.oauthToken(ctx, q)
	if err != nil {
		return "", err
	}
	q = url.Values{
		"grant_type": {"fb_exchange_token"}, "client_id": {c.o.AppID}, "client_secret": {string(c.o.AppSecret)},
		"fb_exchange_token": {string(short)},
	}
	return c.oauthToken(ctx, q)
}

func (c *Channel) oauthToken(ctx context.Context, q url.Values) (logging.Secret, error) {
	var out tokenReply
	if _, err := c.client.Do(ctx, http.MethodGet, "oauth/access_token", "", q, nil, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", ErrNoToken
	}
	return logging.Secret(out.AccessToken), nil
}

type pageReply struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	AccessToken string   `json:"access_token"`
	Tasks       []string `json:"tasks"`
}

func (p pageReply) info() channel.PageInfo {
	has := func(t string) bool { return slices.Contains(p.Tasks, t) || slices.Contains(p.Tasks, "MANAGE") }
	return channel.PageInfo{PageID: p.ID, Name: p.Name, CanMessage: has("MESSAGING"), CanModerate: has("MODERATE")}
}

// ListPages liệt kê các Page người dùng quản trị (me/accounts, theo trang, tối đa 1 000 Page), kèm quyền nhắn tin
// (task MESSAGING) và kiểm duyệt (MODERATE); task MANAGE gồm cả hai.
func (c *Channel) ListPages(ctx context.Context, userToken logging.Secret) ([]channel.PageInfo, error) {
	q := url.Values{"fields": {"id,name,tasks"}, "limit": {strconv.Itoa(listPagesLimit)}}
	var pages []channel.PageInfo
	for range listPagesMax {
		var out struct {
			Data   []pageReply `json:"data"`
			Paging struct {
				Cursors struct {
					After string `json:"after"`
				} `json:"cursors"`
				Next string `json:"next"`
			} `json:"paging"`
		}
		if _, err := c.client.Do(ctx, http.MethodGet, "me/accounts", userToken, q, nil, &out); err != nil {
			return nil, err
		}
		for _, p := range out.Data {
			pages = append(pages, p.info())
		}
		if out.Paging.Next == "" || out.Paging.Cursors.After == "" {
			break
		}
		q.Set("after", out.Paging.Cursors.After)
	}
	return pages, nil
}

// PageToken lấy token Page pageID bằng token người dùng hay token system user ({page-id}?fields=access_token,name,tasks).
func (c *Channel) PageToken(ctx context.Context, token logging.Secret, pageID string) (logging.Secret, channel.PageInfo, error) {
	var out pageReply
	q := url.Values{"fields": {"access_token,name,tasks"}}
	if _, err := c.client.Do(ctx, http.MethodGet, pageID, token, q, nil, &out); err != nil {
		return "", channel.PageInfo{}, err
	}
	if out.AccessToken == "" {
		return "", channel.PageInfo{}, ErrNoToken
	}
	if out.ID == "" {
		out.ID = pageID
	}
	return logging.Secret(out.AccessToken), out.info(), nil
}

// Subscribe đăng ký app CRM nhận webhook của Page với fields (nil: đủ 11 trường SubscribedFields).
func (c *Channel) Subscribe(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, fields []string) error {
	if fields == nil {
		fields = SubscribedFields()
	}
	body := map[string]string{"subscribed_fields": strings.Join(fields, ",")}
	_, err := c.client.Do(ctx, http.MethodPost, acc.ExternalID+"/subscribed_apps", cred.AccessToken, nil, body, nil)
	return err
}

// Unsubscribe hủy đăng ký webhook của app CRM trên Page.
func (c *Channel) Unsubscribe(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials) error {
	_, err := c.client.Do(ctx, http.MethodDelete, acc.ExternalID+"/subscribed_apps", cred.AccessToken, nil, nil, nil)
	return err
}
