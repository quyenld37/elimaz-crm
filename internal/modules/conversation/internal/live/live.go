// Package live là phần của module conversation: phiên live: khóa thuê, nguồn comment, chốt đơn, đơn nháp thiếu địa chỉ (Task 170–173).
package live

import "elimaz.com.vn/crm/internal/modules/conversation/internal/deps"

// Parts là phần chạy nền của phần live: lệnh command.fb.live_detect và lịch live.detect khi bộ adapter có kênh nhận
// biết được live (Task 170); bộ quản lý luồng đọc comment khi có nguồn comment (Task 171 cắm nguồn Facebook).
func Parts(d deps.Deps) deps.Parts {
	var p deps.Parts
	if canDetect(d.Registry) {
		c := NewDetectCommand(d)
		p.Commands = append(p.Commands, c)
		p.Schedules = append(p.Schedules, DetectScheduleFor(c))
	}
	if src := commentSource(d); src != nil {
		p.Background = append(p.Background, NewManager(d, src, Options{}).Run)
	}
	return p
}

// commentSource trả nguồn comment live của bộ adapter; chưa có nguồn thật (Task 171) → nil, không chạy bộ quản lý
// (nhận phiên mà không đọc được chỉ giữ khóa vô ích).
func commentSource(deps.Deps) CommentSource { return nil } //nolint:ireturn // nguồn là interface phía dùng
