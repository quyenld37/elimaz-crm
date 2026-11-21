package webcache

import "time"

// SetResyncRetry đổi chu kỳ thử lại resync (chỉ trong test, gọi trước Start).
func (v *Invalidator) SetResyncRetry(d time.Duration) { v.retry = d }
