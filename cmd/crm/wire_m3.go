package main

import (
	"errors"
	"fmt"

	"elimaz.com.vn/crm/internal/adapters/carrier"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// m3Deps là phụ thuộc của hai module M3 trong một tiến trình (vai trò ingest chỉ cần cấu hình: không dùng trường nào).
type m3Deps struct {
	DB      *db.DB
	Bus     realtime.Bus
	Events  *events.Registry
	Metrics *metrics.Registry
	// Files là kho tệp (đọc tệp bảng kê COD của cash, Task M3-31); nil: lệnh đọc tệp chờ trong hàng đợi.
	Files files.FileStore
}

// m3Parts là phần của M3 mà tiến trình đăng ký: consumer vào sổ sự kiện (api và worker), lệnh, handler vào, lịch, dọn
// theo tenant vào worker, nguồn webhook vào ingest và việc tách gói của worker. Fulfillment, Cash là hai module (Service
// cho lớp HTTP).
type m3Parts struct {
	Fulfillment *fulfillment.Module
	Cash        *cash.Module
	Consumers   []events.Consumer
	Commands    []commands.Handler
	Inbound     []webhooks.InboundHandler
	Sources     []webhooks.Source
	Schedules   []schedules.Schedule
	Cleanups    []schedules.CleanupFunc
}

// wireM3 dựng hai module M3 (fulfillment, cash; kế hoạch M3, Task M3-04) và gom phần chạy nền của chúng; task của từng
// vùng chỉ sửa tệp area_*.go của module, không sửa tệp này. Phần của M1 giữ nguyên điều kiện cũ: consumer sổ COD
// (cash.Consumer) luôn có; adapter hãng (lệnh fulfillment.create_shipment, fulfillment.refresh_status, nguồn webhook
// /hooks/<hãng>/<route_key> và handler tín hiệu) chỉ khi carrier.FakeOn (ngoài production, có FAKECARRIER_URL). Bí mật
// webhook chỉ ở vai trò ingest (worker chỉ tách gói, không kiểm chữ ký). Hai phần trùng tên → lỗi khởi động.
func wireM3(role config.Role, cfg config.Config, d m3Deps) (m3Parts, error) {
	var adapters []fulfillment.CarrierAdapter
	if carrier.FakeOn(cfg) {
		adapters = carrier.All(cfg)
	}
	reg, err := fulfillment.NewRegistry(adapters...)
	if err != nil {
		return m3Parts{}, fmt.Errorf("dựng bộ adapter hãng: %w", err)
	}
	var keys *fieldcrypt.Keyring
	if cfg.FieldEncKeys != "" {
		if keys, err = fieldcrypt.ParseKeyring(logging.Secret(cfg.FieldEncKeys)); err != nil {
			return m3Parts{}, fmt.Errorf("dựng khóa mã hóa trường: %w", err)
		}
	}
	ful := fulfillment.New(fulfillment.Deps{
		DB: d.DB, Queue: queue.NewQueue(), Bus: d.Bus, Events: d.Events, Carriers: reg, Keys: keys, Metrics: d.Metrics,
		Files: d.Files, // nhãn hãng (Task M3-16): lệnh tải nhãn ghi tệp, đọc lô ký đường dẫn tải
	})
	csh := cash.New(cash.Deps{
		DB: d.DB, Queue: queue.NewQueue(), Bus: d.Bus, Events: d.Events, Carriers: reg, Metrics: d.Metrics, Files: d.Files,
	})
	p := m3Parts{
		Fulfillment: ful, Cash: csh,
		Consumers: background.Collect(ful.Consumers(), csh.Consumers()),
		Commands:  background.Collect(ful.CommandHandlers(), csh.CommandHandlers()),
		Inbound:   background.Collect(ful.InboundHandlers(), csh.InboundHandlers()),
		Sources:   ful.WebhookSources(),
		Schedules: background.Collect(ful.Schedules(), csh.Schedules()),
		Cleanups:  background.Collect(ful.Cleanups(), csh.Cleanups()),
	}
	// Nguồn webhook theo hãng (Task M3-10): /hooks/<hãng>/<route_key> của mọi hãng khai Capabilities().Webhook, cùng tập
	// với handler vào của fulfillment.
	p.Sources = append(p.Sources, carrier.WebhookSources(adapters, func(key string) string { return webhookSecret(role, cfg, key) })...)
	if err := errors.Join(ful.Check(), csh.Check(), cash.CheckStatementFormats(reg), checkM3Unique(p)); err != nil {
		return m3Parts{}, fmt.Errorf("nối module M3: %w", err)
	}
	return p, nil
}

// checkM3Unique kiểm không trùng tên giữa hai module và phần hãng giả nối thêm (Check của mỗi module chỉ xét trong
// module đó).
func checkM3Unique(p m3Parts) error {
	return errors.Join(
		background.UniqueNames("lệnh", p.Commands, func(h commands.Handler) string { return h.Kind().Name }),
		background.UniqueNames("handler vào", p.Inbound, func(h webhooks.InboundHandler) string { return h.Source() }),
		background.UniqueNames("nguồn webhook", p.Sources, func(s webhooks.Source) string { return s.Key() }),
		background.UniqueNames("consumer", p.Consumers, func(c events.Consumer) string { return c.Name() }),
		background.UniqueNames("lịch", p.Schedules, func(s schedules.Schedule) string { return s.Name }),
	)
}

// webhookSecret là bí mật kiểm chữ ký webhook của hãng key: chỉ vai trò ingest (worker chỉ tách gói); hãng giả dùng
// FAKECARRIER_WEBHOOK_SECRET, hãng thật CARRIER_<KEY>_WEBHOOK_SECRET (rỗng khi không đặt).
func webhookSecret(role config.Role, cfg config.Config, key string) string {
	if role != config.RoleIngest {
		return ""
	}
	if key == fake.CarrierKey {
		return cfg.FakeCarrierWebhookSecret
	}
	return string(cfg.CarrierWebhookSecrets[key])
}
