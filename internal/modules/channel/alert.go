package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/observability/alertjobs"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// SendModeAlertKind là lệnh cảnh báo "Page bắt đầu gửi thật" (key send-mode:<id>, mức thường), ghi trong tx của
// Writer.SetSendMode khi chuyển sang enabled; worker gửi qua Alerter sau commit (không gọi Telegram trong tx).
const SendModeAlertKind = "channel.send_mode_alert"

const (
	sendModeAlertTimeout = 10 * time.Second
	severityNormal       = "normal"
	reasonBadPayload     = "bad_payload"
)

type sendModeAlertPayload struct {
	AccountID uuid.UUID `json:"accountId"`
	Channel   string    `json:"channel"`
}

func sendModeAlertKind() queue.Kind {
	return commands.CommandKind(SendModeAlertKind, 0, sendModeAlertTimeout)
}

// enqueueSendModeAlert ghi lệnh cảnh báo cho tài khoản a vừa sang enabled (mỗi version một lệnh).
func enqueueSendModeAlert(ctx context.Context, tx pgx.Tx, a ChannelAccount) error {
	tenant := a.TenantID
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, sendModeAlertKind(), queue.NewJob{
		TenantID: &tenant, Payload: sendModeAlertPayload{AccountID: a.ID, Channel: a.Channel},
		DedupeKey: "send-mode:" + a.ID.String() + ":" + strconv.Itoa(int(a.Version)),
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo chế độ gửi: %w", err)
	}
	return nil
}

// NewSendModeAlertCommand trả lệnh SendModeAlertKind: gửi cảnh báo key "send-mode:<accountId>", mức thường, văn bản
// chỉ có kênh và mã tài khoản, qua alertjobs.Notify (Alerter nil → rejected; Notify lỗi → log, not_sent, tự gửi lại).
// cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewSendModeAlertCommand(a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return sendModeAlert{a: a, log: logger}
}

type sendModeAlert struct {
	a   observability.Alerter
	log *slog.Logger
}

func (sendModeAlert) Kind() queue.Kind       { return sendModeAlertKind() }
func (sendModeAlert) AutoRetryNotSent() bool { return true }

func (s sendModeAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p sendModeAlertPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	text := fmt.Sprintf("Tài khoản kênh %s (%s) vừa chuyển sang gửi thật: nhân viên đã gửi được tin ra ngoài", p.AccountID, p.Channel)
	return alertjobs.Notify(ctx, s.a, s.log, "send-mode:"+p.AccountID.String(), severityNormal, text), nil
}
