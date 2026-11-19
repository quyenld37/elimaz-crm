package customer

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Background trả phần chạy nền của module customer (khách): việc nền, consumer sự kiện, lịch. cmd/crm gọi đúng một lần mỗi
// tiến trình (kế hoạch M2, Task M2-02); task M2 sau thêm phần tử vào đây. Lệnh nhập khách customer.import (Task M2-13)
// là lệnh ra ngoài (đọc kho tệp ngoài transaction), cmd/crm nối bằng NewImporter cùng các lệnh khác. Task M2-25:
// consumer customer_stats, lịch customer.tag_rules_nightly và handler của nó (đường ghi dựng từ Events, Queue, Bus của
// d).
func Background(d background.Deps) background.Parts {
	w := Writer{Events: d.Events, Queue: d.Queue, Realtime: d.Bus}
	return background.Parts{
		JobHandlers: []queue.Handler{nightlyHandler{w: w, q: d.Queue, now: time.Now, batch: tagRulesBatch}},
		Consumers:   []events.Consumer{StatsConsumer{W: w}},
		Schedules:   []schedules.Schedule{tagRulesSchedule()},
	}
}

// RegisterFilePurposes đăng ký các mục đích tệp của module customer vào sổ của tiến trình (files.RegisterPurpose):
// customer_import (Task M2-13, dòng của platform.file_purposes ở migration 0208). cmd/crm gọi lúc khởi động, trước
// files.CheckPurposes; gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(ImportPurpose()) }
