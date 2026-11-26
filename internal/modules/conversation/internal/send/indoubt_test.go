package send_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// reveal chạy lịch conversation.send_reveal một lần cho tenant A (qua việc nền của lịch, worker thật) với đồng hồ của
// env.
func (e *env) reveal() {
	e.t.Helper()
	p := send.Parts(e.d)
	if len(p.Schedules) != 1 || len(p.Jobs) != 1 {
		e.t.Fatalf("send.Parts: %d lịch, %d việc nền; muốn 1, 1", len(p.Schedules), len(p.Jobs))
	}
	tenant := e.tenant
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, _, err := e.d.Queue.Enqueue(e.t.Context(), tx, p.Schedules[0].Kind, queue.NewJob{TenantID: &tenant, Payload: map[string]any{}, DedupeKey: uuid.NewString()})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	w := queue.NewWorker(e.staff.App, p.Jobs, queue.WorkerOptions{WorkerID: "w-reveal", Logger: slog.New(slog.DiscardHandler)})
	testkit.DrainDue(e.t, e.staff.Owner, w)
}

// resolve chạy tra lại lệnh in_doubt của M0 với Looker của lệnh gửi; trả số done, về hàng chờ, còn in_doubt.
func (e *env) resolve() (done, requeued, still int) {
	e.t.Helper()
	done, requeued, still, err := commands.ResolveInDoubt(e.t.Context(), e.staff.App, send.Parts(e.d).Commands, 50)
	if err != nil {
		e.t.Fatalf("ResolveInDoubt: %v", err)
	}
	return done, requeued, still
}

// Lịch conversation.send_reveal: 15 giây, theo tenant; việc của lịch 1 lượt, hết hạn sau một kỳ.
func TestRevealSchedule(t *testing.T) {
	t.Parallel()
	p := send.Parts(deps.Deps{})
	if len(p.Schedules) != 1 {
		t.Fatalf("lịch = %+v", p.Schedules)
	}
	s := p.Schedules[0]
	if send.RevealSchedule != "conversation.send_reveal" || s.Name != send.RevealSchedule || s.Every != 15*time.Second || !s.PerTenant ||
		s.Kind.Name != p.Jobs[0].Kind().Name || s.Kind.Policy.MaxAttempts != 1 {
		t.Fatalf("lịch = %+v, việc %q", s, p.Jobs[0].Kind().Name)
	}
	if _, ok := any(p.Commands[0]).(commands.Looker); !ok {
		t.Fatal("lệnh gửi tin phải có Looker (tra lại in_doubt)")
	}
}

// checkReveal: tin id (lệnh in_doubt lúc start) còn checking ở +59 s, thành in_doubt kèm một báo hiệu của hội thoại
// conv ở +61 s.
func (e *env) checkReveal(id, conv uuid.UUID, start time.Time) {
	e.t.Helper()
	e.clock.Freeze(start.Add(59 * time.Second))
	e.reveal()
	if got := e.message(id); got.Status != "checking" {
		e.t.Fatalf("+59 s: tin = %+v, muốn còn checking", got)
	}
	before := e.signals(conv)
	e.clock.Freeze(start.Add(61 * time.Second))
	e.reveal()
	if got := e.message(id); got.Status != "in_doubt" {
		e.t.Fatalf("+61 s: tin = %+v, muốn in_doubt", got)
	}
	if n := e.signals(conv); n != before+1 {
		e.t.Fatalf("+61 s: %d báo hiệu mới, muốn 1", n-before)
	}
}

