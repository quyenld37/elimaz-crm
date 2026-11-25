package fake

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
)

// Key là khóa của kênh giả: fakechat, không fake (fake là khóa hãng vận chuyển giả của M3, nguồn webhook /hooks/fake;
// tệp task 143, Quyết định 6).
const Key = "fakechat"

// Adapter trả adapter kênh giả, hoặc nil (NewRegistry bỏ nil). Khung của Task 143 luôn trả nil; Task 145 trả adapter
// thật khi cfg.Env khác production (production luôn nil).
func Adapter(_ config.Config) channel.MessagingAdapter { //nolint:ireturn // Registry nhận interface của kênh
	return nil
}
