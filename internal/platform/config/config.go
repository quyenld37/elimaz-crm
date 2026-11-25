// Package config đọc và kiểm biến môi trường theo vai trò của tiến trình (ingest, api, worker, migrate).
//
// Đây là gói duy nhất được đọc biến môi trường. Mọi gói khác nhận Config đã kiểm.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/fieldcrypt/keyspec"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Role là vai trò của tiến trình, chọn bằng CRM_ROLE.
type Role string

// Các vai trò của tệp chạy crm.
const (
	RoleIngest  Role = "ingest"
	RoleAPI     Role = "api"
	RoleWorker  Role = "worker"
	RoleMigrate Role = "migrate"
)

// ErrInvalid bọc mọi lỗi cấu hình: biến thiếu hoặc sai giá trị.
var ErrInvalid = errors.New("config: cấu hình không hợp lệ")

// Giới hạn độ dài của bí mật dùng chung (kế hoạch M0, Task 3).
const minSecretLen = 32

// Sampler mặc định: không lấy mẫu ở tải thường (ADR-38); tên theo chuẩn OTEL_TRACES_SAMPLER.
const (
	defaultSampler    = "parentbased_traceidratio"
	defaultSamplerArg = 1.0
)

// Config là cấu hình đã kiểm của một vai trò. Trường không thuộc vai trò để rỗng.
type Config struct {
	Role        Role
	Env         string
	DatabaseURL string
	LogDir      string

	TelegramBotToken     string
	TelegramChatID       string
	TelegramUrgentChatID string

	OTelEnabled      bool
	OTelEndpoint     string
	TracesSampler    string
	TracesSamplerArg float64

	HeartbeatURL string
	DBMaxConns   int32
	// Port là cổng HTTP của ingest và api; 0 với vai trò không phục vụ HTTP.
	Port int

	// ingest
	SpoolDir              string
	SelftestWebhookSecret string

	// api
	AccessTeamDomain string
	AccessAUD        string
	AccessMFAClaim   string
	AccessMFAValues  []string
	ConsoleOrigins   []string
	HealthToken      string
	// WebsiteOrigin (api; WEBSITE_ORIGIN, tùy chọn, mặc định DefaultWebsiteOrigin; Task 487) là gốc của website khách,
	// không "/" cuối: màn hình nhân viên ghép liên kết đầy đủ (chiến dịch /uu-dai/…) từ đây thay vì ghi cứng.
	WebsiteOrigin string

	// Kho tệp (api, worker; kế hoạch M1, Global Constraints "Biến môi trường mới"). Từ Task M1-41 bắt buộc với hai vai
	// trò này: S3_ENDPOINT, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY, FILES_PUBLIC_BASE_URL.
	S3Endpoint        string
	S3Region          string // mặc định us-east-1
	S3AccessKeyID     string // danh tính crm-app của kho
	S3SecretAccessKey string
	S3BucketPrefix    string // mặc định crm-
	// FilesPublicBaseURL không có "/" cuối: đường dẫn ảnh = FilesPublicBaseURL + "/" + key.
	FilesPublicBaseURL string

	// worker
	ImagerURL          string // địa chỉ của imager (ADR-45)
	ImagePresetVersion int    // mặc định 1
	// S3ImagerEndpoint là gốc của đường dẫn ký sẵn đưa imager (S3_IMAGER_ENDPOINT, mặc định S3Endpoint): SigV4 ký cả
	// host, mà imager chỉ tới được cổng S3 của kho qua bộ chuyển cổng s3-imaging trên mạng imaging (compose:
	// http://s3-imaging:8333; Task M1-43 Q2, Task 410).
	S3ImagerEndpoint string
	// ImagerMaxParallel là số việc worker gửi imager cùng lúc (IMAGER_MAX_PARALLEL, mặc định 2): đặt bằng biến cùng tên
	// của imager để imager không phải trả 503 (503 kéo dài quá hạn 30 s thì lệnh chờ lại, không tính lượt, Task 409).
	ImagerMaxParallel int
	// ImageRetryMax là trần thời gian thử lại xử lý ảnh khi lỗi hạ tầng tạm (IMAGE_RETRY_MAX_HOURS, giờ, mặc định
	// DefaultImageRetryMax = 23, nhỏ hơn TmpObjectTTL; Task 409, Task 426): quá trần thì ảnh rejected processing_failed
	// và cảnh báo.
	ImageRetryMax time.Duration

	// Hãng vận chuyển giả (ingest, worker), chỉ ngoài production (Q2). Có FakeCarrierURL thì bí mật bắt buộc, ≥ 32 ký tự.
	FakeCarrierURL           string
	FakeCarrierWebhookSecret string
	// FakeChatWebhookSecret là bí mật chữ ký webhook của kênh nhắn tin giả fakechat (FAKECHAT_WEBHOOK_SECRET, ≥ 32 ký tự
	// khi đặt; ingest, api, worker), chỉ ngoài production (kế hoạch M4, Task 145). Rỗng: webhook kênh giả luôn bị từ chối.
	FakeChatWebhookSecret logging.Secret
	// CarrierWebhookSecrets là bí mật kiểm chữ ký webhook của hãng thật theo khóa hãng (vtp, best, spx), chỉ vai trò
	// ingest, đọc từ CARRIER_<KEY>_WEBHOOK_SECRET tùy chọn (≥ 32 ký tự khi đặt); nil với vai trò khác.
	CarrierWebhookSecrets map[string]logging.Secret

	// Luật cảnh báo (Task 19), chỉ vai trò worker: BackupExpected bật backup-age (BACKUP_EXPECTED, mặc định false);
	// DBDiskBudgetBytes > 0 bật db-size (DB_DISK_BUDGET_BYTES, mặc định 0 = tắt); DBConnBudget là tổng kết nối của
	// ứng dụng cho pg-connections (DB_CONN_BUDGET, mặc định 36 = ingest 5 + api 20 + worker 10 + LISTEN 1, ADR-42).
	BackupExpected    bool
	DBDiskBudgetBytes int64
	DBConnBudget      int

	// FileSyncExpected bật luật cảnh báo chép tệp sang kho ngoài (Task M1-45); mặc định false.
	FileSyncExpected bool
	// JobsDoneRetentionDays (worker, JOBS_DONE_RETENTION_DAYS, mặc định 7, trong [1, 30]): việc done/expired giữ bấy
	// nhiêu ngày rồi lịch dọn xóa (Task 394).
	JobsDoneRetentionDays int

	// FieldEncKeys (api, worker; FIELD_ENC_KEYS, Task M2-33) là chuỗi khóa mã hóa trường đã kiểm định dạng
	// (fieldcrypt/keyspec); dựng bằng fieldcrypt.ParseKeyring(logging.Secret(…)) ở cmd/crm. Rỗng khi vắng mặt: M0–M2
	// không cần; vai trò nào bắt buộc thì mốc dùng (M3, M4, M7) thêm một dòng yêu cầu trong Load.
	FieldEncKeys string

	// Valkey (api; kế hoạch M5 Task 184, ADR-39): VALKEY_CACHE_URL, VALKEY_GATE_URL tùy chọn, dạng
	// valkey://user:pass@host:port/0 (hay valkeys://); rỗng thì đường đó chạy chế độ dự phòng (cmd/crm ghi cảnh báo
	// một lần). CacheKeySecret (CACHE_KEY_SECRET, ≥ 32 ký tự, bắt buộc khi có một trong hai URL) là khóa HMAC của
	// cache.SubjectKey. worker, ingest không nối Valkey.
	ValkeyCacheURL string
	ValkeyGateURL  string
	CacheKeySecret logging.Secret
	// RateLimitsJSON (api; RATE_LIMITS_JSON, tùy chọn, Task 189) đè số của giới hạn tần suất website; cmd/crm đọc bằng
	// cache.ParseLimits (sai → không khởi động). Rỗng: cache.DefaultLimits.
	RateLimitsJSON string

	// Meta là cấu hình kênh Facebook (M4, Task 143): ingest, api, worker; rỗng với migrate.
	Meta MetaConfig
	// ConversationRetentionMonths (worker; CONVERSATION_RETENTION_MONTHS, mặc định 24, trong [6, 60]; Q17 của M4): hội
	// thoại không có tin mới quá bấy nhiêu tháng thì việc dọn hằng đêm xóa.
	ConversationRetentionMonths int

	// Migrate chỉ khác nil khi Role là RoleMigrate: vai trò khác không bao giờ giữ chuỗi kết nối của chủ bảng.
	Migrate *MigrateConfig
}

