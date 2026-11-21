// Package realtimetest có bản giả của realtime.Bus cho test của module.
package realtimetest

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// RecordingBus là realtime.Bus giả cho test của module: Publish chỉ ghi lại báo hiệu (kể cả khi tx sau đó lùi), không
// giao cho ai; Subscribe không nhận gì. Dùng an toàn từ nhiều goroutine. Test luồng SSE thật dùng realtime.PGHub.
type RecordingBus struct {
	mu      sync.Mutex
	signals []realtime.Signal
}

// Publish ghi lại s.
func (b *RecordingBus) Publish(_ context.Context, _ pgx.Tx, s realtime.Signal) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.signals = append(b.signals, s)
	return nil
}

// Subscribe không nhận gì; cancel không làm gì.
func (b *RecordingBus) Subscribe(identity.AuthUser, func(realtime.Frame)) func() { return func() {} }

// Status báo đang nghe, 0 client.
func (b *RecordingBus) Status() (listening bool, clients int) { return true, 0 }

// Signals trả bản sao các báo hiệu đã ghi, theo thứ tự Publish.
func (b *RecordingBus) Signals() []realtime.Signal {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]realtime.Signal(nil), b.signals...)
}
