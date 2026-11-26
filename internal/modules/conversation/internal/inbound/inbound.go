// Package inbound là phần của module conversation: tin, comment, echo, đã nhận, đã xem, phiên live vào từ kênh (Task 153, 154, 155, 170).
package inbound

import "elimaz.com.vn/crm/internal/modules/conversation/internal/deps"

// Parts là phần chạy nền của phần inbound: một handler sự kiện vào cho mỗi adapter trong d.Registry (Task 153).
func Parts(d deps.Deps) deps.Parts {
	var p deps.Parts
	if d.Registry == nil {
		return p
	}
	for _, k := range d.Registry.Keys() {
		p.Inbound = append(p.Inbound, NewHandler(d, k))
	}
	return p
}
