package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
	"elimaz.com.vn/crm/internal/testkit/livetest"
)

// Biên của yêu cầu chốt (trên database thật, comment lạ): đúng 20 dòng, số lượng đúng 20 qua bước kiểm (ErrNotFound ở
// bước đọc comment), 21 thì ErrClaimInvalid; thêm vào nháp cần cả mã đơn lẫn version ≥ 1; lỗi chi tiết comment đã
// chốt, khách đã có nháp khớp đúng sentinel của nó và không khớp sentinel kia (Task 182, mutation testing M4).
func TestMutationM4_ClaimInputBoundsAndErrorKinds(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	lines := func(n int, qty int32) []live.ClaimItem {
		out := make([]live.ClaimItem, n)
		for i := range out {
			out[i] = live.ClaimItem{VariantID: uuid.New(), Qty: qty}
		}
		return out
	}
	order := uuid.New()
	for _, c := range []struct {
		name    string
		in      live.ClaimInput
		invalid bool
	}{
		{"20 dòng, mỗi dòng 20", live.ClaimInput{Mode: live.ModeNew, Items: lines(20, 20)}, false},
		{"21 dòng", live.ClaimInput{Mode: live.ModeNew, Items: lines(21, 1)}, true},
		{"số lượng 21", live.ClaimInput{Mode: live.ModeNew, Items: lines(1, 21)}, true},
		{"thêm vào nháp đủ mã đơn và version", live.ClaimInput{Mode: live.ModeAddToDraft, Items: lines(1, 1), OrderID: &order, ExpectedVersion: 1}, false},
		{"thêm vào nháp có mã đơn, version 0", live.ClaimInput{Mode: live.ModeAddToDraft, Items: lines(1, 1), OrderID: &order}, true},
		{"thêm vào nháp thiếu mã đơn, version 1", live.ClaimInput{Mode: live.ModeAddToDraft, Items: lines(1, 1), ExpectedVersion: 1}, true},
	} {
		_, err := e.claim(e.an, uuid.New(), c.in)
		switch {
		case c.invalid && !errors.Is(err, live.ErrClaimInvalid):
			t.Errorf("%s: %v, muốn ErrClaimInvalid", c.name, err)
		case !c.invalid && !errors.Is(err, live.ErrNotFound):
			t.Errorf("%s: %v, muốn qua bước kiểm (ErrNotFound ở bước đọc comment)", c.name, err)
		}
	}
	var claimed error = &live.CommentClaimedError{OrderCode: "DH1"}
	if !errors.Is(claimed, live.ErrCommentClaimed) || errors.Is(claimed, live.ErrOpenDraftExists) {
		t.Errorf("CommentClaimedError khớp sai sentinel")
	}
	var open error = &live.OpenDraftExistsError{}
	if !errors.Is(open, live.ErrOpenDraftExists) || errors.Is(open, live.ErrCommentClaimed) {
		t.Errorf("OpenDraftExistsError khớp sai sentinel")
	}
}

// Thêm vào nháp với [biến thể đã có, biến thể mới] → đơn có đủ hai dòng (cộng số lượng dòng trùng, dòng mới không bị
// bỏ); báo hiệu conversation.changed của hội thoại comment vừa chốt; khách khác trong cùng phiên chọn đơn đó →
// ErrDraftMismatch (Task 182, mutation testing M4).
func TestMutationM4_ClaimAddToDraftMergesAndRejectsOtherCustomer(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	v1, v2 := e.variant(10), e.variant(10)
	lan, mai := e.customer("Lan"), e.customer("Mai")
	c1 := e.comment(e.fbPage, lan, "6100000000000181", "chốt 1", time.Now().Add(-3*time.Minute))
	c2 := e.comment(e.fbPage, lan, "6100000000000182", "chốt thêm", time.Now().Add(-2*time.Minute))
	c3 := e.comment(e.fbPage, mai, "6100000000000183", "chốt", time.Now().Add(-time.Minute))
	in := newClaim(v1, 1)
	in.AskAddress = false
	first := e.mustClaim(c1, in)
	add := conversation.LiveClaimInput{
		Mode: conversation.LiveClaimAddToDraft, OrderID: &first.Order.ID, ExpectedVersion: first.Order.Version,
		Items: []conversation.LiveClaimItem{{VariantID: v1, Qty: 1}, {VariantID: v2, Qty: 2}},
	}
	got := e.mustClaim(c2, add)
	qty := map[uuid.UUID]int32{}
	for _, it := range got.Order.Items {
		qty[it.VariantID] += it.Qty
	}
	if len(got.Order.Items) != 2 || qty[v1] != 2 || qty[v2] != 2 {
		t.Errorf("dòng đơn sau khi thêm = %+v; muốn v1 × 2, v2 × 2", got.Order.Items)
	}
	if !e.signaled("conversation.changed", c2.conv.String()) {
		t.Errorf("thiếu báo hiệu conversation.changed của hội thoại %s: %+v", c2.conv, e.bus.Signals())
	}
	other := add
	other.ExpectedVersion = got.Order.Version
	other.Items = []conversation.LiveClaimItem{{VariantID: v2, Qty: 1}}
	if _, err := e.claim(e.an, c3.id, other); !errors.Is(err, conversation.ErrDraftMismatch) {
		t.Errorf("khách khác chọn đơn nháp của Lan: %v, muốn ErrDraftMismatch", err)
	}
}

