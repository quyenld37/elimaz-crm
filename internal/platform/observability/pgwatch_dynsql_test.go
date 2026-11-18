package observability

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// Các câu của pgwatch_dynsql.go chạy được bằng crm_app trên PostgreSQL 18 thật (cụm dev: archive tắt,
// pg_stat_statements chưa nạp → bỏ qua không lỗi).
func TestReadPGStats(t *testing.T) {
	d := testkit.OpenAppDB(t, db.PoolOptions{})
	s, err := readPGStats(t.Context(), d, RuleOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("readPGStats: %v", err)
	}
	if s.dbBytes <= 0 || s.walBytes <= 0 || s.appConns < 1 || s.longTx != 0 || s.lockWait != 0 || s.jobsDeadTuples < 0 {
		t.Fatalf("số đọc: %+v", s)
	}
}
