package selftest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// PongTopic là chủ đề báo hiệu Inbound phát cho mỗi selftest.ping.
const PongTopic = "selftest.pong"

// pongPermission: ai đăng ký luồng realtime của tenant cũng nhận được báo hiệu thử.
const pongPermission = "realtime.subscribe"

// Publisher phát báo hiệu trong transaction của bên gọi (realtime.PGHub hiện thực).
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, s realtime.Signal) error
}

// Inbound là handler sự kiện vào của nguồn selftest (webhooks.InboundHandler): mỗi selftest.ping phát một báo hiệu
// selftest.pong cho tenant của sự kiện, đối tượng {"selftest", eventId}, để kiểm trọn đường ingest → worker → SSE.
type Inbound struct {
	bus Publisher
}

// NewInbound tạo handler phát qua bus.
func NewInbound(bus Publisher) *Inbound { return &Inbound{bus: bus} }

// Source trả "selftest".
func (*Inbound) Source() string { return Key }

// Handle phát selftest.pong trong tx (chỉ tới màn hình khi việc commit). Loại lạ, dữ liệu không đọc được, thiếu
// eventId → queue.ErrPermanent; lỗi Publish trả lên để thử lại theo lịch.
func (i *Inbound) Handle(ctx context.Context, e webhooks.Inbound, tx pgx.Tx) error {
	if e.Type != pingType {
		return fmt.Errorf("selftest: loại sự kiện %q: %w", e.Type, queue.ErrPermanent)
	}
	var ev event
	if err := json.Unmarshal(e.Data, &ev); err != nil || ev.EventID == "" {
		return fmt.Errorf("selftest: ping không có eventId: %w", queue.ErrPermanent)
	}
	err := i.bus.Publish(ctx, tx, realtime.Signal{
		TenantID: e.TenantID, Topic: PongTopic, Requires: pongPermission,
		Entity: realtime.Entity{Type: Key, ID: ev.EventID},
	})
	if err != nil {
		return fmt.Errorf("selftest: phát %s: %w", PongTopic, err)
	}
	return nil
}
