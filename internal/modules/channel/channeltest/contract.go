// Package channeltest là bộ contract test chung cho mọi adapter kênh nhắn tin (kế hoạch M4, Task 145; 04 mục 4): mỗi
// adapter (facebook, fakechat) dựng một Harness rồi gọi RunContract trong test của mình. Gói không nhập net/http
// (module không biết HTTP): header là map[string][]string, gán thẳng được cho http.Header.
package channeltest

import (
	"errors"
	"maps"
	"slices"
	"sort"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// Hành vi gửi mà Harness.SetSendBehavior nhận.
const (
	SendOK               = "ok"
	SendRejectPolicy     = "reject_policy"
	SendFailBeforeSend   = "fail_before_send"
	SendTimeoutAfterSend = "timeout_after_send"
)

// ExpectedEvent là một sự kiện mong đợi khi tách một fixture: loại chuẩn hóa, khóa chống trùng, khóa thứ tự, khóa định
// tuyến (Route.ExternalKey).
type ExpectedEvent struct {
	Type, DedupeKey, OrderKey, RouteKey string
}

// ErrorCase là một lỗi của adapter và lớp mà ClassifyError phải trả.
type ErrorCase struct {
	Err   error
	Class channel.ErrorClass
}

// Harness là phần mỗi adapter cung cấp cho RunContract.
type Harness interface {
	Adapter() channel.MessagingAdapter
	// SignedFixtures: tên → thân gói webhook (Sign ký được).
	SignedFixtures() map[string][]byte
	// Sign trả header chữ ký hợp lệ cho raw.
	Sign(raw []byte) map[string][]string
	// ExpectedEvents là sự kiện mong đợi của fixture name, đúng thứ tự Split trả.
	ExpectedEvents(name string) []ExpectedEvent
	// UnknownFixture là gói chỉ có một sự kiện loại kênh không hỗ trợ; skipped là khóa phải tăng trong Skipped.
	UnknownFixture() (raw []byte, skipped string)
	// Target là tài khoản và token dùng cho Send.
	Target() (channel.ChannelAccount, channel.Credentials)
	// ClosedWindow là trạng thái hội thoại và thời điểm mà SendPolicy phải từ chối vì quá cửa sổ trả lời.
	ClosedWindow() (channel.ConversationState, time.Time)
	// SetSendBehavior đặt kết quả của lần Send sau (SendOK, SendRejectPolicy, SendFailBeforeSend, SendTimeoutAfterSend).
	SetSendBehavior(b string)
	// SentRefs là ClientRef của các lần kênh đã nhận (kể cả lần quá giờ sau khi gửi).
	SentRefs() []string
	// ErrorCases là các lỗi của adapter cùng lớp mong đợi.
	ErrorCases() []ErrorCase
}

// RunContract chạy bảy luật chung của adapter trên h: (1) mỗi fixture tách đúng sự kiện mong đợi; (2) tách hai lần ra
// cùng DedupeKey, OrderKey; (3) loại lạ vào Skipped, không lỗi; (4) thân sửa một byte → VerifySignature sai, thân
// gốc → đúng; (5) SendPolicy từ chối khi quá cửa sổ, có Reason; (6) Send: ok → sent có ExternalID, reject_policy →
// rejected có Reason, fail_before_send → not_sent, timeout_after_send → unknown; (7) ClassifyError khớp ErrorCases.
func RunContract(t *testing.T, h Harness) {
	t.Helper()
	t.Run("split", func(t *testing.T) { checkSplit(t, h) })
	t.Run("unknown", func(t *testing.T) { checkUnknown(t, h) })
	t.Run("signature", func(t *testing.T) { checkSignature(t, h) })
	t.Run("policy", func(t *testing.T) { checkPolicy(t, h) })
	t.Run("send", func(t *testing.T) { checkSend(t, h) })
	t.Run("classify", func(t *testing.T) { checkClassify(t, h) })
}

func names(m map[string][]byte) []string {
	out := slices.Collect(maps.Keys(m))
	sort.Strings(out)
	return out
}

func checkSplit(t *testing.T, h Harness) {
	t.Helper()
	fx := h.SignedFixtures()
	if len(fx) == 0 {
		t.Fatal("harness không có fixture")
	}
	for _, name := range names(fx) {
		checkFixture(t, h.Adapter(), name, fx[name], h.ExpectedEvents(name))
	}
}

// checkFixture: tách raw ra đúng want (loại, khóa chống trùng, khóa thứ tự, khóa tuyến, đủ payload và thời điểm), và
// tách lần hai ra cùng khóa.
func checkFixture(t *testing.T, a channel.MessagingAdapter, name string, raw []byte, want []ExpectedEvent) {
	t.Helper()
	r1, err1 := a.Split(raw)
	r2, err2 := a.Split(raw)
	if err1 != nil || err2 != nil {
		t.Errorf("%s: Split lỗi %v, %v", name, err1, err2)
		return
	}
	if len(r1.Events) != len(want) || len(r2.Events) != len(want) {
		t.Errorf("%s: %d sự kiện, muốn %d", name, len(r1.Events), len(want))
		return
	}
	for i, w := range want {
		got := r1.Events[i]
		g := ExpectedEvent{Type: got.Type, DedupeKey: got.DedupeKey, OrderKey: got.OrderKey, RouteKey: got.Route.ExternalKey}
		if g != w {
			t.Errorf("%s[%d] = %+v, muốn %+v", name, i, g, w)
		}
		if got.DedupeKey == "" || got.OrderKey == "" || got.Route.Kind == "" || len(got.Payload) == 0 || got.OccurredAt.IsZero() {
			t.Errorf("%s[%d] thiếu khóa, payload hay thời điểm: %+v", name, i, got)
		}
		if again := r2.Events[i]; again.DedupeKey != got.DedupeKey || again.OrderKey != got.OrderKey {
			t.Errorf("%s[%d]: tách lần hai khác khóa", name, i)
		}
	}
}

func checkUnknown(t *testing.T, h Harness) {
	t.Helper()
	raw, key := h.UnknownFixture()
	r, err := h.Adapter().Split(raw)
	if err != nil || len(r.Events) != 0 || r.Skipped[key] != 1 {
		t.Errorf("loại lạ: %+v, %v; muốn 0 sự kiện, Skipped[%q] = 1", r, err, key)
	}
}

func checkSignature(t *testing.T, h Harness) {
	t.Helper()
	a := h.Adapter()
	for _, name := range names(h.SignedFixtures()) {
		raw := h.SignedFixtures()[name]
		hdr := h.Sign(raw)
		if !a.VerifySignature(raw, hdr) {
			t.Errorf("%s: chữ ký đúng bị từ chối", name)
		}
		bad := slices.Clone(raw)
		bad[len(bad)/2] ^= 0x01
		if a.VerifySignature(bad, hdr) {
			t.Errorf("%s: thân bị sửa một byte vẫn qua chữ ký", name)
		}
		if a.VerifySignature(raw, nil) {
			t.Errorf("%s: không có chữ ký vẫn qua", name)
		}
	}
}

func checkPolicy(t *testing.T, h Harness) {
	t.Helper()
	st, now := h.ClosedWindow()
	p := h.Adapter().SendPolicy(st, now)
	if p.Allowed || p.Reason == "" {
		t.Errorf("quá cửa sổ: %+v; muốn từ chối có Reason", p)
	}
}

func checkSend(t *testing.T, h Harness) {
	t.Helper()
	a := h.Adapter()
	for i, c := range []struct {
		behavior string
		want     channel.OutcomeStatus
		sent     bool
	}{
		{SendOK, channel.OutcomeSent, true},
		{SendRejectPolicy, channel.OutcomeRejected, false},
		{SendFailBeforeSend, channel.OutcomeNotSent, false},
		{SendTimeoutAfterSend, channel.OutcomeUnknown, true},
	} {
		h.SetSendBehavior(c.behavior)
		ref := "ref-" + c.behavior
		acc, cred := h.Target()
		out, err := a.Send(t.Context(), acc, cred, channel.OutboundCommand{
			Purpose: channel.PurposeMessage, RecipientID: "user-1", Text: "chào", Mode: channel.SendStandard, ClientRef: ref,
		})
		if c.want == channel.OutcomeUnknown {
			if out.Status != channel.OutcomeUnknown && err == nil {
				t.Errorf("ca %d %s: %+v, %v; muốn unknown hoặc lỗi", i, c.behavior, out, err)
			}
		} else if err != nil || out.Status != c.want {
			t.Errorf("ca %d %s: %+v, %v; muốn %s", i, c.behavior, out, err, c.want)
		}
		switch c.want {
		case channel.OutcomeSent:
			if out.ExternalID == "" {
				t.Errorf("ca %d: sent không có ExternalID", i)
			}
		case channel.OutcomeRejected:
			if out.Reason == "" {
				t.Errorf("ca %d: rejected không có Reason", i)
			}
		case channel.OutcomeNotSent, channel.OutcomeUnknown:
		}
		if got := slices.Contains(h.SentRefs(), ref); got != c.sent {
			t.Errorf("ca %d %s: kênh nhận = %v, muốn %v", i, c.behavior, got, c.sent)
		}
	}
	h.SetSendBehavior(SendOK)
}

func checkClassify(t *testing.T, h Harness) {
	t.Helper()
	cases := h.ErrorCases()
	if len(cases) == 0 {
		t.Fatal("harness không có ca lỗi")
	}
	a := h.Adapter()
	for _, c := range cases {
		if got := a.ClassifyError(c.Err); got != c.Class {
			t.Errorf("ClassifyError(%v) = %s, muốn %s", c.Err, got, c.Class)
		}
		if got := a.ClassifyError(errors.Join(errors.New("bọc"), c.Err)); got != c.Class {
			t.Errorf("ClassifyError(bọc %v) = %s, muốn %s", c.Err, got, c.Class)
		}
	}
}
