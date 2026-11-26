package conversation_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Phần chạy nền đã có: lịch conversation.presence_cleanup (Task 163), conversation.reminders (Task 169) mỗi 60 s và
// channel.comment_poll (Task 150, đọc comment định kỳ) mỗi 30 s, theo tenant, đúng một handler cùng Kind; các phần khác
// còn rỗng; hàm gom không có phần tử nil.
func TestWiring(t *testing.T) {
	t.Parallel()
	d := conversation.Deps{}
	if n := len(conversation.InboundHandlers(d)) + len(conversation.CommandHandlers(d)) + len(conversation.Background(d)); n != 0 {
		t.Errorf("chưa phần nào có handler vào, lệnh, luồng nền; có %d", n)
	}
	s, j := conversation.Schedules(d), conversation.JobHandlers(d)
	want := []string{conversation.PresenceCleanupSchedule, "conversation.reminders", "channel.comment_poll"}
	if len(s) != len(want) || len(j) != len(want) {
		t.Fatalf("lịch %+v, việc %d", s, len(j))
	}
	for i, name := range want {
		every := time.Minute
		if name == "channel.comment_poll" {
			every = 30 * time.Second
		}
		if s[i].Name != name || !s[i].PerTenant || s[i].Every != every || j[i].Kind().Name != s[i].Kind.Name {
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
