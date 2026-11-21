package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// Câu đọc view thống kê của PostgreSQL (pg_stat_archiver, pg_stat_activity, pg_stat_user_tables,
// pg_stat_statements): sqlc không có danh mục các view này nên câu nằm ở đây (CLAUDE.md, mục "Cơ sở dữ liệu"); câu
// tĩnh, chỉ tham số qua $n. crm_app đọc được mọi phiên nhờ pg_monitor (migration 0010).
const (
	// Cột cuối: tuổi (giây) lần đẩy WAL thành công cuối theo đồng hồ của PostgreSQL (bên ghi last_archived_time); NULL
	// khi chưa đẩy lần nào.
	archiverSQL = `SELECT (current_setting('archive_mode') <> 'off')::bool, last_archived_time, last_failed_time, stats_reset,
		extract(epoch FROM now() - last_archived_time)::float8
		FROM pg_catalog.pg_stat_archiver`
	activitySQL = `SELECT
		count(*) FILTER (WHERE backend_type = 'client backend' AND xact_start < $1 AND pid <> pg_backend_pid()),
		count(*) FILTER (WHERE wait_event_type = 'Lock' AND query_start < $2),
		count(*) FILTER (WHERE usename = current_user)
		FROM pg_catalog.pg_stat_activity WHERE datname = current_database()`
	deadTuplesSQL = `SELECT coalesce((SELECT n_dead_tup FROM pg_catalog.pg_stat_user_tables
		WHERE schemaname = 'platform' AND relname = 'jobs'), 0)`
	// pg_stat_statements(false): hàm của extension (schema public, không ghi tên schema được), showtext = false: không
	// đọc câu SQL (có thể mang giá trị hằng), chỉ queryid và số đo.
	slowSQL = `SELECT queryid, calls, mean_exec_time FROM pg_stat_statements(false)
		WHERE dbid = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database()) AND calls >= $1 AND mean_exec_time > $2
		ORDER BY mean_exec_time DESC LIMIT 5`
)

// readPGStats đọc pgStats; so sánh thời gian (transaction dài, chờ khóa) theo o.Now. pg_stat_statements chưa nạp
// (shared_preload_libraries) hay chưa cài thì bỏ qua luật câu chậm, không lỗi.
func readPGStats(ctx context.Context, d *db.DB, o RuleOptions) (pgStats, error) {
	var s pgStats
	var errs []error
	sizes, err := store.New(d.Pool).DBAndWALBytes(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("đọc dung lượng database: %w", err))
	}
	s.dbBytes, s.walBytes = sizes.DbBytes, sizes.WalBytes

	a, err := readArchiver(ctx, d)
	if err != nil {
		errs = append(errs, err)
	}
	s.archiveOn, s.lastArchived, s.lastFailed, s.statsReset = a.archiveOn, a.lastArchived, a.lastFailed, a.statsReset

	err = d.Pool.QueryRow(ctx, activitySQL, o.Now.Add(-longTxAfter), o.Now.Add(-lockWaitAfter)).
		Scan(&s.longTx, &s.lockWait, &s.appConns)
	if err != nil {
		errs = append(errs, fmt.Errorf("đọc pg_stat_activity: %w", err))
	}
	if err := d.Pool.QueryRow(ctx, deadTuplesSQL).Scan(&s.jobsDeadTuples); err != nil {
		errs = append(errs, fmt.Errorf("đọc dòng chết của jobs: %w", err))
	}
	slow, err := readSlow(ctx, d)
	if err != nil {
		errs = append(errs, err)
	}
	s.slow = slow
	return s, errors.Join(errs...)
}

// readArchiver đọc pg_stat_archiver (chỉ các trường archive của pgStats).
func readArchiver(ctx context.Context, d *db.DB) (pgStats, error) {
	var s pgStats
	var archived, failed, reset pgtype.Timestamptz
	var ageS pgtype.Float8
	if err := d.Pool.QueryRow(ctx, archiverSQL).Scan(&s.archiveOn, &archived, &failed, &reset, &ageS); err != nil {
		return s, fmt.Errorf("đọc pg_stat_archiver: %w", err)
	}
	s.lastArchived, s.lastFailed, s.statsReset = tsOrZero(archived), tsOrZero(failed), tsOrZero(reset)
	if ageS.Valid {
		v := int64(ageS.Float64)
		s.lastArchivedAgeS = &v
	}
	return s, nil
}

func readSlow(ctx context.Context, d *db.DB) ([]slowQuery, error) {
	rows, err := d.Pool.Query(ctx, slowSQL, slowMinCalls, slowMeanMs)
	if err != nil {
		return nil, slowErr(err)
	}
	defer rows.Close()
	var out []slowQuery
	for rows.Next() {
		var q slowQuery
		if err := rows.Scan(&q.queryID, &q.calls, &q.meanMs); err != nil {
			return nil, fmt.Errorf("đọc pg_stat_statements: %w", err)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, slowErr(err)
	}
	return out, nil
}

// slowErr: 55000 (chưa nạp qua shared_preload_libraries), 42P01, 42883 (chưa CREATE EXTENSION) → không lỗi.
func slowErr(err error) error {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && (pe.Code == "55000" || pe.Code == "42P01" || pe.Code == "42883") {
		return nil
	}
	return fmt.Errorf("đọc pg_stat_statements: %w", err)
}

func tsOrZero(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}
