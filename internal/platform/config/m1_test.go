package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestLoadM1OptionalVariablesHaveDefaults(t *testing.T) {
	envs := map[config.Role]map[string]string{config.RoleWorker: workerEnv(), config.RoleAPI: apiEnv(), config.RoleIngest: ingestEnv()}
	for role, m := range envs {
		cfg, err := config.Load(role, env(m))
		if err != nil {
			t.Fatalf("%s không khai biến M1 tùy chọn phải hợp lệ: %v", role, err)
		}
		if cfg.FileSyncExpected {
			t.Fatalf("%s: mặc định sai: %+v", role, cfg)
		}
		if role != config.RoleIngest && (cfg.S3Region != "us-east-1" || cfg.S3BucketPrefix != "crm-") {
			t.Fatalf("%s: mặc định S3 sai: %+v", role, cfg)
		}
		if role != config.RoleIngest && cfg.ImagePresetVersion != 1 {
			t.Fatalf("IMAGE_PRESET_VERSION mặc định phải 1, nhận %d", cfg.ImagePresetVersion)
		}
	}
}

func TestLoadWorkerMissingS3EndpointNamesVariable(t *testing.T) {
	m := workerEnv()
	delete(m, "S3_ENDPOINT")
	_, err := config.Load(config.RoleWorker, env(m))
	if err == nil || !strings.Contains(err.Error(), "S3_ENDPOINT") {
		t.Fatalf("worker thiếu S3_ENDPOINT phải lỗi nêu tên biến, nhận: %v", err)
	}
	for _, other := range []string{"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "FILES_PUBLIC_BASE_URL"} {
		if strings.Contains(err.Error(), other) {
			t.Fatalf("lỗi chỉ nêu biến thiếu, có cả %s: %v", other, err)
		}
	}
}

// Task M1-41 (việc treo 1 của Task 25): api và worker cần kho tệp, nên cả nhóm S3 bắt buộc; ingest, migrate không đọc.
func TestLoadS3RequiredForFileRoles(t *testing.T) {
	group := []string{"S3_ENDPOINT", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "FILES_PUBLIC_BASE_URL"}
	for role, m := range map[config.Role]map[string]string{config.RoleAPI: apiEnv(), config.RoleWorker: workerEnv()} {
		for _, k := range group {
			delete(m, k)
		}
		_, err := config.Load(role, env(m))
		if err == nil {
			t.Fatalf("%s không khai kho tệp phải lỗi", role)
		}
		for _, name := range group {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("%s: lỗi thiếu tên %s: %v", role, name, err)
			}
		}
	}
	for role, m := range map[config.Role]map[string]string{
		config.RoleIngest: ingestEnv(), config.RoleMigrate: with(base(), "DATABASE_OWNER_URL", "postgres://o@x/crm"),
	} {
		if _, err := config.Load(role, env(m)); err != nil {
			t.Fatalf("%s không cần kho tệp: %v", role, err)
		}
	}
}

