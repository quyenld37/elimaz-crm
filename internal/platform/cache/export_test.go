package cache

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// NewRateLimiterWithCap dựng RateLimiter với trần chủ thể của bộ đếm dự phòng là capacity (0: mặc định 100.000) và
// trả kèm hàm đọc số chủ thể đang giữ. Chỉ cho test.
func NewRateLimiterWithCap(c *Conn, now func() time.Time, m *metrics.Registry, capacity int) (RateLimiter, func() int) { //nolint:ireturn // như NewRateLimiter
	r := newRateLimiter(c, now, m, capacity)
	return r, r.fb.size
}
