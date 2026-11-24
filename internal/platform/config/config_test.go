package config_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

const (
	secret32 = "0123456789abcdef0123456789abcdef"
	dbURL    = "postgres://crm_app:x@127.0.0.1:5442/crm"
	ownerURL = "postgres://crm_owner:owner-secret-value@127.0.0.1:5442/crm"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{"CRM_ENV": "dev", "DATABASE_URL": dbURL}
}

func with(m map[string]string, kv ...string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// workerEnv: worker hợp lệ tối thiểu (nhóm kho tệp bắt buộc từ Task M1-41).
func workerEnv() map[string]string { return s3Env(base()) }

func apiEnv() map[string]string {
	return with(s3Env(base()),
		"ACCESS_TEAM_DOMAIN", "elimaz.cloudflareaccess.com",
		"ACCESS_AUD", "aud-123",
		"ACCESS_MFA_CLAIM", "amr",
		"ACCESS_MFA_VALUES", "mfa, otp",
		"CONSOLE_ORIGINS", "https://crm.elimaz.com.vn",
		"HEALTH_TOKEN", secret32,
	)
}

func ingestEnv() map[string]string {
	return with(base(), "SPOOL_DIR", "/var/spool/crm", "SELFTEST_WEBHOOK_SECRET", secret32)
}

func TestLoadAPIEmptyListsEveryMissingVariable(t *testing.T) {
	_, err := config.Load(config.RoleAPI, env(nil))
	if err == nil {
		t.Fatal("Load(api, rỗng) phải lỗi")
	}
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("lỗi phải bọc ErrInvalid: %v", err)
	}
	for _, name := range []string{
		"CRM_ENV", "DATABASE_URL", "ACCESS_TEAM_DOMAIN", "ACCESS_AUD", "ACCESS_MFA_CLAIM",
		"ACCESS_MFA_VALUES", "CONSOLE_ORIGINS", "HEALTH_TOKEN",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("lỗi thiếu tên %s: %v", name, err)
		}
	}
}

func TestLoadIngestShortSecretFails(t *testing.T) {
	_, err := config.Load(config.RoleIngest, env(with(ingestEnv(), "SELFTEST_WEBHOOK_SECRET", "0123456789")))
	if err == nil || !strings.Contains(err.Error(), "SELFTEST_WEBHOOK_SECRET") {
		t.Fatalf("bí mật 10 ký tự phải lỗi và nêu tên biến, nhận: %v", err)
	}
}

func TestLoadAPIShortHealthTokenFails(t *testing.T) {
	_, err := config.Load(config.RoleAPI, env(with(apiEnv(), "HEALTH_TOKEN", "short")))
	if err == nil || !strings.Contains(err.Error(), "HEALTH_TOKEN") {
		t.Fatalf("HEALTH_TOKEN ngắn phải lỗi, nhận: %v", err)
	}
}

func TestLoadIngestDoesNotNeedAccess(t *testing.T) {
	cfg, err := config.Load(config.RoleIngest, env(ingestEnv()))
	if err != nil {
		t.Fatalf("ingest hợp lệ không được lỗi: %v", err)
	}
	if cfg.Port != 4191 || cfg.DBMaxConns != 5 || cfg.SpoolDir != "/var/spool/crm" {
		t.Fatalf("mặc định ingest sai: port=%d conns=%d spool=%q", cfg.Port, cfg.DBMaxConns, cfg.SpoolDir)
	}
}

