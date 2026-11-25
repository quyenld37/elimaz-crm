package config_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
)

// metaSecret là META_APP_SECRET hợp lệ (đúng 32 ký tự hex); metaVerify là META_VERIFY_TOKEN hợp lệ (≥ 32 ký tự).
const (
	metaSecret = "0123456789abcdef0123456789abcdef"
	metaVerify = "verify-token-0123456789-abcdefghij"
)

func apiFacebookEnv() map[string]string {
	return with(apiEnv(),
		"META_APP_ID", "1234567890", "META_APP_SECRET", metaSecret, "META_LOGIN_CONFIG_ID", "998877",
		"META_OAUTH_REDIRECT_URL", "https://crm.elimaz.com.vn/settings/channels/facebook/callback",
		"FIELD_ENC_KEYS", fieldKeys)
}

func workerFacebookEnv() map[string]string {
	return with(workerEnv(), "META_APP_ID", "1234567890", "META_APP_SECRET", metaSecret, "FIELD_ENC_KEYS", fieldKeys)
}

func ingestFacebookEnv() map[string]string {
	return with(ingestEnv(), "META_APP_SECRET", metaSecret, "META_VERIFY_TOKEN", metaVerify,
		"HOOKS_PUBLIC_BASE_URL", "https://hooks.elimaz.com.vn")
}

// mustFail: Load lỗi ErrInvalid nêu đủ các tên biến names, không in giá trị bí mật.
func mustFail(t *testing.T, role config.Role, m map[string]string, names ...string) {
	t.Helper()
	_, err := config.Load(role, env(m))
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("%s: mong ErrInvalid, có %v", role, err)
	}
	for _, n := range names {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("%s: lỗi phải nêu %s: %v", role, n, err)
		}
	}
	for _, v := range []string{metaSecret, metaVerify, "AQEBAQ"} {
		if strings.Contains(err.Error(), v) {
			t.Errorf("%s: lỗi in giá trị bí mật: %v", role, err)
		}
	}
}

