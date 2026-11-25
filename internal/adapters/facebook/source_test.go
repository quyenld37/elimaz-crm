package facebook_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Source(cfg): nguồn webhook phần vào chỉ khi kênh Meta bật và có app secret; Key là "facebook". Adapter(cfg) (đăng
// ký vào channel.Registry) vẫn nil tới khi Task 148 có phần ra (tệp task 147, Quyết định 2).
func TestSource(t *testing.T) {
	t.Parallel()
	on := config.Config{Meta: config.MetaConfig{
		Enabled: true, AppSecret: logging.Secret(webhookAppSecret), VerifyToken: logging.Secret(testVerifyToken),
	}}
	s := facebook.Source(on)
	if s == nil || s.Key() != facebook.Key || s.Key() != "facebook" {
		t.Fatalf("Meta bật: nguồn = %v", s)
	}
	raw := fixture(t, "message_text.json")
	if !s.VerifySignature(raw, sign(raw, webhookAppSecret)) {
		t.Error("nguồn từ cấu hình phải kiểm bằng META_APP_SECRET")
	}
	if ans, ok := s.Challenge(map[string][]string{
		"hub.mode": {"subscribe"}, "hub.verify_token": {testVerifyToken}, "hub.challenge": {"42"},
	}); !ok || ans != "42" {
		t.Error("nguồn từ cấu hình phải xác minh bằng META_VERIFY_TOKEN")
	}
	for name, cfg := range map[string]config.Config{
		"Meta tắt":     {Meta: config.MetaConfig{AppSecret: logging.Secret(webhookAppSecret)}},
		"thiếu secret": {Meta: config.MetaConfig{Enabled: true}},
	} {
		if s := facebook.Source(cfg); s != nil {
			t.Errorf("%s: nguồn phải nil (không phải nil có kiểu), được %T", name, s)
		}
	}
	if facebook.Adapter(on) != nil {
		t.Error("Adapter vẫn nil tới Task 148")
	}
	if f := facebook.New(logging.Secret(webhookAppSecret), "").Features(); !f.Comments || !f.LiveComments || !f.PrivateReply ||
		!f.HideComment || !f.QuickReplies || !f.HumanAgent {
		t.Errorf("Features = %+v", f)
	}
}