// Danh sách nháp: dòng có hộp thư trả mã hộp thư, dòng không có hộp thư trả nil; tin xin địa chỉ đã gửi (không lỗi)
// và không xin địa chỉ giữ nguyên lý do, không thay bằng câu lỗi gửi. Nhắc [đơn đã xác nhận, đơn nháp] theo giờ chốt →
// đơn đã xác nhận skipped, đơn nháp sau nó vẫn được nhắc; nhắc chỉ một đơn lạ → một kết quả skipped (Task 182,
// mutation testing M4).
func TestMutationM4_PendingInboxReasonAndRemindAfterConfirmed(t *testing.T) {
	t.Parallel()
	e := newClaimEnv(t)
	e.staff.SeedAdminUnits(t)
	v, now := e.variant(10), time.Now()
	open := now.Add(-30 * time.Minute)
	a := e.claimed("Ánh", "6100000000018201", v, open, now)
	b := e.claimed("Bích", "6100000000018202", v, open, now)
	cust := e.customer("Cúc")
	cc := e.comment(e.fbPage, cust, "6100000000018203", "chốt", now.Add(-10*time.Minute))
	in := newClaim(v, 1)
	in.AskAddress = false
	resC := e.mustClaim(cc, in)
	e.drain()

	rows := map[uuid.UUID]conversation.PendingDraft{}
	for _, r := range e.mustPending(now).Items {
		rows[r.OrderID] = r
	}
	ra, rc := rows[a.order.ID], rows[resC.Order.ID]
	if ra.InboxConversationID == nil || *ra.InboxConversationID != a.inbox {
		t.Errorf("hộp thư của dòng Ánh = %v, muốn %s", ra.InboxConversationID, a.inbox)
	}
	if rc.InboxConversationID != nil {
		t.Errorf("dòng Cúc (chưa có hộp thư) có hộp thư %s", *rc.InboxConversationID)
	}
	if ra.Ask.Reason != "" {
		t.Errorf("lý do xin địa chỉ đã gửi = %q, muốn rỗng", ra.Ask.Reason)
	}
	if rc.Ask.Status != conversation.LiveAskNotRequested || rc.Ask.Reason != resC.Ask.Reason {
		t.Errorf("dòng không xin địa chỉ: ask = %+v, muốn %+v", rc.Ask, resC.Ask)
	}

	if got := e.confirmWithAddress(a.order); got.Status != "confirmed" {
		t.Fatalf("hoàn tất đơn Ánh = %s", got.Status)
	}
	out := e.mustRemind(now, a.order.ID, b.order.ID)
	if r := out[a.order.ID]; r.Outcome != conversation.RemindSkipped || r.Reason != conversation.RemindNotDraftText {
		t.Errorf("đơn đã xác nhận: %+v", r)
	}
	if r := out[b.order.ID]; r.Outcome != conversation.RemindQueued {
		t.Errorf("đơn nháp sau đơn đã xác nhận: %+v, muốn queued", r)
	}
	stray := uuid.New()
	only := e.mustRemind(now, stray)
	if r := only[stray]; len(only) != 1 || r.Outcome != conversation.RemindSkipped || r.Reason != conversation.RemindNotInSessionText {
		t.Errorf("đơn lạ: %+v", only)
	}
}

