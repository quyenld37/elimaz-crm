package automation_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// fuEnv: env của Task 165 với đồng hồ giả (Deps.Now) dùng chung cho lịch, cổng của Run và lệnh gửi; worker dựng lại để
// lệnh dùng đồng hồ này; một mẫu chung "Dạ {ten_khach} ơi…".
type fuEnv struct {
	*env
	mu       sync.Mutex
	clock    time.Time
	template uuid.UUID
}

func newFuEnv(t *testing.T) *fuEnv {
	t.Helper()
	f := &fuEnv{env: newEnv(t), clock: time.Now().UTC()}
	f.d.Now = f.now
	f.worker = f.newWorker("w-fu")
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.reply_templates (tenant_id, shortcut, title, body)
			VALUES ($1, 'nhanlai', 'Nhắn lại', 'Dạ {ten_khach} ơi, shop nhắn lại mình nhé ạ') RETURNING id`, f.tenant).
			Scan(&f.template)
	})
	return f
}

// newFuEnvWithA: newFuEnv, quy tắc follow_up bật và hội thoại A (khách 5 giờ trước, nhân viên 4 giờ trước).
func newFuEnvWithA(t *testing.T) (*fuEnv, uuid.UUID) {
	t.Helper()
	f := newFuEnv(t)
	f.setFollowUp(true)
	a, _ := f.inbox(f.page, "psid-a", "Chị An", 5*time.Hour, 4*time.Hour)
	return f, a
}

func (f *fuEnv) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clock
}

func (f *fuEnv) advance(d time.Duration) {
	f.mu.Lock()
	f.clock = f.clock.Add(d)
	f.mu.Unlock()
}

// setFollowUp lưu quy tắc follow_up (afterHours 3, mẫu của env) bật/tắt.
func (f *fuEnv) setFollowUp(enabled bool) {
	f.t.Helper()
	cfg, err := json.Marshal(automation.FollowUpConfig{Kind: automation.KindFollowUp, AfterHours: 3, TemplateID: f.template})
	if err != nil {
		f.t.Fatal(err)
	}
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		cur, err := automation.LoadRule(f.t.Context(), tx, f.page, automation.KindFollowUp)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, _, err = automation.SaveRule(f.t.Context(), tx, f.page, automation.KindFollowUp, enabled, cfg, cur.Version, f.user)
		return err //nolint:wrapcheck // test
	})
}

// inbox tạo khách name và hội thoại hộp thư PSID psid trên Page page: tin khách cách đồng hồ custAgo, rồi (staffAgo > 0)
// tin nhân viên đã gửi cách staffAgo; mốc của hội thoại khớp các tin. Trả mã hội thoại và mã tin nhân viên.
func (f *fuEnv) inbox(page uuid.UUID, psid, name string, custAgo, staffAgo time.Duration) (conv, staffMsg uuid.UUID) {
	f.t.Helper()
	now := f.now()
	var cust uuid.UUID
	if err := tenancy.WithTenant(f.t.Context(), f.staff.Owner, f.tenant, func(tx pgx.Tx) (err error) {
		cust, err = customer.Create(f.t.Context(), tx, name)
		return err //nolint:wrapcheck // test
	}); err != nil {
		f.t.Fatal(err)
	}
	custAt := now.Add(-custAgo)
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(f.t.Context(), `INSERT INTO conversation.conversations (tenant_id, channel_account_id,
			customer_id, kind, external_thread_ref, last_message_at, last_customer_message_at)
			VALUES ($1, $2, $3, 'inbox', $4, $5, $5) RETURNING id`, f.tenant, page, cust, psid, custAt).Scan(&conv); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(f.t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id,
			channel_account_id, direction, kind, origin, body, sent_at) VALUES ($1, $2, $3, 'in', 'message', 'customer',
			'còn size M không ạ', $4)`, f.tenant, conv, page, custAt); err != nil {
			return err //nolint:wrapcheck // test
		}
		return nil
	})
	if staffAgo > 0 {
		staffMsg = f.staffSays(conv, page, now.Add(-staffAgo))
	}
	return conv, staffMsg
}

