// Package inbound là phần của module conversation: tin, comment, echo, đã nhận, đã xem, phiên live vào từ kênh (Task 153, 154, 155, 170).
package inbound

import (
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/platform/commands"
)

// Parts là phần chạy nền của phần inbound: một handler sự kiện vào cho mỗi adapter trong d.Registry (Task 153); lệnh
// tải tệp đính kèm và đọc hồ sơ khách (Task 155); lệnh đọc bài command.fb.fetch_post khi có adapter đọc được bài
// (Task 154).
func Parts(d deps.Deps) deps.Parts {
	p := deps.Parts{Commands: []commands.Handler{fetchAttachment{d: d}, fetchProfile{d: d}}}
	if d.Registry == nil {
		return p
	}
	fetch := false
	for _, k := range d.Registry.Keys() {
		p.Inbound = append(p.Inbound, NewHandler(d, k))
		fetch = fetch || canFetchPosts(d.Registry, k)
	}
	if fetch {
		p.Commands = append(p.Commands, newFetchPost(d))
	}
	return p
}
