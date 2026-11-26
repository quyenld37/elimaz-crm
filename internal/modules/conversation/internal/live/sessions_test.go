package live_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// statusEvent gửi sự kiện live.status (kênh giả) của Page thử qua đường webhook.
func (e *liveEnv) statusEvent(videoID, status string, at time.Time) {
	e.t.Helper()
	body, _ := json.Marshal(channel.LiveStatusPayload{PageID: pageExternal, LiveVideoID: videoID, VideoID: "v-" + videoID, Status: status, At: at})
	e.mustTx(func(tx pgx.Tx) error {
		return inbound.LiveStatus(e.t.Context(), tx, "fakechat", webhooks.Inbound{
			Type: channel.EventLiveStatus, Data: body, TenantID: e.tenant, ReceivedAt: at, Via: webhooks.ViaWebhook,
		})
	})
}

func (e *liveEnv) sessionByVideo(videoID string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.mustTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM conversation.live_sessions WHERE live_video_id = $1`, videoID).Scan(&id)
	})
	return id
}

// operator là nhân viên có live.operate và conversation.read trên Page page.
func operator(tenant, page uuid.UUID, perms ...string) identity.AuthUser {
	u := identity.AuthUser{UserID: uuid.New(), TenantID: tenant, Kind: identity.KindStaff}
	for _, p := range perms {
		u.Grants = append(u.Grants, identity.Grant{
			Permission: identity.Permission(p), Scope: identity.Scope{Type: identity.ScopePage, ID: page.String()},
		})
	}
	return u
}

// LV01: sự kiện live.status = LIVE → 1 phiên live, detected_by webhook; trong một chu kỳ quét nguồn nhận Run.
// Hỏi định kỳ cùng thấy video đó → vẫn 1 phiên. LIVE_STOPPED → ended, luồng dừng trong một chu kỳ gia hạn; LIVE đến
// sau ended → vẫn ended. Page lạ, trạng thái chưa phát → bỏ qua.
func TestLV01_WebhookDetectsAndStreamOpens(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	start := time.Date(2026, 11, 26, 12, 15, 0, 0, time.UTC)
	e.statusEvent("lv1", "LIVE", start)
	id := e.sessionByVideo("lv1")
	if st, _, _, by := e.session(id); st != live.StatusLive || by != live.DetectedWebhook {
		t.Fatalf("phiên sau webhook: %s %s", st, by)
	}
	// Hỏi định kỳ thấy cùng video → vẫn một phiên, giữ đường nhận biết đầu tiên.
	e.observe("lv1", live.StatusLive, live.DetectedPoll, start.Add(time.Minute))
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions`); n != 1 {
		t.Fatalf("%d phiên, muốn 1", n)
	}
	src := newFakeSource(0, start)
	o := scaled("w1")
	m := live.NewManager(e.d, src, o)
	stop := runManager(t, m)
	eventually(t, o.ScanEvery+time.Second, "nguồn nhận Run", func() bool { return len(src.runsOf(id)) == 1 })
	if r := src.runsOf(id)[0]; !r.since.Equal(start) {
		t.Errorf("since = %v, muốn started_at %v", r.since, start)
	}
	if _, owner, _, _ := e.session(id); owner != "w1" {
		t.Errorf("chủ khóa = %q", owner)
	}
	// Kết thúc → luồng dừng trong một chu kỳ gia hạn (cộng độ trễ database).
	e.statusEvent("lv1", "LIVE_STOPPED", start.Add(time.Hour))
	eventually(t, o.RenewEvery+time.Second, "luồng dừng khi phiên kết thúc", func() bool { return src.runningOf(id) == 0 })
	e.statusEvent("lv1", "LIVE", start.Add(2*time.Hour))
	if st, _, _, _ := e.session(id); st != live.StatusEnded {
		t.Errorf("LIVE sau ended: %s, muốn ended", st)
	}
	stop()
	if _, owner, _, _ := e.session(id); owner != "" {
		t.Errorf("khóa chưa nhả sau khi luồng dừng: %q", owner)
	}
	// Page không có trong shop, trạng thái lên lịch: không tạo phiên.
	body, _ := json.Marshal(channel.LiveStatusPayload{PageID: "khong-co", LiveVideoID: "lv9", Status: "LIVE"})
	e.mustTx(func(tx pgx.Tx) error {
		return inbound.LiveStatus(t.Context(), tx, "fakechat", webhooks.Inbound{Type: channel.EventLiveStatus, Data: body})
	})
	e.statusEvent("lv8", "SCHEDULED_UNPUBLISHED", start)
	if n := e.count(`SELECT count(*) FROM conversation.live_sessions`); n != 1 {
		t.Errorf("%d phiên sau sự kiện bỏ qua, muốn 1", n)
	}
	if err := e.tx(func(tx pgx.Tx) error {
		return inbound.LiveStatus(t.Context(), tx, "fakechat", webhooks.Inbound{Type: channel.EventLiveStatus, Data: []byte("{")})
	}); err == nil {
		t.Error("dữ liệu hỏng phải lỗi vĩnh viễn")
	}
}

