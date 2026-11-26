package live_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// outcomes gom kết quả nhắc theo đơn.
func outcomes(t *testing.T, rs []conversation.RemindOutcome) map[uuid.UUID]conversation.RemindOutcome {
	t.Helper()
	m := make(map[uuid.UUID]conversation.RemindOutcome, len(rs))
	for _, r := range rs {
		m[r.OrderID] = r
	}
	if len(m) != len(rs) {
		t.Fatalf("kết quả trùng đơn: %+v", rs)
	}
	return m
}

func (e *claimEnv) mustRemind(now time.Time, ids ...uuid.UUID) map[uuid.UUID]conversation.RemindOutcome {
	e.t.Helper()
	rs, err := e.remind(e.an, e.fbSession, now, ids...)
	if err != nil {
		e.t.Fatal(err)
	}
	return outcomes(e.t, rs)
}

// LV05: phiên có 3 đơn còn nháp (2 khách còn cửa sổ hộp thư, 1 khách quá 24 giờ và comment đã nhắn riêng lúc chốt), 1
// đơn đã xác nhận, 1 đơn hết hạn. "Nhắc tất cả" → đúng 3 kết quả: 2 queued (tin thường trong hộp thư, người gửi là người
// bấm, có mã đơn), 1 skipped "Đã quá 24 giờ và đã nhắn riêng cho comment này"; máy Meta giả nhận đúng 2 tin thường; ghi
// nhận chính remind_count 1, last_reminded_at = lúc bấm. Bấm lại trong 60 phút → 2 đơn đã nhắc skipped "Đã nhắc lúc
// HH:MM", đơn kia vẫn lý do chính sách; không tin mới. 61 phút sau → nhắc được lại. Chọn đơn đã xác nhận, đơn lạ →
// skipped kèm lý do, không chặn đơn khác.
func TestRemind_LV05(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v, now := e.variant(20), time.Now()
	a, b, c, d, x := e.fiveClaims(v, now, "61000000001731")
	e.confirmWithAddress(d.order)
	e.exec(`UPDATE ord.orders SET draft_expires_at = $2 WHERE id = $1`, x.order.ID, now.Add(-time.Minute).UTC())
	if n := e.expireDrafts(now); n != 1 {
		t.Fatalf("expire_drafts = %d", n)
	}

	got := e.mustRemind(now)
	if len(got) != 3 {
		t.Fatalf("nhắc tất cả: %d kết quả, muốn 3 (%+v)", len(got), got)
	}
	for _, p := range []pendingCust{a, b} {
		e.checkReminded(p, got[p.order.ID], now)
	}
	if r := got[c.order.ID]; r.Outcome != conversation.RemindSkipped || r.Reason != conversation.RemindNoChannelText || r.MessageID != nil {
		t.Errorf("đơn quá 24 giờ: %+v", r)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims WHERE message_id = $1 AND remind_count = 0
		AND last_reminded_at IS NULL`, c.c.id); n != 1 {
		t.Errorf("đơn bị bỏ vẫn ghi nhắc: %d", n)
	}
	e.drain()
	if na, nb, nc := e.sendCalls(a.psid), e.sendCalls(b.psid), e.sendCalls(c.psid); na != 1 || nb != 1 || nc != 0 {
		t.Errorf("Meta nhận tin thường = %d %d %d, muốn 1 1 0", na, nb, nc)
	}

	again := e.mustRemind(now.Add(10 * time.Minute))
	at := now.In(time.FixedZone("ICT", 7*3600)).Format("15:04")
	for _, p := range []pendingCust{a, b} {
		if r := again[p.order.ID]; r.Outcome != conversation.RemindSkipped || r.Reason != "Đã nhắc lúc "+at || r.MessageID != nil {
			t.Errorf("bấm lại %s: %+v, muốn đã nhắc lúc %s", p.order.Code, r, at)
		}
	}
	if r := again[c.order.ID]; r.Outcome != conversation.RemindSkipped || r.Reason != conversation.RemindNoChannelText {
		t.Errorf("bấm lại đơn quá 24 giờ: %+v", r)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE kind = 'message' AND direction = 'out'`); n != 2 {
		t.Errorf("tin thường gửi ra sau bấm lại = %d, muốn 2", n)
	}

	e.checkRemindLater(a, d, now)
}

// checkReminded: kết quả r của khách p là queued, tin thường queued trong hộp thư của p, người gửi An, có mã đơn; ghi
// nhận chính remind_count 1, last_reminded_at = now.
func (e *claimEnv) checkReminded(p pendingCust, r conversation.RemindOutcome, now time.Time) {
	e.t.Helper()
	if r.Outcome != conversation.RemindQueued || r.MessageID == nil {
		e.t.Fatalf("đơn %s: %+v", p.order.Code, r)
	}
	kind, status, _, conv := e.askRow(*r.MessageID)
	if kind != "message" || status != "queued" || conv != p.inbox {
		e.t.Errorf("tin nhắc %s = %s %s %s, muốn message queued %s", p.order.Code, kind, status, conv, p.inbox)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE id = $1 AND sender_staff_id = $2 AND body LIKE '%' || $3 || '%'`,
		*r.MessageID, e.an.UserID, p.order.Code); n != 1 {
		e.t.Errorf("tin nhắc %s: người gửi, mã đơn trong nội dung = %d", p.order.Code, n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims WHERE message_id = $1 AND remind_count = 1
		AND last_reminded_at = $2`, p.c.id, now.UTC().Truncate(time.Microsecond)); n != 1 {
		e.t.Errorf("ghi nhận nhắc %s = %d", p.order.Code, n)
	}
}

