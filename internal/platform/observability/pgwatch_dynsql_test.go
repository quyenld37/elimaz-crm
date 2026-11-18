package observability_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/testkit"
)

// Các câu của pgwatch_dynsql.go chạy được bằng crm_app trên PostgreSQL 18 thật (cụm dev: archive tắt,
// pg_stat_statements chưa nạp → bỏ qua không lỗi).
func TestReadPGStats(t *testing.T) {
	d := testkit.OpenAppDB(t, db.PoolOptions{})
	s, err := observability.ReadPGStats(t.Context(), d, observability.RuleOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("ReadPGStats: %v", err)
	}
	if s.DBBytes <= 0 || s.WALBytes <= 0 || s.AppConns < 1 || s.LongTx != 0 || s.LockWait != 0 || s.JobsDeadTuples < 0 {
		t.Fatalf("số đọc: %+v", s)
	}
}
