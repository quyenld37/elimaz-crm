package automation_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// CH11 (phone_only): comment có SĐT → 1 lệnh ẩn, máy Meta giả is_hidden = true, tin is_hidden = true (vẫn còn dòng tin,
// có lịch sử nguồn automation), lượt done; comment không có SĐT → không lượt, không lệnh; comment của Page → không bao
// giờ.
func TestAutoHide_PhoneOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HidePhoneOnly)
	now := time.Now().UTC().Add(-time.Minute)
	withPhone := e.comment(postA, userA, "chốt size M 0912 345 678", now)
	e.deliver(withPhone)
	if !e.hiddenOnPage(withPhone.CommentID) || e.hideCalls(withPhone.CommentID) != 1 {
		t.Fatalf("Page: ẩn = %v, %d lần gọi", e.hiddenOnPage(withPhone.CommentID), e.hideCalls(withPhone.CommentID))
	}
	if hidden, revs := e.hiddenInCRM(withPhone.CommentID); !hidden || revs != 1 {
		t.Fatalf("CRM: is_hidden = %v, lịch sử automation = %d", hidden, revs)
	}
	if out, rows, jobs := e.run(withPhone.CommentID); out != string(automation.OutcomeDone) || rows != 1 || jobs != 1 {
		t.Fatalf("lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	plain := e.comment(postA, userA, "size 39 cân 50kg còn không ạ", now)
	e.deliver(plain)
	if out, rows, jobs := e.run(plain.CommentID); rows != 0 || jobs != 0 || e.hideCalls(plain.CommentID) != 0 {
		t.Fatalf("comment không SĐT: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if hidden, _ := e.hiddenInCRM(plain.CommentID); hidden {
		t.Fatal("comment không SĐT bị ẩn trong CRM")
	}
}

// all: mọi comment của khách bị ẩn; bài trong exceptPostIds → không; quy tắc tắt → không lượt nào; "off" → không.
func TestAutoHide_AllExceptAndOff(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HideAll, postB)
	now := time.Now().UTC().Add(-time.Minute)
	a := e.comment(postA, userA, "còn hàng không shop", now)
	b := e.comment(postB, userA, "còn hàng không shop", now)
	e.deliver(a)
	e.deliver(b)
	if !e.hiddenOnPage(a.CommentID) {
		t.Fatal("all: comment bài A chưa bị ẩn")
	}
	if _, rows, jobs := e.run(b.CommentID); rows != 0 || jobs != 0 || e.hideCalls(b.CommentID) != 0 {
		t.Fatalf("bài ngoại lệ: %d dòng, %d lệnh", rows, jobs)
	}
	e.setRule(false, automation.HideAll)
	c := e.comment(postA, userA, "0912345678", now)
	e.deliver(c)
	e.setRule(true, automation.HideOff)
	d := e.comment(postA, userA, "0912345678", now)
	e.deliver(d)
	for _, p := range []string{c.CommentID, d.CommentID} {
		if _, rows, jobs := e.run(p); rows != 0 || jobs != 0 {
			t.Fatalf("tắt / off: %d dòng, %d lệnh", rows, jobs)
		}
	}
}

// Comment của chính Page không bao giờ kích hoạt (ADR-13), kể cả chế độ all.
func TestAutoHide_PageCommentNever(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HideAll)
	now := time.Now().UTC().Add(-time.Minute)
	cust := e.comment(postA, userA, "giá bao nhiêu ạ", now)
	e.deliver(cust)
	reply := e.comment(postA, pageExt, "Dạ shop inbox 0912345678 ạ", now)
	reply.ParentID = cust.CommentID
	e.deliver(reply)
	if _, rows, jobs := e.run(reply.CommentID); rows != 0 || jobs != 0 || e.hideCalls(reply.CommentID) != 0 {
		t.Fatalf("comment của Page: %d dòng, %d lệnh", rows, jobs)
	}
}

// Page read_only → skipped_read_only, 0 lệnh; sự kiện cũ 16 phút (hỏi bù) → skipped_stale, 0 lệnh.
func TestAutoHide_ReadOnlyAndStale(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HideAll)
	stale := e.comment(postA, userA, "0912345678", time.Now().UTC().Add(-16*time.Minute))
	e.enqueue(stale, webhooks.ViaBackfill)
	e.drain()
	if out, rows, jobs := e.run(stale.CommentID); out != string(automation.OutcomeSkippedStale) || rows != 1 || jobs != 0 {
		t.Fatalf("cũ: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET send_mode = 'read_only'`)
		return err //nolint:wrapcheck // test
	})
	ro := e.comment(postA, userA, "0912345678", time.Now().UTC().Add(-time.Minute))
	e.deliver(ro)
	if out, rows, jobs := e.run(ro.CommentID); out != string(automation.OutcomeSkippedReadOnly) || rows != 1 || jobs != 0 {
		t.Fatalf("chỉ xem: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if e.hideCalls(stale.CommentID)+e.hideCalls(ro.CommentID) != 0 {
		t.Fatal("có lời gọi ẩn tới Meta")
	}
}

// Xử lý lại: cùng comment tới qua webhook, đọc comment định kỳ và hỏi bù (ba việc vào khác Via) → đúng 1 lượt enqueued
// → done, 1 lệnh, 1 lời gọi Meta.
func TestAutoHide_ReprocessedOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HidePhoneOnly)
	p := e.comment(postA, userA, "ib mình 0987.654.321", time.Now().UTC().Add(-time.Minute))
	for _, via := range []string{webhooks.ViaWebhook, webhooks.ViaCommentPoll, webhooks.ViaBackfill} {
		e.enqueue(p, via)
	}
	e.drain()
	if out, rows, jobs := e.run(p.CommentID); out != string(automation.OutcomeDone) || rows != 1 || jobs != 1 {
		t.Fatalf("lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if n := e.hideCalls(p.CommentID); n != 1 {
		t.Fatalf("Meta nhận %d lời gọi ẩn", n)
	}
}

// Tin lịch sử (ViaHistory) → không dòng nào, không lệnh.
func TestAutoHide_HistoryNever(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HideAll)
	p := e.comment(postA, userA, "0912345678", time.Now().UTC().Add(-time.Minute))
	e.enqueue(p, webhooks.ViaHistory)
	e.drain()
	if _, rows, jobs := e.run(p.CommentID); rows != 0 || jobs != 0 {
		t.Fatalf("lịch sử: %d dòng, %d lệnh", rows, jobs)
	}
}

// Lệnh ẩn tự động không rõ đã tới chưa (mất kết nối sau khi Meta đọc yêu cầu) → in_doubt → lượt dropped, không lệnh
// thứ hai, không gửi lại.
func TestAutoHide_InDoubtDropped(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setRule(true, automation.HideAll)
	p := e.comment(postA, userA, "còn size S không", time.Now().UTC().Add(-time.Minute))
	e.fg.DropAfterRead(p.CommentID)
	e.deliver(p)
	e.drain()
	if out, rows, jobs := e.run(p.CommentID); out != string(automation.OutcomeDropped) || rows != 1 || jobs != 1 {
		t.Fatalf("lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'in_doubt'`,
		automation.KindHideCommand); n != 1 {
		t.Fatalf("lệnh in_doubt = %d", n)
	}
	if n := e.hideCalls(p.CommentID); n != 1 {
		t.Fatalf("Meta nhận %d lời gọi ẩn, muốn 1 (không gửi lại)", n)
	}
	if hidden, _ := e.hiddenInCRM(p.CommentID); hidden {
		t.Fatal("CRM ghi đã ẩn khi không rõ")
	}
}