// staffSays chèn tin nhân viên đã gửi lúc at vào hội thoại conv và đẩy mốc tin nhân viên.
func (f *fuEnv) staffSays(conv, page uuid.UUID, at time.Time) (id uuid.UUID) {
	f.t.Helper()
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(f.t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id,
			channel_account_id, direction, kind, origin, body, sender_staff_id, send_status, sent_at)
			VALUES ($1, $2, $3, 'out', 'message', 'staff', 'Dạ còn ạ', $4, 'sent', $5) RETURNING id`,
			f.tenant, conv, page, f.user, at).Scan(&id); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(f.t.Context(), `UPDATE conversation.conversations SET last_staff_message_at = $2,
			last_message_at = GREATEST(last_message_at, $2) WHERE id = $1`, conv, at)
		return err //nolint:wrapcheck // test
	})
	return id
}

// sweep chạy lịch nhắn lại một lần (một tx tenant) và trả số lượt vừa ghi lệnh.
func (f *fuEnv) sweep() int {
	f.t.Helper()
	var n int
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) (err error) {
		n, err = automation.RunFollowUps(f.t.Context(), tx, f.d, f.now())
		return err //nolint:wrapcheck // test
	})
	return n
}

// fuRuns là các kết quả lượt follow_up của hội thoại conv (mọi tin nhân viên).
func (f *fuEnv) fuRuns(conv uuid.UUID) []string {
	f.t.Helper()
	var out []string
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(f.t.Context(), `SELECT outcome FROM conversation.automation_runs
			WHERE external_ref LIKE $1 ORDER BY created_at, id`, "fu:"+conv.String()+":%")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	return out
}

// autoMessages là tin automation của hội thoại conv: thân và trạng thái gửi.
func (f *fuEnv) autoMessages(conv uuid.UUID) (bodies, statuses []string) {
	f.t.Helper()
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(f.t.Context(), `SELECT coalesce(body, ''), coalesce(send_status, ''), coalesce(fail_reason, '')
			FROM conversation.messages WHERE conversation_id = $1 AND origin = 'automation' ORDER BY created_at, id`, conv)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var b, s, r string
			if err := rows.Scan(&b, &s, &r); err != nil {
				return err //nolint:wrapcheck // test
			}
			bodies, statuses = append(bodies, b), append(statuses, strings.TrimSuffix(s+"/"+r, "/"))
		}
		return rows.Err()
	})
	return bodies, statuses
}

// sentTo là các tin máy Meta giả nhận cho PSID psid.
func (f *fuEnv) sentTo(psid string) []fakegraph.Message {
	var out []fakegraph.Message
	for _, m := range f.fg.Messages() {
		if m.PSID == psid && m.FromPage {
			out = append(out, m)
		}
	}
	return out
}

func readerOf(tenant uuid.UUID, pages ...uuid.UUID) identity.AuthUser {
	u := identity.AuthUser{UserID: uuid.New(), TenantID: tenant, Kind: identity.KindStaff}
	for _, p := range pages {
		u.Grants = append(u.Grants, identity.Grant{
			Permission: "conversation.read",
			Scope:      identity.Scope{Type: identity.ScopePage, ID: p.String()},
		})
	}
	return u
}

func (f *fuEnv) list(u identity.AuthUser, pageID *uuid.UUID, afterHours int) []automation.FollowUpItem {
	f.t.Helper()
	var out []automation.FollowUpItem
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) (err error) {
		out, err = automation.ListFollowUps(f.t.Context(), tx, u, pageID, afterHours, 50, f.now())
		return err //nolint:wrapcheck // test
	})
	return out
}

// CH16 danh sách (afterHours 3): A nhân viên nói cuối 4 giờ trước, khách 5 giờ → có; B khách trả lời sau → không; C
// khách nói cuối 23 giờ 58 phút (cửa sổ còn dưới 5 phút) → không, C2 cửa sổ còn 10 phút → có; D do_not_contact →
// không; E thuộc Page không được xem → không; F khách chặn Page, G đã xong, H nhân viên mới nói 2 giờ → không; I tin
// cuối là tin tự động (chưa nhân viên nào trả lời) → không. Sắp theo cửa sổ đóng sớm trước; pageId không được xem →
// rỗng; afterHours ngoài 1–23 → ErrInvalidFollowUp.
func TestFollowUps_List(t *testing.T) {
	t.Parallel()
	f := newFuEnv(t)
	other := f.staff.ChannelAccount(t, f.tenant, facebook.Key, "104000000000999", "Page khác")
	a, _ := f.inbox(f.page, "psid-a", "Chị An", 5*time.Hour, 4*time.Hour)
	b, _ := f.inbox(f.page, "psid-b", "Chị Bình", 5*time.Hour, 4*time.Hour)
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.conversations SET last_customer_message_at = $2 WHERE id = $1`,
			b, f.now().Add(-time.Hour))
		return err //nolint:wrapcheck // test
	})
	f.inbox(f.page, "psid-c", "Chị Chi", 23*time.Hour+58*time.Minute, 23*time.Hour)
	c2, _ := f.inbox(f.page, "psid-c2", "Chị Cúc", 23*time.Hour+50*time.Minute, 23*time.Hour)
	d, _ := f.inbox(f.page, "psid-d", "Chị Dung", 5*time.Hour, 4*time.Hour)
	f.inbox(other, "psid-e", "Chị Em", 5*time.Hour, 4*time.Hour)
	fb, _ := f.inbox(f.page, "psid-f", "Chị Phương", 5*time.Hour, 4*time.Hour)
	g, _ := f.inbox(f.page, "psid-g", "Chị Giang", 5*time.Hour, 4*time.Hour)
	f.inbox(f.page, "psid-h", "Chị Hà", 5*time.Hour, 2*time.Hour)
	i, _ := f.inbox(f.page, "psid-i", "Chị Ivy", 5*time.Hour, 0)
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE customer.customers SET do_not_contact = true, do_not_contact_at = now()
			WHERE id = (SELECT customer_id FROM conversation.conversations WHERE id = $1)`, d); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(t.Context(), `UPDATE conversation.conversations SET blocked_by_user = true WHERE id = $1`, fb); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(t.Context(), `UPDATE conversation.conversations SET status = 'done' WHERE id = $1`, g); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			direction, kind, origin, body, send_status, sent_at) VALUES ($1, $2, $3, 'out', 'message', 'automation', 'Chào',
			'sent', $4)`, f.tenant, i, f.page, f.now().Add(-4*time.Hour))
		return err //nolint:wrapcheck // test
	})

	got := f.list(readerOf(f.tenant, f.page), nil, 3)
	if len(got) != 2 || got[0].ConversationID != c2 || got[1].ConversationID != a {
		t.Fatalf("danh sách = %+v, muốn [C2, A]", got)
	}
	first := got[1]
	if first.PageID != f.page || first.CustomerName != "Chị An" || first.CustomerID == uuid.Nil ||
		!first.LastStaffMessageAt.Equal(f.now().Add(-4*time.Hour).Truncate(time.Microsecond)) ||
		!first.WindowEndsAt.Equal(f.now().Add(19*time.Hour).Truncate(time.Microsecond)) {
		t.Fatalf("mục A = %+v", first)
	}
	if got := f.list(readerOf(f.tenant, f.page), &other, 3); len(got) != 0 {
		t.Fatalf("pageId không được xem: %+v", got)
	}
	if got := f.list(readerOf(f.tenant, f.page, other), &other, 3); len(got) != 1 {
		t.Fatalf("pageId Page khác (được xem): %+v", got)
	}
	if got := f.list(readerOf(f.tenant, f.page), nil, 5); len(got) != 1 || got[0].ConversationID != c2 {
		t.Fatalf("afterHours 5 (A mới im 4 giờ): %+v, muốn [C2]", got)
	}
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		for _, h := range []int{0, 24} {
			if _, err := automation.ListFollowUps(t.Context(), tx, readerOf(f.tenant, f.page), nil, h, 50, f.now()); !errors.Is(err, automation.ErrInvalidFollowUp) {
				t.Errorf("afterHours %d: lỗi %v", h, err)
			}
		}
		return nil
	})
}