// Lệnh cảnh báo: payload đúng JSON mà thiếu mã phiên → rejected, không gửi; văn bản live-missing nêu đúng số thiếu
// (meta − got) (Task 182, mutation testing M4).
func TestMutationM4_AlertRejectsNilSessionAndCountsMissing(t *testing.T) {
	t.Parallel()
	a := &recordingAlerter{}
	c := live.NewAlertCommand(a, slog.New(slog.DiscardHandler))
	out, err := c.Execute(t.Context(), queue.Job{Payload: []byte(`{"kind":"source"}`)})
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" || len(a.sent) != 0 {
		t.Errorf("thiếu mã phiên: %+v %v, đã gửi %q", out, err, a.sent)
	}
	body, _ := json.Marshal(map[string]any{"kind": "missing", "sessionId": uuid.New(), "got": 15, "meta": 20})
	if out, err := c.Execute(t.Context(), queue.Job{Payload: body}); err != nil || out.Status != commands.StatusDone {
		t.Fatalf("live-missing: %+v %v", out, err)
	}
	if len(a.sent) != 1 || !strings.Contains(a.sent[0], "15/20") || !strings.Contains(a.sent[0], "(thiếu 5)") {
		t.Errorf("văn bản cảnh báo: %q, muốn 15/20 (thiếu 5)", a.sent)
	}
}

// Đối chiếu: payload JSON đúng mà không có mã phiên → rejected bad_payload (không đọc database); kết quả hỏng →
// ErrPermanent; Meta đếm vượt int32 → meta_comment_count chặn ở 2 147 483 647 và vẫn ghi cảnh báo live-missing kể cả
// khi Deps không có hàng đợi (Task 182, mutation testing M4).
func TestMutationM4_ReconcilePayloadAndOverflow(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	s := e.observe("lv-m4", live.StatusLive, live.DetectedManual, time.Now())
	c := live.NewReconcileCommand(e.d)
	out, err := c.Execute(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: []byte(`{}`)})
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
		t.Errorf("payload thiếu mã phiên: %+v %v", out, err)
	}
	payload, _ := json.Marshal(map[string]any{"sessionId": s.ID})
	j := queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: payload}
	if err := e.tx(func(tx pgx.Tx) error { return c.OnDone(t.Context(), j, []byte(`không phải JSON`), tx) }); !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("kết quả hỏng: %v, muốn ErrPermanent", err)
	}
	d := e.d
	d.Queue = nil
	nq := live.NewReconcileCommand(d)
	e.mustTx(func(tx pgx.Tx) error {
		return nq.OnDone(t.Context(), j, []byte(`{"got":5,"meta":3000000000}`), tx)
	})
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE id = $1 AND meta_comment_count = 2147483647`, s.ID); n != 1 {
		t.Errorf("meta_comment_count không chặn ở trần int32")
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`, live.AlertCommandName,
		"live-missing:"+s.ID.String()+":"+j.ID.String()); n != 1 {
		t.Errorf("%d cảnh báo live-missing, muốn 1", n)
	}
}

