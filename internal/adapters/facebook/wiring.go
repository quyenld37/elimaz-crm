package facebook

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Key là khóa kênh Facebook (đường /hooks/facebook, cột channel_accounts.channel).
const Key = "facebook"

// Adapter trả adapter Facebook theo cấu hình, hoặc nil khi kênh chưa bật (cfg.Meta.Enabled sai) hay chưa hiện thực:
// NewRegistry bỏ nil. Còn nil tới khi Task 148 thêm phần ra (Channel chưa đủ channel.MessagingAdapter); Task 148 trả
// adapter dựng như Source (tệp task 147, Quyết định 2).
func Adapter(_ config.Config) channel.MessagingAdapter { //nolint:ireturn // Registry nhận interface của kênh
	return nil
}

// Source trả phần vào của adapter Facebook (nguồn webhook /hooks/facebook) khi kênh Meta bật và có META_APP_SECRET;
// không thì nil thật (không phải nil có kiểu). Chưa được cmd nối riêng: nguồn vào ingest và worker theo
// channel.Registry qua Adapter (Task 148), để không tách ra việc inbound.facebook khi chưa có handler (Task 153).
func Source(cfg config.Config) webhooks.Source { //nolint:ireturn // nguồn webhook là interface của nền tảng
	if !cfg.Meta.Enabled || cfg.Meta.AppSecret == "" {
		return nil
	}
	return New(cfg.Meta.AppSecret, cfg.Meta.VerifyToken)
}
