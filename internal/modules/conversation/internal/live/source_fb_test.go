package live_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
	"elimaz.com.vn/crm/internal/testkit/fbtest"
)

const (
	fbPageExt = "104000000000171"
	fbToken   = "EAAB-page-token-171"
	fbLiveID  = "lv-171"
)

// fbBase là giờ comment live đầu tiên (comment i lúc fbBase + i giây; Meta lọc since theo giây).
var fbBase = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // hằng thời gian của test

// fbEnv: database riêng, tenant có Page Facebook (máy Meta giả, có token) và phiên live fbLiveID (webhook, bắt đầu
// fbBase − 1 phút); adapter Facebook thật nối máy giả.
type fbEnv struct {
	*liveEnv
	fg *fakegraph.Server
	fb *facebook.Channel
	s  live.Session
}

func newFBEnv(t *testing.T) *fbEnv {
	t.Helper()
	fbe := fbtest.New(t, fbtest.Options{PageID: fbPageExt, PageToken: fbToken})
	fbe.Graph.AddLiveVideo(fakegraph.LiveVideo{ID: fbLiveID, PageID: fbPageExt, VideoID: "v-171", Status: "LIVE"})
	e := &fbEnv{
		liveEnv: &liveEnv{t: t, staff: fbe.Staff, tenant: fbe.Tenant, page: fbe.Page, d: deps.Deps{
			DB: fbe.Staff.App, Queue: queue.NewQueue(), Metrics: metrics.NewRegistry(), Registry: fbe.Registry, Keys: fbe.Keys,
		}},
		fg: fbe.Graph, fb: fbe.Channel,
	}
	e.s = e.observe(fbLiveID, live.StatusLive, live.DetectedWebhook, fbBase.Add(-time.Minute))
	return e
}

// fastSource là cấu hình nguồn thu nhỏ cho test (hằng thật kiểm ở TestSourceConstants).
func fastSource() live.SourceOptions {
	return live.SourceOptions{
		PollEvery: 200 * time.Millisecond, SilenceAfter: time.Second, RetrySSEEvery: time.Minute, FailWindow: 10 * time.Second,
		ReconnectDelays: []time.Duration{300 * time.Millisecond, 300 * time.Millisecond, 300 * time.Millisecond},
	}
}

// start chạy bộ quản lý (khóa thuê thu nhỏ) với nguồn Facebook cấu hình o.
func (e *fbEnv) start(o live.SourceOptions) (stop func()) {
	e.t.Helper()
	return runManager(e.t, live.NewManager(e.d, live.NewSource(e.d, o), scaled("w1")))
}

func fbComment(i int) fakegraph.Comment {
	return fakegraph.Comment{
		ID: fmt.Sprintf("%s_c%04d", fbLiveID, i), FromID: "u" + strconv.Itoa(i), FromName: "Khách " + strconv.Itoa(i),
		Message: "chốt " + strconv.Itoa(i), Created: fbBase.Add(time.Duration(i) * time.Second),
	}
}

// webhookComment đưa comment i qua đường webhook feed (tách bằng adapter thật, ghi bằng EnqueueInbound via webhook).
func (e *fbEnv) webhookComment(i int) {
	e.t.Helper()
	c := fbComment(i)
	raw := fmt.Sprintf(`{"object":"page","entry":[{"id":%q,"time":1,"changes":[{"field":"feed","value":{"from":{"id":%q,"name":%q},"post_id":%q,"comment_id":%q,"created_time":%d,"item":"comment","verb":"add","message":%q}}]}]}`,
		fbPageExt, c.FromID, c.FromName, fbLiveID, c.ID, c.Created.Unix(), c.Message)
	res, err := e.fb.Split([]byte(raw))
	if err != nil || len(res.Events) != 1 {
		e.t.Fatalf("tách webhook: %+v %v", res, err)
	}
	e.mustTx(func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, e.d.Queue, e.tenant, facebook.Key, res.Events[0], time.Now(), webhooks.ViaWebhook)
		return err //nolint:wrapcheck // test
	})
}

