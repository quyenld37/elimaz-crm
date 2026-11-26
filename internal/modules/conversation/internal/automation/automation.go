// Package automation là phần của module conversation: quy tắc tự động: tự ẩn, tự trả lời comment, tin chào, nhắn lại (Task 165–168).
//
// Lõi (Task 165): Run là cổng chung của mọi quy tắc (tắt, chống vòng lặp, sự kiện cũ, chỉ xem, không liên hệ, chạy một
// lần nhờ unique của automation_runs); auto_hide ẩn comment qua lệnh command.fb.auto_hide_comment.
//
// Tin chào (Task 167): greeting chào khách nhắn Page lần đầu, có nút trả lời nhanh, qua lệnh command.fb.auto_send của
// send (kết quả cuối ghi vào lượt bằng settleRun).
//
// Tự trả lời comment, tự nhắn riêng (Task 166): autoreply.go, lệnh command.fb.auto_reply_comment,
// command.fb.auto_private_reply (thân lệnh của gói send), việc chuẩn bị task.conversation.auto_private_reply.
package automation

import (
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Parts là phần chạy nền của phần automation: lệnh ẩn comment tự động (Task 165); lệnh gửi tin tự động (tin chào,
// Task 167); lệnh tự trả lời comment, tự nhắn riêng và việc chuẩn bị tin riêng tự động (Task 166).
func Parts(d deps.Deps) deps.Parts {
	return deps.Parts{
		Commands: append([]commands.Handler{hideCommand{d: d, now: d.Clock()}, send.NewAutoCommand(d, settleRun)},
			send.AutoCommands(d, settleRun)...),
		Jobs: []queue.Handler{prepareHandler{d: d}},
	}
}
