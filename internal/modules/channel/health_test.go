package channel_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// vn là giờ Việt Nam (UTC+7) cho đồng hồ của test.
func vn() *time.Location { return time.FixedZone("ICT", 7*3600) }

// runSchedule chạy việc của lịch name (handler trong channel.JobHandlers) một lần cho tenant của e, lúc đồng hồ hiện tại.
func (e *connEnv) runSchedule(name string) json.RawMessage {
	e.t.Helper()
	var h queue.Handler
	for _, x := range e.jobs {
		if x.Kind().Name == "task."+name {
			h = x
		}
	}
	if h == nil {
		e.t.Fatalf("không có việc của lịch %s", name)
	}
	var out json.RawMessage
	if err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		out, err = h.Handle(e.t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("lịch %s: %v", name, err)
	}
	return out
}

// payloads trả payload các việc kind của tenant e theo thứ tự ghi.
func (e *connEnv) payloads(kind string) []string {
	e.t.Helper()
	return e.jobColumn("payload::text", kind)
}

// inboundJobs đếm việc inbound.facebook của tenant e (via rỗng: mọi đường tới).
func (e *connEnv) inboundJobs(via string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook' AND ($1 = '' OR payload->>'via' = $1)`, via)
}

// connectedPage nối Page 1001 "Shop Một" token EAAB-page-1 và chạy lệnh đăng ký.
func (e *connEnv) connectedPage() channel.ChannelAccount {
	e.t.Helper()
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	e.staff.DrainJobs(e.t, e.worker)
	return a
}

// alert chạy lệnh cảnh báo kind (channel.HealthAlertCommands) với payload p, trả cảnh báo đã gửi.
func alert(t *testing.T, kind, p string) *recNotifier {
	t.Helper()
	n := &recNotifier{}
	for _, h := range channel.HealthAlertCommands(n, nil) {
		if h.Kind().Name != kind {
			continue
		}
		out, err := h.Execute(t.Context(), queue.Job{Payload: json.RawMessage(p)})
		if err != nil || out.Status != commands.StatusDone {
			t.Fatalf("lệnh cảnh báo %s: %+v, %v", kind, out, err)
		}
		return n
	}
	t.Fatalf("không có lệnh cảnh báo %s", kind)
	return nil
}

