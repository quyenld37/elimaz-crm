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
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Không có bộ adapter: không handler vào, luồng nền. Có bộ adapter: mỗi adapter đúng một handler vào cùng khóa
// (Task 153). Lệnh luôn có command.fb.send_message (Task 156; không có adapter thì lệnh từ chối tin còn chờ), thêm
// command.fb.fetch_post (Task 154), command.fb.live_detect và lịch live.detect (Task 170) khi có adapter Facebook.
// Phần chạy nền luôn có lịch conversation.presence_cleanup (Task 163), conversation.reminders (Task 169) mỗi 60 s và
// channel.comment_poll (Task 150, đọc comment định kỳ) mỗi 30 s, theo tenant, đúng một handler cùng Kind.
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
	// Có adapter Facebook: thêm lệnh command.fb.fetch_post (đọc bài, Task 154) và command.fb.live_detect cùng lịch
	// live.detect 30 s theo tenant (nhận biết live, Task 170).
	reg, err = channel.NewRegistry(fake.New("0123456789abcdef0123456789abcdef"), facebook.New(facebook.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	d.Registry = reg
	checkCommands(t, d)
	var detect []schedules.Schedule
	for _, s := range conversation.Schedules(d) {
		if s.Name == "live.detect" {
			detect = append(detect, s)
		}
	}
	if len(detect) != 1 || detect[0].Kind.Name != "command.fb.live_detect" || !detect[0].PerTenant || detect[0].Every != 30*time.Second {
		t.Errorf("lịch live.detect khi có Facebook: %+v", detect)
	}
}

// checkCommands: lệnh của conversation theo thứ tự đăng ký: command.fb.fetch_post (khi có adapter Facebook),
// command.fb.send_message hạn 3 phút (luôn có), command.fb.live_detect (khi có adapter Facebook).
func checkCommands(t *testing.T, d conversation.Deps) {
	t.Helper()
	fb := d.Registry != nil && slices.Contains(d.Registry.Keys(), facebook.Key)
	want := []string{conversation.SendMessageKind}
	if fb {
		want = []string{"command.fb.fetch_post", conversation.SendMessageKind, "command.fb.live_detect"}
	}
	var got []string
	for _, h := range conversation.CommandHandlers(d) {
		got = append(got, h.Kind().Name)
		if h.Kind().Name == conversation.SendMessageKind && h.Kind().Policy.ExpiresAfter != 3*time.Minute {
			t.Errorf("lệnh %s hạn %v, mong 3 phút", conversation.SendMessageKind, h.Kind().Policy.ExpiresAfter)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("lệnh: %v, mong %v", got, want)
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
