package conversation_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Phần chạy nền đã có: lịch conversation.presence_cleanup và việc của nó (Task 163); các phần khác còn rỗng; hàm gom
// không có phần tử nil.
func TestWiring(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.CommandHandlers(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("chưa phần nào có handler vào, lệnh, luồng nền; có %d", n)
	}
	s, j := conversation.Schedules(d), conversation.JobHandlers(d)
	if len(s) != 1 || s[0].Name != conversation.PresenceCleanupSchedule || len(j) != 1 || j[0].Kind().Name != s[0].Kind.Name {
		t.Errorf("lịch %+v, việc %d", s, len(j))
	}
}

// Hằng chủ đề của phần presence khớp TopicPresence (phần không nhập được gói gốc).
func TestPresenceTopic(t *testing.T) {
	t.Parallel()
	if conversation.TopicPresence != "conversation.presence" {
		t.Errorf("TopicPresence = %q", conversation.TopicPresence)
	}
}
