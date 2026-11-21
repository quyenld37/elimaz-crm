package cache

import "context"

// Health là trạng thái của một tiến trình Valkey trong /health/deep (kế hoạch M5 Task 184).
type Health string

// Ba trạng thái: ok (Ping được), down (cầu dao mở hay Ping lỗi), off (không cấu hình Valkey: Conn nil).
const (
	HealthOK   Health = "ok"
	HealthDown Health = "down"
	HealthOff  Health = "off"
)

// Health Ping qua cầu dao (hạn OpTimeout): Conn nil → off; lỗi hay cầu dao mở → down. Valkey down không phải hỏng
// (chậm đi, ADR-39): bên gọi không đổi trạng thái chung theo kết quả này.
func (c *Conn) Health(ctx context.Context) Health {
	if c == nil {
		return HealthOff
	}
	if err := c.Ping(ctx); err != nil {
		return HealthDown
	}
	return HealthOK
}
