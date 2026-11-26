package send_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Q23: echo của CRM không metadata tới TRƯỚC khi lệnh gửi ghi xong (kênh đã nhận, phản hồi còn trên đường) → dòng
// own_echo_unmatched; lệnh ghi xong → đúng 1 dòng mang mid, origin staff, sent, giữ mốc đã nhận của dòng echo; kênh
// nhận đúng 1 lần.
func TestEchoBeforeResponse(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.echo("fakechat-1", fake.Key, "")
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'own_echo_unmatched' AND external_id = 'fakechat-1'`); n != 1 {
		t.Fatalf("echo không metadata: %d dòng own_echo_unmatched, muốn 1", n)
	}
	delivered := time.Now().UTC().Truncate(time.Microsecond)
	e.exec(`UPDATE conversation.messages SET delivered_at = $1 WHERE external_id = 'fakechat-1'`, delivered)
	e.drain()
	if n := len(e.chat.Sent()); n != 1 {
		t.Fatalf("kênh nhận %d lần, muốn 1", n)
	}
	if n := e.rowsWithMID(e.fakePage, "fakechat-1"); n != 1 {
		t.Fatalf("%d dòng mang mid, muốn 1", n)
	}
	got := e.message(m.ID)
	if got.Status != "sent" || got.ExternalID != "fakechat-1" || got.Origin != "staff" || got.DeliveredAt == nil ||
		!got.DeliveredAt.Equal(delivered) {
		t.Fatalf("tin sau khi gộp = %+v", got)
	}
}

// Echo không metadata tới SAU khi lệnh ghi xong → không thêm dòng; echo có metadata = client_ref tới trước → tin sent
// bằng echo, lệnh ghi xong không thêm dòng, không báo lỗi.
func TestEchoAfterResponseAndWithMetadata(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	first := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.drain()
	e.echo("fakechat-1", fake.Key, "")
	if n := e.rowsWithMID(e.fakePage, "fakechat-1"); n != 1 {
		t.Fatalf("echo sau: %d dòng mang mid, muốn 1", n)
	}
	if got := e.message(first.ID); got.Status != "sent" || got.ExternalID != "fakechat-1" {
		t.Fatalf("tin đầu = %+v", got)
	}

	second := e.mustSend(e.fakeConv, send.Input{Text: "Tin thứ hai"})
	e.echo("fakechat-2", fake.Key, second.ClientRef.String())
	if got := e.message(second.ID); got.Status != "sent" || got.ExternalID != "fakechat-2" {
		t.Fatalf("echo có metadata: tin = %+v", got)
	}
	e.drain()
	if n := e.rowsWithMID(e.fakePage, "fakechat-2"); n != 1 {
		t.Fatalf("%d dòng mang mid fakechat-2, muốn 1", n)
	}
	if got := e.message(second.ID); got.Status != "sent" || got.ExternalID != "fakechat-2" {
		t.Fatalf("tin thứ hai = %+v", got)
	}
	if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'own_echo_unmatched'`); n != 0 {
		t.Fatalf("còn %d dòng own_echo_unmatched", n)
	}
}

// Kênh không trả lời sau khi đã nhận (timeout_after_send) → lệnh in_doubt, tin checking có mốc; không gửi lại. Kênh
// chưa nhận (fail_before_send) mà quá 3 phút → lệnh expired, tin expired.
func TestSendInDoubtAndExpired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.chat.SetSendBehavior("timeout_after_send")
	doubt := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.staff.DrainJobs(t, e.worker)
	if got := e.message(doubt.ID); got.Status != "checking" || got.CheckingSince == nil {
		t.Fatalf("không rõ: tin = %+v", got)
	}
	if status, _, _, _ := e.job(doubt.ID); status != "in_doubt" {
		t.Fatalf("không rõ: lệnh = %s", status)
	}
	if n := len(e.chat.Sent()); n != 1 {
		t.Fatalf("kênh nhận %d lần, muốn 1", n)
	}

	e.chat.SetSendBehavior("fail_before_send")
	late := e.mustSend(e.fakeConv, send.Input{Text: "Tin trễ"})
	testkit.RunRound(t, e.staff.Owner, e.worker, 10)
	if got := e.message(late.ID); got.Status != "queued" || got.FailReason != "not_sent" {
		t.Fatalf("chưa gửi được: tin = %+v", got)
	}
	if _, err := e.staff.Owner.Pool.Exec(t.Context(), `UPDATE platform.jobs SET expires_at = now() - interval '1 second'
		WHERE kind = $1 AND payload->>'messageId' = $2`, send.KindSendMessage, late.ID.String()); err != nil {
		t.Fatal(err)
	}
	e.staff.DrainJobs(t, e.worker)
	if got := e.message(late.ID); got.Status != "expired" {
		t.Fatalf("quá 3 phút: tin = %+v", got)
	}
	if status, _, _, _ := e.job(late.ID); status != "expired" {
		t.Fatalf("quá 3 phút: lệnh = %s", status)
	}
	if n := len(e.chat.Sent()); n != 1 {
		t.Fatalf("kênh nhận %d lần, muốn 1", n)
	}
}

// Lệnh gửi tin hạn 3 phút, Execute 30 giây (04 mục 5.3); tự gửi lại khi chắc chắn chưa gửi.
func TestSendCommandKind(t *testing.T) {
	t.Parallel()
	k := send.CommandKind()
	if k.Name != "command.fb.send_message" || k.Policy.ExpiresAfter != 3*time.Minute || k.Policy.Timeout != 30*time.Second {
		t.Fatalf("Kind = %+v", k)
	}
}

// Migration 0413: crm_app chỉ xóa được tin own_echo_unmatched; xóa tin khác bị trigger chặn (42501), kể cả khi lọc
// đúng tenant.
func TestMessagesDeleteGuard(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `DELETE FROM conversation.messages WHERE id = $1`, m.ID)
		return err //nolint:wrapcheck // test
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); !ok || pe.Code != "42501" {
		t.Fatalf("xóa tin của nhân viên: lỗi = %v, muốn 42501", err)
	}
	e.echo("fakechat-9", fake.Key, "")
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `DELETE FROM conversation.messages WHERE origin = 'own_echo_unmatched'`)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("xóa own_echo_unmatched: %v", err)
	}
	if n := e.rowsWithMID(e.fakePage, "fakechat-9"); n != 0 {
		t.Fatalf("còn %d dòng", n)
	}
}
