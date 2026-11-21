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
