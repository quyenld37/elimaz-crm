package channel

import "time"

// Loại sự kiện vào đã chuẩn hóa (webhooks.InboundEvent.Type) mọi adapter nhắn tin trả; Payload là JSON của kiểu
// tương ứng dưới đây.
const (
	EventMessageReceived   = "message.received"
	EventMessageEcho       = "message.echo"
	EventMessageDelivered  = "message.delivered"
	EventMessageRead       = "message.read"
	EventMessageReaction   = "message.reaction"
	EventMessageEdited     = "message.edited"
	EventMessageReferral   = "message.referral"
	EventCommentChanged    = "comment.changed"
	EventLiveStatus        = "live.status"
	EventPolicyEnforcement = "policy.enforcement"
)

// MessagePayload là tin nhắn vào (khách gửi) hay echo (Page gửi, IsEcho).
type MessagePayload struct {
	PageID, UserID, MID, Text, QuickReplyPayload, PostbackPayload, ReplyToMID, Metadata, EchoAppID string
	IsEcho                                                                                         bool
	Attachments                                                                                    []AttachmentRef
	SentAt                                                                                         time.Time
}

// AttachmentRef là tệp đính kèm của tin; Type là image, video, audio, file, sticker hoặc fallback.
type AttachmentRef struct {
	Type, URL, StickerID, Name string
}

// WatermarkPayload là "đã nhận" hay "đã xem" tới mốc Watermark.
type WatermarkPayload struct {
	PageID, UserID string
	MIDs           []string
	Watermark      time.Time
}

// ReactionPayload là thả, bỏ cảm xúc trên một tin.
type ReactionPayload struct {
	PageID, UserID, MID, Reaction, Action string
}

// EditPayload là khách sửa tin.
type EditPayload struct {
	PageID, UserID, MID, Text string
	EditedAt                  time.Time
}

// CommentPayload là comment thêm, sửa, xóa, ẩn, hiện (Verb: add, edited, remove, hide, unhide).
type CommentPayload struct {
	PageID, PostID, CommentID, ParentID, FromID, FromName, Text, PhotoURL, Verb string
	FromPage, IsHidden                                                          bool
	CreatedAt                                                                   time.Time
}

// LiveStatusPayload là phiên live bắt đầu, kết thúc.
type LiveStatusPayload struct {
	PageID, LiveVideoID, VideoID, Status, Title string
	At                                          time.Time
}

// PolicyPayload là kênh áp chế tài chính sách lên Page.
type PolicyPayload struct {
	PageID, Action, Reason string
}
