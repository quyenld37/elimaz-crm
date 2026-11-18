// Package config đọc và kiểm biến môi trường theo vai trò của tiến trình (ingest, api, worker, migrate).
//
// Đây là gói duy nhất được đọc biến môi trường. Mọi gói khác nhận Config đã kiểm.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	// host, mà imager chỉ tới được kho qua mạng imaging (dev: http://seaweedfs:8333; Task M1-43, Q2).
	S3ImagerEndpoint string
	// ImagerMaxParallel là số việc worker gửi imager cùng lúc (IMAGER_MAX_PARALLEL, mặc định 2): đặt bằng biến cùng tên
	// của imager để imager không phải trả 503 (503 vẫn tốn một lượt thử của lệnh nếu kéo dài quá hạn 30 s).
	ImagerMaxParallel int

	// Hãng vận chuyển giả (ingest, worker), chỉ ngoài production (Q2). Có FakeCarrierURL thì bí mật bắt buộc, ≥ 32 ký tự.
	FakeCarrierURL           string
	FakeCarrierWebhookSecret string

	// FileSyncExpected bật luật cảnh báo chép tệp sang kho ngoài (Task M1-45); mặc định false.
	FileSyncExpected bool

	// Migrate chỉ khác nil khi Role là RoleMigrate: vai trò khác không bao giờ giữ chuỗi kết nối của chủ bảng.
	Migrate *MigrateConfig
}

// MigrateConfig là phần cấu hình chỉ vai trò migrate có.
type MigrateConfig struct {
	OwnerURL string
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
		slog.Bool("filesync_expected", c.FileSyncExpected),
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
	case RoleAPI:
		cfg.Port = r.integer("PORT", 4190, 1, 65535)
		cfg.AccessTeamDomain = r.required("ACCESS_TEAM_DOMAIN")
		cfg.AccessAUD = r.required("ACCESS_AUD")
		cfg.AccessMFAClaim = r.required("ACCESS_MFA_CLAIM")
		cfg.AccessMFAValues = r.list("ACCESS_MFA_VALUES")
		cfg.ConsoleOrigins = r.list("CONSOLE_ORIGINS")
		cfg.HealthToken = r.secret("HEALTH_TOKEN")
		r.files(&cfg)
	case RoleMigrate:
		cfg.Migrate = &MigrateConfig{OwnerURL: r.required("DATABASE_OWNER_URL")}
	case RoleWorker:
		r.files(&cfg)
		cfg.ImagerURL = r.httpURL("IMAGER_URL")
		cfg.ImagePresetVersion = r.integer("IMAGE_PRESET_VERSION", 1, 1, 1000)
		cfg.ImagerMaxParallel = r.integer("IMAGER_MAX_PARALLEL", 2, 1, 64)
		cfg.S3ImagerEndpoint = cfg.S3Endpoint
		if r.optional("S3_IMAGER_ENDPOINT") != "" {
			cfg.S3ImagerEndpoint = r.httpURL("S3_IMAGER_ENDPOINT")
		}
		r.fakeCarrier(&cfg)
	}

	if len(r.problems) > 0 {
		return Config{}, fmt.Errorf("%w (vai trò %s): %s", ErrInvalid, role, strings.Join(r.problems, "; "))
	}
	return cfg, nil
}

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

// httpURL đọc biến tùy chọn; có giá trị thì phải là URL tuyệt đối http hoặc https có host.
func (r *reader) httpURL(key string) string {
	v := r.optional(key)
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.problems = append(r.problems, key+" phải là URL http(s) tuyệt đối")
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
