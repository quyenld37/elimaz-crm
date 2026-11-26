package channel_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

type chEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
}

func newChEnv(t *testing.T) *chEnv {
	t.Helper()
	e := &chEnv{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	return e
}

// in chạy fn trong tx tenant của e bằng crm_app.
func (e *chEnv) in(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(ctx, e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *chEnv) account(id uuid.UUID) channel.ChannelAccount {
	e.t.Helper()
	var a channel.ChannelAccount
	if err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		a, err = channel.Account(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return a
}

// Page mới chèn không chỉ định gì: chỉ xem (MO03), active, version 1; schema đúng luật; tenant khác không đọc được.
func TestAccounts_DefaultsAndIsolation(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "page-1", "Shop A")
	a := e.account(id)
	if a.SendMode != channel.SendModeReadOnly || a.Status != "active" || a.Version != 1 || a.HumanAgentEnabled ||
		a.Channel != "fakechat" || a.ExternalID != "page-1" || a.Name != "Shop A" || a.TenantID != e.tenant {
		t.Fatalf("tài khoản mới = %+v", a)
	}
	if v, err := tenancy.CheckSchemaRules(t.Context(), e.staff.Owner.Pool, []string{"platform.tenant_routes", "platform.jobs"}); err != nil || len(v) != 0 {
		t.Fatalf("CheckSchemaRules = %v, %v", v, err)
	}
	ctx := t.Context()
	if err := e.in(ctx, func(tx pgx.Tx) error {
		got, err := channel.AccountByExternal(ctx, tx, "fakechat", "page-1")
		if err != nil || got.ID != id {
			t.Errorf("AccountByExternal = %+v, %v", got, err)
		}
		if _, err := channel.AccountByExternal(ctx, tx, "facebook", "page-1"); !errors.Is(err, channel.ErrAccountNotFound) {
			t.Errorf("kênh khác: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b := e.staff.Tenant(t, "B")
	if err := tenancy.WithTenant(ctx, e.staff.App, b, func(tx pgx.Tx) error {
		if _, err := channel.Account(ctx, tx, id); !errors.Is(err, channel.ErrAccountNotFound) {
			t.Errorf("tenant B đọc tài khoản của A: %v", err)
		}
		all, err := channel.ListAccounts(ctx, tx, nil)
		if err != nil || len(all) != 0 {
			t.Errorf("tenant B ListAccounts = %v, %v", all, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ListAccounts: nil = tất cả; danh sách = đúng các mã đó; danh sách rỗng = không dòng nào.
func TestAccounts_List(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	a := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-a", "A Page")
	b := e.staff.ChannelAccount(t, e.tenant, "facebook", "p-b", "B Page")
	ctx := t.Context()
	ids := func(in []channel.ChannelAccount) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(in))
		for _, x := range in {
			out = append(out, x.ID)
		}
		return out
	}
	if err := e.in(ctx, func(tx pgx.Tx) error {
		for _, c := range []struct {
			in   []uuid.UUID
			want []uuid.UUID
		}{{nil, []uuid.UUID{a, b}}, {[]uuid.UUID{b}, []uuid.UUID{b}}, {[]uuid.UUID{}, []uuid.UUID{}}, {[]uuid.UUID{uuid.New()}, []uuid.UUID{}}} {
			got, err := channel.ListAccounts(ctx, tx, c.in)
			if err != nil || !slices.Equal(ids(got), c.want) {
				t.Errorf("ListAccounts(%v) = %v, %v; muốn %v", c.in, ids(got), err, c.want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// NoteInbound: mỗi phút ghi tối đa một lần; 100 goroutine cùng lúc không lỗi, mốc là một trong các giá trị gửi.
func TestAccounts_NoteInbound(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	ctx := t.Context()
	t0 := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	note := func(at time.Time, feed bool) {
		t.Helper()
		if err := e.in(ctx, func(tx pgx.Tx) error { return channel.NoteInbound(ctx, tx, id, at, feed) }); err != nil {
			t.Fatal(err)
		}
	}
	at := func(p *time.Time) time.Time {
		if p == nil {
			return time.Time{}
		}
		return p.UTC()
	}
	note(t0, false)
	if a := e.account(id); !at(a.LastWebhookAt).Equal(t0) || a.LastFeedAt != nil {
		t.Fatalf("lần 1: %v %v", a.LastWebhookAt, a.LastFeedAt)
	}
	note(t0.Add(30*time.Second), true)
	if a := e.account(id); !at(a.LastWebhookAt).Equal(t0) || !at(a.LastFeedAt).Equal(t0.Add(30*time.Second)) {
		t.Fatalf("cách 30 s: webhook %v (phải giữ), feed %v (lần đầu phải ghi)", a.LastWebhookAt, a.LastFeedAt)
	}
	note(t0.Add(61*time.Second), false)
	if a := e.account(id); !at(a.LastWebhookAt).Equal(t0.Add(61*time.Second)) || !at(a.LastFeedAt).Equal(t0.Add(30*time.Second)) {
		t.Fatalf("cách 61 s: %v %v", a.LastWebhookAt, a.LastFeedAt)
	}
	// Mốc cũ hơn không đè.
	note(t0, true)
	if a := e.account(id); !at(a.LastWebhookAt).Equal(t0.Add(61 * time.Second)) {
		t.Fatalf("mốc cũ đè mốc mới: %v", a.LastWebhookAt)
	}
	base := t0.Add(time.Hour)
	sent := map[time.Time]bool{}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := range 100 {
		ts := base.Add(time.Duration(i) * time.Millisecond)
		sent[ts] = true
		wg.Go(func() {
			errs <- tenancy.WithTenant(context.Background(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
				return channel.NoteInbound(context.Background(), tx, id, ts, i%2 == 0)
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if a := e.account(id); !sent[at(a.LastWebhookAt)] || !sent[at(a.LastFeedAt)] {
		t.Fatalf("sau 100 goroutine: %v %v", a.LastWebhookAt, a.LastFeedAt)
	}
}

// MarkStatus chỉ báo đổi khi trạng thái hay lý do khác; không tăng version. NoteBlockedUntil chỉ dời về sau.
func TestAccounts_StatusAndBlocked(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	ctx := t.Context()
	t1 := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	if err := e.in(ctx, func(tx pgx.Tx) error {
		for i, c := range []struct {
			status, reason string
			want           bool
		}{{"degraded", "token", true}, {"degraded", "token", false}, {"degraded", "webhook", true}, {"active", "", true}, {"active", "", false}} {
			got, err := channel.MarkStatus(ctx, tx, id, c.status, c.reason)
			if err != nil || got != c.want {
				t.Errorf("ca %d MarkStatus(%s,%s) = %v, %v", i, c.status, c.reason, got, err)
			}
		}
		if _, err := channel.MarkStatus(ctx, tx, id, "lạ", ""); !errors.Is(err, channel.ErrInvalidStatus) {
			t.Errorf("trạng thái lạ: %v", err)
		}
		if _, err := channel.MarkStatus(ctx, tx, uuid.New(), "active", ""); !errors.Is(err, channel.ErrAccountNotFound) {
			t.Errorf("không có tài khoản: %v", err)
		}
		if err := channel.NoteBlockedUntil(ctx, tx, id, t1); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := channel.NoteBlockedUntil(ctx, tx, id, t1.Add(-time.Minute)); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := channel.NoteBlockedUntil(ctx, tx, uuid.New(), t1); !errors.Is(err, channel.ErrAccountNotFound) {
			t.Errorf("chặn tài khoản không có: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a := e.account(id)
	if a.Status != "active" || a.StatusReason != "" || a.Version != 1 || a.BlockedUntil == nil || !a.BlockedUntil.Equal(t1) {
		t.Fatalf("sau khi đổi trạng thái: %+v", a)
	}
}

// SetSendMode: version khớp → đổi, version+1, một dòng audit changed_fields {send_mode}, một báo hiệu channel.status
// theo Page, chuyển sang enabled ghi đúng một lệnh cảnh báo; version cũ → ErrVersionConflict, không đổi gì; giá trị
// lạ → ErrInvalidSendMode; cùng giá trị → không đổi, không audit.
func TestWriter_SetSendMode(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	user := e.staff.User(t, e.tenant, "chu@elimaz.test", "active")
	actor := identity.AuthUser{TenantID: e.tenant, UserID: user}
	bus := &realtimetest.RecordingBus{}
	w := channel.Writer{Realtime: bus, RequestID: "req-1"}
	ctx := t.Context()
	set := func(mode string, ver int32) (channel.ChannelAccount, error) {
		var a channel.ChannelAccount
		err := e.in(ctx, func(tx pgx.Tx) (err error) {
			a, err = w.SetSendMode(ctx, tx, id, mode, ver, actor)
			return err //nolint:wrapcheck // test
		})
		return a, err
	}
	a, err := set(channel.SendModeEnabled, 1)
	if err != nil || a.SendMode != channel.SendModeEnabled || a.Version != 2 {
		t.Fatalf("SetSendMode = %+v, %v", a, err)
	}
	if _, err := set(channel.SendModeReadOnly, 1); !errors.Is(err, channel.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if _, err := set("lạ", 2); !errors.Is(err, channel.ErrInvalidSendMode) {
		t.Fatalf("chế độ lạ: %v", err)
	}
	if _, err := e.setNoop(w, id, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := e.inErr(ctx, func(tx pgx.Tx) error {
		_, err := w.SetSendMode(ctx, tx, uuid.New(), channel.SendModeEnabled, 1, actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, channel.ErrAccountNotFound) {
		t.Fatalf("không có tài khoản: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'channel.send_mode_changed'
		AND entity_id = $1 AND changed_fields = '{send_mode}' AND request_id = 'req-1'`, id.String()); n != 1 {
		t.Fatalf("audit = %d dòng, muốn 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SendModeAlertKind); n != 1 {
		t.Fatalf("lệnh cảnh báo = %d, muốn 1", n)
	}
	sig := bus.Signals()
	want := realtime.Signal{
		TenantID: e.tenant, Topic: channel.TopicStatus, Requires: "conversation.read",
		Scope: &identity.Scope{Type: identity.ScopePage, ID: id.String()}, Entity: realtime.Entity{Type: "channel_account", ID: id.String()},
	}
	if len(sig) != 1 || sig[0].Topic != want.Topic || sig[0].Requires != want.Requires || *sig[0].Scope != *want.Scope ||
		sig[0].Entity != want.Entity || sig[0].TenantID != e.tenant {
		t.Fatalf("báo hiệu = %+v", sig)
	}
	// Về read_only: không thêm lệnh cảnh báo.
	if a, err := set(channel.SendModeReadOnly, 2); err != nil || a.Version != 3 {
		t.Fatalf("về read_only: %+v %v", a, err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SendModeAlertKind); n != 1 {
		t.Fatalf("lệnh cảnh báo sau khi về read_only = %d, muốn 1", n)
	}
}

// setNoop đặt lại đúng giá trị hiện tại: version giữ nguyên, không audit, không báo hiệu.
func (e *chEnv) setNoop(w channel.Writer, id uuid.UUID, actor identity.AuthUser) (channel.ChannelAccount, error) {
	cur := e.account(id)
	var a channel.ChannelAccount
	err := e.in(e.t.Context(), func(tx pgx.Tx) (err error) {
		a, err = w.SetSendMode(e.t.Context(), tx, id, cur.SendMode, cur.Version, actor)
		return err //nolint:wrapcheck // test
	})
	if err == nil && a.Version != cur.Version {
		e.t.Errorf("đặt cùng giá trị đổi version %d → %d", cur.Version, a.Version)
	}
	return a, err
}

func (e *chEnv) inErr(ctx context.Context, fn func(tx pgx.Tx) error) (struct{}, error) {
	return struct{}{}, e.in(ctx, fn)
}

// SetHumanAgent: như SetSendMode, audit changed_fields {human_agent_enabled}, không có lệnh cảnh báo.
func TestWriter_SetHumanAgent(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	id := e.staff.ChannelAccount(t, e.tenant, "fakechat", "p-1", "P")
	actor := identity.AuthUser{TenantID: e.tenant, UserID: e.staff.User(t, e.tenant, "chu@elimaz.test", "active")}
	bus := &realtimetest.RecordingBus{}
	w := channel.Writer{Realtime: bus}
	ctx := t.Context()
	var a channel.ChannelAccount
	if err := e.in(ctx, func(tx pgx.Tx) (err error) {
		a, err = w.SetHumanAgent(ctx, tx, id, true, 1, actor)
		return err //nolint:wrapcheck // test
	}); err != nil || !a.HumanAgentEnabled || a.Version != 2 {
		t.Fatalf("SetHumanAgent = %+v, %v", a, err)
	}
	if err := e.in(ctx, func(tx pgx.Tx) error {
		_, err := w.SetHumanAgent(ctx, tx, id, false, 1, actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, channel.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'channel.human_agent_changed'
		AND changed_fields = '{human_agent_enabled}'`); n != 1 {
		t.Fatalf("audit = %d", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.SendModeAlertKind); n != 0 ||
		len(bus.Signals()) != 1 {
		t.Fatalf("lệnh cảnh báo %d, báo hiệu %d", n, len(bus.Signals()))
	}
}
