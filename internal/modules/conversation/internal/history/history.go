// Package history là phần của module conversation: lấy 20 tin gần nhất khi có hội thoại mới, giữ hội thoại 24 tháng
// (Task 164); đọc comment định kỳ khi webhook feed không về (Task 150, backfill.go).
package history

import "elimaz.com.vn/crm/internal/modules/conversation/internal/deps"

// Parts là phần chạy nền của phần history: lịch đọc comment định kỳ (Task 150).
func Parts(d deps.Deps) deps.Parts { return pollParts(d) }