func (e *connEnv) setLast(id uuid.UUID, webhook, feed *time.Time) {
	e.t.Helper()
	if err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error {
		tag, err := tx.Exec(e.t.Context(),
			`UPDATE channel.channel_accounts SET last_webhook_at = $2, last_feed_at = $3 WHERE id = $1`, id, webhook, feed)
		if err == nil && tag.RowsAffected() != 1 {
			e.t.Errorf("đặt mốc: %d dòng", tag.RowsAffected())
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

// HT15: token bị thu hồi → lịch channel.health ghi lệnh kiểm (khóa <id>:<giờ>) → degraded, đúng một cảnh báo gấp
// channel-auth:<id> có tên Page, không token; lần kiểm sau vẫn hỏng không cảnh báo thêm; trong lúc degraded tuyến
// webhook còn và sự kiện vẫn tạo việc vào; nối lại bằng token mới → active; 31 phút sau kiểm lại không cảnh báo nhắc lại.
func TestHealth_TokenRevoked(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.connectedPage()
	t0 := time.Date(2026, 11, 26, 14, 10, 0, 0, vn())
	e.clock.Set(t0)
	e.adapter.RevokeToken("EAAB-page-1")
	e.runSchedule(channel.HealthSchedule)
	e.runSchedule(channel.HealthSchedule) // cùng giờ: khóa chống trùng, không thêm lệnh
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`, channel.HealthCheckKind,
		a.ID.String()+":"+t0.UTC().Truncate(time.Hour).Format(time.RFC3339)); n != 1 {
		t.Fatalf("lệnh kiểm sức khỏe giờ này = %d, muốn 1", n)
	}
	e.staff.DrainJobs(t, e.worker)
	got := e.account(a.ID)
	if got.Status != channel.StatusDegraded || got.StatusReason != "token_invalid" || got.TokenCheckedAt == nil ||
		!got.TokenCheckedAt.Equal(t0) {
		t.Fatalf("sau kiểm token hỏng = %+v", got)
	}
	ps := e.payloads(channel.AuthAlertKind)
	if len(ps) != 1 || strings.Contains(ps[0], "EAAB") {
		t.Fatalf("lệnh cảnh báo mất quyền = %v", ps)
	}
	n := alert(t, channel.AuthAlertKind, ps[0])
	if n.key != "channel-auth:"+a.ID.String() || n.sev != "urgent" || !strings.Contains(n.text, "Shop Một") ||
		!strings.Contains(n.text, "kết nối lại") || strings.Contains(n.text, "EAAB") {
		t.Fatalf("cảnh báo = %+v", n)
	}
	if !slices.ContainsFunc(e.bus.Signals(), func(s realtime.Signal) bool { return s.Topic == channel.TopicStatus }) {
		t.Fatal("thiếu báo hiệu channel.status khi sang degraded")
	}
	// Một giờ sau vẫn hỏng: kiểm tiếp, không thêm cảnh báo.
	e.clock.Set(t0.Add(time.Hour))
	e.runSchedule(channel.HealthSchedule)
	e.staff.DrainJobs(t, e.worker)
	if ps := e.payloads(channel.AuthAlertKind); len(ps) != 1 {
		t.Fatalf("cảnh báo sau lần kiểm thứ hai = %d, muốn 1", len(ps))
	}
	// Đang degraded: tuyến webhook còn của shop, sự kiện vào vẫn tạo việc inbound.facebook (tin không mất).
	if e.routeTenant("1001") != e.tenant {
		t.Fatal("tuyến fb_page:1001 mất khi degraded")
	}
	for i := range 3 {
		e.enqueueInbound(webhooks.ViaWebhook, msgEvent("1001", "m_deg_"+string(rune('a'+i))))
	}
	if n := e.inboundJobs(""); n != 3 {
		t.Fatalf("việc vào khi degraded = %d, muốn 3", n)
	}
	// Nối lại bằng token mới → active; 31 phút sau kiểm lại: không cảnh báo nhắc lại.
	if r := e.mustConnect("1001", "Shop Một", "EAAB-page-1b", channel.TokenPageUser); r.Status != channel.StatusActive {
		t.Fatalf("nối lại = %+v", r)
	}
	e.staff.DrainJobs(t, e.worker)
	e.clock.Set(t0.Add(time.Hour + 31*time.Minute))
	e.runSchedule(channel.HealthSchedule)
	e.staff.DrainJobs(t, e.worker)
	if got := e.account(a.ID); got.Status != channel.StatusActive || got.StatusReason != "" {
		t.Fatalf("sau nối lại và kiểm = %+v", got)
	}
	if ps := e.payloads(channel.AuthAlertKind); len(ps) != 1 {
		t.Fatalf("cảnh báo sau nối lại = %d, muốn 1", len(ps))
	}
}

// Lịch sức khỏe: Page mất đăng ký webhook (khe Task 149: lệnh hủy đã chạy đúng lúc nối lại) → tự ghi subscribe_page
// và cảnh báo thường channel-fields:<id>; worker đăng ký lại đủ 11 trường. Page degraded vì lỗi token mà token đã tốt
// lại → active, báo hiệu; Page degraded vì Meta áp chế tài → giữ degraded (chỉ unblock của Meta gỡ); Page đã ngắt
// không được kiểm.
func TestHealth_FieldsAndRecovery(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	b := e.mustConnect("1002", "Shop Hai", "EAAB-page-2", channel.TokenPageUser)
	c := e.mustConnect("1003", "Shop Ba", "EAAB-page-3", channel.TokenPageUser)
	off := e.mustConnect("1004", "Shop Bốn", "EAAB-page-4", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	e.disconnect(off.ID)
	e.staff.DrainJobs(t, e.worker)
	if err := e.adapter.Unsubscribe(t.Context(), a, channel.Credentials{AccessToken: "EAAB-page-1"}); err != nil {
		t.Fatal(err)
	}
	w := channel.Writer{Realtime: e.bus}
	if err := e.in(t.Context(), func(tx pgx.Tx) error {
		if err := w.ReportAuthFailure(t.Context(), tx, b.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		return w.ReportPolicyEnforcement(t.Context(), tx, c.ID, "block")
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.account(b.ID); got.Status != channel.StatusDegraded || got.StatusReason != "auth_failed" {
		t.Fatalf("ReportAuthFailure = %+v", got)
	}
	subs := len(e.payloads(channel.SubscribePageKind))
	before := len(e.bus.Signals())
	e.clock.Set(time.Date(2026, 11, 26, 10, 0, 0, 0, vn()))
	e.runSchedule(channel.HealthSchedule)
	e.staff.DrainJobs(t, e.worker)
	if hs := e.adapter.Calls(channeltest.OpHealth); slices.ContainsFunc(hs, func(c channeltest.Call) bool { return c.PageID == "1004" }) {
		t.Fatal("Page đã ngắt vẫn được kiểm")
	}
	if got := len(e.payloads(channel.SubscribePageKind)); got != subs+1 {
		t.Fatalf("lệnh đăng ký lại = %d, muốn %d", got, subs+1)
	}
	if got := e.adapter.Subscribed("1001"); !slices.Equal(got, channel.WebhookFields()) {
		t.Fatalf("sau đăng ký lại: %v", got)
	}
	ps := e.payloads(channel.FieldsAlertKind)
	if len(ps) != 1 {
		t.Fatalf("cảnh báo thiếu trường = %v", ps)
	}
	if n := alert(t, channel.FieldsAlertKind, ps[0]); n.key != "channel-fields:"+a.ID.String() || n.sev != "normal" ||
		!strings.Contains(n.text, "Shop Một") {
		t.Fatalf("cảnh báo thiếu trường = %+v", n)
	}
	if got := e.account(b.ID); got.Status != channel.StatusActive || got.StatusReason != "" {
		t.Fatalf("token tốt lại = %+v", got)
	}
	if got := e.account(c.ID); got.Status != channel.StatusDegraded || got.StatusReason != "meta_policy:block" {
		t.Fatalf("chế tài của Meta bị lịch sức khỏe gỡ: %+v", got)
	}
	if len(e.bus.Signals()) <= before {
		t.Fatal("không báo hiệu khi trở lại active")
	}
	reportAuthTwice(t, e, w, b.ID)
}

// Vi phạm chính sách: block → degraded meta_policy:block, đúng một cảnh báo gấp meta-policy:<id> có tên Page và hành
// động; lặp lại không thêm; unblock → active; unblock khi Page không bị chế tài → không đổi; Page đã ngắt → không đổi.
func TestHealth_PolicyEnforcement(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	w := channel.Writer{Realtime: e.bus}
	report := func(id uuid.UUID, action string) {
		t.Helper()
		if err := e.in(t.Context(), func(tx pgx.Tx) error {
			return w.ReportPolicyEnforcement(t.Context(), tx, id, action)
		}); err != nil {
			t.Fatal(err)
		}
	}
	report(a.ID, "block")
	report(a.ID, "block")
	if got := e.account(a.ID); got.Status != channel.StatusDegraded || got.StatusReason != "meta_policy:block" {
		t.Fatalf("sau block = %+v", got)
	}
	ps := e.payloads(channel.PolicyAlertKind)
	if len(ps) != 1 {
		t.Fatalf("cảnh báo chính sách = %v", ps)
	}
	if n := alert(t, channel.PolicyAlertKind, ps[0]); n.key != "meta-policy:"+a.ID.String() || n.sev != "urgent" ||
		!strings.Contains(n.text, "Shop Một") || !strings.Contains(n.text, "block") {
		t.Fatalf("cảnh báo = %+v", n)
	}
	report(a.ID, "unblock")
	if got := e.account(a.ID); got.Status != channel.StatusActive {
		t.Fatalf("sau unblock = %+v", got)
	}
	report(a.ID, "unblock")
	report(a.ID, "Bad Action; DROP")
	if got := e.account(a.ID); got.StatusReason != "meta_policy:other" {
		t.Fatalf("hành động lạ phải ghi other, có %+v", got)
	}
	if got := len(e.payloads(channel.PolicyAlertKind)); got != 2 {
		t.Fatalf("cảnh báo = %d, muốn 2", got)
	}
	e.disconnect(a.ID)
	report(a.ID, "block")
	if got := e.account(a.ID); got.Status != channel.StatusDisconnected {
		t.Fatalf("Page đã ngắt = %+v", got)
	}
}

// HT16: 20:00 giờ Việt Nam, webhook cuối cách 40 phút → một cảnh báo gấp webhook-silence:<id>, một lệnh đăng ký lại,
// một lệnh hỏi bù từ last_webhook_at − 5 phút; chạy lại trong cùng đợt im lặng không thêm gì. 03:00 → không gì;
// cách 5 giờ → không gì; cách 10 phút → không gì; Page degraded, chưa từng có webhook → không gì.
func TestSilence(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 26, 20, 0, 0, 0, vn())
	cases := []struct {
		name  string
		now   time.Time
		ago   time.Duration
		alert bool
	}{
		{"20:00, 40 phút", at, 40 * time.Minute, true},
		{"03:00, 40 phút", time.Date(2026, 11, 26, 3, 0, 0, 0, vn()), 40 * time.Minute, false},
		{"20:00, 5 giờ", at, 5 * time.Hour, false},
		{"20:00, 10 phút", at, 10 * time.Minute, false},
		{"08:00, 30 phút", time.Date(2026, 11, 26, 8, 0, 0, 0, vn()), 30 * time.Minute, true},
		{"23:00, 40 phút", time.Date(2026, 11, 26, 23, 0, 0, 0, vn()), 40 * time.Minute, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			silenceCase(t, c.now, c.ago, c.alert)
		})
	}
	t.Run("degraded, chưa có webhook", func(t *testing.T) {
		t.Parallel()
		e := newConnEnv(t)
		a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
		e.mustConnect("1002", "Shop Hai", "EAAB-page-2", channel.TokenPageUser)
		last := at.Add(-40 * time.Minute)
		e.setLast(a.ID, &last, nil)
		if err := e.in(t.Context(), func(tx pgx.Tx) error {
			return channel.Writer{}.ReportAuthFailure(t.Context(), tx, a.ID)
		}); err != nil {
			t.Fatal(err)
		}
		e.clock.Set(at) // b chưa từng có webhook (last_webhook_at NULL)
		e.runSchedule(channel.SilenceSchedule)
		if n := len(e.payloads(channel.SilenceAlertKind)) + len(e.payloads(channel.BackfillKind)); n != 0 {
			t.Fatalf("Page degraded hay chưa từng có webhook mà có %d việc", n)
		}
	})
}

// Hỏi bù: kênh trả 3 sự kiện, 1 đã có việc từ webhook → EnqueueInbound 3 lần, tạo mới 2 (kết quả lệnh read 3, created
// 2), tổng 3 việc inbound.facebook; lịch channel.backfill ghi lệnh cho Page active từ now − 30 phút, chỉ một lần mỗi kỳ.
func TestBackfill_Dedupe(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.mustConnect("1001", "Shop Một", "EAAB-page-1", channel.TokenPageUser)
	off := e.mustConnect("1002", "Shop Hai", "EAAB-page-2", channel.TokenPageUser)
	e.staff.DrainJobs(t, e.worker)
	e.disconnect(off.ID)
	e.staff.DrainJobs(t, e.worker)
	evs := []webhooks.InboundEvent{msgEvent("1001", "m_1"), msgEvent("1001", "m_2"), msgEvent("1001", "m_3")}
	e.adapter.SetBackfill("1001", evs...)
	e.enqueueInbound(webhooks.ViaWebhook, evs[1])
	now := time.Date(2026, 11, 26, 15, 4, 0, 0, vn())
	e.clock.Set(now)
	e.runSchedule(channel.BackfillSchedule)
	e.runSchedule(channel.BackfillSchedule)
	if ps := e.payloads(channel.BackfillKind); len(ps) != 1 || !strings.Contains(ps[0], a.ID.String()) {
		t.Fatalf("lệnh hỏi bù = %v, muốn 1 cho Page active", ps)
	}
	e.staff.DrainJobs(t, e.worker)
	bf := e.adapter.Calls(channeltest.OpBackfill)
	if len(bf) != 1 || !bf[0].Since.Equal(now.Add(-30*time.Minute)) || bf[0].Token != "EAAB-page-1" {
		t.Fatalf("lần hỏi bù = %+v", bf)
	}
	if n, b := e.inboundJobs(""), e.inboundJobs(webhooks.ViaBackfill); n != 3 || b != 2 {
		t.Fatalf("việc vào = %d (qua hỏi bù %d), muốn 3 (2)", n, b)
	}
	var res string
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT result::text FROM platform.jobs WHERE tenant_id = $1 AND kind = $2`,
		e.tenant, channel.BackfillKind).Scan(&res); err != nil {
		t.Fatal(err)
	}
	var r struct{ Read, Created int }
	if err := json.Unmarshal([]byte(res), &r); err != nil || r.Read != 3 || r.Created != 2 {
		t.Fatalf("kết quả hỏi bù = %s", res)
	}
	if st := e.jobStatuses(channel.BackfillKind); !slices.Equal(st, []string{"done"}) {
		t.Fatalf("lệnh = %v", st)
	}
}

// Lệnh đọc (hỏi bù, đọc comment): token hỏng → dead và ReportAuthFailure (degraded, một cảnh báo gấp); lỗi chưa gửi →
// tự thử lại rồi xong; in_doubt tra lại → not_sent (đọc lại vô hại); đọc comment ghi sự kiện via comment_poll.
func TestReadCommands_Errors(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	a := e.connectedPage()
	e.adapter.SetComments("1001", commentEvent("1001", "c_1"))
	e.adapter.FailNext(channeltest.OpCommentPoll, channeltest.ErrConnectNotSent)
	now := time.Date(2026, 11, 26, 12, 30, 0, 0, vn())
	if err := e.in(t.Context(), func(tx pgx.Tx) error {
		return channel.EnqueueCommentPoll(t.Context(), tx, a, now.Add(-2*time.Minute), now)
	}); err != nil {
		t.Fatal(err)
	}
	e.staff.DrainJobs(t, e.worker)
	if st := e.jobStatuses(channel.CommentPollKind); !slices.Equal(st, []string{"done"}) {
		t.Fatalf("đọc comment sau lỗi chưa gửi = %v", st)
	}
	if n := e.inboundJobs(webhooks.ViaCommentPoll); n != 1 {
		t.Fatalf("việc vào qua đọc comment = %d, muốn 1", n)
	}
	if cs := e.adapter.Calls(channeltest.OpCommentPoll); len(cs) != 2 || !cs[1].Since.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("lần đọc comment = %+v", cs)
	}
	job := queue.Job{TenantID: &e.tenant, Payload: json.RawMessage(`{"accountId":"` + a.ID.String() + `"}`)}
	for _, k := range []string{channel.BackfillKind, channel.CommentPollKind, channel.HealthCheckKind} {
		st, err := e.handler(k).(commands.Looker).Lookup(t.Context(), job)
		if err != nil || st != commands.StatusNotSent {
			t.Fatalf("Lookup %s = %s, %v; muốn not_sent", k, st, err)
		}
	}
	e.adapter.RevokeToken("EAAB-page-1")
	e.clock.Set(now)
	e.runSchedule(channel.BackfillSchedule)
	e.staff.DrainJobs(t, e.worker)
	if st := e.jobStatuses(channel.BackfillKind); !slices.Equal(st, []string{"dead"}) {
		t.Fatalf("hỏi bù token hỏng = %v", st)
	}
	if got := e.account(a.ID); got.Status != channel.StatusDegraded || got.StatusReason != "auth_failed" {
		t.Fatalf("sau hỏi bù token hỏng = %+v", got)
	}
	if n := len(e.payloads(channel.AuthAlertKind)); n != 1 {
		t.Fatalf("cảnh báo mất quyền = %d, muốn 1", n)
	}
}

// Hạn phiên bản Graph: còn 179 ngày → một cảnh báo graph-version (mức thường, có phiên bản và ngày hết hạn); 181 ngày
// → không; 1 ngày, 5 ngày sau lần cảnh báo → không gửi lại (chống lặp theo đợt 7 ngày tính từ lúc vào khoảng 180 ngày);
// 7 ngày sau → gửi lại; không có hạn → không.
func TestGraphVersion(t *testing.T) {
	t.Parallel()
	e := newConnEnv(t)
	expires := time.Date(2028, 7, 29, 0, 0, 0, 0, time.UTC)
	clock := &testClock{}
	h := graphVersionJob(t, e, expires, clock)
	run := func(at time.Time) {
		t.Helper()
		clock.Set(at)
		if err := h(); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		t.Helper()
		var n int
		if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`,
			channel.GraphVersionAlertKind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	run(expires.Add(-181 * 24 * time.Hour))
	if n := count(); n != 0 {
		t.Fatalf("181 ngày: %d cảnh báo", n)
	}
	first := expires.Add(-179 * 24 * time.Hour)
	run(first)
	if n := count(); n != 1 {
		t.Fatalf("179 ngày: %d cảnh báo, muốn 1", n)
	}
	run(first.Add(24 * time.Hour))
	run(first.Add(5 * 24 * time.Hour))
	if n := count(); n != 1 {
		t.Fatalf("trong 7 ngày: %d cảnh báo, muốn 1", n)
	}
	run(first.Add(7 * 24 * time.Hour))
	if n := count(); n != 2 {
		t.Fatalf("sau 7 ngày: %d cảnh báo, muốn 2", n)
	}
	var p string
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT payload::text FROM platform.jobs WHERE kind = $1 ORDER BY created_at LIMIT 1`,
		channel.GraphVersionAlertKind).Scan(&p); err != nil {
		t.Fatal(err)
	}
	if n := alert(t, channel.GraphVersionAlertKind, p); n.key != "graph-version" || n.sev != "normal" ||
		!strings.Contains(n.text, "v26.0") || !strings.Contains(n.text, "2028-07-29") || !strings.Contains(n.text, "179") {
		t.Fatalf("cảnh báo = %+v", n)
	}
	none := graphVersionJob(t, e, time.Time{}, clock)
	clock.Set(first)
	if err := none(); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("không có hạn mà vẫn cảnh báo: %d", n)
	}
}

