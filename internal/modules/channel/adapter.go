package channel

import (
	"context"
	"io"
	"iter"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// MessagingAdapter là phần mỗi kênh nhắn tin hiện thực (spec 04 mục 4). Phần "Vào" là webhooks.Source của M0 (Key,
// Challenge, VerifySignature, Split) được nhúng nguyên: ingest dùng adapter như một nguồn webhook, và gói này không
// phải nhập net/http (module nghiệp vụ không biết HTTP). Mọi giới hạn chính sách của kênh nằm trong SendPolicy; API và
// worker đều hỏi lại trước khi gửi. Hàm gọi mạng nhận ctx, không bao giờ chạy trong transaction.
type MessagingAdapter interface {
	webhooks.Source
	// Features là tính năng kênh hỗ trợ.
	Features() Features
	// Backfill đọc lại sự kiện từ since (hỏi bù khi webhook im lặng).
	Backfill(ctx context.Context, acc ChannelAccount, cred Credentials, since time.Time) iter.Seq2[webhooks.InboundEvent, error]
	// SendPolicy quyết định có gửi được không theo trạng thái hội thoại lúc now (không gọi mạng).
	SendPolicy(c ConversationState, now time.Time) SendPolicy
	// Send gửi một lệnh ra kênh.
	Send(ctx context.Context, acc ChannelAccount, cred Credentials, cmd OutboundCommand) (SendOutcome, error)
	// Lookup hỏi kênh lần gửi "không rõ" đã tới chưa.
	Lookup(ctx context.Context, acc ChannelAccount, cred Credentials, q LookupQuery) (LookupResult, error)
	// HealthCheck kiểm token, quyền, đăng ký webhook của tài khoản.
	HealthCheck(ctx context.Context, acc ChannelAccount, cred Credentials) (Health, error)
	// ClassifyError xếp lỗi của adapter vào một lớp.
	ClassifyError(err error) ErrorClass
}

// Profile là hồ sơ người dùng kênh (tên, ảnh đại diện) khi kênh cho đọc.
type Profile struct {
	Name, AvatarURL string
}

// PostInfo là thông tin bài viết chứa comment.
type PostInfo struct {
	ID, Kind, Permalink, Snippet string
	CreatedTime                  time.Time
}

// DownloadInfo là thông tin tệp tải từ kênh: ContentType nhận dạng bằng nội dung (không tin header), SHA256 là hex
// của nội dung.
type DownloadInfo struct {
	ContentType string
	Size        int64
	FileName    string
	SHA256      string
}

// LiveVideo là một phiên phát trực tiếp kênh báo đang chạy.
type LiveVideo struct {
	LiveVideoID, VideoID, PostID, Status, Title string
	StartedAt                                   time.Time
}

// PageInfo là một Page người dùng quản trị (khi kết nối).
type PageInfo struct {
	PageID, Name            string
	CanMessage, CanModerate bool
}

// CommentStream là luồng comment live; Next chặn tới khi có lô mới, ctx bị hủy hoặc luồng đóng (io.EOF).
type CommentStream interface {
	Next(ctx context.Context) ([]webhooks.InboundEvent, error)
	io.Closer
}

// ProfileFetcher (tùy chọn) đọc hồ sơ người dùng kênh.
type ProfileFetcher interface {
	FetchProfile(ctx context.Context, acc ChannelAccount, cred Credentials, userID string) (Profile, error)
}

// PostFetcher (tùy chọn) đọc bài viết.
type PostFetcher interface {
	FetchPost(ctx context.Context, acc ChannelAccount, cred Credentials, postID string) (PostInfo, error)
}

// HistoryFetcher (tùy chọn) đọc tối đa limit tin gần nhất của một người dùng (Q33).
type HistoryFetcher interface {
	FetchHistory(ctx context.Context, acc ChannelAccount, cred Credentials, userID string, limit int) ([]webhooks.InboundEvent, error)
}

// CommentPoller (tùy chọn) đọc comment mới hơn since trên các bài gần nhất của Page (đọc comment định kỳ khi webhook
// feed không về, Q4); khóa chống trùng như webhook. Bên gọi ghi bằng webhooks.EnqueueInbound với via = comment_poll.
type CommentPoller interface {
	PollPageComments(ctx context.Context, acc ChannelAccount, cred Credentials, since time.Time) ([]webhooks.InboundEvent, error)
}

// AttachmentDownloader (tùy chọn) tải tệp đính kèm, chỉ từ máy chủ của kênh, tối đa max byte.
type AttachmentDownloader interface {
	Download(ctx context.Context, rawURL string, maxBytes int64) (io.ReadCloser, DownloadInfo, error)
}

// LiveSource (tùy chọn) nhận biết phiên live và đọc comment live.
type LiveSource interface {
	DetectLive(ctx context.Context, acc ChannelAccount, cred Credentials) ([]LiveVideo, error)
	LiveStatus(ctx context.Context, acc ChannelAccount, cred Credentials, videoID string) (string, error)
	StreamComments(ctx context.Context, acc ChannelAccount, cred Credentials, videoID string, since time.Time) (CommentStream, error)
	PollComments(ctx context.Context, acc ChannelAccount, cred Credentials, videoID string, since time.Time) ([]webhooks.InboundEvent, error)
	CountComments(ctx context.Context, acc ChannelAccount, cred Credentials, videoID string) (int, error)
}

// Connector (tùy chọn) kết nối Page: đổi mã đăng nhập, liệt kê Page, lấy token Page, đăng ký và hủy webhook.
type Connector interface {
	ExchangeCode(ctx context.Context, code string) (logging.Secret, error)
	ListPages(ctx context.Context, userToken logging.Secret) ([]PageInfo, error)
	PageToken(ctx context.Context, token logging.Secret, pageID string) (logging.Secret, PageInfo, error)
	Subscribe(ctx context.Context, acc ChannelAccount, cred Credentials, fields []string) error
	Unsubscribe(ctx context.Context, acc ChannelAccount, cred Credentials) error
}
