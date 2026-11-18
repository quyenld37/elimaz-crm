// Package alertjobs nối luật cảnh báo và tin tổng kết của observability vào lịch chạy (lệnh của commands). Tách khỏi
// observability vì queue nhập observability (ghi lỗi của handler): observability không được nhập commands, queue.
package alertjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Lịch của cảnh báo (kế hoạch M0 bản 5, Task 19): cmd/crm đăng ký ở vai trò worker.
const (
	// AlertsSchedule chạy CheckRules mỗi 60 s.
	AlertsSchedule = "platform.alerts"
	AlertsEvery    = 60 * time.Second
	// DigestSchedule thử gửi tin tổng kết mỗi 15 phút; tin chỉ đi từ 08:00 giờ Việt Nam (01:00 UTC), một lần mỗi ngày.
	DigestSchedule = "platform.digest"
	DigestEvery    = 15 * time.Minute

	scheduleTimeout = 25 * time.Second
	reasonFailed    = "alert_failed"
)

// NewAlertsCommand trả lệnh chạy observability.CheckRules(d, a, o với Now = lúc chạy) cho lịch AlertsSchedule. Là lệnh (mode command,
// Runner gọi ngoài transaction) vì Notify gọi Telegram; handler transactional của hàng đợi không được gọi mạng.
// Lỗi được log ở đây (mép của việc nền) rồi trả not_sent: chạy lại trong hạn 60 s, quá hạn thì expired (kỳ sau chạy
// lại từ đầu); cảnh báo đã gửi không gửi lại nhờ chống lặp của Alerter. Đăng ký vào commands.NewRunner.
func NewAlertsCommand(d *db.DB, a observability.Alerter, o observability.RuleOptions, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return scheduled{name: AlertsSchedule, every: AlertsEvery, log: logger, run: func(ctx context.Context, now time.Time) (any, error) {
		o.Now = now
		keys, err := observability.CheckRules(ctx, d, a, o)
		if err != nil {
			return nil, fmt.Errorf("chạy luật cảnh báo: %w", err)
		}
		return map[string]any{"keys": keys}, nil
	}}
}

// NewDigestCommand trả lệnh của lịch DigestSchedule: trước 08:00 giờ Việt Nam không làm gì; sau đó SendDailyDigest
// (tự bỏ qua khi ngày đã có tin).
func NewDigestCommand(d *db.DB, a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return scheduled{name: DigestSchedule, every: DigestEvery, log: logger, run: func(ctx context.Context, now time.Time) (any, error) {
		if !observability.DigestDue(now) {
			return map[string]bool{"due": false}, nil
		}
		if err := observability.SendDailyDigest(ctx, d, a, now); err != nil {
			return nil, fmt.Errorf("gửi tin tổng kết: %w", err)
		}
		return map[string]bool{"due": true}, nil
	}}
}

type scheduled struct {
	name  string
	every time.Duration
	log   *slog.Logger
	run   func(ctx context.Context, now time.Time) (any, error)
}

// Kind: mode command, giữ chỗ hết hạn thì chạy lại (trùng cảnh báo ít hại hơn mất cảnh báo), hạn sống một kỳ.
func (s scheduled) Kind() queue.Kind {
	return queue.Kind{Name: "task." + s.name, Policy: queue.Policy{
		Mode: queue.ModeCommand, MaxAttempts: 3, OnLeaseExpired: queue.LeaseRetry,
		ExpiresAfter: s.every, Timeout: scheduleTimeout,
	}}
}

func (scheduled) AutoRetryNotSent() bool { return true }

// Execute: lỗi log một lần (không chứa dữ liệu khách: chỉ tên luật, lỗi database, mã Telegram) rồi not_sent với lý
// do cố định; Telegram 429 → chờ đúng thời gian Telegram yêu cầu.
func (s scheduled) Execute(ctx context.Context, _ queue.Job) (commands.Outcome, error) {
	res, err := s.run(ctx, time.Now())
	if err != nil {
		if s.log != nil {
			s.log.ErrorContext(ctx, "lịch "+s.name+" lỗi", logging.Err(err))
		}
		out := commands.Outcome{Status: commands.StatusNotSent, Reason: reasonFailed}
		if re, ok := errors.AsType[*observability.TelegramRetryError](err); ok {
			out.RetryAfter = re.After
		}
		return out, nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả %s: %w", s.name, err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}
