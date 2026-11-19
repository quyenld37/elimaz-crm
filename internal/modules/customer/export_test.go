package customer

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// SetAfterBatch cắm hàm chạy sau mỗi lô đã commit của lệnh nhập (processed: số dòng đã xử lý); trả lỗi thì Execute
// dừng ngay với lỗi đó, như tiến trình chết giữa chừng. nil: bỏ.
func SetAfterBatch(i *Importer, fn func(processed int) error) { i.afterBatch = fn }

// SetBudget đổi ngân sách thời gian của một lượt chạy lệnh nhập (0: tự dừng sau mỗi lô).
func SetBudget(i *Importer, d time.Duration) { i.budget = d }

// ImportJob dựng việc customer.import của lần nhập importID để test gọi thẳng Execute.
func ImportJob(tenant, importID uuid.UUID) queue.Job {
	pl, _ := json.Marshal(importPayload{ImportID: importID})
	return queue.Job{ID: uuid.New(), TenantID: &tenant, Kind: ImportKind().Name, Payload: pl, Attempts: 1}
}
