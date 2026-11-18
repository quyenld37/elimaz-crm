package config_test

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/config"
)

// Biến của luật cảnh báo (Task 19), chỉ vai trò worker đọc (lịch platform.alerts chạy ở worker).
func TestLoadAlertOptions(t *testing.T) {
	cfg, err := config.Load(config.RoleWorker, env(s3Env(base())))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackupExpected || cfg.DBDiskBudgetBytes != 0 || cfg.DBConnBudget != 36 {
		t.Fatalf("mặc định: BackupExpected %v, DBDiskBudgetBytes %d, DBConnBudget %d",
			cfg.BackupExpected, cfg.DBDiskBudgetBytes, cfg.DBConnBudget)
	}
	cfg, err = config.Load(config.RoleWorker, env(with(s3Env(base()),
		"BACKUP_EXPECTED", "true", "DB_DISK_BUDGET_BYTES", "21474836480", "DB_CONN_BUDGET", "50")))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BackupExpected || cfg.DBDiskBudgetBytes != 20<<30 || cfg.DBConnBudget != 50 {
		t.Fatalf("đặt giá trị: %+v", cfg)
	}
	_, err = config.Load(config.RoleWorker, env(with(s3Env(base()), "DB_DISK_BUDGET_BYTES", "-1", "DB_CONN_BUDGET", "0")))
	if err == nil || !strings.Contains(err.Error(), "DB_DISK_BUDGET_BYTES") || !strings.Contains(err.Error(), "DB_CONN_BUDGET") {
		t.Fatalf("giá trị sai phải nêu tên biến: %v", err)
	}
}
