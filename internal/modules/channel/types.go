package channel

import (
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// Features là các tính năng một kênh hỗ trợ; màn hình và quy tắc tự động ẩn phần kênh không có.
type Features struct {
	Comments, LiveComments, PrivateReply, HideComment, QuickReplies, HumanAgent bool
}

// ChannelAccount là một tài khoản kênh (một Page) của shop; không bao giờ mang token.
//
//nolint:revive // tên theo Interfaces của kế hoạch M4 (Task 143): channel.ChannelAccount là tên chung của 04 mục 4
type ChannelAccount struct {
	ID, TenantID, SellerID    uuid.UUID
	Channel, ExternalID, Name string
	Status, SendMode          string
	HumanAgentEnabled         bool
	BlockedUntil              *time.Time
	Version                   int32
	// StatusReason là lý do của trạng thái degraded, disconnected (rỗng khi active).
	StatusReason string
	// TokenKind: page_user, system_user, rỗng khi chưa có token.
	TokenKind        string
	SubscribedFields []string
	// TokenCheckedAt, LastWebhookAt, LastFeedAt: mốc kiểm token, mốc webhook gần nhất (mọi loại, feed) — Task 150 dùng.
	TokenCheckedAt, LastWebhookAt, LastFeedAt *time.Time
}

// Credentials là token của tài khoản kênh, chỉ giải mã ngay trước khi gọi kênh; kiểu Secret tự che khi ghi log.
type Credentials struct {
	AccessToken logging.Secret
}

// Purpose là mục đích của một lần gửi ra kênh.
type Purpose string

// Mục đích gửi.
const (
	PurposeMessage      Purpose = "message"
	PurposePrivateReply Purpose = "private_reply"
	PurposeCommentReply Purpose = "comment_reply"
	PurposeHide         Purpose = "hide"
)

// ConversationState là trạng thái của hội thoại mà SendPolicy cần để quyết định có gửi được không.
type ConversationState struct {
	Purpose                                         Purpose
	Kind                                            string
	LastCustomerMessageAt, CommentCreatedAt         *time.Time
	PrivateReplyUsed, CommentRemoved, BlockedByUser bool
	ReadOnly, HumanAgentEnabled, Automated          bool
}

// SendMode là cách gửi kênh cho phép (thẻ tin của Meta…).
type SendMode string

// Cách gửi.
const (
	SendStandard        SendMode = "standard"
	SendHumanAgent      SendMode = "human_agent"
	SendPrivateReply    SendMode = "private_reply"
	SendCommentReply    SendMode = "comment_reply"
	SendUtilityTemplate SendMode = "utility_template"
)

// DenyReason là lý do chính sách gửi không cho gửi.
type DenyReason string

// Lý do không cho gửi.
const (
	DenyWindowClosed        DenyReason = "window_closed"
	DenyPrivateReplyUsed    DenyReason = "private_reply_used"
	DenyPrivateReplyExpired DenyReason = "private_reply_expired"
	DenyBlockedByUser       DenyReason = "blocked_by_user"
	DenyNoPermission        DenyReason = "no_permission"
	DenyReadOnly            DenyReason = "read_only"
	DenyCommentRemoved      DenyReason = "comment_removed"
)

// SendPolicy là câu trả lời của adapter: có gửi được không, gửi kiểu gì, tới lúc nào, còn mấy lần.
type SendPolicy struct {
	Allowed   bool
	Mode      SendMode
	Until     *time.Time
	Remaining *int
	Reason    DenyReason
}

// QuickReply là một nút trả lời nhanh kèm tin.
type QuickReply struct {
	Title, Payload string
}

// OutboundCommand là một lần gửi ra kênh (tin, trả lời comment, nhắn riêng, ẩn/hiện comment).
type OutboundCommand struct {
	Purpose                Purpose
	RecipientID, CommentID string
	Text                   string
	ImageURL               string
	QuickReplies           []QuickReply
	Mode                   SendMode
	// ClientRef là mã của CRM gắn vào tin (metadata) để khớp echo với lần gửi (Q23).
	ClientRef string
	// Hidden: ẩn (true) hay hiện (false) comment khi Purpose là hide.
	Hidden *bool
}

// OutcomeStatus là kết quả một lần gửi: đã gửi, bị từ chối, chắc chắn chưa gửi, không rõ.
type OutcomeStatus string

// Kết quả gửi.
const (
	OutcomeSent     OutcomeStatus = "sent"
	OutcomeRejected OutcomeStatus = "rejected"
	OutcomeNotSent  OutcomeStatus = "not_sent"
	OutcomeUnknown  OutcomeStatus = "unknown"
)

// SendOutcome là kết quả chi tiết của Send. Detail đã lọc dữ liệu cá nhân.
type SendOutcome struct {
	Status                  OutcomeStatus
	ExternalID, RecipientID string
	Reason                  DenyReason
	Code                    int
	RetryAfter              time.Duration
	Detail                  string
}

// LookupQuery hỏi kênh một lần gửi "không rõ" đã tới chưa.
type LookupQuery struct {
	Purpose         Purpose
	CommentID, Text string
	After           time.Time
	WantHidden      *bool
}

// LookupResult là câu trả lời của Lookup.
type LookupResult struct {
	Status     OutcomeStatus
	ExternalID string
}

// ErrorClass là lớp của lỗi từ kênh (quyết định thử lại, cảnh báo).
type ErrorClass string

// Lớp lỗi.
const (
	ErrorNotSentRetry ErrorClass = "not_sent_retry"
	ErrorRateLimited  ErrorClass = "rate_limited"
	ErrorAuth         ErrorClass = "auth"
	ErrorPolicy       ErrorClass = "policy"
	ErrorPermanent    ErrorClass = "permanent"
	ErrorUnknown      ErrorClass = "unknown"
)

// Health là kết quả kiểm sức khỏe tài khoản kênh.
type Health struct {
	TokenValid, PermissionsOK, WebhookSubscribed bool
	MissingPermissions, MissingFields            []string
}
