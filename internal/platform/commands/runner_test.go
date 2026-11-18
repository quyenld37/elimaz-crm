package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

func TestCommandKind(t *testing.T) {
	k := commands.CommandKind("command.x", time.Minute, 2*time.Second)
	want := queue.Policy{
		Mode: queue.ModeCommand, MaxAttempts: 8, OnLeaseExpired: queue.LeaseInDoubt,
		ExpiresAfter: time.Minute, Timeout: 2 * time.Second,
	}
	if k.Name != "command.x" || k.Policy != want {
		t.Fatalf("CommandKind: %+v", k)
	}
}

// ok → done, bên ngoài nhận 1 lần, OnDone một lần trong ngữ cảnh tenant, kết quả được lưu.
func TestOkDone(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeOK}
	h := newCmd("command.ok", ext)
	id := e.enqueue(t, h.kind, "k")
	runOnce(t, worker(e, "w1", h))
	r := e.job(t, id)
	if r.Status != "done" || !strings.Contains(string(r.Result), "x-1") {
		t.Fatalf("muốn done có kết quả: %+v", r)
	}
	_, dones, terms := h.snapshot()
	if len(ext.Calls()) != 1 || len(dones) != 1 || len(terms) != 0 || !h.tenantOK {
		t.Fatalf("calls %v, dones %v, terminals %v, tenantOK %v", ext.Calls(), dones, terms, h.tenantOK)
	}
}

// Execute chạy ngoài transaction: lúc gọi ra ngoài, trạng thái running đã commit (người khác đọc được).
func TestExecuteAfterRunningCommitted(t *testing.T) {
	e := newEnv(t)
	h := newCmd("command.outside", nil)
	var seen string
	h.exec = func(ctx context.Context, j queue.Job) (commands.Outcome, error) {
		if err := e.owner.Pool.QueryRow(ctx, "SELECT status FROM platform.jobs WHERE id = $1", j.ID).Scan(&seen); err != nil {
			return commands.Outcome{}, err //nolint:wrapcheck // test
		}
		return commands.Outcome{Status: commands.StatusDone}, nil
	}
	id := e.enqueue(t, h.kind, "k")
	runOnce(t, worker(e, "w1", h))
	if seen != "running" || e.job(t, id).Status != "done" {
		t.Fatalf("trong Execute người khác phải thấy running, có %q", seen)
	}
}

// Review Focus 1: bên ngoài đã nhận nhưng câu trả lời không về → in_doubt; chạy thêm 3 lượt không gửi lại.
func TestTimeoutAfterSendInDoubtNeverResent(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeTimeoutAfterSend}
	h := newCmd("command.timeout", ext)
	id := e.enqueue(t, h.kind, "k")
	w := worker(e, "w1", h)
	runOnce(t, w)
	if r := e.job(t, id); r.Status != "in_doubt" || r.LastError == nil {
		t.Fatalf("muốn in_doubt có last_error: %+v", r)
	}
	ext.SetMode(testkit.FakeOK)
	for range 3 {
		e.makeDue(t)
		runOnce(t, w)
	}
	_, _, terms := h.snapshot()
	if len(ext.Calls()) != 1 || e.job(t, id).Status != "in_doubt" || len(terms) != 1 || terms[0] != id.String()+":in_doubt" {
		t.Fatalf("calls %v, terminals %v", ext.Calls(), terms)
	}
}

// Review Focus 1: worker chết khi đang giữ lệnh (giữ chỗ đã qua) → in_doubt, Execute không được gọi.
func TestLeaseExpiredInDoubtWithoutExecute(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{}
	h := newCmd("command.crash", ext)
	id := e.enqueue(t, h.kind, "k")
	e.crash(t, id)
	runOnce(t, worker(e, "w1", h))
	execs, _, terms := h.snapshot()
	if r := e.job(t, id); r.Status != "in_doubt" || execs != 0 || len(ext.Calls()) != 0 || len(terms) != 1 {
		t.Fatalf("muốn in_doubt không gọi Execute: %+v, executes %d, terminals %v", r, execs, terms)
	}
}

