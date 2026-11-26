package conversation_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

func (e *chatEnv) createReminder(u identity.AuthUser, conv uuid.UUID, in conversation.ReminderInput, now time.Time,
) (conversation.Reminder, error) {
	var r conversation.Reminder
	err := e.tx(func(tx pgx.Tx) error {
		var err error
		r, err = conversation.CreateReminder(e.t.Context(), tx, u, conv, in, now)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// Tạo nhắc (Task 169): giờ nhắc phải ở tương lai và không quá 90 ngày; Page không được trả lời → không có; giao người
// khác cần conversation.assign và người đó phải đọc được hội thoại.
func TestCreateReminder(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	now := at(14, 0, 0)
	outsider := e.user("ngoai@elimaz.test", e.pageB.String(), "conversation.read") // không có quyền ở Page A
	cases := []struct {
		name string
		u    identity.AuthUser
		conv uuid.UUID
		in   conversation.ReminderInput
		want error
	}{
		{"giờ đã qua", e.sale, e.convA, conversation.ReminderInput{RemindAt: now.Add(-time.Minute)}, conversation.ErrReminderInvalid},
		{"đúng bây giờ", e.sale, e.convA, conversation.ReminderInput{RemindAt: now}, conversation.ErrReminderInvalid},
		{"quá 90 ngày", e.sale, e.convA, conversation.ReminderInput{RemindAt: now.Add(90*24*time.Hour + time.Second)}, conversation.ErrReminderInvalid},
		{"Page B không được trả lời", e.sale, e.convB, conversation.ReminderInput{RemindAt: now.Add(time.Hour)}, conversation.ErrNotFound},
		{"hội thoại không có", e.sale, uuid.New(), conversation.ReminderInput{RemindAt: now.Add(time.Hour)}, conversation.ErrNotFound},
		{"chỉ đọc không tạo được", e.viewer, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour)}, conversation.ErrNotFound},
		{"sale giao người khác", e.sale, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour), UserID: &e.lead.UserID}, conversation.ErrReminderAssignForbidden},
		{"giao người chỉ đọc (không tự đóng được nhắc)", e.lead, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour), UserID: &e.viewer.UserID}, conversation.ErrAssigneeNotAllowed},
		{"giao người chỉ đọc Page khác", e.lead, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour), UserID: &outsider.UserID}, conversation.ErrAssigneeNotAllowed},
		{"giao người không có", e.lead, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour), UserID: new(uuid.New())}, conversation.ErrAssigneeNotAllowed},
	}
	for _, c := range cases {
		_, err := e.createReminder(c.u, c.conv, c.in, now)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, mong %v", c.name, err, c.want)
		}
	}
	r, err := e.createReminder(e.sale, e.convA, conversation.ReminderInput{RemindAt: now.Add(90 * 24 * time.Hour), Note: "gọi lại chốt size"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.UserID != e.sale.UserID || r.Status != "pending" || r.Source != "manual" || r.Note != "gọi lại chốt size" || r.Version != 1 {
		t.Errorf("nhắc của tôi: %+v", r)
	}
	r, err = e.createReminder(e.lead, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour), UserID: &e.sale.UserID}, now)
	if err != nil || r.UserID != e.sale.UserID {
		t.Fatalf("trưởng nhóm giao cho sale: %+v, %v", r, err)
	}
	// Người được giao tự đóng được nhắc của mình.
	e.mustTx(func(tx pgx.Tx) error {
		got, err := conversation.CompleteReminder(t.Context(), tx, e.sale, r.ID)
		if err != nil || got.Status != "done" {
			t.Errorf("sale đóng nhắc được giao: %+v, %v", got, err)
		}
		return nil
	})
}

