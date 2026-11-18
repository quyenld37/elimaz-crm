package selftest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

// fakeBus ghi lại báo hiệu được phát; err khác nil thì Publish trả lỗi đó.
type fakeBus struct {
	got []realtime.Signal
	err error
}

func (b *fakeBus) Publish(_ context.Context, _ pgx.Tx, s realtime.Signal) error {
	if b.err != nil {
		return b.err
	}
	b.got = append(b.got, s)
	return nil
}

func ping(t *testing.T, eventID string) webhooks.Inbound {
	t.Helper()
	data, err := json.Marshal(map[string]string{"kind": "ping", "eventId": eventID, "tenantKey": "k", "sentAt": "2026-11-18T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	return webhooks.Inbound{Type: "selftest.ping", Data: data, JobID: uuid.New(), TenantID: uuid.New(), OccurredAt: time.Now()}
}

// selftest.ping → một báo hiệu selftest.pong đúng tenant, cần realtime.subscribe, đối tượng {"selftest", eventId}.
func TestInboundPingPublishesPong(t *testing.T) {
	bus := &fakeBus{}
	var h webhooks.InboundHandler = selftest.NewInbound(bus)
	if h.Source() != selftest.Key {
		t.Fatalf("Source = %q", h.Source())
	}
	in := ping(t, "ev-1")
	if err := h.Handle(t.Context(), in, nil); err != nil {
		t.Fatal(err)
	}
	want := realtime.Signal{
		TenantID: in.TenantID, Topic: selftest.PongTopic, Requires: "realtime.subscribe",
		Entity: realtime.Entity{Type: selftest.Key, ID: "ev-1"},
	}
	if len(bus.got) != 1 || bus.got[0].TenantID != want.TenantID || bus.got[0].Topic != want.Topic ||
		bus.got[0].Requires != want.Requires || bus.got[0].Entity != want.Entity || bus.got[0].Scope != nil {
		t.Fatalf("báo hiệu = %+v, muốn %+v", bus.got, want)
	}
	if selftest.PongTopic != "selftest.pong" {
		t.Fatalf("PongTopic = %q", selftest.PongTopic)
	}
}

// Dữ liệu không đọc được, thiếu eventId, loại lạ → ErrPermanent (dead ngay), không phát gì; lỗi Publish → lỗi thường
// (thử lại theo lịch).
func TestInboundRejectsBadEvents(t *testing.T) {
	cases := []struct {
		name string
		in   webhooks.Inbound
	}{
		{"không phải JSON", webhooks.Inbound{Type: "selftest.ping", Data: json.RawMessage(`x`), TenantID: uuid.New()}},
		{"thiếu eventId", webhooks.Inbound{Type: "selftest.ping", Data: json.RawMessage(`{"kind":"ping"}`), TenantID: uuid.New()}},
		{"loại lạ", webhooks.Inbound{Type: "selftest.other", Data: json.RawMessage(`{"eventId":"e"}`), TenantID: uuid.New()}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bus := &fakeBus{}
			err := selftest.NewInbound(bus).Handle(t.Context(), c.in, nil)
			if !errors.Is(err, queue.ErrPermanent) || len(bus.got) != 0 {
				t.Fatalf("muốn ErrPermanent và không phát, nhận %v, %d báo hiệu", err, len(bus.got))
			}
		})
	}
	boom := errors.New("boom")
	err := selftest.NewInbound(&fakeBus{err: boom}).Handle(t.Context(), ping(t, "e"), nil)
	if !errors.Is(err, boom) || errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("lỗi Publish phải trả lên, không ErrPermanent: %v", err)
	}
}

// CommandKind: command.selftest, hết hạn 3 phút, Timeout 30 s, mode command, giữ chỗ hết hạn thì in_doubt.
func TestCommandKind(t *testing.T) {
	k := selftest.CommandKind()
	want := commands.CommandKind("command.selftest", 3*time.Minute, 30*time.Second)
	if k != want {
		t.Fatalf("CommandKind = %+v, muốn %+v", k, want)
	}
	var h commands.Handler = selftest.NewCommand(&testkit.FakeExternal{})
	if h.Kind() != want || h.AutoRetryNotSent() {
		t.Fatalf("Kind %+v, AutoRetryNotSent %v", h.Kind(), h.AutoRetryNotSent())
	}
}

// Execute gửi ref = mã lệnh: ok → done (result có ref); mọi lỗi của bên ngoài, kể cả "chưa gửi" → lỗi (Runner hiểu
// là in_doubt, không tự gửi lại). Lookup: bên ngoài có ref → done, chưa có → not_sent.
func TestCommandExecuteAndLookup(t *testing.T) {
	j := queue.Job{ID: uuid.New(), Kind: "command.selftest"}
	ext := &testkit.FakeExternal{Mode: testkit.FakeOK}
	cmd := selftest.NewCommand(ext)
	var looker commands.Looker = cmd
	if s, err := looker.Lookup(t.Context(), j); err != nil || s != commands.StatusNotSent {
		t.Fatalf("Lookup trước khi gửi = %q %v, muốn not_sent", s, err)
	}
	out, err := cmd.Execute(t.Context(), j)
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute ok = %+v %v", out, err)
	}
	var res map[string]string
	if err := json.Unmarshal(out.Result, &res); err != nil || res["ref"] != j.ID.String() {
		t.Fatalf("Result = %s %v", out.Result, err)
	}
	if calls := ext.Calls(); len(calls) != 1 || calls[0] != j.ID.String() {
		t.Fatalf("Calls = %v", calls)
	}
	if s, err := looker.Lookup(t.Context(), j); err != nil || s != commands.StatusDone {
		t.Fatalf("Lookup sau khi gửi = %q %v, muốn done", s, err)
	}
	for _, mode := range []string{testkit.FakeFailBeforeSend, testkit.FakeTimeoutAfterSend, testkit.FakeReject} {
		ext.SetMode(mode)
		if _, err := cmd.Execute(t.Context(), queue.Job{ID: uuid.New()}); err == nil {
			t.Fatalf("chế độ %s: Execute phải trả lỗi", mode)
		}
	}
}

// Loopback: bên ngoài tại chỗ của worker dev: luôn nhận, luôn có.
func TestLoopback(t *testing.T) {
	var ext selftest.External = selftest.Loopback{}
	if err := ext.Send(t.Context(), "r"); err != nil || !ext.Has("r") {
		t.Fatalf("Loopback: %v %v", err, ext.Has("r"))
	}
}
