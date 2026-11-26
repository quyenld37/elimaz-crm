package fake

import (
	"context"
	"strconv"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// Cửa sổ của kênh giả (đơn giản hóa của Messenger): tin thường trong 24 giờ sau tin cuối của khách; thẻ nhân viên
// (khi Page bật và không phải tự động) tới 7 ngày; nhắn riêng một lần trong 7 ngày sau comment.
const (
	standardWindow     = 24 * time.Hour
	humanAgentWindow   = 7 * 24 * time.Hour
	privateReplyWindow = 7 * 24 * time.Hour
)

// Hành vi gửi của SetSendBehavior (cùng chuỗi với channeltest).
const (
	behaviorOK            = "ok"
	behaviorRejectPolicy  = "reject_policy"
	behaviorFailBefore    = "fail_before_send"
	behaviorTimeoutAfter  = "timeout_after_send"
	policyRejectCode      = 10
	privateReplyRemaining = 1
)

// SendPolicy theo cửa sổ của kênh giả (không gọi mạng).
func (*Chat) SendPolicy(c channel.ConversationState, now time.Time) channel.SendPolicy {
	deny := func(r channel.DenyReason) channel.SendPolicy { return channel.SendPolicy{Reason: r} }
	switch {
	case c.ReadOnly:
		return deny(channel.DenyReadOnly)
	case c.BlockedByUser:
		return deny(channel.DenyBlockedByUser)
	}
	switch c.Purpose {
	case channel.PurposePrivateReply:
		if c.PrivateReplyUsed {
			return deny(channel.DenyPrivateReplyUsed)
		}
		if c.CommentCreatedAt == nil || now.Sub(*c.CommentCreatedAt) > privateReplyWindow {
			return deny(channel.DenyPrivateReplyExpired)
		}
		until, left := c.CommentCreatedAt.Add(privateReplyWindow), privateReplyRemaining
		return channel.SendPolicy{Allowed: true, Mode: channel.SendPrivateReply, Until: &until, Remaining: &left}
	case channel.PurposeCommentReply, channel.PurposeHide:
		if c.CommentRemoved {
			return deny(channel.DenyCommentRemoved)
		}
		return channel.SendPolicy{Allowed: true, Mode: channel.SendCommentReply}
	case channel.PurposeMessage:
	}
	if c.LastCustomerMessageAt == nil {
		return deny(channel.DenyWindowClosed)
	}
	if until := c.LastCustomerMessageAt.Add(standardWindow); now.Before(until) {
		return channel.SendPolicy{Allowed: true, Mode: channel.SendStandard, Until: &until}
	}
	if until := c.LastCustomerMessageAt.Add(humanAgentWindow); c.HumanAgentEnabled && !c.Automated && now.Before(until) {
		return channel.SendPolicy{Allowed: true, Mode: channel.SendHumanAgent, Until: &until}
	}
	return deny(channel.DenyWindowClosed)
}

// SetSendBehavior đặt kết quả của các lần Send sau: ok, reject_policy, fail_before_send, timeout_after_send.
func (a *Chat) SetSendBehavior(b string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.behavior = b
}

// Sent trả bản sao các lần kênh giả đã nhận, theo thứ tự.
func (a *Chat) Sent() []SentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]SentMessage(nil), a.sent...)
}

// record ghi một lần kênh nhận cmd; trả mã tin của kênh.
func (a *Chat) record(cmd channel.OutboundCommand) string {
	id := "fakechat-" + strconv.Itoa(len(a.sent)+1)
	a.sent = append(a.sent, SentMessage{ExternalID: id, Command: cmd, At: a.now().UTC()})
	return id
}

// Send mô phỏng một lần gửi theo hành vi đang đặt: ok → sent (ghi bộ nhớ); reject_policy → rejected window_closed;
// fail_before_send → not_sent (không ghi); timeout_after_send → unknown (kênh đã nhận nhưng CRM không biết). Không có
// token → rejected no_permission.
func (a *Chat) Send(_ context.Context, _ channel.ChannelAccount, cred channel.Credentials, cmd channel.OutboundCommand) (channel.SendOutcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cred.AccessToken == "" {
		return channel.SendOutcome{Status: channel.OutcomeRejected, Reason: channel.DenyNoPermission}, nil
	}
	switch a.behavior {
	case behaviorRejectPolicy:
		return channel.SendOutcome{Status: channel.OutcomeRejected, Reason: channel.DenyWindowClosed, Code: policyRejectCode}, nil
	case behaviorFailBefore:
		return channel.SendOutcome{Status: channel.OutcomeNotSent, Detail: "fail_before_send"}, nil
	case behaviorTimeoutAfter:
		a.record(cmd)
		return channel.SendOutcome{Status: channel.OutcomeUnknown, Detail: "timeout_after_send"}, nil
	}
	return channel.SendOutcome{Status: channel.OutcomeSent, ExternalID: a.record(cmd), RecipientID: cmd.RecipientID}, nil
}

// Lookup tìm trong bộ nhớ lần gửi cùng mục đích, cùng comment (nếu có), cùng chữ, từ q.After: thấy → sent kèm mã tin;
// không → not_sent.
func (a *Chat) Lookup(_ context.Context, _ channel.ChannelAccount, _ channel.Credentials, q channel.LookupQuery) (channel.LookupResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.sent {
		if s.Command.Purpose == q.Purpose && s.Command.CommentID == q.CommentID && s.Command.Text == q.Text && !s.At.Before(q.After) {
			return channel.LookupResult{Status: channel.OutcomeSent, ExternalID: s.ExternalID}, nil
		}
	}
	return channel.LookupResult{Status: channel.OutcomeNotSent}, nil
}
