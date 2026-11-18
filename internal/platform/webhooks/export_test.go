package webhooks

import "context"

// SetSave thay bước lưu vào database của in (test giữ chậm bước lưu để thử giới hạn đồng thời).
func SetSave(in *Ingest, f func(ctx context.Context, e Entry) error) { in.save = f }

// Tracked trả số địa chỉ l đang giữ trong bộ nhớ.
func Tracked(l *Lockout) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ips)
}
