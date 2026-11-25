package main

import (
	"fmt"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// modules là phần chạy nền của các module nghiệp vụ, dựng một lần cho mỗi tiến trình: sổ sự kiện (consumer), việc
// nền và lịch của module.
type modules struct {
	// Events là sổ consumer dùng chung cho api (Emit), lệnh vận chuyển (Emit) và worker (handler).
	Events *events.Registry
	// Handlers là việc nền của module (ngoài handler consumer của Events), đăng ký vào hàng đợi của worker.
	Handlers []queue.Handler
	// Schedules là lịch định kỳ của module (Kind có handler trong Handlers).
	Schedules []schedules.Schedule
	// M3 là phần của hai module M3 (wireM3): consumer đã đăng ký vào Events; lệnh, handler vào, nguồn, lịch, dọn để
	// worker đăng ký.
	M3 m3Parts
	// M4 là phần của hai module M4 (wireM4): bộ adapter kênh, nguồn webhook, handler vào, lệnh, việc nền, lịch, luồng nền.
	M4 m4Parts
}

// newModules dựng sổ sự kiện, gọi đúng một lần Background của ba module M2 customer, order, promotion (kế hoạch M2,
// Task M2-02) và wireM3 của hai module M3 fulfillment, cash (Task M3-04; consumer sổ COD cash của M1 ở đó); task sau
// chỉ thêm phần tử trong module.go (M2) hay area_*.go (M3) của module mình. bus nil: không phát báo hiệu. fs là kho
// tệp của M3 (đọc tệp bảng kê COD, Task M3-31); nil: lệnh cần kho tệp chờ trong hàng đợi.
func newModules(cfg config.Config, d *db.DB, bus realtime.Bus, reg *metrics.Registry, fs files.FileStore) (modules, error) {
	evs := events.NewRegistry()
	deps := background.Deps{DB: d, Queue: queue.NewQueue(), Bus: bus, Events: evs}
	parts := []background.Parts{customer.Background(deps), order.Background(deps), promotion.Background(deps)}
	m3, err := wireM3(cfg.Role, cfg, m3Deps{DB: d, Bus: bus, Events: evs, Metrics: reg, Files: fs})
	if err != nil {
		return modules{}, err
	}
	m4, err := wireM4(cfg, m4Deps{DB: d, Bus: bus, Events: evs, Metrics: reg, Files: fs})
	if err != nil {
		return modules{}, err
	}
	out := modules{Events: evs, M3: m3, M4: m4}
	consumers := m3.Consumers
	for _, p := range parts {
		consumers = append(consumers, p.Consumers...)
		out.Handlers = append(out.Handlers, p.JobHandlers...)
		out.Schedules = append(out.Schedules, p.Schedules...)
	}
	// Bộ đệm đọc của website (Task 190): consumer đổi sự kiện sản phẩm, giá, tồn thành báo hiệu hệ thống cho
	// Invalidator ở api. Đăng ký ở mọi vai trò vì Emit (api, worker) tạo việc theo sổ; bus nil (test): không có.
	if bus != nil {
		consumers = append(consumers, webcache.NewConsumer(bus, nil))
	}
	for _, c := range consumers {
		if err := evs.Register(c); err != nil {
			return modules{}, fmt.Errorf("đăng ký consumer %s: %w", c.Name(), err)
		}
	}
	return out, nil
}
