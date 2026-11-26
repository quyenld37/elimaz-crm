package facebook_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Source(cfg), Adapter(cfg): adapter đầy đủ dựng từ cfg.Meta chỉ khi kênh Meta bật và có app secret, không thì nil thật;
// Key là "facebook". Adapter được nối vào channel.Registry từ Task 153 (tệp task 148, Quyết định 1).
func TestSource(t *testing.T) {
	t.Parallel()
	on := config.Config{Meta: config.MetaConfig{
		Enabled: true, AppSecret: logging.Secret(testAppSecret), VerifyToken: logging.Secret(testVerifyToken),
	}}
	s := facebook.Source(on)
	if _, full := s.(channel.MessagingAdapter); !full {
		t.Errorf("nguồn từ cấu hình phải là adapter đầy đủ, được %T", s)
	}
	if s == nil || s.Key() != facebook.Key || s.Key() != "facebook" {
		t.Fatalf("Meta bật: nguồn = %v", s)
	}
	raw := fixture(t, "message_text.json")
	if !s.VerifySignature(raw, sign(raw, testAppSecret)) {
		t.Error("nguồn từ cấu hình phải kiểm bằng META_APP_SECRET")
	}
	if ans, ok := s.Challenge(map[string][]string{
		"hub.mode": {"subscribe"}, "hub.verify_token": {testVerifyToken}, "hub.challenge": {"42"},
	}); !ok || ans != "42" {
		t.Error("nguồn từ cấu hình phải xác minh bằng META_VERIFY_TOKEN")
	}
	for name, cfg := range map[string]config.Config{
		"Meta tắt":     {Meta: config.MetaConfig{AppSecret: logging.Secret(testAppSecret)}},
		"thiếu secret": {Meta: config.MetaConfig{Enabled: true}},
	} {
		if s := facebook.Source(cfg); s != nil {
			t.Errorf("%s: nguồn phải nil (không phải nil có kiểu), được %T", name, s)
		}
		if a := facebook.Adapter(cfg); a != nil {
			t.Errorf("%s: Adapter phải nil (không phải nil có kiểu), được %T", name, a)
		}
	}
	if a := facebook.Adapter(on); a == nil || a.Key() != facebook.Key {
		t.Errorf("Meta bật: Adapter = %v, muốn adapter khóa %q", a, facebook.Key)
	}
	if f := inbound(testAppSecret, "").Features(); !f.Comments || !f.LiveComments || !f.PrivateReply ||
		!f.HideComment || !f.QuickReplies || !f.HumanAgent {
		t.Errorf("Features = %+v", f)
	}
}
