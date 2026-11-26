package facebook

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// RequiredPermissions là quyền token Page phải có (spec 02 mục 1).
func RequiredPermissions() []string {
	return []string{
		"pages_messaging", "pages_manage_metadata", "pages_read_engagement", "pages_read_user_content", "pages_manage_engagement",
	}
}

// HealthCheck kiểm tài khoản Page: token còn hiệu lực (GET {page-id}), quyền đủ (debug_token bằng app token
// "<app_id>|<app_secret>"), app CRM đăng ký đủ 11 trường (GET {page-id}/subscribed_apps). Token hỏng (lớp auth) →
// TokenValid sai, các mục khác sai, lỗi nil (kết quả kiểm, không phải lỗi gọi); lỗi khác (mạng, Meta tạm lỗi) → lỗi để
// bên gọi thử lại.
func (c *Channel) HealthCheck(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials) (channel.Health, error) {
	h := channel.Health{}
	if cred.AccessToken == "" {
		return h, nil
	}
	var page struct {
		ID string `json:"id"`
	}
	if _, err := c.client.Do(ctx, http.MethodGet, acc.ExternalID, cred.AccessToken, url.Values{"fields": {"id,name"}}, nil, &page); err != nil {
		if Classify(err) == channel.ErrorAuth {
			return h, nil
		}
		return h, err
	}
	var dbg struct {
		Data struct {
			IsValid bool     `json:"is_valid"`
			Scopes  []string `json:"scopes"`
		} `json:"data"`
	}
	appToken := logging.Secret(c.o.AppID + "|" + string(c.o.AppSecret))
	if _, err := c.client.Do(ctx, http.MethodGet, "debug_token", appToken, url.Values{"input_token": {string(cred.AccessToken)}}, nil, &dbg); err != nil {
		return h, err
	}
	if !dbg.Data.IsValid {
		return h, nil
	}
	h.TokenValid = true
	h.MissingPermissions = missing(RequiredPermissions(), dbg.Data.Scopes)
	h.PermissionsOK = len(h.MissingPermissions) == 0
	fields, err := c.subscribedFields(ctx, acc, cred)
	if err != nil {
		if Classify(err) == channel.ErrorAuth {
			return channel.Health{}, nil
		}
		return h, err
	}
	h.MissingFields = missing(channel.WebhookFields(), fields)
	h.WebhookSubscribed = len(h.MissingFields) == 0
	return h, nil
}

// subscribedFields trả các trường app CRM (AppID) đang đăng ký trên Page; chưa đăng ký → rỗng.
func (c *Channel) subscribedFields(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials) ([]string, error) {
	var out struct {
		Data []struct {
			ID     string   `json:"id"`
			Fields []string `json:"subscribed_fields"`
		} `json:"data"`
	}
	if _, err := c.client.Do(ctx, http.MethodGet, acc.ExternalID+"/subscribed_apps", cred.AccessToken, nil, nil, &out); err != nil {
		return nil, err
	}
	for _, a := range out.Data {
		if a.ID == c.o.AppID {
			return a.Fields, nil
		}
	}
	return nil, nil
}

// missing trả các phần tử của want không có trong have, giữ thứ tự của want.
func missing(want, have []string) []string {
	return slices.DeleteFunc(slices.Clone(want), func(w string) bool { return slices.Contains(have, w) })
}