// not_sent: AutoRetryNotSent đúng → pending hẹn theo lịch (lần 1: 5 s ±20%); sai → dead + OnTerminal.
func TestNotSent(t *testing.T) {
	for _, c := range []struct {
		name  string
		auto  bool
		after time.Duration
		want  string
		minS  float64
		maxS  float64
	}{
		{"auto", true, 0, "pending", 3.9, 6.1},
		{"retry-after", true, time.Hour, "pending", 3590, 3601},
		{"no-auto", false, 0, "dead", -1e9, 1e9},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			ext := &testkit.FakeExternal{Mode: testkit.FakeFailBeforeSend}
			h := newCmd("command.notsent", ext)
			h.autoRetry, h.retryAfter = c.auto, c.after
			id := e.enqueue(t, h.kind, "k")
			runOnce(t, worker(e, "w1", h))
			r := e.job(t, id)
			_, _, terms := h.snapshot()
			if r.Status != c.want || (c.want == "pending" && (r.RunInS < c.minS || r.RunInS > c.maxS)) {
				t.Fatalf("muốn %s: %+v", c.want, r)
			}
			if wantTerms := map[bool]int{true: 1, false: 0}[c.want == "dead"]; len(terms) != wantTerms {
				t.Fatalf("terminals %v", terms)
			}
			if len(ext.Calls()) != 0 {
				t.Fatalf("chưa gửi mà bên ngoài ghi nhận: %v", ext.Calls())
			}
		})
	}
}

// not_sent ở lần thứ MaxAttempts (8) → dead.
func TestNotSentLastAttemptDead(t *testing.T) {
	e := newEnv(t)
	h := newCmd("command.max", &testkit.FakeExternal{Mode: testkit.FakeFailBeforeSend})
	id := e.enqueue(t, h.kind, "k")
	e.ownerExec(t, "UPDATE platform.jobs SET attempts = 7 WHERE id = $1", id)
	runOnce(t, worker(e, "w1", h))
	if r := e.job(t, id); r.Status != "dead" || r.Attempts != 8 {
		t.Fatalf("lần thứ 8 chưa gửi → dead: %+v", r)
	}
}

// reject → dead, OnTerminal("dead"), last_error mang lý do.
func TestRejectDead(t *testing.T) {
	e := newEnv(t)
	h := newCmd("command.reject", &testkit.FakeExternal{Mode: testkit.FakeReject})
	id := e.enqueue(t, h.kind, "k")
	runOnce(t, worker(e, "w1", h))
	r := e.job(t, id)
	_, _, terms := h.snapshot()
	if r.Status != "dead" || r.LastError == nil || !strings.Contains(*r.LastError, "policy") ||
		len(terms) != 1 || terms[0] != id.String()+":dead" {
		t.Fatalf("muốn dead, lý do policy: %+v, terminals %v", r, terms)
	}
}

// Kết quả lạ (unknown, trạng thái không biết, Execute trả lỗi) → in_doubt.
func TestUnknownOutcomesInDoubt(t *testing.T) {
	for name, exec := range map[string]func(context.Context, queue.Job) (commands.Outcome, error){
		"unknown": func(context.Context, queue.Job) (commands.Outcome, error) {
			return commands.Outcome{Status: commands.StatusUnknown, Reason: "502"}, nil
		},
		"strange": func(context.Context, queue.Job) (commands.Outcome, error) {
			return commands.Outcome{Status: "maybe"}, nil
		},
		"error": func(context.Context, queue.Job) (commands.Outcome, error) {
			return commands.Outcome{Status: commands.StatusDone}, errors.New("connection reset")
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			h := newCmd("command.unknown", nil)
			h.exec = exec
			id := e.enqueue(t, h.kind, "k")
			runOnce(t, worker(e, "w1", h))
			if r := e.job(t, id); r.Status != "in_doubt" {
				t.Fatalf("muốn in_doubt: %+v", r)
			}
		})
	}
}

// Execute quá Policy.Timeout → in_doubt (ctx bên trong hết hạn).
func TestExecuteTimeoutInDoubt(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeHangAfterSend}
	h := newCmd("command.hang", ext)
	h.kind = commands.CommandKind("command.hang", 0, 50*time.Millisecond)
	id := e.enqueue(t, h.kind, "k")
	start := time.Now()
	runOnce(t, worker(e, "w1", h))
	if r := e.job(t, id); r.Status != "in_doubt" || r.LastError == nil || !strings.Contains(*r.LastError, "deadline") {
		t.Fatalf("muốn in_doubt vì quá giờ: %+v", r)
	}
	if time.Since(start) > 3*time.Second || len(ext.Calls()) != 1 {
		t.Fatalf("quá lâu hoặc sai số lần gọi: %v %v", time.Since(start), ext.Calls())
	}
}

// Hết hạn theo loại: luôn not_sent, hạn 100 ms → sau 150 ms và một lượt: expired, OnTerminal("expired") một lần.
func TestCommandExpires(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeFailBeforeSend}
	h := newCmd("command.expire", ext)
	h.kind = commands.CommandKind("command.expire", 100*time.Millisecond, time.Second)
	id := e.enqueue(t, h.kind, "k")
	w := worker(e, "w1", h)
	runOnce(t, w)
	time.Sleep(150 * time.Millisecond)
	runOnce(t, w)
	runOnce(t, w)
	_, _, terms := h.snapshot()
	if r := e.job(t, id); r.Status != "expired" || len(terms) != 1 || terms[0] != id.String()+":expired" {
		t.Fatalf("muốn expired, OnTerminal một lần: %+v %v", r, terms)
	}
}

