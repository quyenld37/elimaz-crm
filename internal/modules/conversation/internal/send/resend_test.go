package send_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// resendAs gửi lại tin id bằng nhân viên u trong một transaction tenant.
func (e *env) resendAs(u identity.AuthUser, id uuid.UUID) (send.ResendResult, error) {
	e.t.Helper()
	return inTx(e, func(tx pgx.Tx) (send.ResendResult, error) {
		return send.Resend(e.t.Context(), tx, e.env(), u, id, e.clock.Now())
	})
}

func (e *env) mustResend(id uuid.UUID) send.ResendResult {
	e.t.Helper()
	res, err := e.resendAs(e.user, id)
	if err != nil {
		e.t.Fatalf("Resend: %v", err)
	}
	return res
}

// discard bỏ tin id bằng nhân viên của env trong một transaction tenant.
func (e *env) discard(id uuid.UUID) (send.Message, error) {
	e.t.Helper()
	return inTx(e, func(tx pgx.Tx) (send.Message, error) {
		return send.Discard(e.t.Context(), tx, e.env(), e.user, id, e.clock.Now())
	})
}

// expireJob đưa lệnh gửi của tin id quá hạn 3 phút (expires_at đã qua) rồi chạy worker: lệnh expired, tin expired.
func (e *env) expireJob(id uuid.UUID) {
	e.t.Helper()
	if _, err := e.staff.Owner.Pool.Exec(e.t.Context(), `UPDATE platform.jobs SET expires_at = now() - interval '1 second', run_at = now()
		WHERE kind = $1 AND payload->>'messageId' = $2`, send.KindSendMessage, id.String()); err != nil {
		e.t.Fatal(err)
	}
	e.staff.DrainJobs(e.t, e.worker)
}

// CH19: Meta trả 613 liên tục → tin chưa gửi; quá 3 phút → lệnh expired, tin expired ("chưa gửi được"); đồng hồ tiến
// 6 giờ, worker chạy → Meta không nhận thêm lời gọi nào. Gửi lại khi cửa sổ còn mở → tin mới sent, mayDuplicate false;
// khi cửa sổ đã đóng → send_not_allowed window_closed, tin cũ giữ expired.
func TestExpired_CH19(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	path := fbPageExt + "/messages"
	rate := fakegraph.GraphErrorSpec{Code: 613, Type: "OAuthException", Message: "Calls to this api have exceeded the rate limit"}
	start := time.Now().UTC()
	e.clock.Freeze(start)
	m := e.mustSend(e.fbConv, send.Input{Text: textHello})
	for range 2 {
		e.fg.FailNext(path, rate)
		if _, err := e.staff.Owner.Pool.Exec(t.Context(), `UPDATE platform.jobs SET run_at = now() WHERE status = 'pending'`); err != nil {
			t.Fatal(err)
		}
		testkit.RunRound(t, e.staff.Owner, e.worker, 10)
	}
	if got := e.message(m.ID); got.Status != "queued" || got.FailReason != "rate_limited" {
		t.Fatalf("613: tin = %+v", got)
	}
	e.expireJob(m.ID)
	if got := e.message(m.ID); got.Status != "expired" {
		t.Fatalf("quá 3 phút: tin = %+v", got)
	}
	calls := len(e.fbCalls())
	e.clock.Freeze(start.Add(6 * time.Hour))
	e.staff.DrainJobs(t, e.worker)
	if n := len(e.fbCalls()); n != calls {
		t.Fatalf("6 giờ sau Meta nhận thêm %d lời gọi", n-calls)
	}

	res := e.mustResend(m.ID)
	if res.MayDuplicate {
		t.Fatal("tin expired chưa từng tới Meta: mayDuplicate phải false")
	}
	e.drain()
	if got := e.message(res.Message.ID); got.Status != "sent" {
		t.Fatalf("tin gửi lại = %+v", got)
	}
	if n := len(e.fbCalls()); n != calls+1 {
		t.Fatalf("Meta nhận %d lời gọi sau gửi lại, muốn %d", n, calls+1)
	}
	if got := e.message(m.ID); got.Status != "discarded" {
		t.Fatalf("tin cũ = %+v", got)
	}

	closed := e.mustSend(e.fbConv, send.Input{Text: "Tin thứ hai"})
	e.expireJob(closed.ID)
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $1 WHERE id = $2`,
		e.clock.Now().Add(-25*time.Hour), e.fbConv)
	_, err := e.resendAs(e.user, closed.ID)
	var na *send.NotAllowedError
	if !errors.As(err, &na) || na.Reason != channel.DenyWindowClosed {
		t.Fatalf("cửa sổ đóng: lỗi = %v, muốn send_not_allowed window_closed", err)
	}
	if got := e.message(closed.ID); got.Status != "expired" {
		t.Fatalf("cửa sổ đóng: tin cũ = %+v, muốn giữ expired", got)
	}
}

// Hai lần gửi lại song song cùng một tin (khác khóa chống trùng) → đúng một thành công, một ErrNotRecoverable; chỉ một
// tin mới.
func TestResend_Concurrent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.expireJob(m.ID)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.resendAs(e.user, m.ID) })
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, send.ErrNotRecoverable):
			conflict++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("thành công %d, xung đột %d; muốn 1, 1", ok, conflict)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND direction = 'out'`, e.fakeConv); n != 2 {
		t.Fatalf("%d tin ra, muốn 2 (cũ + 1 mới)", n)
	}
}

