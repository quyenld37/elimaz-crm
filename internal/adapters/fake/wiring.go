package fake

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
)

// Key là khóa của kênh giả: fakechat, không fake (fake là khóa hãng vận chuyển giả của M3, nguồn webhook /hooks/fake;
// tệp task 143, Quyết định 6).
const Key = "fakechat"

// Adapter trả kênh giả khi cfg.Env khác production (bí mật webhook FAKECHAT_WEBHOOK_SECRET; rỗng thì mọi webhook bị
// từ chối), production luôn nil (NewRegistry bỏ nil).
func Adapter(cfg config.Config) channel.MessagingAdapter { //nolint:ireturn // Registry nhận interface của kênh
	if cfg.Env == "production" {
		return nil
	}
	return New(string(cfg.FakeChatWebhookSecret))
}
