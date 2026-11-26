package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// pageExternal là mã Page (kênh giả fakechat) của Page thử.
const pageExternal = "page-live-1"

// liveEnv: database riêng, một tenant, một Page kênh giả fakechat.
type liveEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	page   uuid.UUID
	d      deps.Deps
}

func newLiveEnv(t *testing.T) *liveEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	page := s.ChannelAccount(t, tenant, "fakechat", pageExternal, "Shop thử")
	return &liveEnv{
		t: t, staff: s, tenant: tenant, page: page,
		d: deps.Deps{DB: s.App, Queue: queue.NewQueue(), Metrics: metrics.NewRegistry()},
	}
}

// newSessionEnv dựng liveEnv có một phiên live "lv1" (webhook, bắt đầu base) và nguồn giả n comment từ base.
func newSessionEnv(t *testing.T, n int) (*liveEnv, live.Session, *fakeSource, time.Time) {
	t.Helper()
	e := newLiveEnv(t)
	base := time.Date(2026, 11, 26, 12, 15, 0, 0, time.UTC)
	return e, e.observe("lv1", live.StatusLive, live.DetectedWebhook, base), newFakeSource(n, base), base
}

// tx chạy fn bằng crm_app (như tiến trình) trong ngữ cảnh tenant; mustTx: lỗi làm test đỏ.
func (e *liveEnv) tx(fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *liveEnv) mustTx(fn func(pgx.Tx) error) {
	e.t.Helper()
	if err := e.tx(fn); err != nil {
		e.t.Fatalf("giao dịch thử của tenant %s: %v", e.tenant, err)
	}
}

func (e *liveEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// observe ghi một lần thấy phiên videoID của Page thử.
func (e *liveEnv) observe(videoID, state, by string, at time.Time) live.Session {
	e.t.Helper()
	var s live.Session
	e.mustTx(func(tx pgx.Tx) error {
		var err error
		s, _, err = live.UpsertSession(e.t.Context(), tx, live.Observation{
			AccountID: e.page, LiveVideoID: videoID, State: state, DetectedBy: by, At: at,
		})
		return err //nolint:wrapcheck // test
	})
	return s
}

// session đọc lại phiên id: trạng thái, chủ khóa, số đã nhận, đường nhận biết.
func (e *liveEnv) session(id uuid.UUID) (status, owner string, received int, detectedBy string) {
	e.t.Helper()
	e.mustTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, coalesce(lease_owner, ''), received_count, detected_by
			FROM conversation.live_sessions WHERE id = $1`, id).Scan(&status, &owner, &received, &detectedBy)
	})
	return status, owner, received, detectedBy
}

// distinctJobs đếm việc vào (inbound.fakechat) khác nhau theo khóa chống trùng — mỗi comment một việc.
func (e *liveEnv) distinctJobs() int {
	e.t.Helper()
	return e.count(`SELECT count(DISTINCT dedupe_key) FROM platform.jobs WHERE kind = $1`, webhooks.InboundKind("fakechat").Name)
}

// scaled là hằng số khóa thuê thu nhỏ 10 lần (Q3) cho test.
func scaled(owner string) live.Options {
	return live.Options{
		Owner: owner, LeaseTTL: time.Second, RenewEvery: 300 * time.Millisecond,
		ScanEvery: 500 * time.Millisecond, Overlap: live.Overlap,
	}
}

// runManager chạy m tới khi test xong hay stop() được gọi; stop chờ Run trả về.
func runManager(t *testing.T, m *live.Manager) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := m.Run(ctx); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	var once sync.Once
	stop = func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return stop
}

// eventually chờ cond đúng, tối đa within (chốt chống treo), trả thời gian đã chờ.
func eventually(t *testing.T, within time.Duration, what string, cond func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	for !cond() {
		if time.Since(start) > within {
			t.Fatalf("quá %v chưa thấy: %s", within, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return time.Since(start)
}

// fakeSource là nguồn comment giả: phát comment có sẵn (OccurredAt từ since trở đi) theo lô 10, chỉ tới mốc avail;
// ghi mỗi lần Run (phiên, since) và phát hiện hai Run của cùng phiên chạy chồng nhau.
type fakeSource struct {
	mu       sync.Mutex
	comments []webhooks.InboundEvent
	avail    int
	runs     []runRec
	running  map[uuid.UUID]int
	overlap  bool
	changed  chan struct{}
}

type runRec struct {
	session uuid.UUID
	since   time.Time
	at      time.Time
}

// newFakeSource dựng nguồn có n comment, cách nhau 100 ms từ base; avail = n.
func newFakeSource(n int, base time.Time) *fakeSource {
	f := &fakeSource{avail: n, running: map[uuid.UUID]int{}, changed: make(chan struct{})}
	for i := range n {
		id := fmt.Sprintf("lc%04d", i)
		body, _ := json.Marshal(channel.CommentPayload{
			PageID: pageExternal, PostID: "lv1", CommentID: id, FromID: "u" + id, Text: "chốt", Verb: "add",
			CreatedAt: base.Add(time.Duration(i) * 100 * time.Millisecond),
		})
		f.comments = append(f.comments, webhooks.InboundEvent{
			Type: channel.EventCommentChanged, DedupeKey: "c:" + id, OrderKey: "post:lv1",
			OccurredAt: base.Add(time.Duration(i) * 100 * time.Millisecond), Payload: body,
		})
	}
	return f
}

func (f *fakeSource) setAvail(n int) {
	f.mu.Lock()
	f.avail = n
	close(f.changed)
	f.changed = make(chan struct{})
	f.mu.Unlock()
}

func (f *fakeSource) Run(ctx context.Context, s live.Session, since time.Time, emit func([]webhooks.InboundEvent, string) error) error {
	f.mu.Lock()
	f.runs = append(f.runs, runRec{session: s.ID, since: since, at: time.Now()})
	f.running[s.ID]++
	if f.running[s.ID] > 1 {
		f.overlap = true
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running[s.ID]--
		f.mu.Unlock()
	}()
	next := 0
	for {
		f.mu.Lock()
		var batch []webhooks.InboundEvent
		for next < f.avail && len(batch) < 10 {
			if c := f.comments[next]; !c.OccurredAt.Before(since) {
				batch = append(batch, c)
			}
			next++
		}
		wait := f.changed
		f.mu.Unlock()
		if len(batch) > 0 {
			if err := emit(batch, webhooks.ViaLivePoll); err != nil {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wait:
		}
	}
}

func (f *fakeSource) runsOf(id uuid.UUID) []runRec {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []runRec
	for _, r := range f.runs {
		if r.session == id {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeSource) runningOf(id uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[id]
}

func (f *fakeSource) overlapped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.overlap
}