// jobs: số việc vào Facebook và số khóa chống trùng khác nhau.
func (e *fbEnv) jobs() (total, distinct int) {
	e.t.Helper()
	k := webhooks.InboundKind(facebook.Key).Name
	return e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, k),
		e.count(`SELECT count(DISTINCT dedupe_key) FROM platform.jobs WHERE kind = $1`, k)
}

func (e *fbEnv) waitJobs(n int) {
	e.t.Helper()
	eventually(e.t, 20*time.Second, fmt.Sprintf("%d việc comment", n), func() bool {
		_, d := e.jobs()
		return d >= n
	})
	if total, d := e.jobs(); total != n || d != n {
		e.t.Fatalf("%d việc, %d khóa khác nhau; muốn đúng %d (không trùng)", total, d, n)
	}
}

func (e *fbEnv) activeSource() string {
	e.t.Helper()
	var src string
	e.mustTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(active_source, '') FROM conversation.live_sessions WHERE id = $1`,
			e.s.ID).Scan(&src)
	})
	return src
}

// alerts đếm lệnh cảnh báo phiên live có khóa chống trùng bắt đầu bằng prefix.
func (e *fbEnv) alerts(prefix string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key LIKE $2`, live.AlertCommandName, prefix+"%")
}

// pollGaps kiểm các lượt hỏi định kỳ {live}/comments cách nhau ≥ every (trừ lỗi đo 20 ms).
func (e *fbEnv) pollGaps(every time.Duration) {
	e.t.Helper()
	var calls []fakegraph.Call // trang đầu của mỗi lượt (trang sau có after, cùng lượt) của hỏi định kỳ (không summary)
	for _, c := range e.fg.Calls(fbLiveID + "/comments") {
		if !c.Query.Has("after") && !c.Query.Has("summary") {
			calls = append(calls, c)
		}
	}
	for i := 1; i < len(calls); i++ {
		if gap := calls[i].At.Sub(calls[i-1].At); gap < every-20*time.Millisecond {
			e.t.Errorf("lần hỏi %d cách lần trước %v, muốn ≥ %v", i, gap, every)
		}
	}
}

// streamAsked: mọi yêu cầu luồng có comment_rate và appsecret_proof đúng.
func (e *fbEnv) streamAsked() int {
	e.t.Helper()
	calls := e.fg.Calls(fbLiveID + "/live_comments")
	for _, c := range calls {
		if c.Query.Get("comment_rate") != "ten_per_second" || !c.ProofOK {
			e.t.Errorf("yêu cầu luồng thiếu comment_rate hay appsecret_proof: %v", c.Query)
		}
	}
	return len(calls)
}

// pushBatch phát 50 comment của lô batch qua luồng; 1/6 trong số đó (comment 0, 6, …, 594: 100 comment cả 12 lô) đi
// thêm qua webhook feed.
func (e *fbEnv) pushBatch(batch int) {
	e.t.Helper()
	for i := batch * 50; i < batch*50+50; i++ {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
	}
	for i := batch * 50; i < batch*50+50; i++ {
		if i%6 == 0 {
			e.webhookComment(i)
		}
	}
}

