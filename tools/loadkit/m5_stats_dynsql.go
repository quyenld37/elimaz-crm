package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Số đo database của bài thử tải M5 (kế hoạch M5 Task 219; chủ bảng, chỉ đọc, ngoài tiến trình ứng dụng). Mọi số là
// tổng cộng dồn từ mốc from (chỉ số theo phút) hay từ khi database chạy (pg_stat_database): m5report chấm phần chênh
// giữa lần đọc trước và sau bài.

// m5Stats là một lần đọc.
type m5Stats struct {
	At               time.Time        `json:"at"`
	Deadlocks        int64            `json:"deadlocks"`   // pg_stat_database.deadlocks của database đang thử
	Connections      int64            `json:"connections"` // số kết nối (numbackends) lúc đọc
	TxRetry          map[string]int64 `json:"tx_retry"`    // tx_retry theo SQLSTATE (nhãn)
	TxRetryExhausted int64            `json:"tx_retry_exhausted"`
	FlashBuyHist     []int            `json:"flash_buy_hist"` // flash_buy_ms, mọi kết quả
	FlashBuyCount    int64            `json:"flash_buy_count"`
	FlashResults     map[string]int64 `json:"flash_results"`     // flash_result theo kết quả
	PickupEventHist  []int            `json:"pickup_event_hist"` // job_pickup_ms của việc sự kiện (kind event.*)
	CacheHit         int64            `json:"cache_hit"`
	CacheMiss        int64            `json:"cache_miss"`
}

const (
	sqlM5DB = `SELECT deadlocks, numbackends FROM pg_catalog.pg_stat_database WHERE datname = current_database()`
	// Bộ đếm (counter) cộng count; histogram cộng từng ngăn bằng Go (mảng ngắn).
	sqlM5Counts = `SELECT name, label, coalesce(sum(count), 0)::bigint FROM platform.metrics_minute
WHERE name = ANY($1) AND bucket >= date_trunc('minute', $2::timestamptz) GROUP BY name, label`
	sqlM5Hist = `SELECT name, label, count, hist FROM platform.metrics_minute
WHERE name = ANY($1) AND hist IS NOT NULL AND bucket >= date_trunc('minute', $2::timestamptz)`
)

func m5CounterNames() []string {
	return []string{"tx_retry", "tx_retry_exhausted", "flash_result", "cache_hit", "cache_miss"}
}

func m5HistNames() []string { return []string{"flash_buy_ms", "job_pickup_ms"} }

// readM5Stats đọc số đo từ phút của from.
func readM5Stats(ctx context.Context, owner *db.DB, from time.Time) (m5Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	s := m5Stats{
		At: time.Now().UTC(), TxRetry: map[string]int64{}, FlashResults: map[string]int64{},
		FlashBuyHist: make([]int, len(metrics.HistBoundsMs)+1), PickupEventHist: make([]int, len(metrics.HistBoundsMs)+1),
	}
	p := owner.Pool
	if err := p.QueryRow(ctx, sqlM5DB).Scan(&s.Deadlocks, &s.Connections); err != nil {
		return s, fmt.Errorf("đọc pg_stat_database: %w", err)
	}
	if err := readM5Counts(ctx, owner, from, &s); err != nil {
		return s, err
	}
	return s, readM5Hists(ctx, owner, from, &s)
}

func readM5Counts(ctx context.Context, owner *db.DB, from time.Time, s *m5Stats) error {
	rows, err := owner.Pool.Query(ctx, sqlM5Counts, m5CounterNames(), from)
	if err != nil {
		return fmt.Errorf("đọc bộ đếm: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, label string
		var n int64
		if err := rows.Scan(&name, &label, &n); err != nil {
			return fmt.Errorf("đọc một dòng bộ đếm: %w", err)
		}
		switch name {
		case "tx_retry":
			s.TxRetry[label] += n
		case "tx_retry_exhausted":
			s.TxRetryExhausted += n
		case "flash_result":
			s.FlashResults[label] += n
		case "cache_hit":
			s.CacheHit += n
		case "cache_miss":
			s.CacheMiss += n
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("đọc bộ đếm: %w", err)
	}
	return nil
}

func readM5Hists(ctx context.Context, owner *db.DB, from time.Time, s *m5Stats) error {
	rows, err := owner.Pool.Query(ctx, sqlM5Hist, m5HistNames(), from)
	if err != nil {
		return fmt.Errorf("đọc histogram: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, label string
		var n int64
		var h []int
		if err := rows.Scan(&name, &label, &n, &h); err != nil {
			return fmt.Errorf("đọc một dòng histogram: %w", err)
		}
		dst := s.FlashBuyHist
		switch {
		case name == "flash_buy_ms":
			s.FlashBuyCount += n
		case strings.HasPrefix(label, "event."):
			dst = s.PickupEventHist
		default:
			continue // job_pickup_ms của việc khác (lịch, lệnh)
		}
		for i := range min(len(h), len(dst)) {
			dst[i] += h[i]
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("đọc histogram: %w", err)
	}
	return nil
}
