package observability

import (
	"fmt"
	"strings"
	"time"
)

// Ngưỡng theo dõi PostgreSQL (ADR-42 điều 4, ADR-37; spec 04 mục 8).
const (
	walArchiveMaxAge = 5 * time.Minute
	walMaxBytes      = 2 << 30
	longTxAfter      = 5 * time.Minute
	lockWaitAfter    = 5 * time.Second
	jobsDeadTupleMax = 50_000
	connMaxShare     = 0.7
	dbSizeMaxShare   = 0.8
	// slowMeanMs, slowMinCalls: ngưỡng đề xuất của kế hoạch, chỉnh sau khi đo.
	slowMeanMs   = 500
	slowMinCalls = 100
)

// slowQuery là một dòng của pg_stat_statements (chỉ mã câu và số đo, không có câu SQL).
type slowQuery struct {
	queryID, calls int64
	meanMs         float64
}

// pgStats là các số đọc từ view hệ thống của PostgreSQL. Thời điểm rỗng (IsZero) là NULL.
type pgStats struct {
	archiveOn                            bool
	lastArchived, lastFailed, statsReset time.Time
	dbBytes, walBytes                    int64
	longTx, lockWait, appConns           int64
	jobsDeadTuples                       int64
	slow                                 []slowQuery
}

// pgRules: wal-archive, db-size (urgent); wal-size, long-tx, jobs-dead-tuples, pg-connections, lock-wait, slow-query
// (normal). long-tx và lock-wait đã so với o.Now lúc đọc (readPGStats).
func pgRules(s pgStats, o RuleOptions) []alert {
	var out []alert
	if s.archiveOn {
		lastOK := s.lastArchived
		if lastOK.IsZero() {
			lastOK = s.statsReset
		}
		switch {
		case !s.lastFailed.IsZero() && s.lastFailed.After(s.lastArchived):
			out = append(out, alert{"wal-archive", SeverityUrgent, "wal-archive: lần đẩy WAL cuối bị lỗi"})
		case !lastOK.IsZero() && o.Now.Sub(lastOK) > walArchiveMaxAge:
			out = append(out, alert{
				"wal-archive", SeverityUrgent,
				fmt.Sprintf("wal-archive: lần đẩy WAL thành công cuối cũ %d phút (quá 5 phút)", int(o.Now.Sub(lastOK).Minutes())),
			})
		}
	}
	if o.DBDiskBudgetBytes > 0 && float64(s.dbBytes+s.walBytes) > dbSizeMaxShare*float64(o.DBDiskBudgetBytes) {
		out = append(out, alert{"db-size", SeverityUrgent, fmt.Sprintf("db-size: database + WAL %d MiB, quá 80%% ngân sách %d MiB",
			(s.dbBytes+s.walBytes)>>20, o.DBDiskBudgetBytes>>20)})
	}
	if s.walBytes > walMaxBytes {
		out = append(out, alert{"wal-size", SeverityNormal, fmt.Sprintf("wal-size: pg_wal %d MiB (quá 2048 MiB)", s.walBytes>>20)})
	}
	if s.longTx > 0 {
		out = append(out, alert{"long-tx", SeverityNormal, fmt.Sprintf("long-tx: %d transaction mở quá 5 phút", s.longTx)})
	}
	if s.jobsDeadTuples > jobsDeadTupleMax {
		out = append(out, alert{
			"jobs-dead-tuples", SeverityNormal,
			fmt.Sprintf("jobs-dead-tuples: platform.jobs có %d dòng chết (quá 50000)", s.jobsDeadTuples),
		})
	}
	if o.ConnBudget > 0 && float64(s.appConns) > connMaxShare*float64(o.ConnBudget) {
		out = append(out, alert{
			"pg-connections", SeverityNormal,
			fmt.Sprintf("pg-connections: %d kết nối của ứng dụng, quá 70%% ngân sách %d", s.appConns, o.ConnBudget),
		})
	}
	if s.lockWait > 0 {
		out = append(out, alert{"lock-wait", SeverityNormal, fmt.Sprintf("lock-wait: %d phiên chờ khóa quá 5 s", s.lockWait)})
	}
	if len(s.slow) > 0 {
		ids := make([]string, len(s.slow))
		for i, q := range s.slow {
			ids[i] = fmt.Sprintf("%d (%d ms, %d lần)", q.queryID, int64(q.meanMs), q.calls)
		}
		out = append(out, alert{"slow-query", SeverityNormal, "slow-query: câu chậm theo pg_stat_statements, queryid: " + strings.Join(ids, ", ")})
	}
	return out
}