// Đối chiếu đếm cả comment is_live nằm trên bài mã video của phiên (không chỉ mã live): 1 comment trên bài mã live, 2
// trên bài mã video → got = 3 (Task 182, mutation testing M4).
func TestMutationM4_ReconcileCountsCommentsOnVideoPost(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	e.mustTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.live_sessions SET video_id = 'v-171' WHERE id = $1`, e.s.ID)
		return err //nolint:wrapcheck // test
	})
	cust := livetest.Customer(t, e.staff, e.tenant, "Khách đối chiếu")
	at := fbBase
	livetest.LiveComment(t, e.staff, e.tenant, e.page, cust, fbLiveID, "u1", fbLiveID+"_m1", "chốt", at)
	livetest.LiveComment(t, e.staff, e.tenant, e.page, cust, "v-171", "u2", "v-171_m2", "chốt", at)
	livetest.LiveComment(t, e.staff, e.tenant, e.page, cust, "v-171", "u3", "v-171_m3", "chốt", at)
	e.fg.SetLiveCommentTotal(fbLiveID, 3)
	body, _ := json.Marshal(map[string]any{"sessionId": e.s.ID})
	out, err := live.NewReconcileCommand(e.d).Execute(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: body})
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute: %+v %v", out, err)
	}
	var res struct{ Got, Meta int }
	if err := json.Unmarshal(out.Result, &res); err != nil || res.Got != 3 || res.Meta != 3 {
		t.Errorf("kết quả đối chiếu = %s, muốn got 3, meta 3", out.Result)
	}
}

// pageAdapter là liveAdapter (trạng thái theo mã) với danh sách video theo từng Page: lần DetectLive đầu tiên lỗi khi
// failFirst; ghi các Page đã được hỏi.
type pageAdapter struct {
	*liveAdapter
	vids      map[uuid.UUID][]channel.LiveVideo
	failFirst bool
	asked     []uuid.UUID
}

func (a *pageAdapter) DetectLive(_ context.Context, acc channel.ChannelAccount, _ channel.Credentials) ([]channel.LiveVideo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, acc.ID)
	if a.failFirst && len(a.asked) == 1 {
		return nil, errors.New("meta tạm lỗi")
	}
	return append([]channel.LiveVideo{}, a.vids[acc.ID]...), nil
}

// pageDetectEnv là detectEnv với bộ adapter chỉ có pageAdapter (cùng khóa mã hóa, token của Page thử).
func pageDetectEnv(t *testing.T) (*liveEnv, *pageAdapter) {
	t.Helper()
	e, base, _ := detectEnv(t)
	a := &pageAdapter{liveAdapter: base, vids: map[uuid.UUID][]channel.LiveVideo{}}
	reg, err := channel.NewRegistry(a)
	if err != nil {
		t.Fatal(err)
	}
	e.d.Registry = reg
	return e, a
}

// pageWithToken tạo Page kênh giả ext tên name của tenant, có token.
func (e *liveEnv) pageWithToken(ext, name string) uuid.UUID {
	e.t.Helper()
	id := e.staff.ChannelAccount(e.t, e.tenant, "fakechat", ext, name)
	e.mustTx(func(tx pgx.Tx) error {
		return channel.StoreCredentials(e.t.Context(), tx, e.d.Keys, id, logging.Secret("token-"+ext), "page_user")
	})
	return id
}

// Nhận biết live: Page đã ngắt kết nối (dù còn token) không được hỏi; Page đầu lỗi thì vẫn hỏi Page sau (video của Page
// sau thành phiên) và lỗi được đếm vào live_detect_failed (Task 182, mutation testing M4).
func TestMutationM4_DetectSkipsDisconnectedAndContinuesAfterError(t *testing.T) {
	t.Parallel()
	e, a := pageDetectEnv(t)
	page2 := e.pageWithToken("page-live-2", "Shop thử 2")
	gone := e.pageWithToken("page-live-3", "Shop đã ngắt")
	livetest.Exec(t, e.staff, e.tenant, `UPDATE channel.channel_accounts SET status = 'disconnected' WHERE id = $1`, gone)
	a.failFirst = true
	a.vids[e.page] = []channel.LiveVideo{{LiveVideoID: "lv-p1", Status: "LIVE"}}
	a.vids[page2] = []channel.LiveVideo{{LiveVideoID: "lv-p2", Status: "LIVE"}}
	a.vids[gone] = []channel.LiveVideo{{LiveVideoID: "lv-p3", Status: "LIVE"}}
	e.detectOnce(live.NewDetectCommand(e.d))
	if len(a.asked) != 2 || a.asked[0] == a.asked[1] {
		t.Fatalf("Page đã hỏi = %v, muốn đúng 2 Page còn kết nối", a.asked)
	}
	for _, id := range a.asked {
		if id == gone {
			t.Errorf("Page đã ngắt kết nối vẫn được hỏi")
		}
	}
	second := map[uuid.UUID]string{e.page: "lv-p1", page2: "lv-p2"}[a.asked[1]]
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE live_video_id = $1 AND status = 'live'`, second); n != 1 {
		t.Errorf("Page hỏi sau Page lỗi: %d phiên %s, muốn 1", n, second)
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions`); n != 1 {
		t.Errorf("%d phiên, muốn 1 (Page lỗi bỏ lượt, Page ngắt không hỏi)", n)
	}
	if r, ok := metricRow(e.d.Metrics.Drain(), "live_detect_failed", "fakechat"); !ok || r.Count != 1 {
		t.Errorf("đếm lỗi nhận biết = %+v %v, muốn 1", r, ok)
	}
}

// Nhận biết live: mọi video LIVE trong danh sách thành phiên (không dừng sau video đầu), video trạng thái khác LIVE hay
// thiếu mã bị bỏ; phiên đã biết còn trong danh sách bị bỏ qua mà phiên sau nó vẫn được hỏi trạng thái; mọi phiên kênh
// báo dừng đều ended; phiên kênh báo vẫn LIVE (dù không có trong danh sách) giữ live (Task 182, mutation testing M4).
func TestMutationM4_DetectEveryVideoAndKnownSession(t *testing.T) {
	t.Parallel()
	e, a := pageDetectEnv(t)
	for _, id := range []string{"lv-k1", "lv-k2", "lv-k3", "lv-k4"} {
		e.observe(id, live.StatusLive, live.DetectedWebhook, time.Now())
	}
	var known []string // theo id tăng dần, như lệnh đọc
	e.mustTx(func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT live_video_id FROM conversation.live_sessions ORDER BY id`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		known, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	// Phiên đầu còn trong danh sách; hai phiên sau đã dừng; phiên cuối kênh vẫn báo LIVE.
	inList, ended, still := known[0], known[1:3], known[3]
	a.status[ended[0]], a.status[ended[1]], a.status[still] = "LIVE_STOPPED", "VOD", "LIVE"
	a.vids[e.page] = []channel.LiveVideo{
		{LiveVideoID: inList, Status: "LIVE"},
		{LiveVideoID: "lv-n1", Status: "LIVE"},
		{LiveVideoID: "lv-n2", Status: "LIVE"},
		{LiveVideoID: "lv-stop", Status: "LIVE_STOPPED"},
		{LiveVideoID: "lv-lạ", Status: "SCHEDULED_UNPUBLISHED"},
		{LiveVideoID: "", Status: "LIVE"},
	}
	e.detectOnce(live.NewDetectCommand(e.d))
	for _, id := range []string{inList, "lv-n1", "lv-n2", still} {
		if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE live_video_id = $1 AND status = 'live'`, id); n != 1 {
			t.Errorf("phiên %s: %d dòng live, muốn 1", id, n)
		}
	}
	for _, id := range ended {
		if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE live_video_id = $1 AND status = 'ended'`, id); n != 1 {
			t.Errorf("phiên %s kênh báo dừng chưa ended", id)
		}
	}
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions`); n != 6 {
		t.Errorf("%d phiên, muốn 6 (video không LIVE hay thiếu mã không thành phiên)", n)
	}
}

// onceSource phát một lô evs một lần rồi chờ tới khi ctx bị hủy.
type onceSource struct {
	evs []webhooks.InboundEvent
}

func (o *onceSource) Run(ctx context.Context, _ live.Session, _ time.Time, emit func([]webhooks.InboundEvent, string) error) error {
	if err := emit(o.evs, webhooks.ViaLivePoll); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// metricRow tìm chỉ số name nhãn label trong rows.
func metricRow(rows []metrics.Row, name, label string) (metrics.Row, bool) {
	for _, r := range rows {
		if r.Name == name && r.Label == label {
			return r, true
		}
	}
	return metrics.Row{}, false
}

// Lô có comment không có giờ: vẫn ghi việc và đếm, nhưng không đặt last_comment_at (giờ 0 không thành mốc hỏi bù);
// bộ quản lý đặt gauge live_readers khi mở luồng (Task 182, mutation testing M4).
func TestMutationM4_ManagerZeroTimeCommentAndReadersGauge(t *testing.T) {
	t.Parallel()
	e, s, f, _ := newSessionEnv(t, 1)
	ev := f.comments[0]
	ev.OccurredAt = time.Time{}
	runManager(t, live.NewManager(e.d, &onceSource{evs: []webhooks.InboundEvent{ev}}, scaled("w-m4")))
	eventually(t, 10*time.Second, "lô comment được ghi", func() bool {
		_, _, received, _ := e.session(s.ID)
		return received == 1
	})
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions WHERE id = $1 AND last_comment_at IS NULL`, s.ID); n != 1 {
		t.Errorf("comment không có giờ đã đặt last_comment_at")
	}
	if r, ok := metricRow(e.d.Metrics.Drain(), "live_readers", "w-m4"); !ok || r.Max < 1 {
		t.Errorf("gauge live_readers = %+v %v, muốn ≥ 1", r, ok)
	}
}

