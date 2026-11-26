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
// (Task 153). Lệnh luôn có command.fb.fetch_attachment, command.fb.fetch_profile (Task 155, chỉ đọc),
// command.fb.send_message (Task 156; không có adapter thì lệnh từ chối tin còn chờ) và command.fb.live_reconcile
// (Task 171), thêm command.fb.fetch_post (Task 154), command.fb.live_detect và lịch live.detect (Task 170), một luồng
// nền đọc comment live (Task 171), command.fb.fetch_history (Task 164, đọc được lịch sử) khi có adapter Facebook.
// Phần chạy nền luôn có lịch conversation.send_reveal mỗi 15 s (Task 157), conversation.presence_cleanup (Task 163),
// conversation.reminders (Task 169) mỗi 60 s, conversation.follow_up (Task 168) mỗi 300 s, channel.comment_poll
// (Task 150, đọc comment định kỳ) mỗi 30 s và conversation.retention (Task 164, dọn theo hạn giữ) mỗi giờ, theo
// tenant, đúng một handler cùng Kind.
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
	// Có adapter Facebook: thêm lệnh command.fb.fetch_post (đọc bài, Task 154), command.fb.live_detect cùng lịch
	// live.detect 30 s theo tenant (nhận biết live, Task 170), command.fb.fetch_history (đọc lịch sử, Task 164) và một
	// luồng nền đọc comment live (Task 171).
	reg, err = channel.NewRegistry(fake.New("0123456789abcdef0123456789abcdef"), facebook.New(facebook.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	d.Registry = reg
	checkCommands(t, d)
	if n := len(conversation.Background(d)); n != 1 {
		t.Errorf("luồng nền khi có Facebook: %d, mong 1 (đọc comment live)", n)
	}
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

// checkCommands: lệnh của conversation theo thứ tự đăng ký: command.fb.fetch_attachment, command.fb.fetch_profile
// (luôn có, Task 155), command.fb.fetch_post (khi có adapter Facebook), command.fb.send_message hạn 3 phút, ba lệnh
// comment của Task 158 (luôn có), command.fb.auto_hide_comment (luôn có, Task 165), command.fb.auto_send hạn 5 phút
// (luôn có, Task 167, 168), command.fb.auto_reply_comment, command.fb.auto_private_reply (luôn có, Task 166),
// command.fb.live_detect (khi có adapter Facebook, Task 170), command.fb.live_reconcile (luôn có, Task 171),
// command.fb.fetch_history (khi có adapter Facebook, Task 164).
// autoHideKind là lệnh ẩn comment của quy tắc auto_hide (Task 165); autoReplyKind, autoPrivateKind là lệnh tự trả lời
// comment, tự nhắn riêng (Task 166); prepareAutoPrivateKind là việc chuẩn bị tin riêng tự động (không có lịch).
const (
	autoHideKind           = "command.fb.auto_hide_comment"
	autoReplyKind          = "command.fb.auto_reply_comment"
	autoPrivateKind        = "command.fb.auto_private_reply"
	prepareAutoPrivateKind = "task.conversation.auto_private_reply"
)

// autoSendKind là lệnh gửi tin tự động (tin chào Task 167, nhắn lại Task 168).
const autoSendKind = conversation.AutoSendKind

func checkCommands(t *testing.T, d conversation.Deps) {
	t.Helper()
	fb := d.Registry != nil && slices.Contains(d.Registry.Keys(), facebook.Key)
	reads := []string{"command.fb.fetch_attachment", "command.fb.fetch_profile"}
	sends := []string{
		conversation.SendMessageKind, conversation.ReplyCommentKind, conversation.PrivateReplyKind,
		conversation.HideCommentKind, autoHideKind, autoSendKind, autoReplyKind, autoPrivateKind,
	}
	want := append(append(slices.Clone(reads), sends...), "command.fb.live_reconcile")
	if fb {
		want = append(append(append(slices.Clone(reads), "command.fb.fetch_post"), sends...), "command.fb.live_detect",
			"command.fb.live_reconcile", conversation.FetchHistoryKind)
	}
	var got []string
	for _, h := range conversation.CommandHandlers(d) {
		got = append(got, h.Kind().Name)
		if exp, ok := map[string]time.Duration{
			conversation.SendMessageKind: 3 * time.Minute, conversation.ReplyCommentKind: 5 * time.Minute,
			conversation.PrivateReplyKind: 5 * time.Minute, conversation.HideCommentKind: time.Hour,
			autoReplyKind: 5 * time.Minute, autoPrivateKind: 5 * time.Minute,
		}[h.Kind().Name]; ok && h.Kind().Policy.ExpiresAfter != exp {
			t.Errorf("lệnh %s hạn %v, mong %v", h.Kind().Name, h.Kind().Policy.ExpiresAfter, exp)
		}
		if h.Kind().Name == autoSendKind && (h.Kind().Policy.ExpiresAfter != 5*time.Minute || h.Kind().Policy.Timeout != 30*time.Second) {
			t.Errorf("lệnh %s: %+v, mong hạn 5 phút, 30 giây mỗi lần", autoSendKind, h.Kind().Policy)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("lệnh: %v, mong %v", got, want)
	}
}

func checkReminders(t *testing.T, d conversation.Deps) {
	t.Helper()
	s, all := conversation.Schedules(d), conversation.JobHandlers(d)
	// Việc không có lịch: chuẩn bị tin riêng tự động (Task 166), đúng một handler.
	j := slices.DeleteFunc(slices.Clone(all), func(h queue.Handler) bool { return h.Kind().Name == prepareAutoPrivateKind })
	if len(all)-len(j) != 1 {
		t.Errorf("việc chuẩn bị tin riêng tự động: %d handler, mong 1", len(all)-len(j))
	}
	want := []string{
		conversation.SendRevealSchedule, conversation.FollowUpSchedule, conversation.PresenceCleanupSchedule,
		"conversation.reminders", "channel.comment_poll", conversation.RetentionSchedule,
	}
	if len(s) != len(want) || len(j) != len(want) {
		t.Fatalf("lịch %+v, việc %d", s, len(j))
	}
	for i, name := range want {
		every := time.Minute
		switch name {
		case "channel.comment_poll":
			every = 30 * time.Second
		case conversation.SendRevealSchedule:
			every = 15 * time.Second
		case conversation.RetentionSchedule:
			every = time.Hour
		case conversation.FollowUpSchedule:
			every = 300 * time.Second
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
