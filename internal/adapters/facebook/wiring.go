package facebook

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Key là khóa kênh Facebook (đường /hooks/facebook, cột channel_accounts.channel).
const Key = "facebook"

// Adapter trả adapter Facebook để đăng ký vào channel.Registry, hoặc nil (NewRegistry bỏ nil). Còn nil dù adapter đã
// đủ (fromConfig): đăng ký thì ingest nhận webhook Facebook và worker tách ra việc inbound.facebook khi chưa có handler
// (Task 153), việc mồ côi dồn ở hàng chờ. Task 153 đổi thành `return fromConfig(cfg)` (tệp task 148, Quyết định 1).
func Adapter(_ config.Config) channel.MessagingAdapter { //nolint:ireturn // Registry nhận interface của kênh
	return nil
}

// Source trả adapter Facebook (dùng như nguồn webhook /hooks/facebook) khi kênh Meta bật và có META_APP_SECRET; không
// thì nil thật (không phải nil có kiểu). Chưa được cmd nối riêng: nguồn vào ingest và worker theo channel.Registry qua
// Adapter.
func Source(cfg config.Config) webhooks.Source { //nolint:ireturn // nguồn webhook là interface của nền tảng
	if c := fromConfig(cfg); c != nil {
		return c
	}
	return nil
}

// fromConfig dựng adapter đầy đủ từ cfg.Meta; nil khi kênh Meta tắt hay thiếu app secret.
func fromConfig(cfg config.Config) *Channel {
	m := cfg.Meta
	if !m.Enabled || m.AppSecret == "" {
		return nil
	}
	return New(Options{
		BaseURL: m.GraphBaseURL, StreamingBaseURL: m.StreamingBaseURL, Version: m.GraphVersion, AppID: m.AppID,
		AppSecret: m.AppSecret, VerifyToken: m.VerifyToken, AttachmentHosts: m.AttachmentHosts,
		OAuthRedirectURL: m.OAuthRedirectURL,
	})
}
