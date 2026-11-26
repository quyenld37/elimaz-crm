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

// SubscribeAlertKind là lệnh cảnh báo "Page không đăng ký nhận tin được" (key channel-subscribe:<id>, mức gấp), ghi
// trong tx kết thúc lệnh subscribe_page thất bại (Task 149).
const SubscribeAlertKind = "channel.subscribe_alert"

const (
	alertTimeout     = 10 * time.Second
	severityNormal   = "normal"
	severityUrgent   = "urgent"
	reasonBadPayload = "bad_payload"
)

// alertPayload là payload của lệnh cảnh báo theo tài khoản kênh: chỉ mã, kênh, tên Page (công khai), không token.
type alertPayload struct {
	AccountID uuid.UUID `json:"accountId"`
	Channel   string    `json:"channel"`
	Name      string    `json:"name,omitempty"`
	// Action là hành động chính sách của Meta (cảnh báo meta-policy, đã lọc [a-z_]).
	Action string `json:"action,omitempty"`
}

func alertKind(name string) queue.Kind {
	return commands.CommandKind(name, 0, alertTimeout)
}

// enqueueAlert ghi lệnh cảnh báo kind cho tài khoản a (dedupe: khóa chống trùng trong loại).
func enqueueAlert(ctx context.Context, tx pgx.Tx, kind string, a ChannelAccount, dedupe string) error {
	tenant := a.TenantID
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, alertKind(kind), queue.NewJob{
		TenantID: &tenant, Payload: alertPayload{AccountID: a.ID, Channel: a.Channel, Name: a.Name}, DedupeKey: dedupe,
	}); err != nil {
		return fmt.Errorf("ghi lệnh %s: %w", kind, err)
	}
	return nil
}

// enqueueSendModeAlert ghi lệnh cảnh báo cho tài khoản a vừa sang enabled (mỗi version một lệnh).
func enqueueSendModeAlert(ctx context.Context, tx pgx.Tx, a ChannelAccount) error {
	return enqueueAlert(ctx, tx, SendModeAlertKind, a, "send-mode:"+a.ID.String()+":"+strconv.Itoa(int(a.Version)))
}

// enqueueSubscribeAlert ghi lệnh cảnh báo cho tài khoản a khi lệnh đăng ký jobID thất bại hẳn (mỗi lệnh một cảnh báo).
func enqueueSubscribeAlert(ctx context.Context, tx pgx.Tx, a ChannelAccount, jobID uuid.UUID) error {
	return enqueueAlert(ctx, tx, SubscribeAlertKind, a, "channel-subscribe:"+a.ID.String()+":"+jobID.String())
}

// NewSendModeAlertCommand trả lệnh SendModeAlertKind: gửi cảnh báo key "send-mode:<accountId>", mức thường, văn bản
// chỉ có kênh và mã tài khoản, qua alertjobs.Notify (Alerter nil → rejected; Notify lỗi → log, not_sent, tự gửi lại).
// cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewSendModeAlertCommand(a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return accountAlert{
		kind: SendModeAlertKind, key: "send-mode:", severity: severityNormal, a: a, log: logger,
		text: func(p alertPayload) string {
			return fmt.Sprintf("Tài khoản kênh %s (%s) vừa chuyển sang gửi thật: nhân viên đã gửi được tin ra ngoài", p.AccountID, p.Channel)
		},
	}
}

// NewSubscribeAlertCommand trả lệnh SubscribeAlertKind: cảnh báo gấp key "channel-subscribe:<accountId>", văn bản có
// tên Page, kênh, mã tài khoản (không token); như NewSendModeAlertCommand.
func NewSubscribeAlertCommand(a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return urgentPageAlert(SubscribeAlertKind, "channel-subscribe:", "không đăng ký nhận tin được", a, logger)
}