// Lệnh not_sent quá hạn → expired được đếm jobs_expired theo kind (Task 19).
func TestCommandExpiredCountsMetric(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeFailBeforeSend}
	h := newCmd("command.expm", ext)
	h.kind = commands.CommandKind("command.expm", 100*time.Millisecond, time.Second)
	e.enqueue(t, h.kind, "k")
	reg := metrics.NewRegistry()
	w := queue.NewWorker(e.app, nil, queue.WorkerOptions{
		WorkerID: "w1", Metrics: reg, Commands: commands.NewRunner(e.app, []commands.Handler{h}),
	})
	runOnce(t, w)
	time.Sleep(150 * time.Millisecond)
	runOnce(t, w)
	runOnce(t, w)
	var n int64
	for _, r := range reg.Drain() {
		if r.Name == "jobs_expired" && r.Label == "command.expm" {
			n += r.Count
		}
	}
	if n != 1 {
		t.Fatalf("jobs_expired = %d, muốn 1", n)
	}
}

// Stop(50ms) trong lúc Execute đang chờ câu trả lời → in_doubt (lệnh có thể đã ra ngoài).
func TestStopDuringExecuteInDoubt(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeHangAfterSend}
	h := newCmd("command.stop", ext)
	id := e.enqueue(t, h.kind, "k")
	w := worker(e, "w1", h)
	done := make(chan error, 1)
	go func() { _, err := w.RunOnce(context.Background(), 10); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for len(ext.Calls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := w.Stop(50 * time.Millisecond); !errors.Is(err, queue.ErrStopGrace) {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if r := e.job(t, id); r.Status != "in_doubt" || len(ext.Calls()) != 1 {
		t.Fatalf("muốn in_doubt: %+v, calls %v", r, ext.Calls())
	}
}

// Worker đã bị Stop hủy trước khi gọi ra ngoài → chưa gửi gì: trả về hàng chờ, không tính lần thử.
func TestStoppedBeforeExecuteReleased(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{}
	h := newCmd("command.prestop", ext)
	id := e.enqueue(t, h.kind, "k")
	w := worker(e, "w1", h)
	_ = w.Stop(0)
	runOnce(t, w)
	if r := e.job(t, id); r.Status != "pending" || r.Attempts != 0 || len(ext.Calls()) != 0 {
		t.Fatalf("muốn pending, attempts 0, không gọi: %+v %v", r, ext.Calls())
	}
}

// Lệnh không có handler trong runner → dead "no handler", không gọi ra ngoài; worker không có runner không nhận lệnh.
func TestCommandWithoutHandler(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{}
	known := newCmd("command.known", ext)
	id := e.enqueue(t, commands.CommandKind("command.other", 0, time.Second), "k")
	runOnce(t, worker(e, "w1", known))
	if r := e.job(t, id); r.Status != "dead" || r.LastError == nil || *r.LastError != "no handler" {
		t.Fatalf("muốn dead no handler: %+v", r)
	}
	id2 := e.enqueue(t, known.kind, "k2")
	plain := queue.NewWorker(e.app, nil, queue.WorkerOptions{WorkerID: "plain"})
	runOnce(t, plain)
	if r := e.job(t, id2); r.Status != "pending" || len(ext.Calls()) != 0 {
		t.Fatalf("worker không có runner không được nhận lệnh: %+v", r)
	}
}

// Ghi done lỗi (OnDone trả lỗi) sau khi bên ngoài đã nhận → in_doubt, không gửi lại.
func TestDoneRecordFailsInDoubt(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{}
	h := &failDone{cmdH: newCmd("command.faildone", ext)}
	id := e.enqueue(t, h.kind, "k")
	w := worker(e, "w1", h)
	runOnce(t, w)
	e.makeDue(t)
	runOnce(t, w)
	if r := e.job(t, id); r.Status != "in_doubt" || len(ext.Calls()) != 1 {
		t.Fatalf("muốn in_doubt, 1 lần gửi: %+v %v", r, ext.Calls())
	}
}

type failDone struct{ *cmdH }

func (*failDone) OnDone(context.Context, queue.Job, json.RawMessage, pgx.Tx) error {
	return errors.New("bảng nghiệp vụ lỗi")
}

// Hạn ghi kết quả tính từ lúc Execute trả về, không từ lúc nhận lệnh: Execute chạy lâu hơn hạn đó vẫn ghi done và
// OnDone (phát hiện của reviewer Task 12). Hạn 1 s đủ rộng cho các lần ghi khi máy tải cao (50 ms từng đỏ, Task 317);
// Execute chạy quá hạn 200 ms, nên nếu hạn tính từ lúc nhận lệnh thì ctx ghi đã hết, dù máy nhanh hay chậm.
func TestRecordAfterLongExecute(t *testing.T) {
	const record = time.Second
	e := newEnv(t)
	ext := &testkit.FakeExternal{}
	h := newCmd("command.slow", ext)
	h.kind = commands.CommandKind("command.slow", 0, 5*time.Second) // hạn của lệnh rộng hơn Execute, chỉ hạn ghi bị vượt
	h.exec = func(ctx context.Context, j queue.Job) (commands.Outcome, error) {
		time.Sleep(record + 200*time.Millisecond)
		return commands.Outcome{Status: commands.StatusDone}, ext.Send(ctx, j.ID.String())
	}
	id := e.enqueue(t, h.kind, "k")
	r := commands.NewRunner(e.app, []commands.Handler{h})
	commands.SetRecordTimeout(r, record)
	runOnce(t, queue.NewWorker(e.app, nil, queue.WorkerOptions{WorkerID: "w1", Commands: r}))
	_, dones, _ := h.snapshot()
	if st := e.job(t, id).Status; st != "done" || len(dones) != 1 {
		t.Fatalf("muốn done, OnDone 1 lần sau Execute chạy lâu: %s %v", st, dones)
	}
}

// Timeout của lệnh không được vượt nửa thời gian giữ chỗ: nếu không, giữ chỗ hết hạn (in_doubt) trong lúc Execute
// còn chạy. Lease 200 ms, Timeout 5 s → Execute bị hủy sau khoảng 100 ms, lệnh in_doubt do chính worker ghi.
func TestCommandTimeoutBelowLease(t *testing.T) {
	e := newEnv(t)
	ext := &testkit.FakeExternal{Mode: testkit.FakeHangAfterSend}
	h := newCmd("command.lease", ext)
	h.kind = commands.CommandKind("command.lease", 0, 5*time.Second)
	id := e.enqueue(t, h.kind, "k")
	w := queue.NewWorker(e.app, nil, queue.WorkerOptions{
		WorkerID: "w1", Lease: 200 * time.Millisecond,
		Commands: commands.NewRunner(e.app, []commands.Handler{h}),
	})
	start := time.Now()
	runOnce(t, w)
	r := e.job(t, id)
	if el := time.Since(start); el > 2*time.Second || r.Status != "in_doubt" || r.LastError == nil ||
		!strings.Contains(*r.LastError, "deadline") {
		t.Fatalf("muốn in_doubt do quá Timeout đã chặn dưới giữ chỗ, sau %v: %+v", el, r)
	}
}

// Lệnh chưa gửi mà đã quá hạn của loại → expired + OnTerminal("expired"), không dead (spec 04 mục 5.3 "pending -->
// expired: quá hạn của loại lệnh"; quyết định của điều phối viên, lượt 2 Task 14): loại tự gửi lại hay không, kể cả
// ở lần thử cuối. Hạn qua trong lúc Execute chạy (lần gửi chậm).
func TestNotSentPastExpiryExpires(t *testing.T) {
	for _, c := range []struct {
		name     string
		auto     bool
		attempts int // lượt đã dùng trước lần này
	}{
		{"auto", true, 0},
		{"no-auto", false, 0},
		{"last-attempt", true, 7},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			h := newCmd("command.late", &testkit.FakeExternal{})
			h.kind = commands.CommandKind("command.late", time.Minute, time.Second)
			h.autoRetry = c.auto
			h.exec = func(ctx context.Context, j queue.Job) (commands.Outcome, error) {
				if _, err := e.owner.Pool.Exec(ctx,
					"UPDATE platform.jobs SET expires_at = now() - interval '1 second' WHERE id = $1", j.ID); err != nil {
					return commands.Outcome{}, err //nolint:wrapcheck // test: lỗi → in_doubt, test đỏ
				}
				return commands.Outcome{Status: commands.StatusNotSent, Reason: "connect refused"}, nil
			}
			id := e.enqueue(t, h.kind, "k")
			e.ownerExec(t, "UPDATE platform.jobs SET attempts = $2 WHERE id = $1", id, c.attempts)
			runOnce(t, worker(e, "w1", h))
			r := e.job(t, id)
			_, _, terms := h.snapshot()
			if r.Status != "expired" || r.LastError == nil || !strings.Contains(*r.LastError, "not_sent") ||
				len(terms) != 1 || terms[0] != id.String()+":expired" {
				t.Fatalf("muốn expired, OnTerminal(expired) một lần: %+v, terminals %v", r, terms)
			}
			runOnce(t, worker(e, "w2", h))
			if execs, _, _ := h.snapshot(); execs != 1 {
				t.Fatalf("expired không được gửi lại: executes %d", execs)
			}
		})
	}
}
