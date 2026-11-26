package fake_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

const secret = "fakechat-secret-0123456789-abcdefgh"

// t0 là mốc giờ cố định của fixture.
func t0() time.Time { return time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC) }

type harness struct {
	a  *fake.Chat
	fx map[string][]fake.Event
}

func newHarness() *harness {
	ev := func(id, typ string, f func(*fake.Event)) fake.Event {
		e := fake.Event{ID: id, Type: typ, PageID: "page-1", UserID: "user-1", At: t0()}
		if f != nil {
			f(&e)
		}
		return e
	}
	return &harness{a: fake.New(secret), fx: map[string][]fake.Event{
		"messages": {
			ev("e1", "message", func(e *fake.Event) { e.MID, e.Text = "m1", "còn size M không" }),
			ev("e2", "echo", func(e *fake.Event) { e.MID, e.Metadata, e.AppID = "m2", "ref-1", fake.Key }),
			ev("e3", "delivered", func(e *fake.Event) { e.MIDs = []string{"m2"} }),
			ev("e4", "read", nil),
			ev("e5", "reaction", func(e *fake.Event) { e.MID, e.Reaction, e.Action = "m2", "love", "react" }),
			ev("e6", "edit", func(e *fake.Event) { e.MID, e.Text = "m1", "còn size L không" }),
		},
		"feed": {
			ev("c1", "comment", func(e *fake.Event) { e.PostID, e.CommentID, e.Verb = "post-9", "cmt-1", "add" }),
			ev("l1", "live", func(e *fake.Event) { e.LiveVideoID, e.Status = "live-3", "live" }),
			ev("p1", "policy", func(e *fake.Event) { e.Action = "warning" }),
		},
	}}
}

func (h *harness) raw(name string) []byte {
	b, err := json.Marshal(fake.Packet{Events: h.fx[name]})
	if err != nil {
		panic(err)
	}
	return b
}

func (h *harness) Adapter() channel.MessagingAdapter { return h.a } //nolint:ireturn // chữ ký của Harness

func (h *harness) SignedFixtures() map[string][]byte {
	return map[string][]byte{"messages": h.raw("messages"), "feed": h.raw("feed")}
}

func (*harness) Sign(raw []byte) map[string][]string {
	return map[string][]string{"X-Fake-Signature": {fake.Sign(raw, secret)}}
}

func (h *harness) ExpectedEvents(name string) []channeltest.ExpectedEvent {
	order := map[string]string{
		channel.EventCommentChanged: "post:post-9", channel.EventLiveStatus: "live:live-3", channel.EventPolicyEnforcement: "policy",
	}
	types := map[string]string{
		"message": channel.EventMessageReceived, "echo": channel.EventMessageEcho, "delivered": channel.EventMessageDelivered,
		"read": channel.EventMessageRead, "reaction": channel.EventMessageReaction, "edit": channel.EventMessageEdited,
		"comment": channel.EventCommentChanged, "live": channel.EventLiveStatus, "policy": channel.EventPolicyEnforcement,
	}
	out := make([]channeltest.ExpectedEvent, 0, len(h.fx[name]))
	for _, e := range h.fx[name] {
		typ := types[e.Type]
		o, ok := order[typ]
		if !ok {
			o = "user:user-1"
		}
		out = append(out, channeltest.ExpectedEvent{Type: typ, DedupeKey: "fakechat:" + e.ID, OrderKey: "fakechat:page-1:" + o, RouteKey: "page-1"})
	}
	return out
}

func (*harness) UnknownFixture() ([]byte, string) {
	b, _ := json.Marshal(fake.Packet{Events: []fake.Event{{ID: "x1", Type: "story_mention", PageID: "page-1", At: t0()}}})
	return b, "story_mention"
}

func (*harness) Target() (channel.ChannelAccount, channel.Credentials) {
	return channel.ChannelAccount{ID: uuid.New(), Channel: fake.Key, ExternalID: "page-1", SendMode: channel.SendModeEnabled},
		channel.Credentials{AccessToken: logging.Secret("tok")}
}