// LV01 bấm tay: đường dẫn video → phiên của mã video, detected_by manual; bấm lại → cùng phiên; không có quyền hay
// Page lạ → ErrNotFound; chuỗi không có mã → ErrVideoInvalid. Kết thúc tay: cần live.operate; hai lần → vẫn ended.
func TestStartAndEndManual(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	now := time.Date(2026, 11, 26, 5, 0, 0, 0, time.UTC)
	op := operator(e.tenant, e.page, "conversation.read", "live.operate")
	viewer := operator(e.tenant, e.page, "conversation.read")
	var s, again live.Session
	e.mustTx(func(tx pgx.Tx) error {
		var err error
		if s, err = live.StartManual(t.Context(), tx, op, e.page, "https://www.facebook.com/elimaz/videos/123456789/", now); err != nil {
			return err //nolint:wrapcheck // test
		}
		again, err = live.StartManual(t.Context(), tx, op, e.page, " 123456789 ", now.Add(time.Minute))
		return err //nolint:wrapcheck // test
	})
	if s.LiveVideoID != "123456789" || s.VideoID != "123456789" || s.Status != live.StatusLive ||
		s.DetectedBy != live.DetectedManual || !s.StartedAt.Equal(now) || again.ID != s.ID {
		t.Fatalf("phiên tay: %+v / %+v", s, again)
	}
	for _, c := range []struct {
		u    identity.AuthUser
		page uuid.UUID
		ref  string
		want error
	}{
		{viewer, e.page, "123", live.ErrNotFound},
		{op, uuid.New(), "123", live.ErrNotFound},
		{op, e.page, "https://www.facebook.com/elimaz/posts/abc", live.ErrVideoInvalid},
	} {
		err := e.tx(func(tx pgx.Tx) error {
			_, err := live.StartManual(t.Context(), tx, c.u, c.page, c.ref, now)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, muốn %v", c.ref, err, c.want)
		}
	}
	if err := e.tx(func(tx pgx.Tx) error {
		_, err := live.EndManual(t.Context(), tx, viewer, s.ID, now)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, live.ErrNotFound) {
		t.Errorf("kết thúc không có quyền: %v", err)
	}
	var end1, end2 live.Session
	e.mustTx(func(tx pgx.Tx) error {
		var err error
		if end1, err = live.EndManual(t.Context(), tx, op, s.ID, now.Add(time.Hour)); err != nil {
			return err //nolint:wrapcheck // test
		}
		end2, err = live.EndManual(t.Context(), tx, op, s.ID, now.Add(2*time.Hour))
		return err //nolint:wrapcheck // test
	})
	if end1.Status != live.StatusEnded || end1.EndedAt == nil || !end1.EndedAt.Equal(now.Add(time.Hour)) ||
		end1.Version != 2 || end2.Version != 2 || !end2.EndedAt.Equal(*end1.EndedAt) {
		t.Errorf("kết thúc tay: %+v / %+v", end1, end2)
	}
	// Danh sách: người không có Page nào không thấy gì; lọc trạng thái.
	var all, liveOnly, none []live.Session
	e.mustTx(func(tx pgx.Tx) error {
		var err error
		if all, err = live.List(t.Context(), tx, viewer, "", nil); err != nil {
			return err //nolint:wrapcheck // test
		}
		if liveOnly, err = live.List(t.Context(), tx, viewer, live.StatusLive, nil); err != nil {
			return err //nolint:wrapcheck // test
		}
		none, err = live.List(t.Context(), tx, operator(e.tenant, uuid.New(), "conversation.read"), "", nil)
		return err //nolint:wrapcheck // test
	})
	if len(all) != 1 || len(liveOnly) != 0 || len(none) != 0 {
		t.Errorf("danh sách: %d, %d, %d", len(all), len(liveOnly), len(none))
	}
}

func TestParseVideoRef(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"123456789": "123456789",
		"https://www.facebook.com/elimaz/videos/123456789/":          "123456789",
		"https://www.facebook.com/elimaz/videos/123456789?ref=share": "123456789",
		"https://www.facebook.com/watch/?v=987654321":                "987654321",
		"https://m.facebook.com/watch/?ref=x&v=555":                  "555",
		"https://www.facebook.com/elimaz/videos/live-ca-trua/42/":    "",
		"abc": "", "": "", "https://www.facebook.com/elimaz/posts/1": "",
	} {
		got, err := live.ParseVideoRef(in)
		if got != want || (want == "") != errors.Is(err, live.ErrVideoInvalid) {
			t.Errorf("%q → %q, %v; muốn %q", in, got, err, want)
		}
	}
}

// IsLiveComment khớp mã bài, mã live hay mã video của phiên trên đúng Page; OnComment phát live.comments theo Page.
func TestIsLiveCommentAndSignal(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	at := time.Date(2026, 11, 26, 5, 0, 0, 0, time.UTC)
	var s live.Session
	e.mustTx(func(tx pgx.Tx) error {
		var err error
		s, _, err = live.UpsertSession(t.Context(), tx, live.Observation{
			AccountID: e.page, LiveVideoID: "lv1", VideoID: "v1", PostExternalID: "page_v1", State: live.StatusLive,
			DetectedBy: live.DetectedPoll, At: at,
		})
		return err //nolint:wrapcheck // test
	})
	bus := &realtimetest.RecordingBus{}
	e.mustTx(func(tx pgx.Tx) error {
		for post, want := range map[string]bool{"lv1": true, "v1": true, "page_v1": true, "post-khac": false} {
			got, err := live.IsLiveComment(t.Context(), tx, e.page, post)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if (got != nil) != want || (got != nil && *got != s.ID) {
				t.Errorf("%s: %v", post, got)
			}
		}
		if got, err := live.IsLiveComment(t.Context(), tx, uuid.New(), "lv1"); err != nil || got != nil {
			t.Errorf("Page khác: %v %v", got, err)
		}
		return live.OnComment(t.Context(), tx, bus, e.tenant, e.page, s.ID)
	})
	sig := bus.Signals()
	if len(sig) != 1 || sig[0].Topic != "live.comments" || sig[0].Requires != "conversation.read" ||
		*sig[0].Scope != (identity.Scope{Type: identity.ScopePage, ID: e.page.String()}) ||
		sig[0].Entity.ID != s.ID.String() || sig[0].TenantID != e.tenant {
		t.Errorf("báo hiệu: %+v", sig)
	}
}

// Quy luật (rapid): chuỗi ngẫu nhiên lần thấy live/ended (cả ba đường) của vài video → mỗi video đúng một phiên khi đã
// từng thấy live; phiên ended khi và chỉ khi có lần ended sau lần live đầu tiên; không bao giờ ended → live.
func TestUpsertSession_StatusOnlyMovesForward(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	at := time.Date(2026, 11, 26, 5, 0, 0, 0, time.UTC)
	round := 0
	rapid.Check(t, func(rt *rapid.T) {
		round++
		prefix := "r" + uuid.NewString()[:8]
		videos := []string{prefix + "a", prefix + "b"}
		seenLive := map[string]bool{}
		ended := map[string]bool{}
		steps := rapid.IntRange(1, 12).Draw(rt, "steps")
		for range steps {
			v := videos[rapid.IntRange(0, 1).Draw(rt, "video")]
			state := rapid.SampledFrom([]string{live.StatusLive, live.StatusEnded}).Draw(rt, "state")
			by := rapid.SampledFrom([]string{live.DetectedWebhook, live.DetectedPoll, live.DetectedManual}).Draw(rt, "by")
			e.observe(v, state, by, at)
			if state == live.StatusLive {
				seenLive[v] = true
			} else if seenLive[v] {
				ended[v] = true
			}
		}
		for _, v := range videos {
			var n int
			var status string
			e.mustTx(func(tx pgx.Tx) error {
				return tx.QueryRow(t.Context(), `SELECT count(*), coalesce(max(status), '') FROM conversation.live_sessions
					WHERE live_video_id = $1`, v).Scan(&n, &status)
			})
			switch {
			case !seenLive[v] && n != 0:
				rt.Fatalf("%s: chưa từng live mà có %d phiên", v, n)
			case seenLive[v] && n != 1:
				rt.Fatalf("%s: %d phiên, muốn 1", v, n)
			case seenLive[v] && (status == live.StatusEnded) != ended[v]:
				rt.Fatalf("%s: trạng thái %s, đã kết thúc = %v", v, status, ended[v])
			}
		}
	})
	if round == 0 {
		t.Fatal("rapid không chạy lượt nào")
	}
}
