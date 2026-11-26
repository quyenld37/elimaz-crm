package facebook_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

var _ channel.MessagingAdapter = (*facebook.Channel)(nil)

// Adapter Facebook hiện thực đủ các interface tùy chọn mà M4 dùng.
var (
	_ channel.ProfileFetcher       = (*facebook.Channel)(nil)
	_ channel.PostFetcher          = (*facebook.Channel)(nil)
	_ channel.HistoryFetcher       = (*facebook.Channel)(nil)
	_ channel.AttachmentDownloader = (*facebook.Channel)(nil)
	_ channel.Connector            = (*facebook.Channel)(nil)
)

// fbHarness là Harness của channeltest cho adapter Facebook: fixture webhook của Task 147, máy Meta giả cho Send.
type fbHarness struct {
	e outEnv

	mu       sync.Mutex
	rejected map[int]bool // chỉ số lời gọi {page-id}/messages bị Meta từ chối
}

func (h *fbHarness) Adapter() channel.MessagingAdapter { return h.e.ch } //nolint:ireturn // chữ ký của Harness

func (*fbHarness) SignedFixtures() map[string][]byte {
	out := map[string][]byte{}
	for name := range fixtureWants() {
		out[name] = mustRead(name)
	}
	return out
}

func (*fbHarness) Sign(raw []byte) map[string][]string { return sign(raw, testAppSecret) }

func (*fbHarness) ExpectedEvents(name string) []channeltest.ExpectedEvent {
	var out []channeltest.ExpectedEvent
	for _, w := range fixtureWants()[name] {
		out = append(out, channeltest.ExpectedEvent{Type: w.Type, DedupeKey: w.Dedupe, OrderKey: w.Order, RouteKey: w.Page})
	}
	return out
}

func (*fbHarness) UnknownFixture() ([]byte, string) {
	return mustRead("feed_status.json"), "feed:status"
}

func (h *fbHarness) Target() (channel.ChannelAccount, channel.Credentials) { return h.e.acc, h.e.cred }

func (*fbHarness) ClosedWindow() (channel.ConversationState, time.Time) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	last := now.Add(-25 * time.Hour)
	return channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: &last}, now
}

// SetSendBehavior: reject_policy → Meta trả 551 sau khi đọc; fail_before_send → đóng kết nối trước bắt tay;
// timeout_after_send → Meta đọc trọn rồi rớt kết nối.
func (h *fbHarness) SetSendBehavior(b string) {
	switch b {
	case channeltest.SendRejectPolicy:
		h.mu.Lock()
		h.rejected[len(h.e.fg.Calls(messagesPath))] = true
		h.mu.Unlock()
		h.e.fg.FailNext(messagesPath, fakegraph.GraphErrorSpec{Code: 551})
	case channeltest.SendFailBeforeSend:
		h.e.refuseNext()
	case channeltest.SendTimeoutAfterSend:
		h.e.fg.DropAfterRead(messagesPath)
	}
}

// SentRefs là metadata của mọi lần máy giả đã đọc trọn thân gửi tin, trừ lần nó trả lỗi từ chối.
func (h *fbHarness) SentRefs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i, c := range h.e.fg.Calls(messagesPath) {
		var b struct {
			Message struct {
				Metadata string `json:"metadata"`
			} `json:"message"`
		}
		if !h.rejected[i] && json.Unmarshal(c.Body, &b) == nil {
			out = append(out, b.Message.Metadata)
		}
	}
	return out
}

func (*fbHarness) ErrorCases() []channeltest.ErrorCase {
	ge := facebook.NewGraphErrorForTest
	return []channeltest.ErrorCase{
		{Err: facebook.NewTransportErrorForTest(false), Class: channel.ErrorNotSentRetry},
		{Err: facebook.NewTransportErrorForTest(true), Class: channel.ErrorUnknown},
		{Err: ge(400, 613, 0, "OAuthException", "t", "limit"), Class: channel.ErrorRateLimited},
		{Err: ge(400, 190, 460, "OAuthException", "t", "token"), Class: channel.ErrorAuth},
		{Err: ge(400, 551, 0, "OAuthException", "t", "blocked"), Class: channel.ErrorPolicy},
		{Err: ge(400, 100, 0, "OAuthException", "t", "bad"), Class: channel.ErrorPermanent},
		{Err: ge(502, 0, 0, "", "", "Bad Gateway"), Class: channel.ErrorUnknown},
	}
}

// 04 mục 4: adapter Facebook qua đủ bảy luật chung (1)–(7) của channeltest.
func TestContract(t *testing.T) {
	t.Parallel()
	channeltest.RunContract(t, &fbHarness{e: newOutEnv(t), rejected: map[int]bool{}})
}
