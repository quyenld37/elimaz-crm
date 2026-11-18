package commands_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
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