// Không bật Facebook (không biến Meta riêng của vai trò): mọi vai trò khởi động như trước M4 (Quyết định 2 của tệp task
// 143), có mặc định Graph, hạn giữ hội thoại; Meta.Enabled = false.
func TestLoad_M4Defaults(t *testing.T) {
	for role, m := range map[config.Role]map[string]string{
		config.RoleAPI: apiEnv(), config.RoleWorker: workerEnv(), config.RoleIngest: ingestEnv(),
	} {
		cfg, err := config.Load(role, env(m))
		if err != nil {
			t.Fatalf("%s không có biến Meta: %v", role, err)
		}
		mc := cfg.Meta
		if mc.Enabled || mc.GraphVersion != "v26.0" || mc.GraphBaseURL != "https://graph.facebook.com" ||
			mc.StreamingBaseURL != "https://streaming-graph.facebook.com" ||
			!mc.GraphVersionExpires.Equal(time.Date(2028, 7, 29, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("%s mặc định Meta sai: %+v", role, mc)
		}
	}
	cfg, err := config.Load(config.RoleWorker, env(workerEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConversationRetentionMonths != 24 ||
		!slices.Equal(cfg.Meta.AttachmentHosts, []string{"fbcdn.net", "fbsbx.com", "facebook.com"}) {
		t.Errorf("worker mặc định: giữ %d tháng, máy chủ tệp %v", cfg.ConversationRetentionMonths, cfg.Meta.AttachmentHosts)
	}
}

// Bật Facebook đủ biến: ba vai trò khởi động, giữ giá trị; bí mật không lộ qua LogValue.
func TestLoad_M4FacebookValid(t *testing.T) {
	for role, m := range map[config.Role]map[string]string{
		config.RoleAPI: apiFacebookEnv(), config.RoleWorker: workerFacebookEnv(), config.RoleIngest: ingestFacebookEnv(),
	} {
		cfg, err := config.Load(role, env(m))
		if err != nil {
			t.Fatalf("%s đủ biến Meta: %v", role, err)
		}
		if !cfg.Meta.Enabled || string(cfg.Meta.AppSecret) != metaSecret {
			t.Errorf("%s: Meta %+v", role, cfg.Meta)
		}
		if s := cfg.LogValue().String(); strings.Contains(s, metaSecret) || strings.Contains(s, metaVerify) ||
			!strings.Contains(s, "meta_enabled=true") {
			t.Errorf("%s LogValue: %s", role, s)
		}
	}
	cfg, err := config.Load(config.RoleIngest, env(ingestFacebookEnv()))
	if err != nil || string(cfg.Meta.VerifyToken) != metaVerify || cfg.Meta.HooksPublicBaseURL != "https://hooks.elimaz.com.vn" {
		t.Errorf("ingest: %v %+v", err, cfg.Meta)
	}
}

// Thiếu biến khi đã bật Facebook (kế hoạch Task 143 Step 1): api thiếu META_APP_SECRET và FIELD_ENC_KEYS → lỗi nêu cả
// hai; ingest không đòi FIELD_ENC_KEYS; worker thiếu FIELD_ENC_KEYS → lỗi.
func TestLoad_M4MissingWhenEnabled(t *testing.T) {
	m := apiFacebookEnv()
	delete(m, "META_APP_SECRET")
	delete(m, "FIELD_ENC_KEYS")
	mustFail(t, config.RoleAPI, m, "META_APP_SECRET", "FIELD_ENC_KEYS")
	mustFail(t, config.RoleAPI, with(apiEnv(), "META_APP_ID", "1"),
		"META_APP_SECRET", "META_LOGIN_CONFIG_ID", "META_OAUTH_REDIRECT_URL", "FIELD_ENC_KEYS")
	if _, err := config.Load(config.RoleIngest, env(ingestFacebookEnv())); err != nil {
		t.Errorf("ingest không đòi FIELD_ENC_KEYS: %v", err)
	}
	mustFail(t, config.RoleIngest, with(ingestEnv(), "META_APP_SECRET", metaSecret), "META_VERIFY_TOKEN", "HOOKS_PUBLIC_BASE_URL")
	w := workerFacebookEnv()
	delete(w, "FIELD_ENC_KEYS")
	mustFail(t, config.RoleWorker, w, "FIELD_ENC_KEYS")
}

// Giá trị sai luôn bị bắt: META_APP_SECRET 31 ký tự hay không phải hex, META_VERIFY_TOKEN ngắn, phiên bản Graph sai
// dạng, ngày hết hạn sai, META_OAUTH_REDIRECT_URL khác gốc mọi CONSOLE_ORIGINS, FILES_PUBLIC_BASE_URL trùng gốc một
// CONSOLE_ORIGINS, CONVERSATION_RETENTION_MONTHS ngoài 6–60.
func TestLoad_M4BadValues(t *testing.T) {
	mustFail(t, config.RoleAPI, with(apiFacebookEnv(), "META_APP_SECRET", metaSecret[:31]), "META_APP_SECRET")
	mustFail(t, config.RoleAPI, with(apiFacebookEnv(), "META_APP_SECRET", strings.Repeat("z", 32)), "META_APP_SECRET")
	mustFail(t, config.RoleIngest, with(ingestFacebookEnv(), "META_VERIFY_TOKEN", "ngan"), "META_VERIFY_TOKEN")
	mustFail(t, config.RoleWorker, with(workerEnv(), "META_GRAPH_VERSION", "26"), "META_GRAPH_VERSION")
	mustFail(t, config.RoleWorker, with(workerEnv(), "META_GRAPH_VERSION_EXPIRES", "29/07/2028"), "META_GRAPH_VERSION_EXPIRES")
	mustFail(t, config.RoleAPI, with(apiFacebookEnv(), "META_OAUTH_REDIRECT_URL", "https://evil.example/cb"), "META_OAUTH_REDIRECT_URL")
	mustFail(t, config.RoleAPI, with(apiEnv(), "FILES_PUBLIC_BASE_URL", "https://crm.elimaz.com.vn/files"), "FILES_PUBLIC_BASE_URL")
	mustFail(t, config.RoleWorker, with(workerEnv(), "CONVERSATION_RETENTION_MONTHS", "3"), "CONVERSATION_RETENTION_MONTHS")
	mustFail(t, config.RoleWorker, with(workerEnv(), "META_GRAPH_BASE_URL", "ftp://x"), "META_GRAPH_BASE_URL")
	cfg, err := config.Load(config.RoleWorker, env(with(workerEnv(), "CONVERSATION_RETENTION_MONTHS", "60",
		"META_GRAPH_VERSION", "v27.0", "META_ATTACHMENT_HOSTS", "fbcdn.net, cdn.example")))
	if err != nil || cfg.ConversationRetentionMonths != 60 || cfg.Meta.GraphVersion != "v27.0" ||
		!slices.Equal(cfg.Meta.AttachmentHosts, []string{"fbcdn.net", "cdn.example"}) {
		t.Errorf("giá trị đặt được: %v %+v", err, cfg)
	}
}