// MigrateConfig là phần cấu hình chỉ vai trò migrate có.
type MigrateConfig struct {
	OwnerURL string
}

// MetaConfig là cấu hình kênh Facebook (kế hoạch M4, Task 143). Enabled khi vai trò có ít nhất một biến Meta riêng của
// vai trò đó (api: META_APP_ID, META_APP_SECRET, META_LOGIN_CONFIG_ID, META_OAUTH_REDIRECT_URL; worker: META_APP_ID,
// META_APP_SECRET; ingest: META_APP_SECRET, META_VERIFY_TOKEN, HOOKS_PUBLIC_BASE_URL); khi đó mọi biến đó (và
// FIELD_ENC_KEYS ở api, worker) bắt buộc. Không bật: vai trò chạy như trước M4, không nối adapter Facebook (tệp task
// 143, Quyết định 2). Phiên bản, địa chỉ Graph có mặc định ở mọi vai trò trừ migrate.
type MetaConfig struct {
	Enabled bool
	// GraphVersion (META_GRAPH_VERSION, mặc định v26.0, dạng vNN.N; Q6) và GraphVersionExpires
	// (META_GRAPH_VERSION_EXPIRES, YYYY-MM-DD, mặc định 2028-07-29): hạn của phiên bản ghim, để nhắc trước 6 tháng.
	GraphVersion        string
	GraphVersionExpires time.Time
	// GraphBaseURL (META_GRAPH_BASE_URL), StreamingBaseURL (META_STREAMING_BASE_URL): gốc Graph API và luồng comment
	// live; test, máy chủ Meta giả đặt địa chỉ khác.
	GraphBaseURL     string
	StreamingBaseURL string
	// api, worker.
	AppID     string
	AppSecret logging.Secret // META_APP_SECRET: đúng 32 ký tự hex (chữ ký webhook, appsecret_proof)
	// api: đăng nhập Facebook for Business mở trong tab; OAuthRedirectURL cùng gốc với một CONSOLE_ORIGINS.
	LoginConfigID    string
	OAuthRedirectURL string
	// ingest: mã xác minh webhook (≥ 32 ký tự) và gốc công khai của /hooks (khai với Meta).
	VerifyToken        logging.Secret
	HooksPublicBaseURL string
	// worker (META_ATTACHMENT_HOSTS, phân cách dấu phẩy): máy chủ được tải tệp đính kèm của khách (và tên miền con).
	AttachmentHosts []string
}

