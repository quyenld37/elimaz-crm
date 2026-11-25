package facebook

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
)

// Key là khóa kênh Facebook (đường /hooks/facebook, cột channel_accounts.channel).
const Key = "facebook"

// Adapter trả adapter Facebook theo cấu hình, hoặc nil khi kênh chưa bật (cfg.Meta.Enabled sai) hay chưa hiện thực:
// NewRegistry bỏ nil. Khung của Task 143; Task 147 trả adapter thật.
func Adapter(_ config.Config) channel.MessagingAdapter { //nolint:ireturn // Registry nhận interface của kênh
	return nil
}
