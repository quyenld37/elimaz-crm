package order

import (
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Background trả phần chạy nền của module order (đơn): việc nền, consumer sự kiện, lịch. cmd/crm gọi đúng một lần mỗi
// tiến trình (kế hoạch M2, Task M2-02); task M2 sau thêm phần tử vào đây. Task M2-21: lịch order.expire_drafts và
// handler của nó (đường ghi dựng từ Events, Queue, Bus của d).
func Background(d background.Deps) background.Parts {
	w := Writer{Events: d.Events, Queue: d.Queue, Realtime: d.Bus}
	return background.Parts{
		JobHandlers: []queue.Handler{expireDraftsHandler{w: w}},
		Schedules:   []schedules.Schedule{expireDraftsSchedule()},
	}
}
