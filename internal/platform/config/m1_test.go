package config_test

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// Biến môi trường M1 (kế hoạch M1, Global Constraints "Biến môi trường mới"; viết trước ở Task M1-25, Q37).

func s3Env(m map[string]string) map[string]string {
	return with(m,
		"S3_ENDPOINT", "http://127.0.0.1:8333",
		"S3_ACCESS_KEY_ID", "crm-app",
		"S3_SECRET_ACCESS_KEY", "s3-secret-value",
		"FILES_PUBLIC_BASE_URL", "https://img.elimaz.com.vn",
	)
}

func TestLoadM1WithoutNewVariablesStillValid(t *testing.T) {
	envs := map[config.Role]map[string]string{config.RoleWorker: base(), config.RoleAPI: apiEnv(), config.RoleIngest: ingestEnv()}
	for role, m := range envs {
		cfg, err := config.Load(role, env(m))
		if err != nil {
			t.Fatalf("%s không khai biến M1 phải hợp lệ: %v", role, err)
		}
		if cfg.FileSyncExpected || cfg.S3Endpoint != "" {
			t.Fatalf("%s: mặc định sai: %+v", role, cfg)
		}
		if role != config.RoleIngest && (cfg.S3Region != "us-east-1" || cfg.S3BucketPrefix != "crm-") {
			t.Fatalf("%s: mặc định S3 sai: %+v", role, cfg)
		}
		if role == config.RoleWorker && cfg.ImagePresetVersion != 1 {
			t.Fatalf("IMAGE_PRESET_VERSION mặc định phải 1, nhận %d", cfg.ImagePresetVersion)
		}
	}
}

func TestLoadWorkerMissingS3EndpointNamesVariable(t *testing.T) {
	m := s3Env(base())
	delete(m, "S3_ENDPOINT")
	_, err := config.Load(config.RoleWorker, env(m))
	if err == nil || !strings.Contains(err.Error(), "S3_ENDPOINT") {
		t.Fatalf("worker thiếu S3_ENDPOINT (đã khai S3 khác) phải lỗi nêu tên biến, nhận: %v", err)
	}
	for _, other := range []string{"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "FILES_PUBLIC_BASE_URL"} {
		if strings.Contains(err.Error(), other) {
			t.Fatalf("lỗi chỉ nêu biến thiếu, có cả %s: %v", other, err)
		}
	}
}

func TestLoadS3GroupAllOrNothing(t *testing.T) {
	_, err := config.Load(config.RoleAPI, env(with(apiEnv(), "FILES_PUBLIC_BASE_URL", "https://img.elimaz.com.vn")))
	if err == nil {
		t.Fatal("khai một biến của nhóm S3 thì phải đủ nhóm")
	}
	for _, name := range []string{"S3_ENDPOINT", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("lỗi thiếu tên %s: %v", name, err)
		}
	}
}

func TestLoadS3Valid(t *testing.T) {
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker} {
		m := s3Env(base())
		if role == config.RoleAPI {
			m = s3Env(apiEnv())
		}
		m = with(m, "S3_REGION", "vn-1", "S3_BUCKET_PREFIX", "stg-")
		cfg, err := config.Load(role, env(m))
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if cfg.S3Endpoint != "http://127.0.0.1:8333" || cfg.S3AccessKeyID != "crm-app" ||
			cfg.S3SecretAccessKey != "s3-secret-value" || cfg.FilesPublicBaseURL != "https://img.elimaz.com.vn" ||
			cfg.S3Region != "vn-1" || cfg.S3BucketPrefix != "stg-" {
			t.Fatalf("%s: đọc sai: %+v", role, cfg)
		}
		if strings.Contains(cfg.LogValue().String(), "s3-secret-value") {
			t.Fatalf("LogValue lộ S3_SECRET_ACCESS_KEY")
		}
	}
}

