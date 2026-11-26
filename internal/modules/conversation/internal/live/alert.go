package live

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/observability/alertjobs"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// AlertCommandName là lệnh cảnh báo của phiên live (mức thường), ghi trong transaction nghiệp vụ; worker gửi qua
// Alerter sau commit: live-source:<phiên> (luồng hỏng, đã chuyển sang hỏi định kỳ) và live-missing:<phiên> (đối chiếu
// cuối phiên thiếu comment, kèm hai con số). Văn bản chỉ có mã phiên và con số, không nội dung comment.
const AlertCommandName = "conversation.live_alert"

const (
	alertTimeout     = 10 * time.Second
	alertSource      = "source"
	alertMissing     = "missing"
	reasonBadPayload = "bad_payload"
)

type alertPayload struct {
	Kind      string    `json:"kind"`
	SessionID uuid.UUID `json:"sessionId"`
	Got       int       `json:"got,omitempty"`
	Meta      int       `json:"meta,omitempty"`
}

func alertKind() queue.Kind { return commands.CommandKind(AlertCommandName, 0, alertTimeout) }

// enqueueAlert ghi lệnh cảnh báo p của tenant trong tx (dedupe: khóa chống trùng).
func enqueueAlert(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenant uuid.UUID, p alertPayload, dedupe string) error {
	if q == nil {
		q = queue.NewQueue()
	}
	if _, _, err := q.Enqueue(ctx, tx, alertKind(), queue.NewJob{TenantID: &tenant, Payload: p, DedupeKey: dedupe}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo phiên live: %w", err)
	}
	return nil
}

// AlertCommand là lệnh AlertCommandName.
type AlertCommand struct {
	a   observability.Alerter
	log *slog.Logger
}

// NewAlertCommand dựng lệnh cảnh báo phiên live; cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewAlertCommand(a observability.Alerter, logger *slog.Logger) *AlertCommand {
	return &AlertCommand{a: a, log: logger}
}

// Kind là loại lệnh.
func (*AlertCommand) Kind() queue.Kind { return alertKind() }

// AutoRetryNotSent trả true: Alerter chống lặp theo key.
func (*AlertCommand) AutoRetryNotSent() bool { return true }

// Execute gửi cảnh báo qua alertjobs.Notify (Alerter nil → rejected; lỗi → not_sent, tự gửi lại).
func (c *AlertCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p alertPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.SessionID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	var key, text string
	switch p.Kind {
	case alertSource:
		key = "live-source:" + p.SessionID.String()
		text = fmt.Sprintf("Phiên live %s: luồng comment trực tiếp của Meta hỏng, CRM đã chuyển sang hỏi định kỳ mỗi "+
			"2 giây (comment có thể tới chậm vài giây) và sẽ tự thử lại luồng.", p.SessionID)
	case alertMissing:
		key = "live-missing:" + p.SessionID.String()
		text = fmt.Sprintf("Phiên live %s đối chiếu cuối phiên: CRM có %d/%d comment Meta đếm (thiếu %d).",
			p.SessionID, p.Got, p.Meta, p.Meta-p.Got)
	default:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	return alertjobs.Notify(ctx, c.a, c.log, key, observability.SeverityNormal, text), nil
}