// LogValue chỉ in các trường không bí mật, để Config ghi vào log được.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("role", string(c.Role)),
		slog.String("env", c.Env),
		slog.Int("port", c.Port),
		slog.Int("db_max_conns", int(c.DBMaxConns)),
		slog.Bool("otel_enabled", c.OTelEnabled),
		slog.String("otel_traces_sampler", c.TracesSampler),
		slog.Float64("otel_traces_sampler_arg", c.TracesSamplerArg),
		slog.Bool("log_dir_set", c.LogDir != ""),
		slog.Bool("s3_configured", c.S3Endpoint != ""),
		slog.Bool("imager_configured", c.ImagerURL != ""),
		slog.Int("image_preset_version", c.ImagePresetVersion),
		slog.Bool("fakecarrier_configured", c.FakeCarrierURL != ""),
		slog.Int("carrier_webhook_secrets", len(c.CarrierWebhookSecrets)),
		slog.Bool("filesync_expected", c.FileSyncExpected),
		slog.Bool("backup_expected", c.BackupExpected),
		slog.Int64("db_disk_budget_bytes", c.DBDiskBudgetBytes),
		slog.Int("db_conn_budget", c.DBConnBudget),
		slog.Int("jobs_done_retention_days", c.JobsDoneRetentionDays),
		slog.Bool("field_enc_keys_set", c.FieldEncKeys != ""),
		slog.Bool("valkey_cache_configured", c.ValkeyCacheURL != ""),
		slog.Bool("valkey_gate_configured", c.ValkeyGateURL != ""),
		slog.Bool("meta_enabled", c.Meta.Enabled),
		slog.String("meta_graph_version", c.Meta.GraphVersion),
	)
}

