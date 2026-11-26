// Package automation là phần của module conversation: quy tắc tự động: tự ẩn, tự trả lời comment, tin chào, nhắn lại (Task 165–168).
//
// Lõi (Task 165): Run là cổng chung của mọi quy tắc (tắt, chống vòng lặp, sự kiện cũ, chỉ xem, không liên hệ, chạy một
// lần nhờ unique của automation_runs); auto_hide ẩn comment qua lệnh command.fb.auto_hide_comment.
//
// Nhắn lại (Task 168, followup.go): danh sách khách chưa trả lời và lịch conversation.follow_up của quy tắc follow_up.
package automation

import (
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// KindAutoSendCommand là lệnh gửi tin tự động (nhắn lại, Task 168) vào hội thoại hộp thư.
const KindAutoSendCommand = send.KindAutoSend

// Parts là phần chạy nền của phần automation: lệnh ẩn comment tự động, lệnh gửi tin tự động (kết quả cuối ghi vào lượt
// chạy), lịch nhắn lại conversation.follow_up và việc của nó.
func Parts(d deps.Deps) deps.Parts {
	return deps.Parts{
		Commands:  []commands.Handler{hideCommand{d: d, now: d.Clock()}, send.NewAutoCommand(d, finishStatus)},
		Jobs:      []queue.Handler{followUpHandler{d: d}},
		Schedules: []schedules.Schedule{followUpSchedule()},
	}
}