func TestLoadAPIValid(t *testing.T) {
	cfg, err := config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil {
		t.Fatalf("api hợp lệ: %v", err)
	}
	if cfg.Port != 4190 || cfg.DBMaxConns != 20 || cfg.Role != config.RoleAPI {
		t.Fatalf("mặc định api sai: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.AccessMFAValues, []string{"mfa", "otp"}) {
		t.Fatalf("ACCESS_MFA_VALUES = %v", cfg.AccessMFAValues)
	}
	if !reflect.DeepEqual(cfg.ConsoleOrigins, []string{"https://crm.elimaz.com.vn"}) {
		t.Fatalf("CONSOLE_ORIGINS = %v", cfg.ConsoleOrigins)
	}
}

func TestLoadDefaultsPerRole(t *testing.T) {
	tests := []struct {
		role  config.Role
		env   map[string]string
		conns int32
		port  int
	}{
		{config.RoleIngest, ingestEnv(), 5, 4191},
		{config.RoleAPI, apiEnv(), 20, 4190},
		{config.RoleWorker, workerEnv(), 10, 0},
		{config.RoleMigrate, with(base(), "DATABASE_OWNER_URL", ownerURL), 2, 0},
	}
	for _, tc := range tests {
		t.Run(string(tc.role), func(t *testing.T) {
			cfg, err := config.Load(tc.role, env(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DBMaxConns != tc.conns || cfg.Port != tc.port {
				t.Fatalf("conns=%d port=%d, muốn %d %d", cfg.DBMaxConns, cfg.Port, tc.conns, tc.port)
			}
			if !cfg.OTelEnabled || cfg.TracesSampler != "parentbased_traceidratio" || cfg.TracesSamplerArg != 1.0 {
				t.Fatalf("mặc định dấu vết sai: %v %q %v", cfg.OTelEnabled, cfg.TracesSampler, cfg.TracesSamplerArg)
			}
		})
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := config.Load(config.RoleWorker, env(with(workerEnv(),
		"DB_MAX_CONNS", "7", "OTEL_ENABLED", "false", "LOG_DIR", "/var/log/crm",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "http://jaeger:4318", "HEARTBEAT_URL", "https://hc.example/x",
		"TELEGRAM_BOT_TOKEN", "bot", "TELEGRAM_CHAT_ID", "1", "TELEGRAM_URGENT_CHAT_ID", "2",
	)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBMaxConns != 7 || cfg.OTelEnabled || cfg.LogDir != "/var/log/crm" ||
		cfg.OTelEndpoint != "http://jaeger:4318" || cfg.HeartbeatURL != "https://hc.example/x" ||
		cfg.TelegramBotToken != "bot" || cfg.TelegramChatID != "1" || cfg.TelegramUrgentChatID != "2" {
		t.Fatalf("giá trị không được đọc đúng: %+v", cfg)
	}
}

// Điểm dừng A, quyết định 2: tỷ lệ lấy mẫu đọc từ biến chuẩn của OpenTelemetry.
func TestLoadTracesSampler(t *testing.T) {
	tests := []struct {
		sampler, arg string
		wantName     string
		wantArg      float64
		wantErr      bool
	}{
		{"", "", "parentbased_traceidratio", 1.0, false},
		{"traceidratio", "0.25", "traceidratio", 0.25, false},
		{"parentbased_traceidratio", "0.1", "parentbased_traceidratio", 0.1, false},
		{"always_off", "", "always_off", 1.0, false},
		{"parentbased_always_on", "", "parentbased_always_on", 1.0, false},
		{"jaeger_remote", "", "", 0, true},
		{"traceidratio", "1.5", "", 0, true},
		{"traceidratio", "-0.1", "", 0, true},
		{"traceidratio", "abc", "", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.sampler+"/"+tc.arg, func(t *testing.T) {
			cfg, err := config.Load(config.RoleWorker, env(with(workerEnv(),
				"OTEL_TRACES_SAMPLER", tc.sampler, "OTEL_TRACES_SAMPLER_ARG", tc.arg)))
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "OTEL_TRACES_SAMPLER") {
					t.Fatalf("phải lỗi nêu OTEL_TRACES_SAMPLER, nhận %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.TracesSampler != tc.wantName || cfg.TracesSamplerArg != tc.wantArg {
				t.Fatalf("nhận %q %v, muốn %q %v", cfg.TracesSampler, cfg.TracesSamplerArg, tc.wantName, tc.wantArg)
			}
		})
	}
}

func TestLoadRejectsBadNumbersAndBools(t *testing.T) {
	for _, kv := range [][2]string{{"DB_MAX_CONNS", "0"}, {"DB_MAX_CONNS", "x"}, {"DB_MAX_CONNS", "101"}, {"OTEL_ENABLED", "maybe"}, {"PORT", "70000"}} {
		_, err := config.Load(config.RoleAPI, env(with(apiEnv(), kv[0], kv[1])))
		if err == nil || !strings.Contains(err.Error(), kv[0]) {
			t.Errorf("%s=%s phải lỗi nêu tên biến, nhận %v", kv[0], kv[1], err)
		}
	}
}

func TestLoadMigrateNeedsOwnerURL(t *testing.T) {
	_, err := config.Load(config.RoleMigrate, env(base()))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_OWNER_URL") {
		t.Fatalf("migrate thiếu DATABASE_OWNER_URL phải lỗi, nhận %v", err)
	}
	cfg, err := config.Load(config.RoleMigrate, env(with(base(), "DATABASE_OWNER_URL", ownerURL)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Migrate == nil || cfg.Migrate.OwnerURL != ownerURL {
		t.Fatalf("migrate phải có OwnerURL: %+v", cfg.Migrate)
	}
}

// Vai trò khác migrate không được giữ chuỗi kết nối của chủ bảng ở bất kỳ trường nào.
func TestLoadNonMigrateNeverHoldsOwnerURL(t *testing.T) {
	for _, tc := range []struct {
		role config.Role
		env  map[string]string
	}{
		{config.RoleWorker, workerEnv()},
		{config.RoleAPI, apiEnv()},
		{config.RoleIngest, ingestEnv()},
	} {
		cfg, err := config.Load(tc.role, env(with(tc.env, "DATABASE_OWNER_URL", ownerURL)))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Migrate != nil {
			t.Fatalf("%s: Migrate phải nil", tc.role)
		}
		if path, ok := containsString(reflect.ValueOf(cfg), "owner-secret-value", "Config"); ok {
			t.Fatalf("%s: giá trị chủ bảng nằm ở %s", tc.role, path)
		}
	}
}

func containsString(v reflect.Value, needle, path string) (string, bool) {
	switch v.Kind() {
	case reflect.String:
		return path, strings.Contains(v.String(), needle)
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "", false
		}
		return containsString(v.Elem(), needle, path)
	case reflect.Struct:
		for i := range v.NumField() {
			if p, ok := containsString(v.Field(i), needle, path+"."+v.Type().Field(i).Name); ok {
				return p, true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if p, ok := containsString(v.Index(i), needle, path); ok {
				return p, true
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if p, ok := containsString(v.MapIndex(k), needle, path); ok {
				return p, true
			}
		}
	default:
	}
	return "", false
}

func TestParseRole(t *testing.T) {
	for _, s := range []string{"ingest", "api", "worker", "migrate"} {
		r, err := config.ParseRole(s)
		if err != nil || string(r) != s {
			t.Errorf("ParseRole(%q) = %q, %v", s, r, err)
		}
	}
	if _, err := config.ParseRole("web"); err == nil || !errors.Is(err, config.ErrInvalid) {
		t.Errorf("ParseRole(web) phải lỗi ErrInvalid, nhận %v", err)
	}
}

func TestConfigLogValueHidesSecrets(t *testing.T) {
	cfg, err := config.Load(config.RoleAPI, env(with(apiEnv(), "TELEGRAM_BOT_TOKEN", "bot-token-xyz")))
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.LogValue().String()
	for _, secret := range []string{secret32, "bot-token-xyz", "crm_app:x"} {
		if strings.Contains(s, secret) {
			t.Fatalf("LogValue lộ %q: %s", secret, s)
		}
	}
	if !strings.Contains(s, "api") {
		t.Fatalf("LogValue phải có vai trò: %s", s)
	}
}

// Hạn giữ việc xong (Task 394), chỉ vai trò worker: mặc định 7 ngày, đặt được trong [1, 30], ngoài khoảng là lỗi.
func TestLoadJobsDoneRetentionDays(t *testing.T) {
	cfg, err := config.Load(config.RoleWorker, env(workerEnv()))
	if err != nil || cfg.JobsDoneRetentionDays != 7 {
		t.Fatalf("mặc định: %d, %v", cfg.JobsDoneRetentionDays, err)
	}
	cfg, err = config.Load(config.RoleWorker, env(with(workerEnv(), "JOBS_DONE_RETENTION_DAYS", "2")))
	if err != nil || cfg.JobsDoneRetentionDays != 2 {
		t.Fatalf("đặt 2: %d, %v", cfg.JobsDoneRetentionDays, err)
	}
	for _, v := range []string{"0", "31", "x"} {
		_, err := config.Load(config.RoleWorker, env(with(workerEnv(), "JOBS_DONE_RETENTION_DAYS", v)))
		if err == nil || !strings.Contains(err.Error(), "JOBS_DONE_RETENTION_DAYS") {
			t.Errorf("JOBS_DONE_RETENTION_DAYS=%s phải lỗi nêu tên biến, nhận %v", v, err)
		}
	}
	if cfg, err := config.Load(config.RoleAPI, env(apiEnv())); err != nil || cfg.JobsDoneRetentionDays != 0 {
		t.Fatalf("api không giữ hạn của worker: %d, %v", cfg.JobsDoneRetentionDays, err)
	}
}

// fieldKeys là FIELD_ENC_KEYS hợp lệ (hai khóa 32 byte, base64).
const fieldKeys = "k2:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=,k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

// FIELD_ENC_KEYS (Task M2-33): vắng mặt thì mọi vai trò của M0–M2 vẫn khởi động; có mặt ở api, worker thì được kiểm định
// dạng và giữ trong Config (không lộ qua LogValue); sai định dạng → lỗi nêu tên biến, không in giá trị.
func TestLoadFieldEncKeys(t *testing.T) {
	roles := map[config.Role]map[string]string{
		config.RoleAPI: apiEnv(), config.RoleWorker: workerEnv(), config.RoleIngest: ingestEnv(),
		config.RoleMigrate: with(base(), "DATABASE_OWNER_URL", ownerURL),
	}
	for role, m := range roles {
		cfg, err := config.Load(role, env(m))
		if err != nil || cfg.FieldEncKeys != "" {
			t.Fatalf("%s không có FIELD_ENC_KEYS: %v, FieldEncKeys %q", role, err, cfg.FieldEncKeys)
		}
	}
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker} {
		cfg, err := config.Load(role, env(with(roles[role], "FIELD_ENC_KEYS", " "+fieldKeys+" ")))
		if err != nil || cfg.FieldEncKeys != fieldKeys {
			t.Fatalf("%s FIELD_ENC_KEYS hợp lệ: %v, %q", role, err, cfg.FieldEncKeys)
		}
		if s := cfg.LogValue().String(); strings.Contains(s, "AgICAg") || strings.Contains(s, "AQEBAQ") ||
			!strings.Contains(s, "field_enc_keys_set=true") {
			t.Fatalf("%s LogValue: %s", role, s)
		}
		checkBadFieldKeys(t, role, roles[role])
	}
}

// checkBadFieldKeys: FIELD_ENC_KEYS sai (31 byte, trùng key_id, thiếu key_id) → ErrInvalid nêu tên biến, không in khóa.
func checkBadFieldKeys(t *testing.T, role config.Role, m map[string]string) {
	t.Helper()
	bad := []string{
		"k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ==",
		fieldKeys + ",k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
		"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
	}
	for _, v := range bad {
		_, err := config.Load(role, env(with(m, "FIELD_ENC_KEYS", v)))
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "FIELD_ENC_KEYS") {
			t.Fatalf("%s FIELD_ENC_KEYS sai phải lỗi nêu tên biến: %v", role, err)
		}
		if strings.Contains(err.Error(), "AQEBAQ") || strings.Contains(err.Error(), "AgICAg") {
			t.Fatalf("%s lỗi in giá trị khóa: %v", role, err)
		}
	}
}

// WEBSITE_ORIGIN (api, Task 487): vắng → DefaultWebsiteOrigin; có thì phải là gốc http(s) không đường dẫn (bỏ "/"
// cuối); sai → lỗi nêu tên biến. Vai trò khác không giữ.
func TestLoadWebsiteOrigin(t *testing.T) {
	cfg, err := config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil || cfg.WebsiteOrigin != config.DefaultWebsiteOrigin || config.DefaultWebsiteOrigin != "https://elimaz.com.vn" {
		t.Fatalf("mặc định: %q, %v", cfg.WebsiteOrigin, err)
	}
	cfg, err = config.Load(config.RoleAPI, env(with(apiEnv(), "WEBSITE_ORIGIN", "https://staging.elimaz.com.vn/")))
	if err != nil || cfg.WebsiteOrigin != "https://staging.elimaz.com.vn" {
		t.Fatalf("đặt: %q, %v", cfg.WebsiteOrigin, err)
	}
	for _, v := range []string{"elimaz.com.vn", "ftp://elimaz.com.vn", "https://elimaz.com.vn/uu-dai", "https://"} {
		_, err := config.Load(config.RoleAPI, env(with(apiEnv(), "WEBSITE_ORIGIN", v)))
		if err == nil || !strings.Contains(err.Error(), "WEBSITE_ORIGIN") {
			t.Errorf("WEBSITE_ORIGIN=%s phải lỗi nêu tên biến, nhận %v", v, err)
		}
	}
	if cfg, err := config.Load(config.RoleWorker, env(workerEnv())); err != nil || cfg.WebsiteOrigin != "" {
		t.Fatalf("worker không giữ: %q, %v", cfg.WebsiteOrigin, err)
	}
}
