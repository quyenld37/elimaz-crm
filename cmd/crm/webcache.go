package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// webCacheMaxValue: giá trị lớn nhất được ghi vào bộ đệm đọc của website (một trang 200 mẫu kèm bài viết có thể quá
// 1 MiB mặc định của ReadCache; Valkey bộ đệm có maxmemory riêng, QĐ14).
const webCacheMaxValue = 8 << 20

// webCache là bộ đệm đọc của các route website ở vai trò api (Task 190): ReadCache trên Valkey bộ đệm và
// webcache.Invalidator xóa khóa khi có báo hiệu hệ thống. stop dừng Invalidator (chờ goroutine) rồi đóng Valkey.
type webCache struct {
	rc   cache.ReadCache
	stop func()
}

// newWebCache quay số VALKEY_CACHE_URL (trống → Conn nil: mọi lần đọc tải thẳng PostgreSQL có giới hạn, ghi cảnh báo
// một lần), dựng ReadCache và Invalidator nghe hub (đã Start). Consumer webcache (phát báo hiệu) đăng ký ở newModules.
// opTimeout là hạn mỗi lệnh Valkey (cache.DialOptions.OpTimeout): main truyền 0 (50 ms, QĐ22); test truyền hạn dài hơn
// để máy tải không làm lệnh hết hạn rồi rơi về PostgreSQL (Task 498).
func newWebCache(ctx context.Context, cfg config.Config, d *db.DB, hub *realtime.PGHub, reg *metrics.Registry,
	logger *slog.Logger, opTimeout time.Duration,
) (webCache, error) {
	var conn *cache.Conn
	if cfg.ValkeyCacheURL == "" {
		logger.Warn("VALKEY_CACHE_URL trống: bộ đệm đọc của website đọc thẳng PostgreSQL (giới hạn số lần tải đồng thời)")
	} else {
		var err error
		if conn, err = cache.Dial(ctx, cfg.ValkeyCacheURL, cache.DialOptions{Name: "cache", OpTimeout: opTimeout, Metrics: reg, Tracing: true}); err != nil {
			return webCache{}, fmt.Errorf("nối Valkey bộ đệm: %w", err)
		}
	}
	rc := cache.NewReadCache(conn, cache.ReadCacheOptions{MaxValueBytes: webCacheMaxValue, Metrics: reg})
	inv := webcache.NewInvalidator(hub, rc, d, logger)
	inv.Start() //nolint:contextcheck // Invalidator tự giữ context của mình, dừng bằng Stop (Task 185)
	return webCache{rc: rc, stop: func() {
		inv.Stop()
		conn.Close()
	}}, nil
}