func TestLoadS3Valid(t *testing.T) {
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker} {
		m := workerEnv()
		if role == config.RoleAPI {
			m = apiEnv()
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
		{"endpoint không phải URL", config.RoleWorker, with(workerEnv(), "S3_ENDPOINT", "127.0.0.1:8333"), "S3_ENDPOINT"},
		{"đường dẫn công khai có / cuối", config.RoleWorker, with(workerEnv(), "FILES_PUBLIC_BASE_URL", "https://img.x/"), "FILES_PUBLIC_BASE_URL"},
		{"imager không phải URL", config.RoleWorker, with(workerEnv(), "IMAGER_URL", "imager:4193"), "IMAGER_URL"},
		{"preset 0", config.RoleWorker, with(workerEnv(), "IMAGE_PRESET_VERSION", "0"), "IMAGE_PRESET_VERSION"},
		{"preset 0 ở api", config.RoleAPI, with(apiEnv(), "IMAGE_PRESET_VERSION", "0"), "IMAGE_PRESET_VERSION"},
		{"filesync không phải bool", config.RoleAPI, with(apiEnv(), "FILESYNC_EXPECTED", "có"), "FILESYNC_EXPECTED"},
		{"hãng giả thiếu bí mật", config.RoleWorker, with(workerEnv(), "FAKECARRIER_URL", "http://127.0.0.1:4194"), "FAKECARRIER_WEBHOOK_SECRET"},
		{"hãng giả bí mật ngắn", config.RoleIngest, with(ingestEnv(), "FAKECARRIER_URL", "http://127.0.0.1:4194",
			"FAKECARRIER_WEBHOOK_SECRET", "short"), "FAKECARRIER_WEBHOOK_SECRET"},
		{"hãng giả ở production", config.RoleWorker, with(workerEnv(), "CRM_ENV", "production",
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
	cfg, err := config.Load(config.RoleWorker, env(with(workerEnv(),
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

// Task M1-41 (việc treo 2 của Task 25): CRM_ENV chỉ nhận bốn giá trị; giá trị lạ như "prod" làm khởi động lỗi, vì
// "prod" sẽ lọt luật chặn hãng giả ở production (luật chỉ so "production").
func TestLoadEnvMustBeKnown(t *testing.T) {
	for _, v := range []string{"dev", "test", "staging", "production"} {
		if _, err := config.Load(config.RoleIngest, env(with(ingestEnv(), "CRM_ENV", v))); err != nil {
			t.Fatalf("CRM_ENV=%s phải hợp lệ: %v", v, err)
		}
	}
	for _, v := range []string{"prod", "Production", "development", "stg", "live"} {
		for role, m := range map[config.Role]map[string]string{
			config.RoleIngest: ingestEnv(), config.RoleWorker: workerEnv(), config.RoleAPI: apiEnv(),
			config.RoleMigrate: with(base(), "DATABASE_OWNER_URL", "postgres://o@x/crm"),
		} {
			_, err := config.Load(role, env(with(m, "CRM_ENV", v)))
			if err == nil || !strings.Contains(err.Error(), "CRM_ENV") {
				t.Fatalf("%s CRM_ENV=%q phải lỗi nêu CRM_ENV, nhận: %v", role, v, err)
			}
		}
	}
	// Lỗi do "prod" không được để lọt hãng giả: Load trả lỗi, không trả Config nào.
	cfg, err := config.Load(config.RoleWorker, env(with(workerEnv(), "CRM_ENV", "prod",
		"FAKECARRIER_URL", "http://127.0.0.1:4194", "FAKECARRIER_WEBHOOK_SECRET", secret32)))
	if err == nil || cfg.FakeCarrierURL != "" {
		t.Fatalf("CRM_ENV=prod với hãng giả phải lỗi, nhận cfg=%q err=%v", cfg.FakeCarrierURL, err)
	}
}

// S3_IMAGER_ENDPOINT, IMAGER_MAX_PARALLEL (worker, Task M1-43 Q2, Q12): địa chỉ kho mà imager tới được từ mạng imaging; mặc định S3_ENDPOINT.
func TestLoadWorkerImagerEndpoint(t *testing.T) {
	cfg, err := config.Load(config.RoleWorker, env(workerEnv()))
	if err != nil || cfg.S3ImagerEndpoint != cfg.S3Endpoint || cfg.ImagerMaxParallel != 2 {
		t.Fatalf("mặc định: %q / %q, %d %v", cfg.S3ImagerEndpoint, cfg.S3Endpoint, cfg.ImagerMaxParallel, err)
	}
	if _, err := config.Load(config.RoleWorker, env(with(workerEnv(), "IMAGER_MAX_PARALLEL", "0"))); err == nil ||
		!strings.Contains(err.Error(), "IMAGER_MAX_PARALLEL") {
		t.Fatalf("IMAGER_MAX_PARALLEL 0 phải lỗi: %v", err)
	}
	cfg, err = config.Load(config.RoleWorker, env(with(workerEnv(), "S3_IMAGER_ENDPOINT", "http://seaweedfs:8333")))
	if err != nil || cfg.S3ImagerEndpoint != "http://seaweedfs:8333" {
		t.Fatalf("khai: %q %v", cfg.S3ImagerEndpoint, err)
	}
	if _, err := config.Load(config.RoleWorker, env(with(workerEnv(), "S3_IMAGER_ENDPOINT", "seaweedfs:8333"))); err == nil ||
		!strings.Contains(err.Error(), "S3_IMAGER_ENDPOINT") {
		t.Fatalf("không phải URL phải lỗi: %v", err)
	}
}

// IMAGE_RETRY_MAX_HOURS (worker, Task 409, Task 426): trần thời gian thử lại xử lý ảnh khi lỗi hạ tầng tạm; mặc định 23
// giờ, phải nhỏ hơn hạn sống của crm-tmp (config.TmpObjectTTL, 24 giờ): bằng hay lớn hơn thì từ chối khởi động.
func TestLoadWorkerImageRetryMax(t *testing.T) {
	cfg, err := config.Load(config.RoleWorker, env(workerEnv()))
	if err != nil || cfg.ImageRetryMax != 23*time.Hour {
		t.Fatalf("mặc định: %v %v", cfg.ImageRetryMax, err)
	}
	if config.DefaultImageRetryMax != 23*time.Hour || config.DefaultImageRetryMax >= config.TmpObjectTTL {
		t.Fatalf("mặc định %v không nhỏ hơn hạn crm-tmp %v", config.DefaultImageRetryMax, config.TmpObjectTTL)
	}
	for _, h := range []int{1, 23} {
		cfg, err = config.Load(config.RoleWorker, env(with(workerEnv(), "IMAGE_RETRY_MAX_HOURS", strconv.Itoa(h))))
		if err != nil || cfg.ImageRetryMax != time.Duration(h)*time.Hour {
			t.Fatalf("khai %d (hợp lệ): %v %v", h, cfg.ImageRetryMax, err)
		}
	}
	// Bằng hạn (24), lớn hơn (25, 48, 168): ảnh còn lỗi hạ tầng ở giờ cuối ra source_missing dù hợp lệ.
	for _, v := range []string{"24", "25", "48", "168"} {
		_, err := config.Load(config.RoleWorker, env(with(workerEnv(), "IMAGE_RETRY_MAX_HOURS", v)))
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "IMAGE_RETRY_MAX_HOURS") ||
			!strings.Contains(err.Error(), "crm-tmp") {
			t.Fatalf("IMAGE_RETRY_MAX_HOURS=%s (≥ hạn crm-tmp) phải từ chối, nêu crm-tmp: %v", v, err)
		}
	}
	for _, v := range []string{"0", "169", "abc"} {
		if _, err := config.Load(config.RoleWorker, env(with(workerEnv(), "IMAGE_RETRY_MAX_HOURS", v))); err == nil ||
			!strings.Contains(err.Error(), "IMAGE_RETRY_MAX_HOURS") {
			t.Fatalf("IMAGE_RETRY_MAX_HOURS=%s phải lỗi: %v", v, err)
		}
	}
}

// Hạn sống của crm-tmp có một nguồn Go (config.TmpObjectTTL); hạn thật do docker/seaweedfs/bootstrap.sh đặt lên kho
// (quy tắc Expiration Days, fast path gắn ttlSec theo đó). Hai nơi lệch nhau là test đỏ (Task 426).
func TestTmpObjectTTLMatchesBootstrap(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docker", "seaweedfs", "bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`tmp\?lifecycle"[^\n]*\n[^\n]*<Expiration><Days>(\d+)</Days></Expiration>`).FindSubmatch(b)
	if m == nil {
		t.Fatal("bootstrap.sh: không tìm thấy quy tắc <Expiration><Days> của bucket tmp")
	}
	days, err := strconv.Atoi(string(m[1]))
	if err != nil || time.Duration(days)*24*time.Hour != config.TmpObjectTTL {
		t.Fatalf("bootstrap.sh đặt hạn crm-tmp %s ngày, config.TmpObjectTTL là %v", m[1], config.TmpObjectTTL)
	}
}