// CH16 tự gửi: quy tắc tắt (mặc định) → 0 lượt; bật → lịch chạy → đúng 1 lệnh cho A, máy Meta giả nhận đúng 1 tin chữ
// của mẫu đã điền tên, tin origin automation sent, lượt done; chạy lại → 0. Nhân viên gửi tin mới, 3 giờ sau khách vẫn
// im → lượt mới (khác external_ref), tin nhắn lại tự động không tính là tin nhân viên (không sinh lượt thứ ba).
func TestFollowUps_ScheduleOneStep(t *testing.T) {
	t.Parallel()
	f := newFuEnv(t)
	a, _ := f.inbox(f.page, "psid-a", "Chị An", 5*time.Hour, 4*time.Hour)
	if n := f.sweep(); n != 0 || len(f.fuRuns(a)) != 0 {
		t.Fatalf("quy tắc tắt: %d lệnh, lượt %v", n, f.fuRuns(a))
	}
	f.setFollowUp(true)
	if n := f.sweep(); n != 1 {
		t.Fatalf("bật: %d lệnh, muốn 1", n)
	}
	f.drain()
	sent := f.sentTo("psid-a")
	if len(sent) != 1 || sent[0].Text != "Dạ Chị An ơi, shop nhắn lại mình nhé ạ" {
		t.Fatalf("Meta nhận %+v", sent)
	}
	if bodies, st := f.autoMessages(a); len(bodies) != 1 || st[0] != "sent" || bodies[0] != sent[0].Text {
		t.Fatalf("tin tự động %v %v", bodies, st)
	}
	if runs := f.fuRuns(a); len(runs) != 1 || runs[0] != string(automation.OutcomeDone) {
		t.Fatalf("lượt %v", runs)
	}
	if n := f.sweep(); n != 0 {
		t.Fatalf("chạy lại: %d lệnh", n)
	}
	f.advance(4 * time.Hour) // tin tự động đã 4 giờ, khách vẫn im: không phải tin nhân viên → không lượt mới
	if n := f.sweep(); n != 0 {
		t.Fatalf("sau tin tự động: %d lệnh", n)
	}
	f.staffSays(a, f.page, f.now())
	f.advance(3*time.Hour + time.Minute)
	if n := f.sweep(); n != 1 {
		t.Fatalf("tin nhân viên mới, 3 giờ sau: %d lệnh, muốn 1", n)
	}
	f.drain()
	if runs := f.fuRuns(a); len(runs) != 2 || len(f.sentTo("psid-a")) != 2 {
		t.Fatalf("lượt %v, Meta nhận %d tin", runs, len(f.sentTo("psid-a")))
	}
}

