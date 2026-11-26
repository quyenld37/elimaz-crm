package conversation_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Không có bộ adapter: không handler vào, luồng nền. Có bộ adapter: mỗi adapter đúng một handler vào cùng khóa
// (Task 153). Lệnh luôn chỉ có command.fb.send_message (Task 156; không có adapter thì lệnh từ chối tin còn chờ). Phần chạy nền luôn có lịch conversation.presence_cleanup (Task 163) và conversation.reminders (Task 169),
// mỗi lịch theo tenant, 60 s, đúng một handler cùng Kind.
func TestWiring(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("không bộ adapter mà có %d handler vào, luồng nền", n)
	}
	checkCommands(t, d)
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
	if n := len(conversation.Background(d)); n != 0 {
		t.Errorf("luồng nền: %d, mong 0", n)
	}
	checkCommands(t, d)
	checkReminders(t, d)
}

// checkCommands: lệnh của conversation đúng một, command.fb.send_message, hạn 3 phút.
func checkCommands(t *testing.T, d conversation.Deps) {
	t.Helper()
	cs := conversation.CommandHandlers(d)
	if len(cs) != 1 || cs[0].Kind().Name != conversation.SendMessageKind || cs[0].Kind().Policy.ExpiresAfter != 3*time.Minute {
		t.Errorf("lệnh: %d, mong 1 %s", len(cs), conversation.SendMessageKind)
	}
}

func checkReminders(t *testing.T, d conversation.Deps) {
	t.Helper()
	s, j := conversation.Schedules(d), conversation.JobHandlers(d)
	want := []string{conversation.PresenceCleanupSchedule, "conversation.reminders"}
	if len(s) != len(want) || len(j) != len(want) {
		t.Fatalf("lịch %+v, việc %d", s, len(j))
	}
	for i, name := range want {
		if s[i].Name != name || !s[i].PerTenant || s[i].Every != time.Minute || j[i].Kind().Name != s[i].Kind.Name {
			t.Errorf("lịch %d = %+v, việc %q; muốn %s", i, s[i], j[i].Kind().Name, name)
		}
	}
}

// Hằng chủ đề của phần presence khớp TopicPresence (phần không nhập được gói gốc).
func TestPresenceTopic(t *testing.T) {
	t.Parallel()
	if conversation.TopicPresence != "conversation.presence" {
		t.Errorf("TopicPresence = %q", conversation.TopicPresence)
	}
}

// jobFor trả handler việc nền của lịch tên name (khớp theo Kind của lịch), dừng test khi không có.
func jobFor(t *testing.T, d conversation.Deps, name string) queue.Handler { //nolint:ireturn // handler của module chỉ có kiểu interface queue.Handler
	t.Helper()
	for _, s := range conversation.Schedules(d) {
		if s.Name != name {
			continue
		}
		for _, j := range conversation.JobHandlers(d) {
			if j.Kind().Name == s.Kind.Name {
				return j
			}
		}
	}
	t.Fatalf("không có việc nền của lịch %s", name)
	return nil
}