// Options 0 dùng hằng của gói (không phải 0): bộ quản lý dựng với chỉ Owner vẫn nhận phiên và đọc đủ comment (Task 182,
// mutation testing M4).
func TestMutationM4_ManagerZeroOptionsUseDefaults(t *testing.T) {
	t.Parallel()
	e, s, f, _ := newSessionEnv(t, 3)
	runManager(t, live.NewManager(e.d, f, live.Options{Owner: "w-def"}))
	eventually(t, 10*time.Second, "3 việc comment", func() bool { return e.distinctJobs() == 3 })
	if _, owner, _, _ := e.session(s.ID); owner != "w-def" {
		t.Errorf("chủ khóa = %q, muốn w-def", owner)
	}
}

// Luồng SSE nhận comment rồi im (hỏi định kỳ không thấy gì mới): vẫn một luồng (không mở lại sau mỗi lô), active_source
// giữ sse, không cảnh báo, không đếm lỗi nguồn nào (kể cả ghi active_source) (Task 182, mutation testing M4).
func TestMutationM4_QuietStreamStaysOnSSE(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	o := fastSource()
	o.SilenceAfter = 300 * time.Millisecond
	e.start(o)
	eventually(t, 10*time.Second, "luồng SSE mở", func() bool { return e.fg.StreamCount(fbLiveID) == 1 })
	for i := range 3 {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
	}
	e.waitJobs(3)
	polls := func() int {
		n := 0
		for _, c := range e.fg.Calls(fbLiveID + "/comments") {
			if !c.Query.Has("after") && !c.Query.Has("summary") {
				n++
			}
		}
		return n
	}
	eventually(t, 10*time.Second, "4 lượt hỏi định kỳ lúc luồng im", func() bool { return polls() >= 4 })
	if n := e.streamAsked(); n != 1 {
		t.Errorf("%d lần mở luồng, muốn 1", n)
	}
	if src, n := e.activeSource(), e.alerts("live-source:"); src != live.SourceSSE || n != 0 {
		t.Errorf("active_source = %q, %d cảnh báo live-source; muốn sse, 0", src, n)
	}
	for _, r := range e.d.Metrics.Drain() {
		if r.Name == "live_source_failed" {
			t.Errorf("đếm lỗi nguồn %q = %d, muốn không có", r.Label, r.Count)
		}
	}
}

