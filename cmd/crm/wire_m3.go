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
// (cash.Consumer) luôn có; hãng giả (lệnh fulfillment.create_shipment, fulfillment.refresh_status, nguồn webhook
// fakecarrier và handler tín hiệu của nó) chỉ khi carrier.FakeOn (ngoài production, có FAKECARRIER_URL). Bí mật webhook
// chỉ ở vai trò ingest (worker chỉ tách gói, không kiểm chữ ký). Hai phần trùng tên → lỗi khởi động.
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
	})
	csh := cash.New(cash.Deps{
		DB: d.DB, Queue: queue.NewQueue(), Bus: d.Bus, Events: d.Events, Carriers: reg, Metrics: d.Metrics,
	})
	if err := errors.Join(ful.Check(), csh.Check()); err != nil {
		return m3Parts{}, fmt.Errorf("nối module M3: %w", err)
	}
	p := m3Parts{
		Fulfillment: ful, Cash: csh,
		Consumers: background.Collect(ful.Consumers(), csh.Consumers()),
		Commands:  background.Collect(ful.CommandHandlers(), csh.CommandHandlers()),
		Inbound:   background.Collect(ful.InboundHandlers(), csh.InboundHandlers()),
		Sources:   ful.WebhookSources(),
		Schedules: background.Collect(ful.Schedules(), csh.Schedules()),
		Cleanups:  background.Collect(ful.Cleanups(), csh.Cleanups()),
	}
	if carrier.FakeOn(cfg) {
		secret := ""
		if role == config.RoleIngest {
			secret = cfg.FakeCarrierWebhookSecret
		}
		p.Sources = append(p.Sources, fake.NewSource(secret))
		p.Inbound = append(p.Inbound, fulfillment.NewSignalHandler(fake.Key))
	}
	return p, nil
}
