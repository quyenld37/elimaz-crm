package facebook_test

import (
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

func without(all []string, drop string) []string {
	return slices.DeleteFunc(slices.Clone(all), func(s string) bool { return s == drop })
}

// HealthCheck: đủ quyền và đủ 11 trường → khỏe; thiếu feed → WebhookSubscribed sai, MissingFields = [feed]; thiếu
// pages_manage_engagement → PermissionsOK sai; chưa đăng ký → thiếu cả 11; token thu hồi → TokenValid sai, không lỗi.
func TestHealthCheck(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	ctx := t.Context()
	if len(facebook.SubscribedFields()) != 11 {
		t.Fatalf("SubscribedFields = %v, muốn 11 trường", facebook.SubscribedFields())
	}
	h, err := e.ch.HealthCheck(ctx, e.acc, e.cred)
	if err != nil || !h.TokenValid || !h.PermissionsOK || h.WebhookSubscribed || len(h.MissingFields) != 11 {
		t.Errorf("chưa đăng ký: %+v, %v", h, err)
	}
	if err := e.ch.Subscribe(ctx, e.acc, e.cred, nil); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := e.fg.Subscribed(testPageID); !slices.Equal(got, facebook.SubscribedFields()) {
		t.Errorf("đăng ký = %v", got)
	}
	h, err = e.ch.HealthCheck(ctx, e.acc, e.cred)
	if err != nil || !h.TokenValid || !h.PermissionsOK || !h.WebhookSubscribed || len(h.MissingFields)+len(h.MissingPermissions) != 0 {
		t.Errorf("khỏe: %+v, %v", h, err)
	}
	if call := e.fg.Calls("debug_token"); len(call) == 0 || call[0].Token != testAppID+"|"+testAppSecret ||
		call[0].Query.Get("input_token") != testToken {
		t.Errorf("debug_token phải gọi bằng app token, input_token là token Page: %+v", call)
	}

	if err := e.ch.Subscribe(ctx, e.acc, e.cred, without(facebook.SubscribedFields(), "feed")); err != nil {
		t.Fatal(err)
	}
	h, _ = e.ch.HealthCheck(ctx, e.acc, e.cred)
	if h.WebhookSubscribed || !slices.Equal(h.MissingFields, []string{"feed"}) {
		t.Errorf("thiếu feed: %+v", h)
	}

	e.fg.AddPage(fakegraph.Page{
		ID: testPageID, Name: "Shop thử", Token: testToken,
		Scopes: without(facebook.RequiredPermissions(), "pages_manage_engagement"),
	})
	h, _ = e.ch.HealthCheck(ctx, e.acc, e.cred)
	if !h.TokenValid || h.PermissionsOK || !slices.Equal(h.MissingPermissions, []string{"pages_manage_engagement"}) {
		t.Errorf("thiếu quyền: %+v", h)
	}

	e.fg.RevokeToken(testToken)
	h, err = e.ch.HealthCheck(ctx, e.acc, e.cred)
	if err != nil || h.TokenValid || h.PermissionsOK || h.WebhookSubscribed {
		t.Errorf("token thu hồi: %+v, %v; muốn TokenValid sai, không lỗi", h, err)
	}
	if err := e.ch.Unsubscribe(ctx, e.acc, e.cred); e.ch.ClassifyError(err) != channel.ErrorAuth {
		t.Errorf("hủy đăng ký bằng token thu hồi: %v", err)
	}
}

// Lỗi mạng khi kiểm sức khỏe là lỗi (bên gọi thử lại), không phải "token hỏng".
func TestHealthCheck_TransportError(t *testing.T) {
	t.Parallel()
	e := newOutEnv(t)
	e.refuseNext()
	if h, err := e.ch.HealthCheck(t.Context(), e.acc, e.cred); err == nil || h.TokenValid {
		t.Errorf("rớt kết nối: %+v, %v; muốn lỗi", h, err)
	}
}
