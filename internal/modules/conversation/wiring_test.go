package conversation_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Khung Task 143: mọi phần trả Parts rỗng, các hàm gom không có phần tử nào (kể cả nil).
func TestWiringEmpty(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.CommandHandlers(d)) + len(conversation.JobHandlers(d)) +
		len(conversation.Schedules(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("khung conversation chưa được có phần chạy nền, có %d", n)
	}
}
