// Package live là phần của module conversation: phiên live: khóa thuê, nguồn comment, chốt đơn, đơn nháp thiếu địa chỉ (Task 170–173).
package live

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
)

// Parts là phần chạy nền của phần live: lệnh command.fb.live_detect và lịch live.detect khi bộ adapter có kênh nhận
// biết được live (Task 170); lệnh đối chiếu cuối phiên command.fb.live_reconcile luôn có (phiên nào kết thúc cũng ghi
// lệnh; kênh không đọc được comment live thì lệnh từ chối, không treo chờ), và khi có kênh đọc được comment live
// (channel.LiveSource, Task 171) thêm bộ quản lý luồng đọc comment bằng FBSource. Lệnh cảnh báo conversation.live_alert
// cần Alerter nên cmd/crm đăng ký cùng các lệnh cảnh báo khác (conversation.NewLiveAlertCommand).
func Parts(d deps.Deps) deps.Parts {
	var p deps.Parts
	if anyAdapter[detector](d.Registry) {
		c := NewDetectCommand(d)
		p.Commands = append(p.Commands, c)
		p.Schedules = append(p.Schedules, DetectScheduleFor(c))
	}
	p.Commands = append(p.Commands, NewReconcileCommand(d))
	if anyAdapter[channel.LiveSource](d.Registry) {
		p.Background = append(p.Background, NewManager(d, NewSource(d, SourceOptions{}), Options{}).Run)
	}
	return p
}
