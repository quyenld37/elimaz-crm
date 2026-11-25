package facebook

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Channel là adapter kênh Facebook (khóa Key). Task 147 có phần vào (webhooks.Source: bước GET xác minh, chữ ký trên
// thân thô, tách gói); phần ra (Send, SendPolicy, Lookup, HealthCheck, Backfill, ClassifyError) là Task 148, khi đó
// Channel hiện thực đủ channel.MessagingAdapter và được nối vào channel.Registry. Tên không phải Adapter vì hàm nối
// Adapter(cfg) của Task 143 đã dùng tên đó (tệp task 147, Quyết định 6).
type Channel struct {
	appSecret   logging.Secret
	verifyToken logging.Secret
}

// New dựng adapter: appSecret (META_APP_SECRET) ký webhook; verifyToken (META_VERIFY_TOKEN) cho bước GET xác minh,
// rỗng ở vai trò không nhận webhook (mọi bước GET khi đó sai). Task 148 đổi thành New(o Options) khi có Graph client
// (tệp task 147, Quyết định 1).
func New(appSecret, verifyToken logging.Secret) *Channel {
	return &Channel{appSecret: appSecret, verifyToken: verifyToken}
}

// Features trả tính năng của Facebook: comment, comment live, nhắn riêng, ẩn comment, nút trả lời nhanh, thẻ Human Agent.
func (*Channel) Features() channel.Features {
	return channel.Features{
		Comments: true, LiveComments: true, PrivateReply: true, HideComment: true, QuickReplies: true, HumanAgent: true,
	}
}
