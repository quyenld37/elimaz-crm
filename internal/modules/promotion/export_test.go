package promotion

import (
	"time"

	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ErrOrderHasCoupon cho test gói ngoài: đơn đã có lượt mã đang hiệu lực (lỗi lập trình, không ánh xạ HTTP).
var ErrOrderHasCoupon = errOrderHasCoupon

// WithCodeGen trả w sinh mã bằng gen thay cho NewCode (test ca mã sinh trùng).
func WithCodeGen(w Writer, gen func(prefix string) (string, error)) Writer {
	w.newCode = gen
	return w
}

// DueGift cho test gói ngoài: quà sinh nhật đến hạn lúc now (luật ngày của website).
func DueGift(day, month int, saved, now time.Time, s BirthdaySettings) (dueGift, bool) {
	return dueGiftAt(day, month, saved, now, s)
}

// BirthdayRunHandler trả handler của lịch promotion.birthday_run với đồng hồ now.
//
//nolint:ireturn // test: handler chưa xuất, gói ngoài dùng qua queue.Handler
func BirthdayRunHandler(now func() time.Time) queue.Handler {
	return birthdayRunHandler{now: now}
}

// FlashTickHandler trả handler của lịch promotion.flash_tick với đồng hồ now (Writer có Events, Queue mới).
//
//nolint:ireturn // test: handler chưa xuất, gói ngoài dùng qua queue.Handler
func FlashTickHandler(now func() time.Time) queue.Handler {
	return flashTickHandler{w: Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}, now: now}
}
