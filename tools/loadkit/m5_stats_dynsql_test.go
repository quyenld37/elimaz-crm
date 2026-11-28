package main

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

func addMetric(t *testing.T, owner *db.DB, bucket time.Time, name, label string, count int64, hist []int) {
	t.Helper()
	var h any
	if hist != nil {
		h = hist
	}
	if _, err := owner.Pool.Exec(t.Context(), `INSERT INTO platform.metrics_minute (bucket, process, name, label, count,
		sum, max, hist) VALUES ($1, 'api-1', $2, $3, $4, $5, 1, $6)`, bucket, name, label, count, float64(count), h); err != nil {
		t.Fatal(err)
	}
}

// readM5Stats cộng bộ đếm theo nhãn, histogram flash_buy_ms mọi nhãn, job_pickup_ms chỉ của việc event.*, bỏ phút
// trước mốc from; đọc deadlocks và số kết nối của database đang thử.
func TestM5ReadStats(t *testing.T) {
	owner := openOwnerDB(t)
	now := time.Now().UTC().Truncate(time.Minute)
	old := now.Add(-10 * time.Minute)
	h := func(i, n int) []int { x := make([]int, len(metrics.HistBoundsMs)+1); x[i] = n; return x }
	addMetric(t, owner, now, "tx_retry", "40001", 3, nil)
	addMetric(t, owner, now.Add(time.Minute), "tx_retry", "40001", 2, nil)
	addMetric(t, owner, now, "tx_retry", "40P01", 1, nil)
	addMetric(t, owner, old, "tx_retry", "40001", 100, nil) // trước mốc: bỏ
	addMetric(t, owner, now, "tx_retry_exhausted", "40001", 1, nil)
	addMetric(t, owner, now, "flash_result", "bought", 500, nil)
	addMetric(t, owner, now, "cache_hit", "catalog", 90, nil)
	addMetric(t, owner, now, "cache_hit", "flash", 5, nil)
	addMetric(t, owner, now, "cache_miss", "catalog", 5, nil)
	addMetric(t, owner, now, "flash_buy_ms", "bought", 500, h(0, 500))
	addMetric(t, owner, now, "flash_buy_ms", "sold_out", 20, h(3, 20))
	addMetric(t, owner, now, "job_pickup_ms", "event.customer_stats", 7, h(2, 7))
	addMetric(t, owner, now, "job_pickup_ms", "task.flash_tick", 9, h(8, 9)) // không phải sự kiện: bỏ

	s, err := readM5Stats(t.Context(), owner, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.TxRetry["40001"] != 5 || s.TxRetry["40P01"] != 1 || s.TxRetryExhausted != 1 {
		t.Fatalf("tx_retry = %v, exhausted %d", s.TxRetry, s.TxRetryExhausted)
	}
	if s.FlashResults["bought"] != 500 || s.CacheHit != 95 || s.CacheMiss != 5 {
		t.Fatalf("flash_result %v, cache %d/%d", s.FlashResults, s.CacheHit, s.CacheMiss)
	}
	if s.FlashBuyCount != 520 || s.FlashBuyHist[0] != 500 || s.FlashBuyHist[3] != 20 {
		t.Fatalf("flash_buy_ms %d %v", s.FlashBuyCount, s.FlashBuyHist)
	}
	if s.PickupEventHist[2] != 7 || s.PickupEventHist[8] != 0 {
		t.Fatalf("job_pickup_ms event.* = %v", s.PickupEventHist)
	}
	if s.Connections < 1 || s.Deadlocks < 0 {
		t.Fatalf("pg_stat_database: %d kết nối, %d deadlock", s.Connections, s.Deadlocks)
	}
}