// graphVersionJob trả hàm chạy việc của lịch channel.graph_version (không tenant) với hạn expires trong một
// transaction không tenant của crm_app (như worker chạy việc không tenant).
func graphVersionJob(t *testing.T, e *connEnv, expires time.Time, clock *testClock) func() error {
	t.Helper()
	d := channel.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Now: clock.Now}
	d.Config.Meta.GraphVersion = "v26.0"
	d.Config.Meta.GraphVersionExpires = expires
	for _, h := range channel.JobHandlers(d) {
		if h.Kind().Name == "task."+channel.GraphVersionSchedule {
			return func() error {
				return e.staff.App.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
					_, err := h.Handle(t.Context(), queue.Job{ID: uuid.New()}, tx)
					return err //nolint:wrapcheck // test
				})
			}
		}
	}
	t.Fatal("không có việc của lịch channel.graph_version")
	return nil
}

func msgEvent(page, mid string) webhooks.InboundEvent {
	p, _ := json.Marshal(channel.MessagePayload{PageID: page, UserID: "7001", MID: mid, Text: "x", SentAt: time.Now().UTC()})
	return webhooks.InboundEvent{
		Type: channel.EventMessageReceived, DedupeKey: "m:" + mid, Route: webhooks.Route{Kind: "fb_page", ExternalKey: page},
		OrderKey: "fb:" + page + ":7001", OccurredAt: time.Now().UTC(), Payload: p,
	}
}

