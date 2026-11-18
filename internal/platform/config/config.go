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
	ImagerURL          string // rỗng tới Task M1-43
	ImagePresetVersion int    // mặc định 1

	// Hãng vận chuyển giả (ingest, worker), chỉ ngoài production (Q2). Có FakeCarrierURL thì bí mật bắt buộc, ≥ 32 ký tự.
	FakeCarrierURL           string
	FakeCarrierWebhookSecret string

	// Luật cảnh báo (Task 19), chỉ vai trò worker: BackupExpected bật backup-age (BACKUP_EXPECTED, mặc định false);
	// DBDiskBudgetBytes > 0 bật db-size (DB_DISK_BUDGET_BYTES, mặc định 0 = tắt); DBConnBudget là tổng kết nối của
	// ứng dụng cho pg-connections (DB_CONN_BUDGET, mặc định 36 = ingest 5 + api 20 + worker 10 + LISTEN 1, ADR-42).
	BackupExpected    bool
	DBDiskBudgetBytes int64
	DBConnBudget      int

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
		slog.Bool("backup_expected", c.BackupExpected),
		slog.Int64("db_disk_budget_bytes", c.DBDiskBudgetBytes),
		slog.Int("db_conn_budget", c.DBConnBudget),
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
		cfg.BackupExpected = r.boolean("BACKUP_EXPECTED", false)
		cfg.DBDiskBudgetBytes = int64(r.integer("DB_DISK_BUDGET_BYTES", 0, 0, math.MaxInt))
		cfg.DBConnBudget = r.integer("DB_CONN_BUDGET", defaultConnBudget, 1, 10_000)
		cfg.ImagerURL = r.httpURL("IMAGER_URL")
		cfg.ImagePresetVersion = r.integer("IMAGE_PRESET_VERSION", 1, 1, 1000)
		r.fakeCarrier(&cfg)
	}

	if len(r.problems) > 0 {
		return Config{}, fmt.Errorf("%w (vai trò %s): %s", ErrInvalid, role, strings.Join(r.problems, "; "))
	}
	return cfg, nil
}

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
