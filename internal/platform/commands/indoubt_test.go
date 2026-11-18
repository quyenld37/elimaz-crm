package commands_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// inDoubtJob dựng một lệnh in_doubt: bên ngoài trả lời không về (sent = true) hoặc worker chết trước khi gửi.
func inDoubtJob(t *testing.T, e *env, h *lookH, key string, sent bool) uuid.UUID {
	t.Helper()
	id := e.enqueue(t, h.kind, key)
	if sent {
		h.ext.SetMode(testkit.FakeTimeoutAfterSend)
		runOnce(t, worker(e, "w-"+key, h))
		h.ext.SetMode(testkit.FakeOK)
	} else {
		e.crash(t, id)
		runOnce(t, worker(e, "w-"+key, h))
	}
	if r := e.job(t, id); r.Status != "in_doubt" {
		t.Fatalf("dựng in_doubt: %+v", r)
	}
	return id
}

func resolve(t *testing.T, e *env, hs ...commands.Handler) (done, requeued, still int) {
	t.Helper()
	done, requeued, still, err := commands.ResolveInDoubt(t.Context(), e.app, hs, 10)
	if err != nil {
		t.Fatalf("ResolveInDoubt: %v", err)
	}
	return done, requeued, still
}

// Tra thấy đã thực hiện → done + OnDone, không gửi thêm.
func TestResolveDone(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	id := inDoubtJob(t, e, h, "k", true)
	if d, r, s := resolve(t, e, h); d != 1 || r != 0 || s != 0 {
		t.Fatalf("đếm %d %d %d", d, r, s)
	}
	_, dones, _ := h.snapshot()
	if st := e.job(t, id).Status; st != "done" || len(h.ext.Calls()) != 1 || len(dones) != 1 || !h.tenantOK {
		t.Fatalf("muốn done không gửi thêm: %s, calls %v, dones %v", st, h.ext.Calls(), dones)
	}
}

// Tra thấy chắc chắn chưa có → pending; lượt sau gửi đúng 1 lần.
func TestResolveNotSentRequeues(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	id := inDoubtJob(t, e, h, "k", false)
	if d, r, s := resolve(t, e, h); d != 0 || r != 1 || s != 0 {
		t.Fatalf("đếm %d %d %d", d, r, s)
	}
	if st := e.job(t, id).Status; st != "pending" || len(h.ext.Calls()) != 0 {
		t.Fatalf("muốn pending chưa gửi: %s %v", st, h.ext.Calls())
	}
	runOnce(t, worker(e, "w2", h))
	if st := e.job(t, id).Status; st != "done" || len(h.ext.Calls()) != 1 {
		t.Fatalf("muốn done sau 1 lần gửi: %s %v", st, h.ext.Calls())
	}
}

// Không tra được (unknown) → vẫn in_doubt; handler không có Looker → không đụng tới.
func TestResolveUnknownAndNoLooker(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{}), look: func(context.Context, queue.Job) (string, error) {
		return commands.StatusUnknown, nil
	}}
	id := inDoubtJob(t, e, h, "k", true)
	plain := newCmd("command.plain", &testkit.FakeExternal{})
	pid := e.enqueue(t, plain.kind, "p")
	e.crash(t, pid)
	runOnce(t, worker(e, "wp", plain))
	if d, r, s := resolve(t, e, h, plain); d != 0 || r != 0 || s != 1 {
		t.Fatalf("đếm %d %d %d", d, r, s)
	}
	if e.job(t, id).Status != "in_doubt" || e.job(t, pid).Status != "in_doubt" {
		t.Fatal("cả hai phải còn in_doubt")
	}
}

// Kết quả tra đã cũ: trong lúc tra, việc được chạy lại và lại in_doubt (bên ngoài đã nhận lần đó) → không được đưa
// về pending theo kết quả tra cũ.
func TestResolveStaleLookupIgnored(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	id := inDoubtJob(t, e, h, "k", false)
	h.look = func(ctx context.Context, _ queue.Job) (string, error) {
		// Người khác (nhân viên chạy lại) làm việc chạy lại và thành in_doubt lần nữa trong lúc tra.
		_, err := e.owner.Pool.Exec(ctx, "UPDATE platform.jobs SET finished_at = now() + interval '1 second', attempts = attempts + 1 WHERE id = $1", id)
		return commands.StatusNotSent, err //nolint:wrapcheck // test
	}
	if d, r, _ := resolve(t, e, h); d != 0 || r != 0 {
		t.Fatalf("kết quả tra cũ không được áp: %d %d", d, r)
	}
	if st := e.job(t, id).Status; st != "in_doubt" {
		t.Fatalf("muốn còn in_doubt, có %s", st)
	}
}