// Gửi lại, bỏ chỉ cho trạng thái hợp lệ: tin sent, queued, discarded, failed lý do vĩnh viễn → ErrNotRecoverable
// (gửi lại); failed rate_limited gửi lại được; bỏ được failed, expired nhưng không bỏ được sent, queued. Không có quyền
// trên Page hay tin không có → access.ErrNotFound; Page chỉ xem → ErrReadOnly.
func TestRecover_States(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	sent := e.mustSend(e.fakeConv, send.Input{Text: "Đã gửi"})
	e.drain()
	queued := e.mustSend(e.fakeConv, send.Input{Text: "Đang chờ"})
	setFailed := func(reason string) uuid.UUID {
		m := e.mustSend(e.fakeConv, send.Input{Text: "Lỗi " + reason})
		e.expireJob(m.ID)
		e.exec(`UPDATE conversation.messages SET send_status = 'failed', fail_reason = $1 WHERE id = $2`, reason, m.ID)
		return m.ID
	}
	blocked, limited := setFailed(string(channel.DenyBlockedByUser)), setFailed("rate_limited")
	for name, id := range map[string]uuid.UUID{"sent": sent.ID, "queued": queued.ID, "blocked": blocked} {
		if _, err := e.resendAs(e.user, id); !errors.Is(err, send.ErrNotRecoverable) {
			t.Errorf("gửi lại %s: lỗi = %v, muốn ErrNotRecoverable", name, err)
		}
	}
	for name, id := range map[string]uuid.UUID{"sent": sent.ID, "queued": queued.ID} {
		if _, err := e.discard(id); !errors.Is(err, send.ErrNotRecoverable) {
			t.Errorf("bỏ %s: lỗi = %v, muốn ErrNotRecoverable", name, err)
		}
	}
	if _, err := e.discard(blocked); err != nil {
		t.Errorf("bỏ tin failed: %v", err)
	}
	if _, err := e.discard(blocked); !errors.Is(err, send.ErrNotRecoverable) {
		t.Errorf("bỏ lần hai: lỗi = %v, muốn ErrNotRecoverable", err)
	}
	stranger := identity.AuthUser{UserID: e.user.UserID, TenantID: e.tenant}
	if _, err := e.resendAs(stranger, limited); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("không quyền: lỗi = %v, muốn ErrNotFound", err)
	}
	if _, err := e.resendAs(e.user, uuid.New()); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("tin không có: lỗi = %v, muốn ErrNotFound", err)
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fakePage)
	if _, err := e.resendAs(e.user, limited); !errors.Is(err, send.ErrReadOnly) {
		t.Errorf("chỉ xem: lỗi = %v, muốn ErrReadOnly", err)
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.fakePage)
	res, err := e.resendAs(e.user, limited)
	if err != nil || res.MayDuplicate {
		t.Fatalf("gửi lại rate_limited: %+v, %v", res, err)
	}
}
