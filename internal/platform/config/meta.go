package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// Mặc định của kênh Facebook (kế hoạch M4, Q6, Global Constraints "Graph API").
const (
	defaultGraphVersion     = "v26.0"
	defaultGraphExpires     = "2028-07-29"
	defaultGraphBaseURL     = "https://graph.facebook.com"
	defaultStreamingBaseURL = "https://streaming-graph.facebook.com"
	defaultAttachmentHosts  = "fbcdn.net,fbsbx.com,facebook.com"
	metaAppSecretLen        = 32
)

var (
	graphVersionRe = regexp.MustCompile(`^v[0-9]{1,3}\.[0-9]$`)
	hexRe          = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// metaRoleVars là các biến Meta riêng của vai trò: có một biến thì kênh Facebook bật và mọi biến bắt buộc.
func metaRoleVars(role Role) []string {
	switch role {
	case RoleAPI:
		return []string{"META_APP_ID", "META_APP_SECRET", "META_LOGIN_CONFIG_ID", "META_OAUTH_REDIRECT_URL"}
	case RoleWorker:
		return []string{"META_APP_ID", "META_APP_SECRET"}
	case RoleIngest:
		return []string{"META_APP_SECRET", "META_VERIFY_TOKEN", "HOOKS_PUBLIC_BASE_URL"}
	case RoleMigrate:
	}
	return nil
}

// m4 đọc cấu hình M4 của cfg.Role (ingest, api, worker): kênh Facebook, nơi phục vụ tệp (api), hạn giữ hội thoại
// (worker). Gọi sau khi đọc FIELD_ENC_KEYS (api, worker), CONSOLE_ORIGINS và FILES_PUBLIC_BASE_URL (api).
func (r *reader) m4(cfg *Config) {
	r.graph(&cfg.Meta)
	r.meta(cfg)
	r.fakeChat(cfg)
	switch cfg.Role {
	case RoleAPI:
		r.filesOriginApart(*cfg)
	case RoleWorker:
		cfg.ConversationRetentionMonths = r.integer("CONVERSATION_RETENTION_MONTHS", 24, 6, 60)
		cfg.Meta.DataUseCheckupDue = r.date("META_DATA_USE_CHECKUP_DUE")
	case RoleIngest, RoleMigrate:
	}
}

// date đọc ngày YYYY-MM-DD tùy chọn (UTC, 00:00); vắng → zero; sai dạng → lỗi nêu tên biến.
func (r *reader) date(key string) time.Time {
	v := r.optional(key)
	if v == "" {
		return time.Time{}
	}
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		r.problems = append(r.problems, key+" phải là ngày YYYY-MM-DD")
	}
	return d
}

// graph đọc phiên bản, hạn phiên bản, địa chỉ Graph API (có mặc định, không phụ thuộc Enabled).
func (r *reader) graph(m *MetaConfig) {
	m.GraphVersion = r.optional("META_GRAPH_VERSION")
	if m.GraphVersion == "" {
		m.GraphVersion = defaultGraphVersion
	} else if !graphVersionRe.MatchString(m.GraphVersion) {
		r.problems = append(r.problems, "META_GRAPH_VERSION phải có dạng vNN.N, vd v26.0")
	}
	exp := r.optional("META_GRAPH_VERSION_EXPIRES")
	if exp == "" {
		exp = defaultGraphExpires
	}
	d, err := time.Parse(time.DateOnly, exp)
	if err != nil {
		r.problems = append(r.problems, "META_GRAPH_VERSION_EXPIRES phải là ngày YYYY-MM-DD")
	}
	m.GraphVersionExpires = d
	m.GraphBaseURL = r.urlOrDefault("META_GRAPH_BASE_URL", defaultGraphBaseURL)
	m.StreamingBaseURL = r.urlOrDefault("META_STREAMING_BASE_URL", defaultStreamingBaseURL)
}