// Lịch platform.resolve_in_doubt (lượt 2 Task 14): việc tra lại chạy qua Runner (ngoài transaction) như một lệnh,
// gọi ResolveInDoubt cho các handler có Looker rồi done với số đếm; không gửi gì thêm ra ngoài.
func TestInDoubtResolverJob(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	sent := inDoubtJob(t, e, h, "a", true)
	unsent := inDoubtJob(t, e, h, "b", false)
	res := commands.NewInDoubtResolver(e.app, []commands.Handler{h})
	if p := res.Kind().Policy; p.Mode != queue.ModeCommand || p.OnLeaseExpired != queue.LeaseRetry ||
		p.ExpiresAfter != commands.ResolveInDoubtEvery || p.Timeout <= 0 || p.Timeout > 30*time.Second {
		t.Fatalf("chính sách của việc tra lại: %+v", p)
	}
	rid := e.enqueueJob(t, res.Kind(), queue.NewJob{DedupeKey: "slot-1"}) // không tenant, như Ticker
	runOnce(t, worker(e, "wr", h, res))
	r := e.job(t, rid)
	if r.Status != "done" || string(r.Result) != `{"done": 1, "still": 0, "requeued": 1}` {
		t.Fatalf("muốn việc tra lại done kèm số đếm: %+v (%s)", r, r.Result)
	}
	if e.job(t, sent).Status != "done" || e.job(t, unsent).Status != "pending" || len(h.ext.Calls()) != 1 {
		t.Fatalf("tra lại sai: %s %s, calls %v", e.job(t, sent).Status, e.job(t, unsent).Status, h.ext.Calls())
	}
}

// Tra lại lỗi (bên ngoài không trả lời): việc tra lại về hàng chờ (chạy lại an toàn, chỉ đọc), lý do là mã cố định;
// lệnh vẫn in_doubt.
func TestInDoubtResolverJobLookupFails(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{}), look: func(context.Context, queue.Job) (string, error) {
		return "", errors.New("upstream 0912345678 lỗi")
	}}
	id := inDoubtJob(t, e, h, "a", true)
	res := commands.NewInDoubtResolver(e.app, []commands.Handler{h})
	rid := e.enqueueJob(t, res.Kind(), queue.NewJob{DedupeKey: "slot-1"}) // không tenant, như Ticker
	runOnce(t, worker(e, "wr", h, res))
	r := e.job(t, rid)
	if r.Status != "pending" || r.LastError == nil || *r.LastError != "not_sent: lookup_failed" {
		t.Fatalf("muốn pending, lý do mã cố định: %+v", r)
	}
	if e.job(t, id).Status != "in_doubt" {
		t.Fatal("lệnh phải còn in_doubt")
	}
}

// Lookup lỗi: lệnh giữ in_doubt, được đếm vào "còn lại" và lỗi trả lên (mutation testing Task 23: still++ ở nhánh lỗi).
func TestResolveLookupErrorCountedStill(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{}), look: func(context.Context, queue.Job) (string, error) {
		return "", errors.New("tra lỗi")
	}}
	id := inDoubtJob(t, e, h, "a", true)
	d, r, s, err := commands.ResolveInDoubt(t.Context(), e.app, []commands.Handler{h}, 10)
	if err == nil {
		t.Fatal("Lookup lỗi mà ResolveInDoubt không trả lỗi")
	}
	if d != 0 || r != 0 || s != 1 {
		t.Fatalf("đếm done %d, requeued %d, still %d; muốn 0 0 1", d, r, s)
	}
	if e.job(t, id).Status != "in_doubt" {
		t.Fatal("lệnh phải còn in_doubt")
	}
}

// Loại không đặt Policy.Timeout (0): Lookup chạy với hạn mặc định của hàng đợi, không phải hạn 0 đã hết ngay
// (mutation testing Task 23: điều kiện timeout <= 0 của lookup).
func TestResolveZeroTimeoutUsesDefault(t *testing.T) {
	e := newEnv(t)
	var left time.Duration
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{}), look: func(ctx context.Context, _ queue.Job) (string, error) {
		if dl, ok := ctx.Deadline(); ok {
			left = time.Until(dl)
		}
		return commands.StatusUnknown, nil
	}}
	inDoubtJob(t, e, h, "a", true)
	h.kind.Policy.Timeout = 0
	if _, _, s := resolve(t, e, h); s != 1 {
		t.Fatalf("still %d, muốn 1", s)
	}
	if want := queue.DefaultPolicy.Timeout - 5*time.Second; left < want {
		t.Fatalf("Lookup còn %v trước hạn, muốn gần hạn mặc định %v", left, queue.DefaultPolicy.Timeout)
	}
}

