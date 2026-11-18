package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Cảnh báo "thiếu so với phần đang giữ" sau khi áp dụng kiểm kho (kế hoạch M1, Task M1-35; ADR-46 điểm 3): ApplyStocktake
// ghi lệnh ShortageAlertKind trong transaction áp dụng; lệnh chạy ngoài transaction và gọi Notifier.Notify. Văn bản chỉ
// có mã phiên và số mã hàng (không tên mẫu, không dữ liệu khách).

// ShortageAlertKind là tên loại lệnh cảnh báo thiếu hàng sau kiểm kho.
const ShortageAlertKind = "inventory.shortage_alert"

const (
	shortageAlertTimeout = 10 * time.Second
	severityNormal       = "normal"
	reasonNotifyFailed   = "alert_failed"
)

// Notifier gửi một cảnh báo vận hành (observability.Alerter thỏa interface này; cmd/crm nối).
type Notifier interface {
	Notify(ctx context.Context, key, severity, text string) error
}

type shortagePayload struct {
	StocktakeID uuid.UUID `json:"stocktakeId"`
	Code        string    `json:"code"`
	Count       int       `json:"count"`
}

func shortageAlertKind() queue.Kind {
	return commands.CommandKind(ShortageAlertKind, 0, shortageAlertTimeout)
}

// NewShortageAlertCommand trả lệnh của ShortageAlertKind: Notify(key "stock-shortage:<id phiên>", normal,
// "Kiểm kho <mã>: <n> mã hàng thiếu so với phần đang giữ"). Notify lỗi → log một lần ở đây (mép của việc nền; logger
// nil → không log), not_sent và tự gửi lại (trùng tin ít hại hơn mất cảnh báo; Alerter chống lặp theo khóa); Telegram
// 429 → chờ đúng thời gian Telegram yêu cầu. Đăng ký vào commands.NewRunner.
func NewShortageAlertCommand(n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return shortageAlert{n: n, log: logger}
}

type shortageAlert struct {
	n   Notifier
	log *slog.Logger
}

func (shortageAlert) Kind() queue.Kind { return shortageAlertKind() }

func (shortageAlert) AutoRetryNotSent() bool { return true }

func (s shortageAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p shortagePayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: "bad_payload"}, nil
	}
	text := fmt.Sprintf("Kiểm kho %s: %d mã hàng thiếu so với phần đang giữ", p.Code, p.Count)
	return notify(ctx, s.n, s.log, "stock-shortage:"+p.StocktakeID.String(), text,
		"gửi cảnh báo thiếu hàng sau kiểm kho lỗi"), nil
}

// notify gửi một cảnh báo normal và đổi kết quả thành Outcome của lệnh: lỗi → log logMsg một lần (mép của việc nền;
// logger nil → không log), not_sent (Handler có AutoRetryNotSent); Telegram 429 → chờ đúng thời gian Telegram yêu cầu.
func notify(ctx context.Context, n Notifier, logger *slog.Logger, key, text, logMsg string) commands.Outcome {
	if err := n.Notify(ctx, key, severityNormal, text); err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, logMsg, logging.Err(err))
		}
		out := commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNotifyFailed}
		if re, ok := errors.AsType[*observability.TelegramRetryError](err); ok {
			out.RetryAfter = re.After
		}
		return out
	}
	return commands.Outcome{Status: commands.StatusDone}
}
