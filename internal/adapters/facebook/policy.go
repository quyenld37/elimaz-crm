package facebook

import (
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// Các cửa sổ gửi của Messenger (spec 02 mục 3): tin thường trong 24 giờ sau tin cuối của khách; thẻ HUMAN_AGENT tới 7
// ngày, chỉ cho tin người gõ; nhắn riêng một tin trong 7 ngày sau comment.
const (
	standardWindow     = 24 * time.Hour
	humanAgentWindow   = 7 * 24 * time.Hour
	privateReplyWindow = 7 * 24 * time.Hour
	privateReplyLimit  = 1
)

// SendPolicy quyết định có gửi được không theo trạng thái hội thoại c lúc now (không gọi mạng). Mọi giới hạn chính
// sách của Meta khóa ở đây; mốc cửa sổ là mốc hết (đúng 24 giờ, đúng 7 ngày là quá). Thứ tự: chế độ chỉ xem, khách
// chặn, rồi luật theo mục đích; mục đích lạ → từ chối (mặc định từ chối).
func (*Channel) SendPolicy(c channel.ConversationState, now time.Time) channel.SendPolicy {
	switch {
	case c.ReadOnly:
		return deny(channel.DenyReadOnly)
	case c.BlockedByUser:
		return deny(channel.DenyBlockedByUser)
	}
	switch c.Purpose {
	case channel.PurposeMessage:
		return messagePolicy(c, now)
	case channel.PurposePrivateReply:
		if c.PrivateReplyUsed {
			return deny(channel.DenyPrivateReplyUsed)
		}
		if c.CommentCreatedAt == nil {
			return deny(channel.DenyPrivateReplyExpired)
		}
		until := c.CommentCreatedAt.Add(privateReplyWindow)
		if !now.Before(until) {
			return deny(channel.DenyPrivateReplyExpired)
		}
		left := privateReplyLimit
		return channel.SendPolicy{Allowed: true, Mode: channel.SendPrivateReply, Until: &until, Remaining: &left}
	case channel.PurposeCommentReply:
		if c.CommentRemoved {
			return deny(channel.DenyCommentRemoved)
		}
		return channel.SendPolicy{Allowed: true, Mode: channel.SendCommentReply}
	case channel.PurposeHide:
		return channel.SendPolicy{Allowed: true, Mode: channel.SendCommentReply}
	}
	return deny(channel.DenyNoPermission)
}

// messagePolicy: trong 24 giờ sau tin cuối của khách → standard; quá 24 giờ, Page bật Human Agent, tin người gõ (không
// tự động), trong 7 ngày → human_agent; còn lại (kể cả khách chưa từng nhắn) → window_closed.
func messagePolicy(c channel.ConversationState, now time.Time) channel.SendPolicy {
	last := c.LastCustomerMessageAt
	if last == nil {
		return deny(channel.DenyWindowClosed)
	}
	switch age := now.Sub(*last); {
	case age < standardWindow:
		return allow(channel.SendStandard, last.Add(standardWindow))
	case age < humanAgentWindow && c.HumanAgentEnabled && !c.Automated:
		return allow(channel.SendHumanAgent, last.Add(humanAgentWindow))
	}
	return deny(channel.DenyWindowClosed)
}

func allow(mode channel.SendMode, until time.Time) channel.SendPolicy {
	return channel.SendPolicy{Allowed: true, Mode: mode, Until: &until}
}

func deny(r channel.DenyReason) channel.SendPolicy { return channel.SendPolicy{Reason: r} }