// Q-5 (M3): handler không tự gửi lại (AutoRetryNotSent = false), tra thấy chắc chắn chưa gửi → dead với lý do
// confirmed_not_sent, OnTerminal(dead) đúng một lần, không gửi gì; lượt worker sau không chạy lại nó.
func TestResolveNotSentNoAutoRetryIsDead(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	h.autoRetry = false
	id := inDoubtJob(t, e, h, "k", false)
	_, _, before := h.snapshot()
	if d, r, s := resolve(t, e, h); d != 0 || r != 1 || s != 0 {
		t.Fatalf("đếm %d %d %d, muốn 0 1 0", d, r, s)
	}
	row := e.job(t, id)
	if row.Status != "dead" || row.LastError == nil || *row.LastError != "confirmed_not_sent" {
		t.Fatalf("muốn dead confirmed_not_sent: %+v (last_error %v)", row, row.LastError)
	}
	_, _, terms := h.snapshot()
	if added := terms[len(before):]; len(added) != 1 || added[0] != id.String()+":dead" {
		t.Fatalf("OnTerminal thêm %v, muốn đúng [%s:dead]", added, id)
	}
	runOnce(t, worker(e, "w2", h))
	if st := e.job(t, id).Status; st != "dead" || len(h.ext.Calls()) != 0 {
		t.Fatalf("lệnh dead không được gửi: %s %v", st, h.ext.Calls())
	}
	// Tra lại lần nữa: không còn in_doubt nên không đổi gì, không gọi OnTerminal thêm.
	if d, r, s := resolve(t, e, h); d != 0 || r != 0 || s != 0 {
		t.Fatalf("lần tra sau đếm %d %d %d, muốn 0 0 0", d, r, s)
	}
	if _, _, again := h.snapshot(); len(again) != len(terms) {
		t.Fatalf("OnTerminal bị gọi thêm: %v", again)
	}
}

// abandon gọi commands.Abandon trong ngữ cảnh tenant của e (hoặc tenant khác).
func abandon(t *testing.T, e *env, tenant uuid.UUID, id uuid.UUID) error {
	t.Helper()
	return tenancy.WithTenant(t.Context(), e.app, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		return commands.Abandon(t.Context(), tx, id, "order_cancelled")
	})
}

// Abandon (M4, M3): lệnh in_doubt hay pending → expired với lý do; gọi lại không đổi, không lỗi; lệnh đang chạy →
// ErrCommandRunning; lệnh của tenant khác hay không có → ErrCommandNotFound; lệnh đã bỏ không bị worker chạy.
func TestAbandon(t *testing.T) {
	e := newEnv(t)
	h := &lookH{cmdH: newCmd("command.look", &testkit.FakeExternal{})}
	doubt := inDoubtJob(t, e, h, "k1", true)
	pending := e.enqueue(t, h.kind, "k2")
	for _, id := range []uuid.UUID{doubt, pending} {
		for range 2 {
			if err := abandon(t, e, e.tenant, id); err != nil {
				t.Fatalf("Abandon %s: %v", id, err)
			}
		}
		row := e.job(t, id)
		if row.Status != "expired" || row.LastError == nil || *row.LastError != "abandoned: order_cancelled" {
			t.Fatalf("muốn expired: %+v %v", row, row.LastError)
		}
	}
	calls := len(h.ext.Calls())
	runOnce(t, worker(e, "w2", h))
	if len(h.ext.Calls()) != calls {
		t.Fatalf("lệnh đã bỏ bị gửi: %v", h.ext.Calls())
	}
	running := e.enqueue(t, h.kind, "k3")
	e.ownerExec(t, `UPDATE platform.jobs SET status = 'running', attempts = 1, locked_by = 'w', lease_until = now() + interval '1 minute'
		WHERE id = $1`, running)
	if err := abandon(t, e, e.tenant, running); !errors.Is(err, commands.ErrCommandRunning) {
		t.Fatalf("lệnh đang chạy: %v, muốn ErrCommandRunning", err)
	}
	if st := e.job(t, running).Status; st != "running" {
		t.Fatalf("lệnh đang chạy bị đổi: %s", st)
	}
	var other uuid.UUID
	if err := e.owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('B') RETURNING id").Scan(&other); err != nil {
		t.Fatal(err)
	}
	other4 := e.enqueue(t, h.kind, "k4")
	if err := abandon(t, e, other, other4); !errors.Is(err, commands.ErrCommandNotFound) {
		t.Fatalf("tenant khác: %v, muốn ErrCommandNotFound", err)
	}
	if err := abandon(t, e, e.tenant, uuid.New()); !errors.Is(err, commands.ErrCommandNotFound) {
		t.Fatalf("mã lạ: %v, muốn ErrCommandNotFound", err)
	}
	if st := e.job(t, other4).Status; st != "pending" {
		t.Fatalf("lệnh của tenant A bị tenant B bỏ: %s", st)
	}
}