// LV02 (02 mục 2): 600 comment qua luồng SSE, Meta cắt luồng hai lần giữa chừng (comment phát lúc luồng rớt được lượt
// hỏi định kỳ lúc nối lại lấp), webhook feed đưa 100 comment trong số đó → đúng 600 việc comment khác nhau, không
// trùng; active_source = sse, không cảnh báo; mỗi yêu cầu luồng có comment_rate, appsecret_proof; hỏi định kỳ cách
// nhau ≥ PollEvery. Kết thúc phiên → lệnh đối chiếu: meta_comment_count = 600, không cảnh báo live-missing.
func TestLV02_StreamDropsAndWebhookStillExact(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	o := fastSource()
	e.start(o)
	eventually(t, 10*time.Second, "luồng SSE mở", func() bool { return e.fg.StreamCount(fbLiveID) == 1 })
	for batch := range 12 {
		dropped := batch == 4 || batch == 8
		if dropped {
			// Luồng đang rớt (nối lại sau 300 ms): 50 comment của lô này chỉ đọc được bằng hỏi định kỳ lúc nối lại.
			e.fg.DropStream(fbLiveID)
		}
		e.pushBatch(batch)
		if dropped {
			eventually(t, 10*time.Second, "luồng mở lại", func() bool { return e.fg.StreamCount(fbLiveID) == 1 })
		}
		eventually(t, 10*time.Second, "lô đã ghi", func() bool { _, n := e.jobs(); return n >= (batch+1)*50 })
	}
	e.waitJobs(600)
	if src := e.activeSource(); src != live.SourceSSE {
		t.Errorf("active_source = %q, muốn sse", src)
	}
	if n := e.alerts("live-source:"); n != 0 {
		t.Errorf("%d cảnh báo live-source, muốn 0", n)
	}
	if n := e.streamAsked(); n < 3 {
		t.Errorf("%d lần mở luồng, muốn ≥ 3 (mở + 2 lần nối lại)", n)
	}
	e.pollGaps(o.PollEvery)
	e.observe(fbLiveID, live.StatusEnded, live.DetectedPoll, time.Now())
	res := e.reconcile()
	if res.meta != 600 || res.missing != 0 {
		t.Errorf("đối chiếu: %+v; muốn meta 600, không cảnh báo", res)
	}
}

// Luồng SSE trả 404 → sau 3 lần chuyển sang hỏi định kỳ: active_source = poll, đúng 1 cảnh báo live-source (kể cả khi
// thử lại luồng vẫn lỗi), vẫn đủ 600 comment; hỏi định kỳ cách nhau ≥ PollEvery.
func TestStream404FallsBackToPoll(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	path := fbLiveID + "/live_comments"
	for range 20 {
		e.fg.FailNext(path, fakegraph.GraphErrorSpec{HTTPStatus: http.StatusNotFound, Code: 803, Type: "OAuthException", Message: "(#803) not found"})
	}
	o := fastSource()
	o.RetrySSEEvery = 700 * time.Millisecond
	e.start(o)
	eventually(t, 10*time.Second, "chuyển sang hỏi định kỳ", func() bool { return e.activeSource() == live.SourcePoll })
	// Chuyển sau đúng 3 lần: lần mở thứ 4 là lần thử lại sau RetrySSEEvery, không phải lần nối lại sau 300 ms.
	eventually(t, 10*time.Second, "thử lại luồng", func() bool { return e.streamAsked() >= 4 })
	opens := e.fg.Calls(fbLiveID + "/live_comments")
	if gap := opens[3].At.Sub(opens[2].At); gap < o.RetrySSEEvery-50*time.Millisecond {
		t.Errorf("lần mở thứ 4 sau lần thứ 3 %v, muốn ≥ RetrySSEEvery %v (chuyển sau 3 lần)", gap, o.RetrySSEEvery)
	}
	for i := range 600 {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
	}
	e.waitJobs(600)
	eventually(t, 10*time.Second, "thử lại luồng sau RetrySSEEvery", func() bool { return e.streamAsked() >= 5 })
	if src := e.activeSource(); src != live.SourcePoll {
		t.Errorf("active_source = %q, muốn poll", src)
	}
	if n := e.alerts("live-source:" + e.s.ID.String()); n != 1 {
		t.Errorf("%d cảnh báo live-source, muốn đúng 1", n)
	}
	e.pollGaps(o.PollEvery)
}

// Luồng mở nhưng im SilenceAfter trong khi Meta có comment mới (chỉ đọc được bằng hỏi định kỳ) → lấy được comment đó,
// chuyển sang hỏi định kỳ, 1 cảnh báo; sau RetrySSEEvery thử luồng lại được → active_source = sse.
func TestSilentStreamFallsBackToPoll(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	o := fastSource()
	o.RetrySSEEvery = 2 * time.Second
	e.start(o)
	eventually(t, 10*time.Second, "luồng SSE mở và lượt lấp chỗ hở xong", func() bool {
		return e.fg.StreamCount(fbLiveID) == 1 && len(e.fg.Calls(fbLiveID+"/comments")) == 1
	})
	for i := range 3 {
		e.fg.AddLiveComment(fbLiveID, fbComment(i))
	}
	e.waitJobs(3)
	eventually(t, 10*time.Second, "chuyển sang hỏi định kỳ", func() bool { return e.activeSource() == live.SourcePoll })
	if n := e.alerts("live-source:"); n != 1 {
		t.Errorf("%d cảnh báo live-source, muốn 1", n)
	}
	eventually(t, 10*time.Second, "luồng mở lại sau RetrySSEEvery", func() bool { return e.activeSource() == live.SourceSSE })
	e.fg.PushLiveComment(fbLiveID, fbComment(3))
	e.waitJobs(4)
	if n := e.alerts("live-source:"); n != 1 {
		t.Errorf("sau khi thử lại: %d cảnh báo live-source, muốn 1", n)
	}
}