// Page read_only → lượt skipped_read_only, 0 lệnh, Meta không nhận gì; mẫu đã xóa → skipped_policy; hai lịch chạy song
// song cùng lúc → đúng 1 lượt, 1 lệnh.
func TestFollowUps_GatesAndConcurrency(t *testing.T) {
	t.Parallel()
	f, a := newFuEnvWithA(t)
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET send_mode = 'read_only'`)
		return err //nolint:wrapcheck // test
	})
	if n := f.sweep(); n != 0 {
		t.Fatalf("read_only: %d lệnh", n)
	}
	if runs := f.fuRuns(a); len(runs) != 1 || runs[0] != string(automation.OutcomeSkippedReadOnly) {
		t.Fatalf("read_only: lượt %v", runs)
	}
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET send_mode = 'enabled'`); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `UPDATE conversation.reply_templates SET deleted_at = now()`)
		return err //nolint:wrapcheck // test
	})
	b, _ := f.inbox(f.page, "psid-b", "Chị Bình", 5*time.Hour, 4*time.Hour)
	if n := f.sweep(); n != 0 {
		t.Fatalf("mẫu đã xóa: %d lệnh", n)
	}
	if runs := f.fuRuns(b); len(runs) != 1 || runs[0] != string(automation.OutcomeSkippedPolicy) {
		t.Fatalf("mẫu đã xóa: lượt %v", runs)
	}
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.reply_templates SET deleted_at = NULL`)
		return err //nolint:wrapcheck // test
	})
	c, _ := f.inbox(f.page, "psid-c", "Chị Chi", 5*time.Hour, 4*time.Hour)
	var wg sync.WaitGroup
	counts := make([]int, 2)
	for k := range counts {
		wg.Go(func() {
			if err := tenancy.WithTenant(t.Context(), f.staff.App, f.tenant, func(tx pgx.Tx) (err error) {
				counts[k], err = automation.RunFollowUps(t.Context(), tx, f.d, f.now())
				return err //nolint:wrapcheck // test
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	jobs := f.staff.CountIn(t, f.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, automation.KindAutoSendCommand)
	if counts[0]+counts[1] != 1 || jobs != 1 || len(f.fuRuns(c)) != 1 {
		t.Fatalf("song song: %v lệnh, %d việc, lượt %v", counts, jobs, f.fuRuns(c))
	}
}

// Lệnh nhắn lại tự động không rõ đã gửi (kết nối đứt sau khi Meta đọc yêu cầu) → lượt dropped, tin discarded lý do
// auto_dropped (tin tự động không rõ thì bỏ, Task 167), không gửi lại; Meta từ chối (khách chặn) → lượt failed, tin
// failed.
func TestFollowUps_InDoubtDropped(t *testing.T) {
	t.Parallel()
	f, a := newFuEnvWithA(t)
	f.fg.DropAfterRead(pageExt + "/messages")
	if n := f.sweep(); n != 1 {
		t.Fatalf("%d lệnh", n)
	}
	f.drain()
	if runs := f.fuRuns(a); len(runs) != 1 || runs[0] != string(automation.OutcomeDropped) {
		t.Fatalf("in_doubt: lượt %v", runs)
	}
	if _, st := f.autoMessages(a); len(st) != 1 || st[0] != "discarded/auto_dropped" {
		t.Fatalf("in_doubt: tin %v", st)
	}
	if calls := len(f.fg.Calls(pageExt + "/messages")); calls != 1 {
		t.Fatalf("in_doubt: Meta nhận %d lần gọi, muốn 1", calls)
	}
	b, _ := f.inbox(f.page, "psid-b", "Chị Bình", 5*time.Hour, 4*time.Hour)
	f.fg.FailNext(pageExt+"/messages", fakegraph.GraphErrorSpec{Code: 551, Type: "OAuthException", Message: "This person isn't available right now."})
	f.sweep()
	f.drain()
	if runs := f.fuRuns(b); len(runs) != 1 || runs[0] != string(automation.OutcomeFailed) {
		t.Fatalf("bị từ chối: lượt %v", runs)
	}
	if _, st := f.autoMessages(b); len(st) != 1 || !strings.HasPrefix(st[0], "failed") {
		t.Fatalf("bị từ chối: tin %v", st)
	}
}

// Bật follow_up với templateId không phải mẫu dùng được (không có, của Page khác) → ErrInvalidRule; tắt thì lưu được.
func TestFollowUps_SaveRuleChecksTemplate(t *testing.T) {
	t.Parallel()
	f := newFuEnv(t)
	other := f.staff.ChannelAccount(t, f.tenant, facebook.Key, "104000000000998", "Page khác")
	var foreign uuid.UUID
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.reply_templates (tenant_id, channel_account_id, shortcut,
			title, body) VALUES ($1, $2, 'khac', 'Khác', 'Dạ') RETURNING id`, f.tenant, other).Scan(&foreign)
	})
	for _, tpl := range []uuid.UUID{uuid.New(), foreign, uuid.Nil} {
		cfg, _ := json.Marshal(automation.FollowUpConfig{Kind: automation.KindFollowUp, AfterHours: 3, TemplateID: tpl})
		err := f.staff.TryExec(t.Context(), f.tenant, func(tx pgx.Tx) error {
			_, _, err := automation.SaveRule(t.Context(), tx, f.page, automation.KindFollowUp, true, cfg, 0, f.user)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, automation.ErrInvalidRule) {
			t.Errorf("mẫu %s: lỗi %v", tpl, err)
		}
		if err := f.staff.TryExec(t.Context(), f.tenant, func(tx pgx.Tx) error {
			_, _, err := automation.SaveRule(t.Context(), tx, f.page, automation.KindFollowUp, false, cfg, 0, f.user)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Errorf("tắt, mẫu %s: %v", tpl, err)
		}
		f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `DELETE FROM conversation.automation_rules WHERE kind = 'follow_up'`)
			return err //nolint:wrapcheck // test
		})
	}
}

