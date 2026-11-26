package facebook

import (
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// Channel là adapter kênh Facebook (khóa Key), hiện thực đủ channel.MessagingAdapter cùng các interface tùy chọn
// ProfileFetcher, PostFetcher, HistoryFetcher, AttachmentDownloader, Connector, CommentPoller (Task 150). Phần vào (webhooks.Source: bước GET
// xác minh, chữ ký trên thân thô, tách gói) là Task 147; phần ra (SendPolicy, Send, Lookup, HealthCheck, Backfill, đọc
// hồ sơ, bài, lịch sử, kết nối Page) là Task 148, mọi lời gọi Meta đi qua Graph client. Tên không phải Adapter vì hàm
// nối Adapter(cfg) của Task 143 đã dùng tên đó (tệp task 147, Quyết định 6). Dùng được từ nhiều goroutine.
type Channel struct {
	o      Options
	client *Client
	// backfillGap là khoảng tối thiểu giữa hai lời gọi Graph trong một lần hỏi bù (tối đa 2 lời gọi mỗi giây mỗi Page).
	backfillGap time.Duration
}

// defaultBackfillGap: 2 lời gọi mỗi giây mỗi Page (kế hoạch M4, Task 148).
const defaultBackfillGap = 500 * time.Millisecond

// New dựng adapter từ o: AppSecret (META_APP_SECRET) ký webhook và tạo appsecret_proof; VerifyToken
// (META_VERIFY_TOKEN) cho bước GET xác minh, rỗng ở vai trò không nhận webhook (mọi bước GET khi đó sai); phần còn lại
// cấu hình Graph client (NewClient). Phần vào không gọi mạng nên dựng được chỉ với hai bí mật.
func New(o Options) *Channel {
	c := NewClient(o)
	return &Channel{o: c.o, client: c, backfillGap: defaultBackfillGap}
}

// Features trả tính năng của Facebook: comment, comment live, nhắn riêng, ẩn comment, nút trả lời nhanh, thẻ Human Agent.
func (*Channel) Features() channel.Features {
	return channel.Features{
		Comments: true, LiveComments: true, PrivateReply: true, HideComment: true, QuickReplies: true, HumanAgent: true,
	}
}

// ClassifyError xếp lỗi của adapter vào một lớp của channel (Classify).
func (*Channel) ClassifyError(err error) channel.ErrorClass { return Classify(err) }