// urgentPageAlert là lệnh cảnh báo gấp theo Page: key = keyPrefix + mã tài khoản, văn bản "Page <tên> (<kênh>, tài
// khoản <mã>) <problem>: quản trị cần kết nối lại Page" (không token).
func urgentPageAlert(kind, keyPrefix, problem string, a observability.Alerter, logger *slog.Logger) accountAlert {
	return accountAlert{
		kind: kind, key: keyPrefix, severity: severityUrgent, a: a, log: logger,
		text: func(p alertPayload) string {
			return fmt.Sprintf("Page %s (%s, tài khoản %s) %s: quản trị cần kết nối lại Page", p.Name, p.Channel, p.AccountID, problem)
		},
	}
}

// accountAlert là một lệnh cảnh báo theo tài khoản kênh: key = tiền tố + mã tài khoản.
type accountAlert struct {
	kind, key, severity string
	text                func(alertPayload) string
	a                   observability.Alerter
	log                 *slog.Logger
}

func (s accountAlert) Kind() queue.Kind     { return alertKind(s.kind) }
func (accountAlert) AutoRetryNotSent() bool { return true }

func (s accountAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p alertPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	return alertjobs.Notify(ctx, s.a, s.log, s.key+p.AccountID.String(), s.severity, s.text(p)), nil
}

// HealthAlertCommands trả các lệnh cảnh báo của sức khỏe kênh (Task 150), cmd/crm đăng ký cùng các lệnh cảnh báo khác
// (dùng chung Alerter): mất quyền channel-auth:<id> (gấp), thiếu trường webhook channel-fields:<id> (thường), im lặng
// bất thường webhook-silence:<id> (gấp), vi phạm chính sách meta-policy:<id> (gấp), hạn phiên bản Graph graph-version
// (thường). Văn bản chỉ có tên Page (công khai), kênh, mã tài khoản, hành động chính sách; không token, không nội dung.
func HealthAlertCommands(a observability.Alerter, logger *slog.Logger) []commands.Handler {
	ref := func(p alertPayload) string { return fmt.Sprintf("kênh %s, tài khoản %s", p.Channel, p.AccountID) }
	return []commands.Handler{
		NewAuthAlertCommand(a, logger),
		accountAlert{
			kind: FieldsAlertKind, key: "channel-fields:", severity: severityNormal, a: a, log: logger,
			text: func(p alertPayload) string {
				return fmt.Sprintf("Page %s thiếu đăng ký nhận tin từ Meta; CRM đã tự đăng ký lại (%s).", p.Name, ref(p))
			},
		},
		accountAlert{
			kind: SilenceAlertKind, key: "webhook-silence:", severity: severityUrgent, a: a, log: logger,
			text: func(p alertPayload) string {
				return fmt.Sprintf("Im lặng bất thường trong giờ bán: Page %s không nhận sự kiện nào từ Meta hơn 30 phút; "+
					"CRM đã tự đăng ký lại và hỏi bù (%s).", p.Name, ref(p))
			},
		},
		accountAlert{
			kind: PolicyAlertKind, key: "meta-policy:", severity: severityUrgent, a: a, log: logger,
			text: func(p alertPayload) string {
				return fmt.Sprintf("Meta báo vi phạm chính sách trên Page %s (hành động: %s); không xử lý trong 7 ngày có thể bị "+
					"giới hạn gửi tin, quản trị cần kiểm tra (%s).", p.Name, p.Action, ref(p))
			},
		},
		graphAlert{a: a, log: logger},
	}
}

// graphAlert là lệnh cảnh báo hạn phiên bản Graph API (không theo tenant, key graph-version).
type graphAlert struct {
	a   observability.Alerter
	log *slog.Logger
}

func (graphAlert) Kind() queue.Kind       { return alertKind(GraphVersionAlertKind) }
func (graphAlert) AutoRetryNotSent() bool { return true }

func (g graphAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p graphPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.Version == "" {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	text := fmt.Sprintf("Phiên bản Graph API %s hết hạn ngày %s (còn %d ngày): nâng META_GRAPH_VERSION và chạy lại "+
		"contract test của adapter Facebook.", p.Version, p.Expires, p.Days)
	return alertjobs.Notify(ctx, g.a, g.log, "graph-version", severityNormal, text), nil
}