func TestLoadS3NotReadByIngest(t *testing.T) {
	cfg, err := config.Load(config.RoleIngest, env(with(ingestEnv(), "S3_ENDPOINT", "http://x")))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3Endpoint != "" {
		t.Fatalf("ingest không đọc S3: %q", cfg.S3Endpoint)
	}
}

func TestLoadM1BadValues(t *testing.T) {
	cases := []struct {
		name string
		role config.Role
		env  map[string]string
		want string
	}{
		{"endpoint không phải URL", config.RoleWorker, with(s3Env(base()), "S3_ENDPOINT", "127.0.0.1:8333"), "S3_ENDPOINT"},
		{"đường dẫn công khai có / cuối", config.RoleWorker, with(s3Env(base()), "FILES_PUBLIC_BASE_URL", "https://img.x/"), "FILES_PUBLIC_BASE_URL"},
		{"imager không phải URL", config.RoleWorker, with(base(), "IMAGER_URL", "imager:4193"), "IMAGER_URL"},
		{"preset 0", config.RoleWorker, with(base(), "IMAGE_PRESET_VERSION", "0"), "IMAGE_PRESET_VERSION"},
		{"filesync không phải bool", config.RoleAPI, with(apiEnv(), "FILESYNC_EXPECTED", "có"), "FILESYNC_EXPECTED"},
		{"hãng giả thiếu bí mật", config.RoleWorker, with(base(), "FAKECARRIER_URL", "http://127.0.0.1:4194"), "FAKECARRIER_WEBHOOK_SECRET"},
		{"hãng giả bí mật ngắn", config.RoleIngest, with(ingestEnv(), "FAKECARRIER_URL", "http://127.0.0.1:4194",
			"FAKECARRIER_WEBHOOK_SECRET", "short"), "FAKECARRIER_WEBHOOK_SECRET"},
		{"hãng giả ở production", config.RoleWorker, with(base(), "CRM_ENV", "production",
			"FAKECARRIER_URL", "http://127.0.0.1:4194", "FAKECARRIER_WEBHOOK_SECRET", secret32), "FAKECARRIER_URL"},
		{"bí mật hãng giả ở production", config.RoleIngest, with(ingestEnv(), "CRM_ENV", "production",
			"FAKECARRIER_WEBHOOK_SECRET", secret32), "FAKECARRIER_WEBHOOK_SECRET"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.Load(c.role, env(c.env))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("phải lỗi nêu %s, nhận: %v", c.want, err)
			}
		})
	}
}

func TestLoadM1WorkerAndIngestValues(t *testing.T) {
	cfg, err := config.Load(config.RoleWorker, env(with(base(),
		"IMAGER_URL", "http://127.0.0.1:4193", "IMAGE_PRESET_VERSION", "2", "FILESYNC_EXPECTED", "true",
		"FAKECARRIER_URL", "http://127.0.0.1:4194", "FAKECARRIER_WEBHOOK_SECRET", secret32)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImagerURL != "http://127.0.0.1:4193" || cfg.ImagePresetVersion != 2 || !cfg.FileSyncExpected ||
		cfg.FakeCarrierURL != "http://127.0.0.1:4194" || cfg.FakeCarrierWebhookSecret != secret32 {
		t.Fatalf("worker đọc sai: %+v", cfg)
	}
	if strings.Contains(cfg.LogValue().String(), secret32) {
		t.Fatal("LogValue lộ FAKECARRIER_WEBHOOK_SECRET")
	}
	in, err := config.Load(config.RoleIngest, env(with(ingestEnv(),
		"FAKECARRIER_URL", "http://127.0.0.1:4194", "FAKECARRIER_WEBHOOK_SECRET", secret32, "IMAGER_URL", "http://x")))
	if err != nil {
		t.Fatal(err)
	}
	if in.FakeCarrierWebhookSecret != secret32 || in.ImagerURL != "" {
		t.Fatalf("ingest đọc hãng giả, không đọc imager: %+v", in)
	}
}
