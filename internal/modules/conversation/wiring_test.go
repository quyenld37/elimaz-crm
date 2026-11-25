package conversation_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Khung Task 143: phần chưa hiện thực trả Parts rỗng; phần nhắc việc (Task 169) có lịch conversation.reminders (theo
// tenant, 60 s) và đúng một handler cùng Kind.
func TestWiringEmpty(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.CommandHandlers(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("khung conversation chưa được có handler vào, lệnh, luồng nền, có %d", n)
	}
	s, j := conversation.Schedules(d), conversation.JobHandlers(d)
	if len(s) != 1 || s[0].Name != "conversation.reminders" || !s[0].PerTenant || s[0].Every != time.Minute ||
		len(j) != 1 || j[0].Kind().Name != s[0].Kind.Name {
		t.Errorf("lịch, việc nền của conversation: %+v, %d handler", s, len(j))
	}
}
