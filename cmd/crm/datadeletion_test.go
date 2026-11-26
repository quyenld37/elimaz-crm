package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/datadeletion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Ingest bật kênh Facebook có POST /hooks/facebook/data-deletion: chữ ký sai 400; chữ ký đúng (app secret thật,
// database không tới được) 200 kèm mã và một tệp trong <SPOOL_DIR>/data-deletion. Không bật kênh: không có đường.
func TestRegisterDataDeletion(t *testing.T) {
	t.Parallel()
	const secret = "0123456789abcdef0123456789abcdef"
	dead, err := db.Open(t.Context(), "postgres://crm_app:x@127.0.0.1:1/crm?sslmode=disable&connect_timeout=1", db.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dead.Pool.Close)
	cfg := config.Config{SpoolDir: t.TempDir(), Meta: config.MetaConfig{
		Enabled: true, AppSecret: logging.Secret(secret), HooksPublicBaseURL: "https://hooks.elimaz.test",
	}}
	mux := http.NewServeMux()
	if err := registerDataDeletion(mux, cfg, dead, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	post := func(sr string) int {
		res, err := http.PostForm(srv.URL+"/hooks/facebook/data-deletion", url.Values{"signed_request": {sr}}) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	if st := post("sai.chuky"); st != http.StatusBadRequest {
		t.Errorf("chữ ký sai: %d", st)
	}
	p := base64.RawURLEncoding.EncodeToString([]byte(`{"algorithm":"HMAC-SHA256","user_id":"218471"}`))
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(p))
	if st := post(base64.RawURLEncoding.EncodeToString(m.Sum(nil)) + "." + p); st != http.StatusOK {
		t.Errorf("chữ ký đúng: %d", st)
	}
	files, _ := os.ReadDir(filepath.Join(cfg.SpoolDir, datadeletion.SpoolSubdir))
	if len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".json") {
		t.Errorf("tệp tạm: %v", files)
	}
	off := http.NewServeMux()
	if err := registerDataDeletion(off, config.Config{SpoolDir: t.TempDir()}, dead, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if _, pattern := off.Handler(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/hooks/facebook/data-deletion", nil)); pattern != "" {
		t.Errorf("chưa bật kênh mà có đường %q", pattern)
	}
}

// Lịch channel.data_use_checkup được định nghĩa cùng lệnh có đúng Kind (worker chạy được), lịch platform.data_deletion
// có handler cùng Kind.
func TestAlertSchedulesDataUseCheckup(t *testing.T) {
	t.Parallel()
	plan := schedules.NewRegistry()
	cmds, err := alertSchedules(plan, nil, config.Config{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cmds {
		found = found || c.Kind().Name == "task."+channel.DataUseCheckupSchedule
	}
	if !found {
		t.Error("thiếu lệnh nhắc Data Use Checkup")
	}
	if err := plan.Define(schedules.Schedule{
		Name: channel.DataUseCheckupSchedule, Every: channel.DataUseCheckupEvery,
		Kind: cmds[0].Kind(),
	}); err == nil {
		t.Error("lịch channel.data_use_checkup chưa được định nghĩa")
	}
	p := dataDeletionProcessor()
	if s := p.Schedule(); s.Name != datadeletion.ScheduleName || s.Kind.Name != p.Kind().Name {
		t.Errorf("lịch xóa dữ liệu: %+v", s)
	}
}
