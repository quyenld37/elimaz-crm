package config_test

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

const (
	cacheURL = "valkey://crm_cache:mat-khau-cache@127.0.0.1:6390/0"
	gateURL  = "valkeys://crm_gate:mat-khau-gate@valkey-gate:6379/0"
)

// Valkey (kế hoạch M5 Task 184), chỉ vai trò api: hai URL tùy chọn (rỗng là chế độ dự phòng); có một trong hai thì
// CACHE_KEY_SECRET bắt buộc, ≥ 32 ký tự. Lỗi nêu tên biến, không in giá trị.
func TestLoadValkey(t *testing.T) {
	cfg, err := config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil || cfg.ValkeyCacheURL != "" || cfg.ValkeyGateURL != "" || cfg.CacheKeySecret != "" {
		t.Fatalf("không đặt Valkey: %+v, %v", cfg, err)
	}
	cfg, err = config.Load(config.RoleAPI, env(with(apiEnv(),
		"VALKEY_CACHE_URL", cacheURL, "VALKEY_GATE_URL", gateURL, "CACHE_KEY_SECRET", secret32)))
	if err != nil || cfg.ValkeyCacheURL != cacheURL || cfg.ValkeyGateURL != gateURL || string(cfg.CacheKeySecret) != secret32 {
		t.Fatalf("đủ biến: %+v, %v", cfg, err)
	}
	if s := cfg.LogValue().String(); strings.Contains(s, "mat-khau") || strings.Contains(s, secret32) {
		t.Fatalf("LogValue lộ mật khẩu Valkey hay khóa băm: %s", s)
	}
	for _, tc := range []struct {
		name string
		kv   []string
		want string
	}{
		{"thiếu khóa băm", []string{"VALKEY_CACHE_URL", cacheURL}, "CACHE_KEY_SECRET"},
		{"chỉ gate, thiếu khóa", []string{"VALKEY_GATE_URL", gateURL}, "CACHE_KEY_SECRET"},
		{"khóa 20 ký tự", []string{"VALKEY_CACHE_URL", cacheURL, "CACHE_KEY_SECRET", strings.Repeat("k", 20)}, "CACHE_KEY_SECRET"},
		{"sai scheme", []string{"VALKEY_CACHE_URL", "redis://crm_cache:mat-khau-x@h:1/0", "CACHE_KEY_SECRET", secret32}, "VALKEY_CACHE_URL"},
		{"không người dùng", []string{"VALKEY_GATE_URL", "valkey://h:1/0", "CACHE_KEY_SECRET", secret32}, "VALKEY_GATE_URL"},
		{"không host", []string{"VALKEY_GATE_URL", "valkey://crm_gate:mat-khau-x@/0", "CACHE_KEY_SECRET", secret32}, "VALKEY_GATE_URL"},
	} {
		_, err := config.Load(config.RoleAPI, env(with(apiEnv(), tc.kv...)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: lỗi %v, muốn nêu %s", tc.name, err, tc.want)
			continue
		}
		if strings.Contains(err.Error(), "mat-khau") {
			t.Errorf("%s: lỗi lộ mật khẩu: %v", tc.name, err)
		}
	}
	// worker, ingest không nối Valkey: biến có đặt cũng bỏ qua.
	for role, m := range map[config.Role]map[string]string{config.RoleWorker: workerEnv(), config.RoleIngest: ingestEnv()} {
		cfg, err := config.Load(role, env(with(m, "VALKEY_CACHE_URL", cacheURL)))
		if err != nil || cfg.ValkeyCacheURL != "" {
			t.Errorf("%s: %+v, %v", role, cfg.ValkeyCacheURL, err)
		}
	}
}

// RATE_LIMITS_JSON (Task 189): chỉ vai trò api đọc, giữ nguyên chuỗi (cmd/crm kiểm bằng cache.ParseLimits).
func TestLoadRateLimitsJSON(t *testing.T) {
	const raw = `{"web_key":{"max":1500}}`
	cfg, err := config.Load(config.RoleAPI, env(with(apiEnv(), "RATE_LIMITS_JSON", raw)))
	if err != nil || cfg.RateLimitsJSON != raw {
		t.Fatalf("api: %q, %v", cfg.RateLimitsJSON, err)
	}
	cfg, err = config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil || cfg.RateLimitsJSON != "" {
		t.Fatalf("không đặt: %q, %v", cfg.RateLimitsJSON, err)
	}
}

// FLASH_ADMIT_PER_SECOND, FLASH_ADMIT_GLOBAL_PER_SECOND, FLASH_MAX_INFLIGHT (Task 203, QĐ23): vắng → 100, 300, 64; đặt
// → giữ số; ngoài khoảng hay không phải số → không khởi động, lỗi nêu tên biến.
func TestLoadFlashAdmission(t *testing.T) {
	cfg, err := config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil || cfg.FlashAdmitPerSecond != 100 || cfg.FlashAdmitGlobalPerSecond != 300 || cfg.FlashMaxInflight != 64 {
		t.Fatalf("mặc định: %d %d %d, %v", cfg.FlashAdmitPerSecond, cfg.FlashAdmitGlobalPerSecond, cfg.FlashMaxInflight, err)
	}
	cfg, err = config.Load(config.RoleAPI, env(with(with(with(apiEnv(),
		"FLASH_ADMIT_PER_SECOND", "5"), "FLASH_ADMIT_GLOBAL_PER_SECOND", "20"), "FLASH_MAX_INFLIGHT", "8")))
	if err != nil || cfg.FlashAdmitPerSecond != 5 || cfg.FlashAdmitGlobalPerSecond != 20 || cfg.FlashMaxInflight != 8 {
		t.Fatalf("đặt: %d %d %d, %v", cfg.FlashAdmitPerSecond, cfg.FlashAdmitGlobalPerSecond, cfg.FlashMaxInflight, err)
	}
	for _, c := range []struct{ key, val string }{
		{"FLASH_ADMIT_PER_SECOND", "0"},
		{"FLASH_ADMIT_PER_SECOND", "abc"},
		{"FLASH_ADMIT_GLOBAL_PER_SECOND", "-1"},
		{"FLASH_MAX_INFLIGHT", "0"},
		{"FLASH_MAX_INFLIGHT", "100000"},
	} {
		mustFail(t, config.RoleAPI, with(apiEnv(), c.key, c.val), c.key)
	}
}