// source_mode = poll ép hỏi định kỳ: không mở luồng, không cảnh báo.
func TestSourceModePollNeverStreams(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	e.mustTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.live_sessions SET source_mode = 'poll' WHERE id = $1`, e.s.ID)
		return err //nolint:wrapcheck // test
	})
	for i := range 5 {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
	}
	e.start(fastSource())
	e.waitJobs(5)
	if n := e.streamAsked(); n != 0 {
		t.Errorf("%d lần mở luồng ở chế độ poll", n)
	}
	if src := e.activeSource(); src != live.SourcePoll {
		t.Errorf("active_source = %q", src)
	}
	if n := e.alerts("live-source:"); n != 0 {
		t.Errorf("%d cảnh báo ở chế độ poll ép", n)
	}
}

// LV10 với nguồn thật: worker 1 đọc 100 comment qua luồng rồi chết (không nhả khóa) → worker 2 nhận phiên, lượt hỏi
// định kỳ đầu tiên có since = last_comment_at − 5 s, comment phát lúc không ai đọc được lấp; tổng đủ 200, không trùng.
func TestLV10_RealSourceReopensAndBackfills(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	o := fastSource()
	m1 := live.NewManager(e.d, live.NewSource(e.d, o), scaled("w1"))
	stop1 := runManager(t, m1)
	eventually(t, 10*time.Second, "luồng SSE mở", func() bool { return e.fg.StreamCount(fbLiveID) == 1 })
	for i := range 100 {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
	}
	e.waitJobs(100)
	m1.Crash()
	stop1()
	before := len(e.fg.Calls(fbLiveID + "/comments"))
	for i := 100; i < 200; i++ {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
	}
	runManager(t, live.NewManager(e.d, live.NewSource(e.d, o), scaled("w2")))
	e.waitJobs(200)
	calls := e.fg.Calls(fbLiveID + "/comments")
	if len(calls) <= before {
		t.Fatal("worker 2 không hỏi bù")
	}
	want := fbComment(99).Created.Add(-live.Overlap).Unix()
	if got := calls[before].Query.Get("since"); got != strconv.FormatInt(want, 10) {
		t.Errorf("since của lượt hỏi bù = %s, muốn last_comment_at − 5 s = %d", got, want)
	}
}

// Hằng thật của nguồn (02 mục 2) và Parts khi bộ adapter có kênh đọc được comment live: lệnh nhận biết, lệnh đối
// chiếu và một luồng nền.
func TestSourceConstantsAndParts(t *testing.T) {
	t.Parallel()
	if live.PollEvery != 2*time.Second || live.SilenceAfter != 30*time.Second || live.RetrySSEEvery != 5*time.Minute ||
		live.FailWindow != time.Minute {
		t.Error("hằng nguồn comment live sai")
	}
	if d := live.ReconnectDelays(); len(d) != 3 || d[0] != time.Second || d[1] != 2*time.Second || d[2] != 5*time.Second {
		t.Errorf("khoảng nối lại: %v", d)
	}
	reg, err := channel.NewRegistry(facebook.New(facebook.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	p := live.Parts(deps.Deps{Registry: reg})
	var names []string
	for _, c := range p.Commands {
		names = append(names, c.Kind().Name)
	}
	if fmt.Sprint(names) != fmt.Sprint([]string{live.DetectCommandName, live.ReconcileCommandName}) || len(p.Background) != 1 {
		t.Errorf("Parts: lệnh %v, %d luồng nền", names, len(p.Background))
	}
}
