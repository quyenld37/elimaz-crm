package main

import (
	"context"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Câu đọc số đo của bộ thử tải (chủ bảng, ngoài tiến trình ứng dụng; chỉ đọc). SQL dựng lúc chạy nằm ở tệp
// *_dynsql.go theo ADR-44 chốt 3: công cụ đo không có khối sqlc riêng.

const statsTimeout = 30 * time.Second

// dbStats là số đo database tại một lúc (At) cho lượt chạy run và khung giờ [from, to].
type dbStats struct {
	At              time.Time `json:"at"`
	InboundDone     int64     `json:"inbound_done"`  // việc inbound.selftest của lượt chạy đã done
	InboundOther    int64     `json:"inbound_other"` // việc inbound.selftest của lượt chạy chưa done (pending, running, dead…)
	CommandsTotal   int64     `json:"commands_total"`
	CommandsDone    int64     `json:"commands_done"`
	CommandsInDoubt int64     `json:"commands_in_doubt"`
	CommandsResent  int64     `json:"commands_resent"` // lệnh có attempts > 1: Execute chạy hơn một lần
	Dead            int64     `json:"dead"`            // mọi việc dead tạo trong khung
	PickupHist      []int     `json:"pickup_hist"`     // job_pickup_ms cộng mọi tiến trình, mọi loại, trong khung
	JobsDeadTuples  int64     `json:"jobs_dead_tuples"`
	JobsBytes       int64     `json:"jobs_bytes"`
}

const (
	sqlInbound = `SELECT count(*) FILTER (WHERE status = 'done'), count(*) FILTER (WHERE status <> 'done')
FROM platform.jobs WHERE kind = 'inbound.selftest' AND starts_with(dedupe_key, $1)`
	sqlCommands = `SELECT count(*), count(*) FILTER (WHERE status = 'done'), count(*) FILTER (WHERE status = 'in_doubt'),
	count(*) FILTER (WHERE attempts > 1)
FROM platform.jobs WHERE kind = 'command.selftest' AND starts_with(dedupe_key, $1)`
	sqlDead   = `SELECT count(*) FROM platform.jobs WHERE status = 'dead' AND created_at >= $1 AND created_at <= $2`
	sqlPickup = `SELECT hist FROM platform.metrics_minute
WHERE name = 'job_pickup_ms' AND hist IS NOT NULL AND bucket >= date_trunc('minute', $1::timestamptz) AND bucket <= $2`
	sqlJobsTable = `SELECT n_dead_tup, pg_total_relation_size(relid) FROM pg_catalog.pg_stat_user_tables
WHERE schemaname = 'platform' AND relname = 'jobs'`
)

// readStats đọc số đo của lượt chạy run (eventId và lệnh có tiền tố lt.<run>.) trong khung [from, to].
func readStats(ctx context.Context, owner *db.DB, run string, from, to time.Time) (dbStats, error) {
	ctx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	s := dbStats{At: time.Now().UTC(), PickupHist: make([]int, len(metrics.HistBoundsMs)+1)}
	p := owner.Pool
	prefix := "lt." + run + "."
	if err := p.QueryRow(ctx, sqlInbound, prefix).Scan(&s.InboundDone, &s.InboundOther); err != nil {
		return s, fmt.Errorf("đếm việc inbound.selftest: %w", err)
	}
	if err := p.QueryRow(ctx, sqlCommands, prefix+"cmd.").Scan(&s.CommandsTotal, &s.CommandsDone, &s.CommandsInDoubt,
		&s.CommandsResent); err != nil {
		return s, fmt.Errorf("đếm lệnh command.selftest: %w", err)
	}
	if err := p.QueryRow(ctx, sqlDead, from, to).Scan(&s.Dead); err != nil {
		return s, fmt.Errorf("đếm việc dead: %w", err)
	}
	rows, err := p.Query(ctx, sqlPickup, from, to)
	if err != nil {
		return s, fmt.Errorf("đọc histogram nhận việc: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var h []int
		if err := rows.Scan(&h); err != nil {
			return s, fmt.Errorf("đọc một dòng histogram: %w", err)
		}
		for i := range min(len(h), len(s.PickupHist)) {
			s.PickupHist[i] += h[i]
		}
	}
	if err := rows.Err(); err != nil {
		return s, fmt.Errorf("đọc histogram nhận việc: %w", err)
	}
	if err := p.QueryRow(ctx, sqlJobsTable).Scan(&s.JobsDeadTuples, &s.JobsBytes); err != nil {
		return s, fmt.Errorf("đọc thống kê bảng jobs: %w", err)
	}
	return s, nil
}