// Nhắc tới hạn (Task 169 Step 1): nhắc 15:00; lịch 14:59:30 chưa bắn; lịch 15:00:30 → fired, hội thoại done → open,
// báo hiệu reminder.fired theo Page; chạy lại không bắn lần hai; /me/reminders của người khác không thấy.
func TestFireDueReminders(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	r, err := e.createReminder(e.sale, e.convA, conversation.ReminderInput{RemindAt: at(15, 0, 0), Note: "bí mật"}, at(14, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	clock := at(14, 59, 30)
	bus := &realtimetest.RecordingBus{}
	deps := conversation.Deps{DB: e.staff.App, Bus: bus, Now: func() time.Time { return clock }}
	handler := jobFor(t, deps, "conversation.reminders")
	run := func() {
		t.Helper()
		e.mustTx(func(tx pgx.Tx) error {
			_, err := handler.Handle(t.Context(), queue.Job{TenantID: &e.tenant}, tx)
			return err //nolint:wrapcheck // test
		})
	}
	run()
	if n := e.count(`SELECT count(*) FROM conversation.reminders WHERE status = 'fired'`); n != 0 || len(bus.Signals()) != 0 {
		t.Fatalf("14:59:30 đã bắn %d, %d báo hiệu", n, len(bus.Signals()))
	}
	clock = at(15, 0, 30)
	run()
	run()
	if n := e.count(`SELECT count(*) FROM conversation.reminders WHERE id = $1 AND status = 'fired' AND fired_at = $2 AND version = 2`,
		r.ID, at(15, 0, 30)); n != 1 {
		t.Errorf("nhắc chưa bắn đúng một lần lúc 15:00:30")
	}
	if n := e.count(`SELECT count(*) FROM conversation.conversations WHERE id = $1 AND status = 'open' AND version = 2`, e.convA); n != 1 {
		t.Errorf("hội thoại chưa mở lại")
	}
	sigs := bus.Signals()
	if len(sigs) != 2 {
		t.Fatalf("báo hiệu: %+v", sigs)
	}
	for i, want := range []struct{ topic, kind, id string }{
		{conversation.TopicReminderFired, "reminder", r.ID.String()},
		{conversation.TopicConversationChanged, "conversation", e.convA.String()},
	} {
		s := sigs[i]
		if s.Topic != want.topic || s.Entity.Type != want.kind || s.Entity.ID != want.id || s.Requires != "conversation.read" ||
			s.Scope == nil || s.Scope.Type != identity.ScopePage || s.Scope.ID != e.pageA.String() || s.TenantID != e.tenant {
			t.Errorf("báo hiệu %d: %+v", i, s)
		}
	}
	mine := func(u identity.AuthUser, status string) []conversation.Reminder {
		var out []conversation.Reminder
		e.mustTx(func(tx pgx.Tx) error {
			var err error
			out, err = conversation.ListMyReminders(t.Context(), tx, u.UserID, status)
			return err //nolint:wrapcheck // test
		})
		return out
	}
	if got := mine(e.sale, conversation.ReminderFired); len(got) != 1 || got[0].ID != r.ID || got[0].FiredAt == nil {
		t.Errorf("nhắc đã bắn của sale: %+v", got)
	}
	if got := mine(e.lead, conversation.ReminderFired); len(got) != 0 {
		t.Errorf("người khác thấy nhắc của sale: %+v", got)
	}
	if got := mine(e.sale, conversation.ReminderPending); len(got) != 0 {
		t.Errorf("còn nhắc chờ: %+v", got)
	}
	e.mustTx(func(tx pgx.Tx) error {
		_, err := conversation.ListMyReminders(t.Context(), tx, e.sale.UserID, "done")
		if !errors.Is(err, conversation.ErrReminderInvalid) {
			t.Errorf("lọc trạng thái lạ: %v", err)
		}
		return nil
	})
}

// Đóng nhắc (Task 169 Step 1): hai lần "xong" song song → một thành công, một ErrReminderClosed; người khác không có
// quyền giao việc không thấy nhắc; trưởng nhóm (conversation.assign) hủy được nhắc của người khác.
func TestCloseReminder(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	now := at(14, 0, 0)
	r, err := e.createReminder(e.sale, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	other := e.user("sale2@elimaz.test", e.pageA.String(), "conversation.read", "conversation.reply")
	if err := e.tx(func(tx pgx.Tx) error {
		_, err := conversation.CompleteReminder(t.Context(), tx, other, r.ID)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("sale khác đóng nhắc: %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			errs[i] = e.tx(func(tx pgx.Tx) error {
				_, err := conversation.CompleteReminder(t.Context(), tx, e.sale, r.ID)
				return err //nolint:wrapcheck // test
			})
		})
	}
	wg.Wait()
	ok, closed := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, conversation.ErrReminderClosed):
			closed++
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || closed != 1 {
		t.Errorf("hai lần xong song song: %d thành công, %d đã đóng", ok, closed)
	}
	r2, err := e.createReminder(e.sale, e.convA, conversation.ReminderInput{RemindAt: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	e.mustTx(func(tx pgx.Tx) error {
		got, err := conversation.CancelReminder(t.Context(), tx, e.lead, r2.ID)
		if err != nil || got.Status != "cancelled" || got.Version != 2 {
			t.Errorf("trưởng nhóm hủy: %+v, %v", got, err)
		}
		return nil
	})
}
