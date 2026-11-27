package config_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
)

// Mặc định của biến báo cáo M6 (kế hoạch M6, Global Constraints; Task 225): api có chỗ xem tại chỗ, hạn 30 s, 5 lần
// chờ mỗi người, link tải 5 phút; worker có chỗ chạy nền, hạn 60 s, 200.000 dòng, tệp 7 ngày, danh sách khách 24 giờ.
// Mỗi vai trò chỉ đọc phần của mình (phần kia để không).
func TestLoad_ReportDefaults(t *testing.T) {
	api, err := config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil {
		t.Fatal(err)
	}
	wantAPI := config.ReportConfig{APISlots: 2, InteractiveTimeout: 30 * time.Second, MaxPendingPerUser: 5, LinkTTL: 5 * time.Minute}
	if api.Report != wantAPI {
		t.Errorf("api: %+v, mong %+v", api.Report, wantAPI)
	}
	w, err := config.Load(config.RoleWorker, env(with(workerEnv(), "REPORT_TMP_DIR", "/var/tmp/crm-reports")))
	if err != nil {
		t.Fatal(err)
	}
	wantWorker := config.ReportConfig{
		WorkerSlots: 2, BackgroundTimeout: 60 * time.Second, TmpDir: "/var/tmp/crm-reports", MaxRows: 200_000,
		FileTTL: 168 * time.Hour, CustomerExportTTL: 24 * time.Hour,
	}
	if w.Report != wantWorker {
		t.Errorf("worker: %+v, mong %+v", w.Report, wantWorker)
	}
	for _, role := range []config.Role{config.RoleIngest, config.RoleMigrate} {
		m := ingestEnv()
		if role == config.RoleMigrate {
			m = with(base(), "DATABASE_OWNER_URL", "postgres://owner@localhost/crm")
		}
		cfg, err := config.Load(role, env(m))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Report != (config.ReportConfig{}) {
			t.Errorf("%s không đọc biến báo cáo: %+v", role, cfg.Report)
		}
	}
}

// Đè giá trị hợp lệ của từng biến.
func TestLoad_ReportOverrides(t *testing.T) {
	api, err := config.Load(config.RoleAPI, env(with(apiEnv(),
		"REPORT_API_SLOTS", "4", "REPORT_INTERACTIVE_TIMEOUT", "20s", "REPORT_MAX_PENDING_PER_USER", "3",
		"REPORT_LINK_TTL", "2m")))
	if err != nil {
		t.Fatal(err)
	}
	if r := api.Report; r.APISlots != 4 || r.InteractiveTimeout != 20*time.Second || r.MaxPendingPerUser != 3 || r.LinkTTL != 2*time.Minute {
		t.Errorf("api: %+v", r)
	}
	w, err := config.Load(config.RoleWorker, env(with(workerEnv(),
		"REPORT_WORKER_SLOTS", "8", "REPORT_BACKGROUND_TIMEOUT", "74s", "REPORT_TMP_DIR", "/tmp/r", "REPORT_MAX_ROWS", "5",
		"REPORT_FILE_TTL", "48h", "CUSTOMER_EXPORT_TTL", "1h")))
	if err != nil {
		t.Fatal(err)
	}
	if r := w.Report; r.WorkerSlots != 8 || r.BackgroundTimeout != 74*time.Second || r.MaxRows != 5 ||
		r.FileTTL != 48*time.Hour || r.CustomerExportTTL != time.Hour {
		t.Errorf("worker: %+v", r)
	}
}

// Giá trị sai báo lỗi nêu tên biến (kế hoạch Task M6-02 Step 1): hạn chạy nền ≥ 75 s (phải dưới hạn giữ chỗ 90 s của
// hàng đợi trừ thời gian ghi tệp), số chỗ ngoài 1–8, thời lượng sai dạng, REPORT_TMP_DIR không phải đường dẫn tuyệt
// đối. api không đòi REPORT_TMP_DIR.
func TestLoad_ReportBadValues(t *testing.T) {
	worker := []struct{ key, val string }{
		{"REPORT_BACKGROUND_TIMEOUT", "80s"},
		{"REPORT_BACKGROUND_TIMEOUT", "75s"},
		{"REPORT_BACKGROUND_TIMEOUT", "60"},
		{"REPORT_BACKGROUND_TIMEOUT", "0s"},
		{"REPORT_WORKER_SLOTS", "0"},
		{"REPORT_WORKER_SLOTS", "9"},
		{"REPORT_MAX_ROWS", "0"},
		{"REPORT_MAX_ROWS", "1048576"},
		{"REPORT_FILE_TTL", "30m"},
		{"CUSTOMER_EXPORT_TTL", "169h"},
		{"REPORT_TMP_DIR", "tmp/reports"},
	}
	for _, c := range worker {
		mustFail(t, config.RoleWorker, with(workerEnv(), c.key, c.val), c.key)
	}
	api := []struct{ key, val string }{
		{"REPORT_API_SLOTS", "0"},
		{"REPORT_API_SLOTS", "9"},
		{"REPORT_INTERACTIVE_TIMEOUT", "61s"},
		{"REPORT_INTERACTIVE_TIMEOUT", "abc"},
		{"REPORT_MAX_PENDING_PER_USER", "0"},
		{"REPORT_LINK_TTL", "2h"},
	}
	for _, c := range api {
		mustFail(t, config.RoleAPI, with(apiEnv(), c.key, c.val), c.key)
	}
	if _, err := config.Load(config.RoleAPI, env(apiEnv())); err != nil {
		t.Errorf("api không đòi REPORT_TMP_DIR: %v", err)
	}
}

// REPORT_TMP_DIR chưa bắt buộc ở worker trong Task 225 (tệp task, "Quyết định thay chủ shop" 1): worker thiếu biến vẫn
// khởi động, TmpDir rỗng; Task M6-14 (người dùng đầu tiên) đặt bắt buộc cùng biến trong compose.
func TestLoad_ReportTmpDirOptionalUntilExport(t *testing.T) {
	w, err := config.Load(config.RoleWorker, env(workerEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if w.Report.TmpDir != "" {
		t.Errorf("TmpDir = %q, mong rỗng", w.Report.TmpDir)
	}
}
