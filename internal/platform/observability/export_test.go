package observability

import (
	"context"

	"elimaz.com.vn/crm/internal/platform/db"
)

// PGStatsView là các số readPGStats đọc được, mở cho test ở gói observability_test (gói đó mới được nhập testkit:
// testkit → files → queue → observability).
type PGStatsView struct {
	DBBytes, WALBytes, AppConns, LongTx, LockWait, JobsDeadTuples int64
}

// ReadPGStats chạy readPGStats (các câu của pgwatch_dynsql.go) và trả các số đã đọc.
func ReadPGStats(ctx context.Context, d *db.DB, o RuleOptions) (PGStatsView, error) {
	s, err := readPGStats(ctx, d, o)
	return PGStatsView{
		DBBytes: s.dbBytes, WALBytes: s.walBytes, AppConns: s.appConns,
		LongTx: s.longTx, LockWait: s.lockWait, JobsDeadTuples: s.jobsDeadTuples,
	}, err
}
