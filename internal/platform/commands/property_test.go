package commands_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/testkit"
)

//nolint:gochecknoglobals // danh sách chế độ chỉ đọc của test
var fakeModes = []string{testkit.FakeOK, testkit.FakeFailBeforeSend, testkit.FakeTimeoutAfterSend, testkit.FakeHangAfterSend, testkit.FakeReject}

// Quy luật (vùng rủi ro cao, ADR-25; Review Focus 1): với chuỗi ngẫu nhiên các chế độ của bên ngoài, các lượt chạy,
// các lần worker chết (trước hoặc sau khi gửi), các lần tắt giữa chừng, các lần tra lại và thời gian trôi, số lần bên
// ngoài ghi nhận cùng một ref không bao giờ vượt 1 khi Lookup nói đúng sự thật; không lệnh nào kẹt running.
func TestPropertyNeverSentTwice(t *testing.T) {
	e := newEnv(t)
	run := 0
	rapid.Check(t, func(rt *rapid.T) {
		run++
		// Lượt trước không được ảnh hưởng lượt này.
		e.ownerExec(t, `UPDATE platform.jobs SET status = 'expired', lease_until = NULL, locked_by = NULL
			WHERE status NOT IN ('done', 'expired')`)
		p := &propRun{t: t, e: e, run: run, ext: &testkit.FakeExternal{Mode: rapid.SampledFrom(fakeModes).Draw(rt, "mode0")}}
		name := fmt.Sprintf("command.prop%d", run)
		p.h = &lookH{cmdH: newCmd(name, p.ext)}
		p.h.kind = commands.CommandKind(name, 0, 30*time.Millisecond)
		p.h.autoRetry = rapid.Bool().Draw(rt, "auto")
		p.ids = make([]uuid.UUID, rapid.IntRange(1, 4).Draw(rt, "jobs"))
		for i := range p.ids {
			p.ids[i] = e.enqueue(t, p.h.kind, fmt.Sprintf("r%d-%d", run, i))
		}
		for s := range rapid.IntRange(5, 30).Draw(rt, "steps") {
			p.step(rt, s)
		}
		seen := map[string]int{}
		for _, ref := range p.ext.Calls() {
			if seen[ref]++; seen[ref] > 1 {
				rt.Fatalf("ref %s được bên ngoài ghi nhận %d lần", ref, seen[ref])
			}
		}
		if who := stuckRunning(t, e, name); who != "" {
			rt.Fatalf("lệnh kẹt running sau khi mọi worker đã xong: %s", who)
		}
	})
}

type propRun struct {
	t   *testing.T
	e   *env
	run int
	ext *testkit.FakeExternal
	h   *lookH
	ids []uuid.UUID
}

// step chạy một bước ngẫu nhiên.
func (p *propRun) step(rt *rapid.T, s int) {
	wid := fmt.Sprintf("w%d-%d", p.run, s)
	switch op := rapid.IntRange(0, 6).Draw(rt, "op"); op {
	case 0:
		p.ext.SetMode(rapid.SampledFrom(fakeModes).Draw(rt, "mode"))
	case 1:
		p.e.runOnce(p.t, worker(p.e, wid, p.h))
	case 2, 3: // worker nhận lệnh rồi chết, sau (2) hoặc trước (3) khi bên ngoài nhận
		id := p.ids[rapid.IntRange(0, len(p.ids)-1).Draw(rt, "pick")]
		tag, err := p.e.owner.Pool.Exec(context.Background(), `UPDATE platform.jobs SET status = 'running',
			attempts = attempts + 1, locked_by = 'dead', lease_until = now() - interval '1 second'
			WHERE id = $1 AND status = 'pending' AND run_at <= now()`, id)
		if err != nil {
			p.t.Fatal(err)
		}
		if tag.RowsAffected() == 1 && op == 2 {
			p.ext.Record(id.String())
		}
	case 4: // tắt giữa chừng: Start rồi Stop(5ms) sau 0–15 ms (như SIGTERM lúc đang gửi)
		w := worker(p.e, wid, p.h)
		if err := w.Start(context.Background()); err != nil {
			p.t.Fatal(err)
		}
		time.Sleep(time.Duration(rapid.IntRange(0, 15).Draw(rt, "stop_after_ms")) * time.Millisecond)
		_ = w.Stop(5 * time.Millisecond)
	case 5:
		if _, _, _, err := commands.ResolveInDoubt(p.t.Context(), p.e.app, []commands.Handler{p.h}, 10); err != nil {
			p.t.Fatal(err)
		}
	case 6:
		p.e.makeDue(p.t)
	}
}

// stuckRunning: lệnh running của kind không phải do worker giả "chết" giữ (lệnh của worker chết có giữ chỗ đã qua là
// running hợp lệ tới lượt quét sau). Rỗng khi không có.
func stuckRunning(t *testing.T, e *env, kind string) string {
	t.Helper()
	var who *string
	if err := e.owner.Pool.QueryRow(context.Background(), `SELECT string_agg(locked_by || ' attempts=' || attempts, ', ')
		FROM platform.jobs WHERE kind = $1 AND status = 'running' AND locked_by <> 'dead'`, kind).Scan(&who); err != nil {
		t.Fatal(err)
	}
	if who == nil {
		return ""
	}
	return *who
}
