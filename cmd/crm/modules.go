package main

import (
	"fmt"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
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
}

// newModules dựng sổ sự kiện có consumer sổ COD cash (shipment.status_changed, Task M1-32) và gọi đúng một lần
// Background của ba module M2 customer, order, promotion (kế hoạch M2, Task M2-02); task M2 sau chỉ thêm phần tử trong
// module.go của module mình. bus nil: không phát báo hiệu.
func newModules(d *db.DB, bus realtime.Bus) (modules, error) {
	evs := events.NewRegistry()
	deps := background.Deps{DB: d, Queue: queue.NewQueue(), Bus: bus, Events: evs}
	parts := []background.Parts{customer.Background(deps), order.Background(deps), promotion.Background(deps)}
	out := modules{Events: evs}
	consumers := []events.Consumer{cash.Consumer{}}
	for _, p := range parts {
		consumers = append(consumers, p.Consumers...)
		out.Handlers = append(out.Handlers, p.JobHandlers...)
		out.Schedules = append(out.Schedules, p.Schedules...)
	}
	for _, c := range consumers {
		if err := evs.Register(c); err != nil {
			return modules{}, fmt.Errorf("đăng ký consumer %s: %w", c.Name(), err)
		}
	}
	return out, nil
}