// CH18: Meta đã nhận tin rồi rớt kết nối (DropAfterRead) → lệnh in_doubt, tin checking; chạy worker 3 lượt và tra
// lại → Meta vẫn 1 lời gọi, tin vẫn checking; trước 60 giây lịch không đổi gì, ở +61 s tin thành in_doubt kèm báo
// hiệu. Gửi lại → Meta 2 lời gọi, mayDuplicate, tin cũ discarded, tin mới sent. Nhánh khác: bỏ → Meta không thêm lời
// gọi, lệnh expired, tin discarded.
func TestInDoubt_CH18(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	path := fbPageExt + "/messages"
	start := time.Now().UTC()
	e.clock.Freeze(start)
	e.fg.DropAfterRead(path)
	doubt := e.mustSend(e.fbConv, send.Input{Text: textHello})
	e.drain()
	if got := e.message(doubt.ID); got.Status != "checking" || got.CheckingSince == nil {
		t.Fatalf("sau khi rớt: tin = %+v", got)
	}
	for range 3 {
		testkit.RunRound(t, e.staff.Owner, e.worker, 10)
	}
	if done, requeued, still := e.resolve(); done != 0 || requeued != 0 || still != 1 {
		t.Fatalf("tra lại: done %d, requeued %d, still %d; muốn 0, 0, 1", done, requeued, still)
	}
	if n := len(e.fbCalls()); n != 1 {
		t.Fatalf("Meta nhận %d lần, muốn 1", n)
	}
	if status, _, _, _ := e.job(doubt.ID); status != "in_doubt" {
		t.Fatalf("lệnh = %s, muốn in_doubt", status)
	}
	e.checkReveal(doubt.ID, e.fbConv, start)

	other := e.mustSend(e.fbConv, send.Input{Text: "Tin thứ hai"})
	e.fg.DropAfterRead(path)
	e.drain()
	if n := len(e.fbCalls()); n != 2 {
		t.Fatalf("Meta nhận %d lần, muốn 2", n)
	}

	res := e.mustResend(doubt.ID)
	if !res.MayDuplicate || res.Message.ID == doubt.ID || res.Message.ClientRef == doubt.ClientRef ||
		res.Message.Body != textHello || res.Message.SendStatus != "queued" {
		t.Fatalf("gửi lại = %+v", res)
	}
	e.drain()
	if n := len(e.fbCalls()); n != 3 {
		t.Fatalf("sau gửi lại Meta nhận %d lần, muốn 3", n)
	}
	if got := e.message(doubt.ID); got.Status != "discarded" {
		t.Fatalf("tin cũ = %+v, muốn discarded", got)
	}
	if status, _, _, _ := e.job(doubt.ID); status != "expired" {
		t.Fatalf("lệnh cũ = %s, muốn expired (đã bỏ)", status)
	}
	if got := e.message(res.Message.ID); got.Status != "sent" || got.ExternalID == "" {
		t.Fatalf("tin mới = %+v", got)
	}

	m, err := e.discard(other.ID)
	if err != nil || m.SendStatus != "discarded" || m.ID != other.ID {
		t.Fatalf("bỏ tin checking: %+v, %v", m, err)
	}
	e.drain()
	if n := len(e.fbCalls()); n != 3 {
		t.Fatalf("sau khi bỏ Meta nhận %d lần, muốn 3", n)
	}
	if status, _, _, _ := e.job(other.ID); status != "expired" {
		t.Fatalf("lệnh của tin bỏ = %s, muốn expired", status)
	}
	if got := e.message(other.ID); got.Status != "discarded" {
		t.Fatalf("tin bỏ = %+v", got)
	}
}

// Echo có metadata = client_ref tới ở +30 s → tin sent, lịch ở +61 s không đổi nó sang in_doubt; tra lại đưa lệnh về
// done (Looker: tin đã có mã ngoài).
func TestInDoubt_EchoWithin60s(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	start := time.Now().UTC()
	e.clock.Freeze(start)
	e.chat.SetSendBehavior("timeout_after_send")
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.drain()
	if got := e.message(m.ID); got.Status != "checking" {
		t.Fatalf("tin = %+v", got)
	}
	e.clock.Freeze(start.Add(30 * time.Second))
	e.echo("fakechat-echo-1", fake.Key, m.ClientRef.String())
	e.clock.Freeze(start.Add(61 * time.Second))
	e.reveal()
	if got := e.message(m.ID); got.Status != "sent" || got.ExternalID != "fakechat-echo-1" {
		t.Fatalf("tin = %+v, muốn sent", got)
	}
	if done, _, still := e.resolve(); done != 1 || still != 0 {
		t.Fatalf("tra lại: done %d, still %d", done, still)
	}
	if status, _, _, _ := e.job(m.ID); status != "done" {
		t.Fatalf("lệnh = %s, muốn done", status)
	}
	if n := len(e.chat.Sent()); n != 1 {
		t.Fatalf("kênh nhận %d lần, muốn 1", n)
	}
}