// meta đọc biến Meta riêng của vai trò (tệp task 143, Quyết định 2): có một biến thì bật kênh Facebook và mọi biến
// của vai trò (cùng FIELD_ENC_KEYS ở api, worker) bắt buộc.
func (r *reader) meta(cfg *Config) {
	m := &cfg.Meta
	vars := metaRoleVars(cfg.Role)
	for _, k := range vars {
		if r.optional(k) != "" {
			m.Enabled = true
		}
	}
	if cfg.Role == RoleWorker {
		m.AttachmentHosts = splitList(r.optional("META_ATTACHMENT_HOSTS"))
		if len(m.AttachmentHosts) == 0 {
			m.AttachmentHosts = splitList(defaultAttachmentHosts)
		}
	}
	if !m.Enabled {
		return
	}
	for _, k := range vars {
		r.required(k)
	}
	if cfg.Role != RoleIngest && cfg.FieldEncKeys == "" {
		r.problems = append(r.problems, "thiếu FIELD_ENC_KEYS (bắt buộc khi bật kênh Facebook: token Page mã hóa)")
	}
	m.AppID = r.optional("META_APP_ID")
	m.AppSecret = logging.Secret(r.metaAppSecret())
	m.LoginConfigID = r.optional("META_LOGIN_CONFIG_ID")
	switch cfg.Role {
	case RoleAPI:
		m.OAuthRedirectURL = r.httpURL("META_OAUTH_REDIRECT_URL")
		if m.OAuthRedirectURL != "" && !sameOriginAsAny(m.OAuthRedirectURL, cfg.ConsoleOrigins) {
			r.problems = append(r.problems, "META_OAUTH_REDIRECT_URL phải cùng gốc với một CONSOLE_ORIGINS")
		}
	case RoleIngest:
		if r.optional("META_VERIFY_TOKEN") != "" {
			m.VerifyToken = logging.Secret(r.secret("META_VERIFY_TOKEN"))
		}
		m.HooksPublicBaseURL = strings.TrimSuffix(r.httpURL("HOOKS_PUBLIC_BASE_URL"), "/")
	case RoleWorker, RoleMigrate:
	}
}

// metaAppSecret đọc META_APP_SECRET khi có: đúng 32 ký tự hex. Lỗi chỉ nêu tên biến.
func (r *reader) metaAppSecret() string {
	v := r.optional("META_APP_SECRET")
	if v != "" && (len(v) != metaAppSecretLen || !hexRe.MatchString(v)) {
		r.problems = append(r.problems, fmt.Sprintf("META_APP_SECRET phải đúng %d ký tự hex", metaAppSecretLen))
	}
	return v
}

// urlOrDefault đọc biến URL http(s) tùy chọn; vắng → def.
func (r *reader) urlOrDefault(key, def string) string {
	if r.optional(key) == "" {
		return def
	}
	return strings.TrimSuffix(r.httpURL(key), "/")
}

// filesOriginApart (api, M4 Global Constraints "Nội dung từ khách"): tệp (kể cả tệp khách gửi) phục vụ từ gốc khác mọi
// gốc của màn hình, để tệp không chạy được như trang CRM.
func (r *reader) filesOriginApart(cfg Config) {
	if cfg.FilesPublicBaseURL != "" && sameOriginAsAny(cfg.FilesPublicBaseURL, cfg.ConsoleOrigins) {
		r.problems = append(r.problems, "FILES_PUBLIC_BASE_URL phải khác gốc mọi CONSOLE_ORIGINS")
	}
}

// sameOriginAsAny: raw có cùng gốc (scheme, host, cổng) với một trong origins.
func sameOriginAsAny(raw string, origins []string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, o := range origins {
		ou, err := url.Parse(o)
		if err == nil && strings.EqualFold(ou.Scheme, u.Scheme) && strings.EqualFold(ou.Host, u.Host) {
			return true
		}
	}
	return false
}

// splitList tách danh sách phân cách dấu phẩy, bỏ khoảng trắng và phần rỗng.
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fakeChat đọc bí mật webhook của kênh nhắn tin giả (Task 145): cấm ở production; ngoài production tùy chọn, có thì
// ≥ 32 ký tự.
func (r *reader) fakeChat(cfg *Config) {
	const key = "FAKECHAT_WEBHOOK_SECRET"
	switch {
	case r.optional(key) == "":
	case cfg.Env == "production":
		r.problems = append(r.problems, key+" không được đặt ở production")
	default:
		cfg.FakeChatWebhookSecret = logging.Secret(r.secret(key))
	}
}