// ParseRole đổi giá trị CRM_ROLE thành Role.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleIngest, RoleAPI, RoleWorker, RoleMigrate:
		return r, nil
	default:
		return "", fmt.Errorf("%w: CRM_ROLE %q không phải ingest, api, worker hay migrate", ErrInvalid, s)
	}
}

// FromOS đọc biến môi trường của tiến trình; truyền vào Load từ cmd/crm.
func FromOS(key string) string { return os.Getenv(key) }

// Load đọc cấu hình của role qua getenv. Lỗi bọc ErrInvalid và liệt kê mọi biến thiếu hay sai cùng lúc.
func Load(role Role, getenv func(string) string) (Config, error) {
	if _, err := ParseRole(string(role)); err != nil {
		return Config{}, err
	}
	r := reader{getenv: getenv}
	cfg := Config{
		Role:                 role,
		Env:                  r.env(),
		DatabaseURL:          r.required("DATABASE_URL"),
		LogDir:               r.optional("LOG_DIR"),
		TelegramBotToken:     r.optional("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:       r.optional("TELEGRAM_CHAT_ID"),
		TelegramUrgentChatID: r.optional("TELEGRAM_URGENT_CHAT_ID"),
		OTelEnabled:          r.boolean("OTEL_ENABLED", true),
		OTelEndpoint:         r.optional("OTEL_EXPORTER_OTLP_ENDPOINT"),
		HeartbeatURL:         r.optional("HEARTBEAT_URL"),
		DBMaxConns:           int32(r.integer("DB_MAX_CONNS", defaultConns(role), 1, 100)), //nolint:gosec // integer giới hạn trong [1, 100]
		FileSyncExpected:     r.boolean("FILESYNC_EXPECTED", false),
	}
	cfg.TracesSampler, cfg.TracesSamplerArg = r.sampler()

	switch role {
	case RoleIngest:
		cfg.Port = r.integer("PORT", 4191, 1, 65535)
		cfg.SpoolDir = r.required("SPOOL_DIR")
		cfg.SelftestWebhookSecret = r.secret("SELFTEST_WEBHOOK_SECRET")
		r.fakeCarrier(&cfg)
		cfg.CarrierWebhookSecrets = r.carrierWebhookSecrets()
	case RoleAPI:
		cfg.Port = r.integer("PORT", 4190, 1, 65535)
		cfg.AccessTeamDomain = r.required("ACCESS_TEAM_DOMAIN")
		cfg.AccessAUD = r.required("ACCESS_AUD")
		cfg.AccessMFAClaim = r.required("ACCESS_MFA_CLAIM")
		cfg.AccessMFAValues = r.list("ACCESS_MFA_VALUES")
		cfg.ConsoleOrigins = r.list("CONSOLE_ORIGINS")
		cfg.HealthToken = r.secret("HEALTH_TOKEN")
		cfg.WebsiteOrigin = r.websiteOrigin()
		r.files(&cfg)
		// getProduct chọn bộ kích thước ảnh theo cùng biến với worker (Task M1-44).
		cfg.ImagePresetVersion = r.integer("IMAGE_PRESET_VERSION", 1, 1, 1000)
		cfg.FieldEncKeys = r.fieldEncKeys()
		r.valkey(&cfg)
	case RoleMigrate:
		cfg.Migrate = &MigrateConfig{OwnerURL: r.required("DATABASE_OWNER_URL")}
	case RoleWorker:
		r.files(&cfg)
		cfg.BackupExpected = r.boolean("BACKUP_EXPECTED", false)
		cfg.DBDiskBudgetBytes = int64(r.integer("DB_DISK_BUDGET_BYTES", 0, 0, math.MaxInt))
		cfg.DBConnBudget = r.integer("DB_CONN_BUDGET", defaultConnBudget, 1, 10_000)
		cfg.JobsDoneRetentionDays = r.integer("JOBS_DONE_RETENTION_DAYS", 7, 1, 30)
		cfg.ImagerURL = r.httpURL("IMAGER_URL")
		cfg.ImagePresetVersion = r.integer("IMAGE_PRESET_VERSION", 1, 1, 1000)
		cfg.ImagerMaxParallel = r.integer("IMAGER_MAX_PARALLEL", 2, 1, 64)
		cfg.ImageRetryMax = time.Duration(r.integer("IMAGE_RETRY_MAX_HOURS", int(DefaultImageRetryMax/time.Hour), 1, 168)) *
			time.Hour
		if cfg.ImageRetryMax >= TmpObjectTTL {
			r.problems = append(r.problems, fmt.Sprintf("IMAGE_RETRY_MAX_HOURS=%d phải nhỏ hơn hạn sống của crm-tmp (%d giờ): "+
				"quá hạn thì ảnh gốc đã bị kho xóa trước lượt thử cuối", int(cfg.ImageRetryMax/time.Hour), int(TmpObjectTTL/time.Hour)))
		}
		cfg.S3ImagerEndpoint = cfg.S3Endpoint
		if r.optional("S3_IMAGER_ENDPOINT") != "" {
			cfg.S3ImagerEndpoint = r.httpURL("S3_IMAGER_ENDPOINT")
		}
		r.fakeCarrier(&cfg)
		cfg.FieldEncKeys = r.fieldEncKeys()
	}
	if role != RoleMigrate {
		r.m4(&cfg)
	}

	if len(r.problems) > 0 {
		return Config{}, fmt.Errorf("%w (vai trò %s): %s", ErrInvalid, role, strings.Join(r.problems, "; "))
	}
	return cfg, nil
}

// TmpObjectTTL là hạn sống của đối tượng trong bucket <tiền tố>tmp (R10, Q18; Task 410): docker/seaweedfs/bootstrap.sh
// đặt quy tắc Expiration Days và bật TTL fast path theo đó, kho tự xóa. Nguồn Go duy nhất của hạn này; test
// TestTmpObjectTTLMatchesBootstrap đòi bootstrap.sh khớp (Task 426).
const TmpObjectTTL = 24 * time.Hour

// DefaultImageRetryMax là mặc định của IMAGE_RETRY_MAX_HOURS: nhỏ hơn TmpObjectTTL một giờ để lượt thử cuối còn ảnh
// gốc ở crm-tmp (trần tính từ lúc tệp vào processing; đường PUT ký sẵn hạn 15 phút; Task 426).
const DefaultImageRetryMax = TmpObjectTTL - time.Hour

// defaultConnBudget là tổng kết nối của ứng dụng (ADR-42): ingest 5 + api 20 + worker 10 + kết nối LISTEN 1.
const defaultConnBudget = 36

// defaultConns là ngân sách kết nối của từng vai trò (ADR-42).
func defaultConns(role Role) int {
	switch role {
	case RoleIngest:
		return 5
	case RoleAPI:
		return 20
	case RoleWorker:
		return 10
	case RoleMigrate:
		return 2
	}
	return 1
}

// reader gom mọi lỗi để báo một lần.
type reader struct {
	getenv   func(string) string
	problems []string
}

func (r *reader) optional(key string) string { return strings.TrimSpace(r.getenv(key)) }

func (r *reader) required(key string) string {
	v := r.optional(key)
	if v == "" {
		r.problems = append(r.problems, "thiếu "+key)
	}
	return v
}

func (r *reader) secret(key string) string {
	v := r.optional(key)
	switch {
	case v == "":
		r.problems = append(r.problems, "thiếu "+key)
	case len(v) < minSecretLen:
		r.problems = append(r.problems, fmt.Sprintf("%s phải dài ít nhất %d ký tự", key, minSecretLen))
	}
	return v
}

// files đọc nhóm kho tệp, bắt buộc với api và worker (Task M1-41; Task 25 để tùy chọn, quyết định D1).
// fieldEncKeys đọc FIELD_ENC_KEYS (tùy chọn): có mặt thì kiểm định dạng bằng keyspec; lỗi chỉ nêu tên biến và vị trí
// mục, không in giá trị.
func (r *reader) fieldEncKeys() string {
	v := r.optional("FIELD_ENC_KEYS")
	if v == "" {
		return ""
	}
	if _, err := keyspec.Parse(v); err != nil {
		r.problems = append(r.problems, "FIELD_ENC_KEYS sai định dạng: "+err.Error())
	}
	return v
}

func (r *reader) files(cfg *Config) {
	cfg.S3Region = r.optional("S3_REGION")
	if cfg.S3Region == "" {
		cfg.S3Region = "us-east-1"
	}
	cfg.S3BucketPrefix = r.optional("S3_BUCKET_PREFIX")
	if cfg.S3BucketPrefix == "" {
		cfg.S3BucketPrefix = "crm-"
	}
	for _, k := range []string{"S3_ENDPOINT", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "FILES_PUBLIC_BASE_URL"} {
		r.required(k)
	}
	cfg.S3Endpoint = r.httpURL("S3_ENDPOINT")
	cfg.S3AccessKeyID = r.optional("S3_ACCESS_KEY_ID")
	cfg.S3SecretAccessKey = r.optional("S3_SECRET_ACCESS_KEY")
	cfg.FilesPublicBaseURL = r.httpURL("FILES_PUBLIC_BASE_URL")
	if strings.HasSuffix(cfg.FilesPublicBaseURL, "/") {
		r.problems = append(r.problems, "FILES_PUBLIC_BASE_URL không được kết thúc bằng /")
	}
}

// env đọc CRM_ENV: chỉ dev, test, staging, production (Task M1-41). Giá trị lạ như "prod" là lỗi khởi động, vì luật
// theo môi trường (chặn hãng giả ở production) so đúng chữ "production" và sẽ để lọt giá trị viết khác.
func (r *reader) env() string {
	v := r.required("CRM_ENV")
	switch v {
	case "", "dev", "test", "staging", "production":
		return v
	default:
		r.problems = append(r.problems, fmt.Sprintf("CRM_ENV %q không phải dev, test, staging hay production", v))
		return ""
	}
}

// fakeCarrier đọc hãng giả (Q2): cấm ở production; có URL thì bí mật webhook bắt buộc.
func (r *reader) fakeCarrier(cfg *Config) {
	if cfg.Env == "production" {
		for _, k := range []string{"FAKECARRIER_URL", "FAKECARRIER_WEBHOOK_SECRET"} {
			if r.optional(k) != "" {
				r.problems = append(r.problems, k+" không được đặt ở production")
			}
		}
		return
	}
	cfg.FakeCarrierURL = r.httpURL("FAKECARRIER_URL")
	if cfg.FakeCarrierURL != "" || r.optional("FAKECARRIER_WEBHOOK_SECRET") != "" {
		cfg.FakeCarrierWebhookSecret = r.secret("FAKECARRIER_WEBHOOK_SECRET")
	}
}

// carrierWebhookSecrets đọc CARRIER_<KEY>_WEBHOOK_SECRET tùy chọn của từng hãng có webhook (vtp, best, spx); có thì
// phải ≥ 32 ký tự.
func (r *reader) carrierWebhookSecrets() map[string]logging.Secret {
	m := map[string]logging.Secret{}
	for _, k := range []string{"vtp", "best", "spx"} {
		name := "CARRIER_" + strings.ToUpper(k) + "_WEBHOOK_SECRET"
		if r.optional(name) != "" {
			m[k] = logging.Secret(r.secret(name))
		}
	}
	return m
}

// valkey đọc nhóm Valkey của api (Task 184): URL tùy chọn, kiểm dạng; có URL thì CACHE_KEY_SECRET bắt buộc, ≥ 32 ký
// tự. Lỗi chỉ nêu tên biến (URL có mật khẩu).
func (r *reader) valkey(cfg *Config) {
	cfg.ValkeyCacheURL = r.valkeyURL("VALKEY_CACHE_URL")
	cfg.ValkeyGateURL = r.valkeyURL("VALKEY_GATE_URL")
	cfg.RateLimitsJSON = r.optional("RATE_LIMITS_JSON")
	if r.optional("VALKEY_CACHE_URL") != "" || r.optional("VALKEY_GATE_URL") != "" {
		cfg.CacheKeySecret = logging.Secret(r.secret("CACHE_KEY_SECRET"))
	}
}

// valkeyURL đọc biến tùy chọn; có giá trị thì phải là valkey:// hoặc valkeys:// có người dùng và host.
func (r *reader) valkeyURL(key string) string {
	return r.urlVar(key, "valkey://người-dùng:mật-khẩu@host:cổng/0 (hoặc valkeys://)", func(u *url.URL) bool {
		return (u.Scheme == "valkey" || u.Scheme == "valkeys") && u.Host != "" && u.User.Username() != ""
	})
}

// DefaultWebsiteOrigin là tên miền website của shop (spec README) khi WEBSITE_ORIGIN vắng.
const DefaultWebsiteOrigin = "https://elimaz.com.vn"

// websiteOrigin đọc WEBSITE_ORIGIN: gốc http(s) (không đường dẫn, query) và bỏ "/" cuối; vắng → DefaultWebsiteOrigin.
func (r *reader) websiteOrigin() string {
	v := strings.TrimSuffix(r.urlVar("WEBSITE_ORIGIN", "gốc http(s) không đường dẫn, vd https://elimaz.com.vn",
		func(u *url.URL) bool {
			return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && (u.Path == "" || u.Path == "/") &&
				u.RawQuery == "" && u.Fragment == "" && u.User == nil
		}), "/")
	if v == "" && r.optional("WEBSITE_ORIGIN") == "" {
		return DefaultWebsiteOrigin
	}
	return v
}

// httpURL đọc biến tùy chọn; có giá trị thì phải là URL tuyệt đối http hoặc https có host.
func (r *reader) httpURL(key string) string {
	return r.urlVar(key, "URL http(s) tuyệt đối", func(u *url.URL) bool {
		return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	})
}

// urlVar đọc biến tùy chọn là URL; có giá trị mà không phân tích được hay ok sai thì ghi lỗi "<key> phải là <want>"
// (không in giá trị: URL có thể chứa mật khẩu) và trả rỗng.
func (r *reader) urlVar(key, want string, ok func(*url.URL) bool) string {
	v := r.optional(key)
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || !ok(u) {
		r.problems = append(r.problems, key+" phải là "+want)
		return ""
	}
	return v
}

func (r *reader) list(key string) []string {
	var out []string
	for part := range strings.SplitSeq(r.optional(key), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		r.problems = append(r.problems, "thiếu "+key)
	}
	return out
}

func (r *reader) boolean(key string, def bool) bool {
	v := r.optional(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.problems = append(r.problems, key+" phải là true hoặc false")
		return def
	}
	return b
}

func (r *reader) integer(key string, def, lo, hi int) int {
	v := r.optional(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		r.problems = append(r.problems, fmt.Sprintf("%s phải là số nguyên trong [%d, %d]", key, lo, hi))
		return def
	}
	return n
}

// sampler đọc OTEL_TRACES_SAMPLER và OTEL_TRACES_SAMPLER_ARG theo chuẩn của OpenTelemetry (Điểm dừng A, quyết định 2).
func (r *reader) sampler() (string, float64) {
	name := r.optional("OTEL_TRACES_SAMPLER")
	if name == "" {
		name = defaultSampler
	}
	switch name {
	case "always_on", "always_off", "traceidratio",
		"parentbased_always_on", "parentbased_always_off", "parentbased_traceidratio":
	default:
		r.problems = append(r.problems, fmt.Sprintf("OTEL_TRACES_SAMPLER %q không được hỗ trợ", name))
		return "", 0
	}
	arg := defaultSamplerArg
	if v := r.optional("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			r.problems = append(r.problems, "OTEL_TRACES_SAMPLER_ARG phải là số trong [0, 1]")
			return "", 0
		}
		arg = f
	}
	return name, arg
}