func (*harness) ClosedWindow() (channel.ConversationState, time.Time) {
	last := t0()
	return channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last}, t0().Add(25 * time.Hour)
}

func (h *harness) SetSendBehavior(b string) { h.a.SetSendBehavior(b) }

func (h *harness) SentRefs() []string {
	var out []string
	for _, s := range h.a.Sent() {
		out = append(out, s.Command.ClientRef)
	}
	return out
}

func (*harness) ErrorCases() []channeltest.ErrorCase {
	return []channeltest.ErrorCase{
		{Err: fake.ErrNotSent, Class: channel.ErrorNotSentRetry},
		{Err: fake.ErrRateLimited, Class: channel.ErrorRateLimited},
		{Err: fake.ErrAuth, Class: channel.ErrorAuth},
		{Err: fake.ErrPolicy, Class: channel.ErrorPolicy},
		{Err: fake.ErrPermanent, Class: channel.ErrorPermanent},
		{Err: fmt.Errorf("lạ"), Class: channel.ErrorUnknown},
	}
}

// Bộ contract test chung (04 mục 4) chạy trên kênh giả.
func TestContract(t *testing.T) {
	t.Parallel()
	channeltest.RunContract(t, newHarness())
}

// Payload chuẩn hóa mang đúng nội dung; sự kiện thiếu khóa vào Skipped["invalid"]; thân không phải JSON → lỗi.
func TestSplit_Payload(t *testing.T) {
	t.Parallel()
	h := newHarness()
	r, err := h.a.Split(h.raw("messages"))
	if err != nil {
		t.Fatal(err)
	}
	var m channel.MessagePayload
	if err := json.Unmarshal(r.Events[0].Payload, &m); err != nil || m.Text != "còn size M không" || m.MID != "m1" || m.IsEcho || !m.SentAt.Equal(t0()) {
		t.Fatalf("tin vào = %+v, %v", m, err)
	}
	if err := json.Unmarshal(r.Events[1].Payload, &m); err != nil || !m.IsEcho || m.Metadata != "ref-1" || m.EchoAppID != fake.Key {
		t.Fatalf("echo = %+v, %v", m, err)
	}
	if r.Events[0].Route.Kind != fake.RouteKind {
		t.Fatalf("route = %+v", r.Events[0].Route)
	}
	b, _ := json.Marshal(fake.Packet{Events: []fake.Event{{ID: "", Type: "message", PageID: "p", At: t0()}, {ID: "x", Type: "message", At: t0()}}})
	if r, err := h.a.Split(b); err != nil || len(r.Events) != 0 || r.Skipped["invalid"] != 2 {
		t.Fatalf("thiếu khóa: %+v, %v", r, err)
	}
	if _, err := h.a.Split([]byte("{không phải json")); err == nil {
		t.Fatal("thân hỏng phải lỗi")
	}
}

// Cửa sổ gửi của kênh giả: trong 24 giờ thường; quá 24 giờ có thẻ nhân viên (bật, không tự động) tới 7 ngày; chỉ xem,
// khách chặn, nhắn riêng đã dùng, quá hạn, comment đã xóa đều từ chối có lý do.
func TestSendPolicy(t *testing.T) {
	t.Parallel()
	a := fake.New(secret)
	last := t0()
	old := t0().Add(-8 * 24 * time.Hour)
	for i, c := range []struct {
		st     channel.ConversationState
		now    time.Time
		allow  bool
		mode   channel.SendMode
		reason channel.DenyReason
	}{
		{channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last}, t0().Add(time.Hour), true, channel.SendStandard, ""},
		{channel.ConversationState{Purpose: channel.PurposeMessage}, t0(), false, "", channel.DenyWindowClosed},
		{channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last, HumanAgentEnabled: true}, t0().Add(48 * time.Hour), true, channel.SendHumanAgent, ""},
		{channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last, HumanAgentEnabled: true, Automated: true}, t0().Add(48 * time.Hour), false, "", channel.DenyWindowClosed},
		{channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last, ReadOnly: true}, t0(), false, "", channel.DenyReadOnly},
		{channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last, BlockedByUser: true}, t0(), false, "", channel.DenyBlockedByUser},
		{channel.ConversationState{Purpose: channel.PurposePrivateReply, CommentCreatedAt: &last}, t0(), true, channel.SendPrivateReply, ""},
		{channel.ConversationState{Purpose: channel.PurposePrivateReply, CommentCreatedAt: &last, PrivateReplyUsed: true}, t0(), false, "", channel.DenyPrivateReplyUsed},
		{channel.ConversationState{Purpose: channel.PurposePrivateReply, CommentCreatedAt: &old}, t0(), false, "", channel.DenyPrivateReplyExpired},
		{channel.ConversationState{Purpose: channel.PurposeCommentReply, CommentRemoved: true}, t0(), false, "", channel.DenyCommentRemoved},
		{channel.ConversationState{Purpose: channel.PurposeHide}, t0(), true, channel.SendCommentReply, ""},
	} {
		p := a.SendPolicy(c.st, c.now)
		if p.Allowed != c.allow || p.Mode != c.mode || p.Reason != c.reason {
			t.Errorf("ca %d: %+v", i, p)
		}
	}
}

