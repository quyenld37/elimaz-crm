package conversation_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Không có bộ adapter: không handler vào, lệnh, luồng nền. Có bộ adapter: mỗi adapter đúng một handler vào cùng khóa
// (Task 153). Phần nhắc việc (Task 169) luôn có lịch conversation.reminders (theo tenant, 60 s) và đúng một handler
// cùng Kind.
func TestWiring(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.CommandHandlers(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("không bộ adapter mà có %d handler vào, lệnh, luồng nền", n)
	}
	checkReminders(t, d)
	reg, err := channel.NewRegistry(fake.New("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	d.Registry = reg
	hs := conversation.InboundHandlers(d)
	if len(hs) != 1 || hs[0].Source() != fake.Key {
		t.Errorf("handler vào: %d, mong 1 của %s", len(hs), fake.Key)
	}
	if n := len(conversation.CommandHandlers(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("lệnh, luồng nền: %d, mong 0", n)
	}
	checkReminders(t, d)
}

func checkReminders(t *testing.T, d conversation.Deps) {
	t.Helper()
	s, j := conversation.Schedules(d), conversation.JobHandlers(d)
	if len(s) != 1 || s[0].Name != "conversation.reminders" || !s[0].PerTenant || s[0].Every != time.Minute ||
		len(j) != 1 || j[0].Kind().Name != s[0].Kind.Name {
		t.Errorf("lịch, việc nền của conversation: %+v, %d handler", s, len(j))
	}
}
