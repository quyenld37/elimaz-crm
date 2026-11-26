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
// command.fb.fetch_post khi có adapter đọc được bài (Task 154), command.fb.fetch_history khi có adapter đọc được lịch
// sử (Task 164). Phần chạy nền luôn có lịch conversation.presence_cleanup (Task 163), conversation.reminders (Task 169)
// mỗi 60 s, channel.comment_poll (Task 150, đọc comment định kỳ) mỗi 30 s và conversation.retention (Task 164, dọn theo
// hạn giữ) mỗi giờ, theo tenant, đúng một handler cùng Kind.
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
	// Có adapter đọc được bài, lịch sử (facebook, Task 154, 164): thêm đúng lệnh command.fb.fetch_post (đầu) và
	// command.fb.fetch_history (cuối).
	reg, err = channel.NewRegistry(fake.New("0123456789abcdef0123456789abcdef"), facebook.New(facebook.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	d.Registry = reg
	if cmds := conversation.CommandHandlers(d); len(cmds) != 3 || cmds[0].Kind().Name != "command.fb.fetch_post" ||
		cmds[2].Kind().Name != conversation.FetchHistoryKind {
		t.Errorf("lệnh khi có adapter đọc bài: %d", len(cmds))
	}
	checkCommands(t, d)
}

// checkCommands: conversation luôn có command.fb.send_message, hạn 3 phút; ngoài nó chỉ có command.fb.fetch_post (trước)
// và command.fb.fetch_history (sau) khi có adapter facebook (đọc được bài, lịch sử).
func checkCommands(t *testing.T, d conversation.Deps) {
	t.Helper()
	cs := conversation.CommandHandlers(d)
	want, send := 1, 0
	if d.Registry != nil && slices.Contains(d.Registry.Keys(), facebook.Key) {
		want, send = 3, 1
	}
	if len(cs) != want {
		t.Fatalf("lệnh: %d, mong %d", len(cs), want)
	}
	k := cs[send].Kind()
	if k.Name != conversation.SendMessageKind || k.Policy.ExpiresAfter != 3*time.Minute {
		t.Errorf("lệnh %d: %s, mong %s hạn 3 phút", send, k.Name, conversation.SendMessageKind)
	}
}

func checkReminders(t *testing.T, d conversation.Deps) {
	t.Helper()
	s, j := conversation.Schedules(d), conversation.JobHandlers(d)
	want := []struct {
		name  string
		every time.Duration
	}{
		{conversation.PresenceCleanupSchedule, time.Minute},
		{"conversation.reminders", time.Minute},
		{"channel.comment_poll", 30 * time.Second},
		{conversation.RetentionSchedule, time.Hour},
	}
	if len(s) != len(want) || len(j) != len(want) {
		t.Fatalf("lịch %+v, việc %d", s, len(j))
	}
	for i, w := range want {
		if s[i].Name != w.name || !s[i].PerTenant || s[i].Every != w.every || j[i].Kind().Name != s[i].Kind.Name {
			t.Errorf("lịch %d = %+v, việc %q; muốn %s mỗi %v", i, s[i], j[i].Kind().Name, w.name, w.every)
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
