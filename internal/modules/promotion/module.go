package promotion

import (
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Background trả phần chạy nền của module promotion (mã giảm giá): việc nền, consumer sự kiện, lịch. cmd/crm gọi đúng
// một lần mỗi tiến trình (kế hoạch M2, Task M2-02). Task 468 (M5-56): lịch promotion.birthday_run mỗi giờ mỗi tenant và
// handler của nó (phát quà sinh nhật trong 9:00–21:00 giờ Việt Nam). Task 200 (M5-17): lịch promotion.flash_tick mỗi
// 15 s mỗi tenant mở và đóng đợt flash sale theo giờ.
func Background(d background.Deps) background.Parts {
	w := Writer{Realtime: d.Bus, Events: d.Events, Queue: d.Queue}
	return background.Parts{
		JobHandlers: []queue.Handler{birthdayRunHandler{w: w, now: d.Clock()}, flashTickHandler{w: w, now: d.Clock()}},
		Schedules:   []schedules.Schedule{birthdayRunSchedule(), flashTickSchedule()},
	}
}
