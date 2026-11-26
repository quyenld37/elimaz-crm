package facebook_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// policyNowAt là mốc "bây giờ" của mọi ca SendPolicy (kế hoạch Task 148).
func policyNowAt() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }

func at(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func ago(d time.Duration) *time.Time {
	t := policyNowAt().Add(-d)
	return &t
}

const day = 24 * time.Hour

type policyCase struct {
	name   string
	st     channel.ConversationState
	allow  bool
	mode   channel.SendMode
	reason channel.DenyReason
	until  *time.Time
	left   int // 0: không kiểm Remaining
}

func checkPolicy(t *testing.T, c policyCase) {
	t.Helper()
	p := newAdapter().SendPolicy(c.st, policyNowAt())
	if p.Allowed != c.allow || p.Mode != c.mode || p.Reason != c.reason {
		t.Errorf("%s: %+v; muốn allowed=%v mode=%q reason=%q", c.name, p, c.allow, c.mode, c.reason)
	}
	switch {
	case c.until == nil && p.Until != nil:
		t.Errorf("%s: Until = %v, muốn nil", c.name, *p.Until)
	case c.until != nil && (p.Until == nil || !p.Until.Equal(*c.until)):
		t.Errorf("%s: Until = %v, muốn %v", c.name, p.Until, *c.until)
	}
	if c.left != 0 && (p.Remaining == nil || *p.Remaining != c.left) {
		t.Errorf("%s: Remaining = %v, muốn %d", c.name, p.Remaining, c.left)
	}
	if !p.Allowed && p.Reason == "" {
		t.Errorf("%s: từ chối không có Reason", c.name)
	}
}

// CH09: tin thường trong 24 giờ sau tin cuối của khách (mốc 24 giờ đúng là hết); Human Agent tới 7 ngày chỉ khi Page
// bật và người gõ (không tự động); khách chưa từng nhắn → quá cửa sổ.
func TestSendPolicy_Message(t *testing.T) {
	t.Parallel()
	msg := func(last *time.Time, human, auto bool) channel.ConversationState {
		return channel.ConversationState{
			Purpose: channel.PurposeMessage, LastCustomerMessageAt: last, HumanAgentEnabled: human, Automated: auto,
		}
	}
	threeDays := ago(3 * day)
	for _, c := range []policyCase{
		{
			name: "cách 23 giờ 59 phút", st: msg(at("2026-11-19T10:01:00Z"), false, false), allow: true,
			mode: channel.SendStandard, until: at("2026-11-20T10:01:00Z"),
		},
		{name: "đúng 24 giờ", st: msg(at("2026-11-19T10:00:00Z"), false, false), reason: channel.DenyWindowClosed},
		{
			name: "đúng 24 giờ, Human Agent bật, người gõ", st: msg(at("2026-11-19T10:00:00Z"), true, false), allow: true,
			mode: channel.SendHumanAgent, until: at("2026-11-26T10:00:00Z"),
		},
		{
			name: "cách 3 ngày, Human Agent, người gõ", st: msg(threeDays, true, false), allow: true,
			mode: channel.SendHumanAgent, until: new(threeDays.Add(7 * day)),
		},
		{name: "cách 3 ngày, Human Agent, tự động", st: msg(threeDays, true, true), reason: channel.DenyWindowClosed},
		{name: "cách 3 ngày, Human Agent tắt", st: msg(threeDays, false, false), reason: channel.DenyWindowClosed},
		{name: "cách 7 ngày 1 giây", st: msg(ago(7*day+time.Second), true, false), reason: channel.DenyWindowClosed},
		{name: "đúng 7 ngày", st: msg(ago(7*day), true, false), reason: channel.DenyWindowClosed},
		{name: "khách chưa từng nhắn", st: msg(nil, true, false), reason: channel.DenyWindowClosed},
		{
			name: "trong 24 giờ nhưng tự động vẫn được", st: msg(ago(time.Hour), false, true), allow: true,
			mode: channel.SendStandard, until: new(policyNowAt().Add(-time.Hour).Add(day)),
		},
	} {
		checkPolicy(t, c)
	}
}

// CH05: nhắn riêng một lần trong 7 ngày sau comment; đã dùng, quá hạn (đúng 7 ngày là hết), không biết giờ comment →
// từ chối.
func TestSendPolicy_PrivateReply(t *testing.T) {
	t.Parallel()
	pr := func(created *time.Time, used bool) channel.ConversationState {
		return channel.ConversationState{Purpose: channel.PurposePrivateReply, CommentCreatedAt: created, PrivateReplyUsed: used}
	}
	created := ago(6*day + 23*time.Hour)
	for _, c := range []policyCase{
		{
			name: "6 ngày 23 giờ, chưa dùng", st: pr(created, false), allow: true, mode: channel.SendPrivateReply,
			until: new(created.Add(7 * day)), left: 1,
		},
		{name: "đã dùng", st: pr(created, true), reason: channel.DenyPrivateReplyUsed},
		{name: "đúng 7 ngày", st: pr(ago(7*day), false), reason: channel.DenyPrivateReplyExpired},
		{name: "không biết giờ comment", st: pr(nil, false), reason: channel.DenyPrivateReplyExpired},
	} {
		checkPolicy(t, c)
	}
}

// Trả lời comment được trừ khi comment đã xóa; ẩn/hiện luôn được; ReadOnly, khách chặn thắng mọi luật; mục đích lạ
// → từ chối.
func TestSendPolicy_CommentAndOverrides(t *testing.T) {
	t.Parallel()
	open := ago(time.Minute)
	for _, c := range []policyCase{
		{name: "trả lời comment", st: channel.ConversationState{Purpose: channel.PurposeCommentReply}, allow: true, mode: channel.SendCommentReply},
		{
			name: "trả lời comment đã xóa", st: channel.ConversationState{Purpose: channel.PurposeCommentReply, CommentRemoved: true},
			reason: channel.DenyCommentRemoved,
		},
		{name: "ẩn comment", st: channel.ConversationState{Purpose: channel.PurposeHide}, allow: true, mode: channel.SendCommentReply},
		{
			name: "ẩn comment đã xóa vẫn được", st: channel.ConversationState{Purpose: channel.PurposeHide, CommentRemoved: true},
			allow: true, mode: channel.SendCommentReply,
		},
		{
			name:   "ReadOnly thắng tin trong cửa sổ",
			st:     channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: open, ReadOnly: true, BlockedByUser: true},
			reason: channel.DenyReadOnly,
		},
		{
			name: "ReadOnly thắng nhắn riêng", st: channel.ConversationState{
				Purpose: channel.PurposePrivateReply, CommentCreatedAt: open, ReadOnly: true,
			}, reason: channel.DenyReadOnly,
		},
		{name: "ReadOnly thắng ẩn", st: channel.ConversationState{Purpose: channel.PurposeHide, ReadOnly: true}, reason: channel.DenyReadOnly},
		{
			name: "khách chặn", st: channel.ConversationState{Purpose: channel.PurposeMessage, LastCustomerMessageAt: open, BlockedByUser: true},
			reason: channel.DenyBlockedByUser,
		},
		{name: "mục đích rỗng", st: channel.ConversationState{LastCustomerMessageAt: open}, reason: channel.DenyNoPermission},
		{name: "mục đích lạ", st: channel.ConversationState{Purpose: "broadcast", LastCustomerMessageAt: open}, reason: channel.DenyNoPermission},
	} {
		checkPolicy(t, c)
	}
}
