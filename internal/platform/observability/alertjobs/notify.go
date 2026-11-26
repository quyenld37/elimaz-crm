package alertjobs

import (
	"context"
	"errors"
	"log/slog"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ReasonAlerterMissing là lý do rejected khi tiến trình không có Alerter.
const ReasonAlerterMissing = "alerter_missing"

// Notify gửi một cảnh báo (key chống lặp, severity, text chỉ mang mã bản ghi) từ Execute của một lệnh cảnh báo do
// module ghi trong transaction nghiệp vụ, và trả kết quả lệnh: a nil → rejected alerter_missing (không gửi lại);
// Notify lỗi → log một lần ở đây (logger nil: không log) rồi not_sent alert_failed (lệnh có AutoRetryNotSent tự gửi
// lại, Alerter chống lặp theo key; Telegram 429 → RetryAfter đúng thời gian Telegram yêu cầu); gửi được → done.
//
// Khi nào không dùng: lệnh cần đọc database để dựng văn bản thì đọc trước rồi mới gọi; không gọi trong transaction.
func Notify(ctx context.Context, a observability.Alerter, logger *slog.Logger, key, severity, text string) commands.Outcome {
	if a == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: ReasonAlerterMissing}
	}
	if err := a.Notify(ctx, key, severity, text); err != nil {
		if logger != nil {
			logger.WarnContext(ctx, "gửi cảnh báo lỗi", slog.String("key", key), slog.String("error", queue.ErrorText(err)))
		}
		out := commands.Outcome{Status: commands.StatusNotSent, Reason: reasonFailed}
		if re, ok := errors.AsType[*observability.TelegramRetryError](err); ok {
			out.RetryAfter = re.After // Telegram 429: chờ đúng thời gian Telegram yêu cầu
		}
		return out
	}
	return commands.Outcome{Status: commands.StatusDone}
}