// Lô của lịch không bị chiếm: 101 hội thoại (khách nhắn 6 giờ trước, nhân viên 4 giờ trước) đã có lượt ở lần lịch đầu
// (lô 100 rồi lô sau); một hội thoại mới tới hạn (khách 5 giờ trước, cửa sổ đóng muộn hơn mọi hội thoại cũ) vẫn được
// nhắn ở lần lịch kế tiếp.
func TestFollowUps_BatchNotStarved(t *testing.T) {
	t.Parallel()
	f := newFuEnv(t)
	f.setFollowUp(true)
	var cust uuid.UUID
	if err := tenancy.WithTenant(t.Context(), f.staff.Owner, f.tenant, func(tx pgx.Tx) (err error) {
		cust, err = customer.Create(t.Context(), tx, "Chị Nhiều")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	now := f.now()
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `WITH c AS (
			INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref,
				last_message_at, last_customer_message_at, last_staff_message_at)
			SELECT $1, $2, $3, 'inbox', 'psid-old-' || g, $5, $4, $5 FROM generate_series(1, 101) AS g RETURNING id)
			INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, direction, kind, origin, body,
				send_status, sent_at)
			SELECT $1, c.id, $2, 'out', 'message', 'staff', 'Dạ còn ạ', 'sent', $5 FROM c`,
			f.tenant, f.page, cust, now.Add(-6*time.Hour), now.Add(-4*time.Hour))
		return err //nolint:wrapcheck // test
	})
	if n := f.sweep() + f.sweep(); n != 101 {
		t.Fatalf("hai lần lịch đầu: %d lệnh, muốn 101", n)
	}
	fresh, _ := f.inbox(f.page, "psid-new", "Chị Mới", 5*time.Hour, 4*time.Hour)
	if n := f.sweep(); n != 1 || len(f.fuRuns(fresh)) != 1 {
		t.Fatalf("hội thoại mới: %d lệnh, lượt %v", n, f.fuRuns(fresh))
	}
}
