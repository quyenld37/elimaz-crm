package webhooks

import (
	"context"
	"log/slog"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// defaultDrainInterval: ingest xem thư mục tệp tạm mỗi 5 s.
const defaultDrainInterval = 5 * time.Second

// Drainer chạy trong ingest: ngay khi chạy rồi mỗi Interval, đặt gauge spool_files và, khi có tệp, nạp lại vào
// database (Spool.Drain). Lỗi ghi log một lần khi bắt đầu lỗi và một lần khi hết lỗi, không ghi mỗi lượt.
type Drainer struct {
	Spool *Spool
	DB    *db.DB
	// Interval bằng 0 là 5 s.
	Interval time.Duration
	// Metrics nil: không ghi chỉ số.
	Metrics *metrics.Registry
	Logger  *slog.Logger
}

// Run chạy tới khi ctx bị hủy.
func (d *Drainer) Run(ctx context.Context) {
	interval := d.Interval
	if interval <= 0 {
		interval = defaultDrainInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	failing := false
	for {
		failing = d.tick(ctx, failing)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick làm một lượt; trả trạng thái lỗi mới.
func (d *Drainer) tick(ctx context.Context, failing bool) bool {
	files, _, err := d.Spool.Stats()
	if err != nil {
		if !failing {
			d.Logger.Error("đọc thư mục tệp tạm webhook", logging.Err(err))
		}
		return true
	}
	if d.Metrics != nil {
		d.Metrics.Gauge("spool_files", float64(files), "")
	}
	if files == 0 {
		return false
	}
	n, err := d.Spool.Drain(ctx, d.DB)
	switch {
	case err != nil && !failing:
		d.Logger.Warn("nạp tệp tạm webhook", slog.Int("loaded", n), slog.Int("files", files),
			logging.Err(err))
		return true
	case err != nil:
		return true
	case failing || n > 0:
		d.Logger.Info("đã nạp tệp tạm webhook", slog.Int("loaded", n))
	}
	return false
}
