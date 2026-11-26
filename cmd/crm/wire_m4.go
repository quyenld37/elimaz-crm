package main

import (
	"context"
	"errors"
	"fmt"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
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

// m4Deps là phụ thuộc của hai module M4 trong một tiến trình (vai trò ingest chỉ cần cấu hình).
type m4Deps struct {
	DB      *db.DB
	Bus     realtime.Bus
	Events  *events.Registry
	Metrics *metrics.Registry
	Files   files.FileStore
}

// m4Parts là phần của M4 mà tiến trình đăng ký: bộ adapter kênh; nguồn webhook (mỗi adapter là một nguồn) cho ingest
// và việc tách gói; handler vào, lệnh, việc nền, lịch, luồng nền cho worker.
type m4Parts struct {
	Channels *channel.Registry
	// Keys là khóa mã hóa token của kênh (FIELD_ENC_KEYS; nil khi chưa đặt): api dùng khi kết nối Page (Task 149).
	Keys       *fieldcrypt.Keyring
	Sources    []webhooks.Source
	Inbound    []webhooks.InboundHandler
	Commands   []commands.Handler
	Jobs       []queue.Handler
	Schedules  []schedules.Schedule
	Background []func(context.Context) error
}

// wireM4 dựng bộ adapter kênh (Facebook khi bật cấu hình Meta, kênh giả ngoài production; adapter chưa bật hay chưa
// hiện thực là nil và bị bỏ) và gom phần chạy nền của channel, conversation (kế hoạch M4, Task 143). Task M4 sau chỉ
// sửa tệp của adapter hay phần của mình, không sửa tệp này. Hai phần trùng tên → lỗi khởi động.
func wireM4(cfg config.Config, d m4Deps) (m4Parts, error) {
	reg, err := channel.NewRegistry(facebook.Adapter(cfg), fake.Adapter(cfg))
	if err != nil {
		return m4Parts{}, fmt.Errorf("dựng bộ adapter kênh: %w", err)
	}
	var keys *fieldcrypt.Keyring
	if cfg.FieldEncKeys != "" {
		if keys, err = fieldcrypt.ParseKeyring(logging.Secret(cfg.FieldEncKeys)); err != nil {
			return m4Parts{}, fmt.Errorf("dựng khóa mã hóa trường: %w", err)
		}
	}
	cd := channel.Deps{DB: d.DB, Queue: queue.NewQueue(), Bus: d.Bus, Registry: reg, Keys: keys, Metrics: d.Metrics, Config: cfg}
	vd := conversation.Deps{
		DB: d.DB, Queue: queue.NewQueue(), Bus: d.Bus, Events: d.Events, Registry: reg, Keys: keys, Files: d.Files,
		Metrics: d.Metrics, Config: cfg,
	}
	p := m4Parts{
		Channels:   reg,
		Keys:       keys,
		Inbound:    conversation.InboundHandlers(vd),
		Commands:   background.Collect(channel.CommandHandlers(cd), conversation.CommandHandlers(vd)),
		Jobs:       background.Collect(channel.JobHandlers(cd), conversation.JobHandlers(vd)),
		Schedules:  background.Collect(channel.Schedules(cd), conversation.Schedules(vd)),
		Background: conversation.Background(vd),
	}
	for _, k := range reg.Keys() {
		a, _ := reg.Get(k)
		p.Sources = append(p.Sources, a)
	}
	if err := checkM4Unique(p); err != nil {
		return m4Parts{}, fmt.Errorf("nối module M4: %w", err)
	}
	return p, nil
}

// checkM4Unique kiểm không trùng tên trong phần của M4 (trùng với M3 do webhooks.NewSplitHandler, commands.NewRunner,
// schedules.Registry báo lúc đăng ký).
func checkM4Unique(p m4Parts) error {
	return errors.Join(
		background.UniqueNames("lệnh", p.Commands, func(h commands.Handler) string { return h.Kind().Name }),
		background.UniqueNames("handler vào", p.Inbound, func(h webhooks.InboundHandler) string { return h.Source() }),
		background.UniqueNames("việc nền", p.Jobs, func(h queue.Handler) string { return h.Kind().Name }),
		background.UniqueNames("lịch", p.Schedules, func(s schedules.Schedule) string { return s.Name }),
	)
}