// Worker chết khi đang giữ lệnh (giữ chỗ hết hạn) → lệnh in_doubt, tin checking; Execute không được gọi lại.
func TestInDoubt_WorkerCrash(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	if _, err := e.staff.Owner.Pool.Exec(t.Context(), `UPDATE platform.jobs SET status = 'running', attempts = 1,
		locked_by = 'dead-worker', lease_until = now() - interval '1 second' WHERE kind = $1 AND payload->>'messageId' = $2`,
		send.KindSendMessage, m.ID.String()); err != nil {
		t.Fatal(err)
	}
	e.staff.DrainJobs(t, e.worker)
	if status, _, _, _ := e.job(m.ID); status != "in_doubt" {
		t.Fatalf("lệnh = %s, muốn in_doubt", status)
	}
	if got := e.message(m.ID); got.Status != "checking" || got.CheckingSince == nil {
		t.Fatalf("tin = %+v", got)
	}
	e.resolve()
	e.staff.DrainJobs(t, e.worker)
	if n := len(e.chat.Sent()); n != 0 {
		t.Fatalf("Execute chạy lại: kênh nhận %d lần", n)
	}
}

// Echo tới sau khi nhân viên đã bỏ tin → tin sent (sự thật thắng), báo hiệu, chỉ số late_echo_after_discard = 1.
func TestDiscard_LateEcho(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.chat.SetSendBehavior("timeout_after_send")
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.drain()
	if _, err := e.discard(m.ID); err != nil {
		t.Fatal(err)
	}
	e.reg.Drain()
	before := e.signals(e.fakeConv)
	e.echo("fakechat-late-1", fake.Key, m.ClientRef.String())
	if got := e.message(m.ID); got.Status != "sent" || got.ExternalID != "fakechat-late-1" {
		t.Fatalf("tin = %+v, muốn sent", got)
	}
	if n := e.signals(e.fakeConv); n <= before {
		t.Fatal("echo muộn không báo hiệu")
	}
	var late int64
	for _, r := range e.reg.Drain() {
		if r.Name == "late_echo_after_discard" {
			late += r.Count
		}
	}
	if late != 1 {
		t.Fatalf("late_echo_after_discard = %d, muốn 1", late)
	}
	if n := e.rowsWithMID(e.fakePage, "fakechat-late-1"); n != 1 {
		t.Fatalf("%d dòng mang mid, muốn 1", n)
	}
}

// Echo đọc qua hỏi bù (không metadata, không app id → page_external) tới trước phản hồi của Send (ghi chú Task 148):
// lệnh ghi xong với cùng mid → đúng 1 dòng mang mid, là tin của nhân viên, sent.
func TestOnDone_MergesBackfilledEcho(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.echo("fakechat-1", "", "")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'page_external' AND external_id = 'fakechat-1'`); n != 1 {
		t.Fatalf("echo hỏi bù: %d dòng page_external, muốn 1", n)
	}
	e.drain()
	if n := e.rowsWithMID(e.fakePage, "fakechat-1"); n != 1 {
		t.Fatalf("%d dòng mang mid, muốn 1", n)
	}
	if got := e.message(m.ID); got.Status != "sent" || got.ExternalID != "fakechat-1" || got.Origin != "staff" {
		t.Fatalf("tin = %+v", got)
	}
}
