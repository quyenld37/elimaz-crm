package live_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// liveAdapter là adapter kênh giả chỉ có phần nhận biết live (DetectLive, LiveStatus); phần còn lại của
// MessagingAdapter không được gọi.
type liveAdapter struct {
	channel.MessagingAdapter
	mu     sync.Mutex
	vids   []channel.LiveVideo
	status map[string]string
	fail   bool
	tokens []string
}

func (*liveAdapter) Key() string { return "fakechat" }

func (a *liveAdapter) DetectLive(_ context.Context, _ channel.ChannelAccount, cred channel.Credentials) ([]channel.LiveVideo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokens = append(a.tokens, string(cred.AccessToken))
	if a.fail {
		return nil, errors.New("meta tạm lỗi")
	}
	return append([]channel.LiveVideo{}, a.vids...), nil
}

func (a *liveAdapter) LiveStatus(_ context.Context, _ channel.ChannelAccount, _ channel.Credentials, id string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status[id], nil
}

func (a *liveAdapter) set(fn func(a *liveAdapter)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a)
}

// detectEnv nối adapter giả vào bộ adapter, khóa mã hóa và token của Page thử.
func detectEnv(t *testing.T) (*liveEnv, *liveAdapter, *live.DetectCommand) {
	t.Helper()
	e := newLiveEnv(t)
	a := &liveAdapter{status: map[string]string{}}
	reg, err := channel.NewRegistry(a)
	if err != nil {
		t.Fatal(err)
	}
	kr, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(make([]byte, 32))))
	if err != nil {
		t.Fatal(err)
	}
	e.d.Registry, e.d.Keys = reg, kr
	e.mustTx(func(tx pgx.Tx) error {
		return channel.StoreCredentials(t.Context(), tx, kr, e.page, "token-page", "page_user")
	})
	return e, a, live.NewDetectCommand(e.d)
}

// detectOnce chạy một lượt lệnh như Runner: Execute ngoài transaction, OnDone trong tx tenant.
func (e *liveEnv) detectOnce(c *live.DetectCommand) {
	e.t.Helper()
	j := queue.Job{ID: uuid.New(), TenantID: &e.tenant, Kind: live.DetectCommandName}
	out, err := c.Execute(e.t.Context(), j)
	if err != nil || out.Status != commands.StatusDone {
		e.t.Fatalf("Execute: %+v %v", out, err)
	}
	e.mustTx(func(tx pgx.Tx) error { return c.OnDone(e.t.Context(), j, out.Result, tx) })
}

// LV01 hỏi định kỳ: kênh liệt kê video LIVE → phiên detected_by poll; webhook đã thấy trước → vẫn một phiên; phiên
// không còn trong danh sách mà kênh báo LIVE_STOPPED → ended; kênh lỗi → bỏ lượt, không đổi gì. Parts có lịch
// live.detect 30 s theo tenant cùng lệnh khi bộ adapter nhận biết được live.
func TestDetectCommand_PollFindsAndEndsSessions(t *testing.T) {
	t.Parallel()
	e, a, c := detectEnv(t)
	started := time.Date(2026, 11, 26, 12, 15, 0, 0, time.UTC)
	e.observe("lv-web", live.StatusLive, live.DetectedWebhook, started)
	a.set(func(a *liveAdapter) {
		a.vids = []channel.LiveVideo{
			{LiveVideoID: "lv-poll", VideoID: "v-poll", Status: "LIVE", Title: "Live ca trưa", StartedAt: started},
			{LiveVideoID: "lv-web", VideoID: "v-web", Status: "LIVE"},
		}
	})
	e.detectOnce(c)
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions`); n != 2 {
		t.Fatalf("%d phiên, muốn 2", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE live_video_id = 'lv-poll' AND detected_by = 'poll'
		AND status = 'live' AND video_id = 'v-poll' AND title = 'Live ca trưa' AND started_at = $1`, started); n != 1 {
		t.Error("phiên hỏi định kỳ thiếu hay sai cột")
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE live_video_id = 'lv-web' AND detected_by = 'webhook'
		AND video_id = 'v-web'`); n != 1 {
		t.Error("phiên webhook phải giữ đường nhận biết và được bổ sung mã video")
	}
	if a.tokens[0] != "token-page" {
		t.Errorf("token gửi kênh = %q", a.tokens[0])
	}
	// Kênh lỗi: không đổi gì.
	a.set(func(a *liveAdapter) { a.fail = true })
	e.detectOnce(c)
	// lv-web dừng: không còn trong danh sách, kênh báo LIVE_STOPPED.
	a.set(func(a *liveAdapter) {
		a.fail = false
		a.vids = a.vids[:1]
		a.status["lv-web"] = "LIVE_STOPPED"
	})
	e.detectOnce(c)
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE status = 'ended' AND live_video_id = 'lv-web'
		AND ended_at IS NOT NULL`); n != 1 {
		t.Error("phiên đã dừng phải ended")
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE status = 'live'`); n != 1 {
		t.Errorf("%d phiên live, muốn 1", n)
	}
	if !c.AutoRetryNotSent() {
		t.Error("lệnh chỉ đọc phải tự chạy lại khi chưa gửi")
	}
	if st, err := c.Lookup(t.Context(), queue.Job{}); err != nil || st != commands.StatusNotSent {
		t.Errorf("Lookup = %q %v", st, err)
	}
	p := live.Parts(e.d)
	if len(p.Commands) != 2 || p.Commands[1].Kind().Name != live.ReconcileCommandName || len(p.Background) != 0 || len(p.Schedules) != 1 || p.Schedules[0].Name != live.DetectSchedule ||
		p.Schedules[0].Every != 30*time.Second || !p.Schedules[0].PerTenant ||
		p.Schedules[0].Kind.Name != live.DetectCommandName || p.Commands[0].Kind().Name != live.DetectCommandName {
		t.Errorf("Parts: %+v", p)
	}
	// Không có kênh nhận biết live: chỉ lệnh đối chiếu (Task 171, luôn có).
	if p := live.Parts(newLiveEnv(t).d); len(p.Commands) != 1 || len(p.Schedules)+len(p.Background) != 0 {
		t.Errorf("không có kênh nhận biết live: %+v", p)
	}
}
