package config_test

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// CARRIER_<KEY>_WEBHOOK_SECRET (ingest, tùy chọn): có thì ≥ 32 ký tự, lưu theo khóa hãng chữ thường; không có thì
// không lỗi; LogValue không lộ giá trị; vai trò khác không đọc.
func TestLoadCarrierWebhookSecrets(t *testing.T) {
	cfg, err := config.Load(config.RoleIngest, env(ingestEnv()))
	if err != nil || len(cfg.CarrierWebhookSecrets) != 0 {
		t.Fatalf("không đặt: %v %v", cfg.CarrierWebhookSecrets, err)
	}
	vtpSecret := "vtp-webhook-secret-0123456789abcdef"
	cfg, err = config.Load(config.RoleIngest, env(with(ingestEnv(), "CARRIER_VTP_WEBHOOK_SECRET", vtpSecret)))
	if err != nil || string(cfg.CarrierWebhookSecrets["vtp"]) != vtpSecret || len(cfg.CarrierWebhookSecrets) != 1 {
		t.Fatalf("vtp: %v %v", cfg.CarrierWebhookSecrets, err)
	}
	if s := cfg.LogValue().String(); strings.Contains(s, vtpSecret) {
		t.Fatalf("LogValue lộ bí mật hãng: %s", s)
	}
	_, err = config.Load(config.RoleIngest, env(with(ingestEnv(), "CARRIER_SPX_WEBHOOK_SECRET", "ngan")))
	if err == nil || !strings.Contains(err.Error(), "CARRIER_SPX_WEBHOOK_SECRET") {
		t.Fatalf("bí mật ngắn phải lỗi nêu tên biến: %v", err)
	}
	cfg, err = config.Load(config.RoleWorker, env(with(workerEnv(), "CARRIER_BEST_WEBHOOK_SECRET", "ngan")))
	if err != nil || cfg.CarrierWebhookSecrets != nil {
		t.Fatalf("worker không đọc bí mật webhook hãng: %v %v", cfg.CarrierWebhookSecrets, err)
	}
}
