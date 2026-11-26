package conversation_test

import (
	"slices"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Không có bộ adapter: không handler vào, luồng nền. Có bộ adapter: mỗi adapter đúng một handler vào cùng khóa
// (Task 153). Lệnh luôn có command.fb.send_message (Task 156; không có adapter thì lệnh từ chối tin còn chờ), thêm
// command.fb.fetch_post khi có adapter đọc được bài (Task 154). Phần chạy nền luôn có lịch
// conversation.presence_cleanup (Task 163), conversation.reminders (Task 169) mỗi 60 s và channel.comment_poll
// (Task 150, đọc comment định kỳ) mỗi 30 s, theo tenant, đúng một handler cùng Kind.
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
	// Có adapter đọc được bài (facebook, Task 154): thêm đúng lệnh command.fb.fetch_post.
	reg, err = channel.NewRegistry(fake.New("0123456789abcdef0123456789abcdef"), facebook.New(facebook.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	d.Registry = reg
	if cmds := conversation.CommandHandlers(d); len(cmds) != 2 || cmds[0].Kind().Name != "command.fb.fetch_post" {
		t.Errorf("lệnh khi có adapter đọc bài: %d", len(cmds))
	}
	checkCommands(t, d)
}

// checkCommands: lệnh cuối của conversation là command.fb.send_message, hạn 3 phút; ngoài nó chỉ có
// command.fb.fetch_post khi có adapter đọc được bài.
func checkCommands(t *testing.T, d conversation.Deps) {
	t.Helper()
	cs := conversation.CommandHandlers(d)
	want := 1
	if d.Registry != nil && slices.Contains(d.Registry.Keys(), facebook.Key) {
		want = 2
	}
	if len(cs) != want {
		t.Fatalf("lệnh: %d, mong %d", len(cs), want)
	}
	last := cs[len(cs)-1].Kind()
	if last.Name != conversation.SendMessageKind || last.Policy.ExpiresAfter != 3*time.Minute {
		t.Errorf("lệnh cuối: %s, mong %s hạn 3 phút", last.Name, conversation.SendMessageKind)
	}
}

func checkReminders(t *testing.T, d conversation.Deps) {
	t.Helper()
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