// Không có token → rejected no_permission, kênh không nhận; Lookup thấy lần đã gửi, không thấy lần chưa gửi.
func TestSendAndLookup(t *testing.T) {
	t.Parallel()
	a := fake.New(secret)
	acc := channel.ChannelAccount{ID: uuid.New()}
	cmd := channel.OutboundCommand{Purpose: channel.PurposeMessage, RecipientID: "u", Text: "xin chào", ClientRef: "r1"}
	if out, err := a.Send(t.Context(), acc, channel.Credentials{}, cmd); err != nil || out.Status != channel.OutcomeRejected ||
		out.Reason != channel.DenyNoPermission || len(a.Sent()) != 0 {
		t.Fatalf("không token: %+v, %v", out, err)
	}
	cred := channel.Credentials{AccessToken: "tok"}
	out, err := a.Send(t.Context(), acc, cred, cmd)
	if err != nil || out.Status != channel.OutcomeSent {
		t.Fatal(out, err)
	}
	q := channel.LookupQuery{Purpose: channel.PurposeMessage, Text: "xin chào", After: t0()}
	if r, err := a.Lookup(t.Context(), acc, cred, q); err != nil || r.Status != channel.OutcomeSent || r.ExternalID != out.ExternalID {
		t.Fatalf("Lookup = %+v, %v", r, err)
	}
	q.Text = "khác"
	if r, _ := a.Lookup(t.Context(), acc, cred, q); r.Status != channel.OutcomeNotSent {
		t.Fatalf("Lookup chữ khác = %+v", r)
	}
	if h, _ := a.HealthCheck(t.Context(), acc, cred); !h.TokenValid || !h.WebhookSubscribed {
		t.Fatalf("HealthCheck = %+v", h)
	}
}

// Registry: kênh giả có sẵn ngoài production (khóa fakechat), không có ở production; dùng FAKECHAT_WEBHOOK_SECRET.
func TestAdapter_Registry(t *testing.T) {
	t.Parallel()
	reg, err := channel.NewRegistry(fake.Adapter(config.Config{Env: "dev", FakeChatWebhookSecret: secret}))
	if err != nil {
		t.Fatal(err)
	}
	a, ok := reg.Get("fakechat")
	if !ok || !slices.Equal(reg.Keys(), []string{"fakechat"}) {
		t.Fatalf("Registry = %v", reg.Keys())
	}
	raw := []byte(`{"events":[]}`)
	if !a.VerifySignature(raw, map[string][]string{"X-Fake-Signature": {fake.Sign(raw, secret)}}) {
		t.Fatal("chữ ký bằng FAKECHAT_WEBHOOK_SECRET bị từ chối")
	}
	if fake.Adapter(config.Config{Env: "production", FakeChatWebhookSecret: secret}) != nil {
		t.Fatal("production không được có kênh giả")
	}
	if fake.New("").VerifySignature(raw, map[string][]string{"X-Fake-Signature": {fake.Sign(raw, "")}}) {
		t.Fatal("bí mật rỗng phải luôn sai")
	}
}