func commentEvent(page, id string) webhooks.InboundEvent {
	p, _ := json.Marshal(channel.CommentPayload{PageID: page, PostID: page + "_1", CommentID: id, FromID: "7001", Verb: "add"})
	return webhooks.InboundEvent{
		Type: channel.EventCommentChanged, DedupeKey: "c:" + id + ":add", Route: webhooks.Route{Kind: "fb_page", ExternalKey: page},
		OrderKey: "fbc:" + page + ":" + page + "_1:7001", OccurredAt: time.Now().UTC(), Payload: p,
	}
}

// enqueueInbound ghi sự kiện ev cho tenant của e như đường vào (nguồn facebook) với via.
func (e *connEnv) enqueueInbound(via string, ev webhooks.InboundEvent) {
	e.t.Helper()
	if err := e.in(e.t.Context(), func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), e.tenant, "facebook", ev, time.Now(), via)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

// silenceCase: Page active có webhook cuối cách ago lúc now; chạy lịch im lặng hai lần; want1: đúng một bộ (cảnh báo,
// đăng ký lại, hỏi bù từ webhook cuối − 5 phút), không thì không gì.
func silenceCase(t *testing.T, now time.Time, ago time.Duration, want1 bool) {
	t.Helper()
	e := newConnEnv(t)
	a := e.connectedPage()
	subs := len(e.payloads(channel.SubscribePageKind))
	last := now.Add(-ago)
	e.setLast(a.ID, &last, nil)
	e.clock.Set(now)
	e.runSchedule(channel.SilenceSchedule)
	e.runSchedule(channel.SilenceSchedule)
	want := 0
	if want1 {
		want = 1
	}
	ps := e.payloads(channel.SilenceAlertKind)
	if len(ps) != want || len(e.payloads(channel.SubscribePageKind)) != subs+want || len(e.payloads(channel.BackfillKind)) != want {
		t.Fatalf("cảnh báo %d, đăng ký lại %d, hỏi bù %d; muốn %d mỗi loại", len(ps),
			len(e.payloads(channel.SubscribePageKind))-subs, len(e.payloads(channel.BackfillKind)), want)
	}
	if !want1 {
		return
	}
	if n := alert(t, channel.SilenceAlertKind, ps[0]); n.key != "webhook-silence:"+a.ID.String() || n.sev != "urgent" ||
		!strings.Contains(n.text, "Im lặng bất thường") || !strings.Contains(n.text, "Shop Một") {
		t.Fatalf("cảnh báo = %+v", n)
	}
	e.staff.DrainJobs(t, e.worker)
	bf := e.adapter.Calls(channeltest.OpBackfill)
	if len(bf) != 1 || !bf[0].Since.Equal(last.Add(-5*time.Minute)) {
		t.Fatalf("hỏi bù = %+v, muốn since %v", bf, last.Add(-5*time.Minute))
	}
}

// reportAuthTwice: ReportAuthFailure hai lần trên Page id đang active → đúng một cảnh báo mới (tổng 2 cùng lần đầu).
func reportAuthTwice(t *testing.T, e *connEnv, w channel.Writer, id uuid.UUID) {
	t.Helper()
	if err := e.in(t.Context(), func(tx pgx.Tx) error {
		if err := w.ReportAuthFailure(t.Context(), tx, id); err != nil {
			return err //nolint:wrapcheck // test
		}
		return w.ReportAuthFailure(t.Context(), tx, id)
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(e.payloads(channel.AuthAlertKind)); got != 2 {
		t.Fatalf("cảnh báo mất quyền = %d, muốn 2 (lần đầu và sau khi active lại)", got)
	}
}