// Không cấu hình khoảng nối lại: dùng 1 s, 2 s, 5 s của gói; luồng mở lỗi một lần → đếm sse_open, nối lại sau khoảng
// đầu và mở được (Task 182, mutation testing M4).
func TestMutationM4_StreamOpenFailsUsesDefaultDelays(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	e.fg.FailNext(fbLiveID+"/live_comments", fakegraph.GraphErrorSpec{HTTPStatus: http.StatusNotFound, Code: 803, Type: "OAuthException", Message: "(#803) not found"})
	o := fastSource()
	o.ReconnectDelays = nil
	e.start(o)
	eventually(t, 15*time.Second, "luồng mở lại sau lần lỗi", func() bool { return e.fg.StreamCount(fbLiveID) == 1 })
	if n := e.streamAsked(); n != 2 {
		t.Errorf("%d lần mở luồng, muốn 2", n)
	}
	if r, ok := metricRow(e.d.Metrics.Drain(), "live_source_failed", "sse_open"); !ok || r.Count != 1 {
		t.Errorf("đếm lỗi sse_open = %+v %v, muốn 1", r, ok)
	}
}

// source_mode = sse ép luồng: luồng mở lỗi nhiều lần liền vẫn chỉ nối lại luồng (không chuyển sang hỏi định kỳ, không
// cảnh báo live-source) (Task 182, mutation testing M4).
func TestMutationM4_ForcedSSENeverFallsBackToPoll(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	e.mustTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.live_sessions SET source_mode = 'sse' WHERE id = $1`, e.s.ID)
		return err //nolint:wrapcheck // test
	})
	for range 4 {
		e.fg.FailNext(fbLiveID+"/live_comments", fakegraph.GraphErrorSpec{HTTPStatus: http.StatusNotFound, Code: 803, Type: "OAuthException", Message: "(#803) not found"})
	}
	e.start(fastSource())
	// active_source ghi sau khi luồng mở: chờ cả hai (máy CI chậm, lượt 28/11 thấy luồng mở mà cột chưa ghi).
	eventually(t, 15*time.Second, "luồng mở được sau 4 lần lỗi, active_source = sse", func() bool {
		return e.fg.StreamCount(fbLiveID) == 1 && e.activeSource() == live.SourceSSE
	})
	if n := e.streamAsked(); n != 5 {
		t.Errorf("%d lần mở luồng, muốn 5", n)
	}
	if n := e.alerts("live-source:"); n != 0 {
		t.Errorf("%d cảnh báo live-source ở chế độ sse ép, muốn 0", n)
	}
}