// checkRemindLater: 61 phút sau lần nhắc đầu, nhắc đơn của a (được lại), đơn đã xác nhận của d và một đơn lạ.
func (e *claimEnv) checkRemindLater(a, d pendingCust, now time.Time) {
	e.t.Helper()
	t := e.t
	later := e.mustRemind(now.Add(61*time.Minute), a.order.ID, d.order.ID, uuid.New())
	if len(later) != 3 || later[a.order.ID].Outcome != conversation.RemindQueued {
		t.Errorf("61 phút sau, đơn A: %+v", later)
	}
	if r := later[d.order.ID]; r.Outcome != conversation.RemindSkipped || r.Reason != conversation.RemindNotDraftText {
		t.Errorf("đơn đã xác nhận: %+v", r)
	}
	for id, r := range later {
		if id != a.order.ID && id != d.order.ID && (r.Outcome != conversation.RemindSkipped || r.Reason != conversation.RemindNotInSessionText) {
			t.Errorf("đơn lạ: %+v", r)
		}
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_comment_claims WHERE message_id = $1 AND remind_count = 2`, a.c.id); n != 1 {
		t.Errorf("remind_count đơn A sau lần hai = %d", n)
	}
}

// Hai nhân viên bấm "Nhắc tất cả" cùng lúc (10 lượt) → mỗi đơn đúng một tin nhắc; các kết quả còn lại skipped "Đã nhắc
// lúc"; không lỗi, không deadlock.
func TestRemind_ConcurrentOneMessagePerOrder(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(20)
	now := time.Now()
	ps := []pendingCust{
		e.claimed("An", "6100000000173201", v, now.Add(-time.Hour), now),
		e.claimed("Bảo", "6100000000173202", v, now.Add(-time.Hour), now),
		e.claimed("Chi", "6100000000173203", v, now.Add(-time.Hour), now),
	}
	dl := e.deadlocks()
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := range 10 {
		u := e.an
		if i%2 == 1 {
			u = e.binh
		}
		wg.Go(func() {
			_, err := e.remind(u, e.fbSession, now)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range ps {
		if n := e.count(`SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND kind = 'message'
			AND direction = 'out'`, p.inbox); n != 1 {
			t.Errorf("đơn %s: %d tin nhắc, muốn 1", p.order.Code, n)
		}
	}
	if got := e.deadlocks(); got != dl {
		t.Errorf("deadlock tăng %d → %d", dl, got)
	}
}

// Chính sách nhắc: Page chỉ xem → skipped lý do chỉ xem; khách quá 24 giờ mà comment chưa nhắn riêng (chốt không xin
// địa chỉ) → nhắn riêng comment đó (tin private_reply); không có hộp thư và comment đã quá 7 ngày → skipped có lý do.
// Không live.operate trên Page → ErrNotFound; phiên lạ → ErrNotFound; mẫu lạ → lỗi, không gì được ghi.
func TestRemind_Policy(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v := e.variant(20)
	now := time.Now()

	cust := e.customer("Hạnh")
	cm := e.comment(e.fbPage, cust, "6100000000173301", "chốt", now.Add(-time.Hour))
	e.inbox(e.fbPage, cust, "6100000000173301", now.Add(-48*time.Hour))
	quiet := newClaim(v, 1)
	quiet.AskAddress = false
	priv := e.mustClaim(cm, quiet)

	old := e.customer("Oanh")
	oc := e.comment(e.fbPage, old, "6100000000173302", "chốt", now.Add(-8*24*time.Hour))
	gone := e.mustClaim(oc, quiet)

	got := e.mustRemind(now)
	r := got[priv.Order.ID]
	if r.Outcome != conversation.RemindQueued || r.MessageID == nil {
		t.Fatalf("nhắn riêng khi quá 24 giờ: %+v", r)
	}
	if kind, status, _, _ := e.askRow(*r.MessageID); kind != "private_reply" || status != "queued" {
		t.Errorf("tin nhắc = %s %s, muốn private_reply queued", kind, status)
	}
	if r := got[gone.Order.ID]; r.Outcome != conversation.RemindSkipped || !strings.Contains(r.Reason, "7 ngày") ||
		!strings.Contains(r.Reason, "hộp thư") {
		t.Errorf("quá 7 ngày, không hộp thư: %+v", r)
	}

	fc := e.comment(e.fakePage, e.customer("Giả"), "fake-u-173", "chốt", now.Add(-time.Minute))
	fq := e.mustClaim(fc, quiet)
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fakePage)
	rs, err := e.remind(e.an, e.fakeSess, now)
	if err != nil {
		t.Fatal(err)
	}
	if m := outcomes(t, rs); m[fq.Order.ID].Outcome != conversation.RemindSkipped || !strings.Contains(m[fq.Order.ID].Reason, "chỉ xem") {
		t.Errorf("Page chỉ xem: %+v", rs)
	}

	viewer := e.an
	viewer.Grants = []identity.Grant{{Permission: "conversation.read", Scope: identity.Scope{Type: identity.ScopePage, ID: e.fbPage.String()}}}
	if _, err := e.remind(viewer, e.fbSession, now); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("không live.operate: %v", err)
	}
	if _, err := e.remind(e.an, uuid.New(), now); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("phiên lạ: %v", err)
	}
	msgs := e.count(`SELECT count(*) FROM conversation.messages`)
	e.tpl = uuid.New()
	if _, err := e.remind(e.an, e.fbSession, now.Add(2*time.Hour)); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("mẫu lạ: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages`); n != msgs {
		t.Errorf("mẫu lạ vẫn ghi tin: %d → %d", msgs, n)
	}
}
