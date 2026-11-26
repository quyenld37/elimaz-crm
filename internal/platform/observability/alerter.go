package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/urlerr"
)

// Hai mức cảnh báo (spec 04 mục 8, ADR-30).
const (
	// SeverityUrgent: gửi kênh gấp, nhắc lại mỗi 30 phút khi còn.
	SeverityUrgent = "urgent"
	// SeverityNormal: gửi kênh thường, cùng key trong 10 phút chỉ một lần.
	SeverityNormal = "normal"
)

const (
	normalWindow    = 10 * time.Minute
	urgentWindow    = 30 * time.Minute
	telegramAPI     = "https://api.telegram.org"
	telegramTimeout = 10 * time.Second
	// maxTextRunes: Telegram nhận tối đa 4096 ký tự một tin.
	maxTextRunes = 4000
)

// ErrSeverity báo mức cảnh báo không phải urgent hay normal.
var ErrSeverity = errors.New("mức cảnh báo phải là urgent hoặc normal")

// ErrTelegram báo Telegram không nhận tin (mã khác 2xx hay ok=false).
var ErrTelegram = errors.New("telegram không nhận tin")

// TelegramRetryError báo Telegram từ chối vì quá tải (429), kèm thời gian Telegram yêu cầu chờ.
type TelegramRetryError struct{ After time.Duration }

func (e *TelegramRetryError) Error() string {
	return fmt.Sprintf("telegram quá tải, chờ %v", e.After)
}

// Alerter gửi một cảnh báo vận hành. key là tên luật (kèm kind hay tiến trình), dùng để chống lặp; text chỉ mang tên
// luật, số lượng, kind, mã bản ghi, không bao giờ có dữ liệu khách.
type Alerter interface {
	Notify(ctx context.Context, key, severity, text string) error
}

// NewTelegramAlerter trả Alerter gửi qua Telegram Bot API và chống lặp theo platform.alert_events: normal cùng key
// 10 phút một lần, urgent nhắc lại sau mỗi 30 phút. urgent tới TelegramUrgentChatID (thiếu thì chat thường). Mỗi lần
// gửi được ghi một dòng alert_events (sau khi Telegram nhận, để lần gửi hỏng được thử lại ở lượt sau). Thiếu
// TelegramBotToken hay TelegramChatID: không gọi mạng, chỉ log và ghi alert_events. client nil dùng http.Client hạn
// 10 s; logger nil dùng slog.Default().
//
// Notify của một Alerter chạy lần lượt (mutex); hai tiến trình cùng lúc có thể gửi trùng một tin (chấp nhận: không
// khóa database trong lúc gọi mạng).
func NewTelegramAlerter(d *db.DB, cfg config.Config, client *http.Client, logger *slog.Logger) Alerter { //nolint:ireturn // kế hoạch khai trả Alerter
	if client == nil {
		client = &http.Client{Timeout: telegramTimeout}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &telegramAlerter{
		d: d, token: cfg.TelegramBotToken, chat: cfg.TelegramChatID, urgentChat: cfg.TelegramUrgentChatID,
		client: client, log: logger,
	}
}

type telegramAlerter struct {
	d                       *db.DB
	token, chat, urgentChat string
	client                  *http.Client
	log                     *slog.Logger
	mu                      sync.Mutex
}

// asOfNotifier (Alerter của gói này hiện thực) ghi dòng alert_events với sent_at = asOf (lúc đọc của lượt luật), để
// việc kết thúc trong lúc đang gửi vẫn được lượt sau thấy (luật dead:, in-doubt: so với mốc này).
type asOfNotifier interface {
	notifyAsOf(ctx context.Context, key, severity, text string, asOf time.Time) error
}

func (a *telegramAlerter) Notify(ctx context.Context, key, severity, text string) error {
	return a.notify(ctx, key, severity, text, pgtype.Timestamptz{})
}

func (a *telegramAlerter) notifyAsOf(ctx context.Context, key, severity, text string, asOf time.Time) error {
	return a.notify(ctx, key, severity, text, pgtype.Timestamptz{Time: asOf, Valid: true})
}

func (a *telegramAlerter) notify(ctx context.Context, key, severity, text string, sentAt pgtype.Timestamptz) error {
	window := normalWindow
	switch severity {
	case SeverityNormal:
	case SeverityUrgent:
		window = urgentWindow
	default:
		return fmt.Errorf("cảnh báo %s: %w (%q)", key, ErrSeverity, severity)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	recent, err := store.New(a.d.Pool).AlertSentWithin(ctx, store.AlertSentWithinParams{Key: key, WindowS: window.Seconds()})
	if err != nil {
		return fmt.Errorf("đọc lần gửi trước của cảnh báo %s: %w", key, err)
	}
	if recent {
		return nil
	}
	if a.token == "" || a.chat == "" {
		a.log.WarnContext(ctx, "cảnh báo (chưa cấu hình Telegram)", slog.String("key", key), slog.String("severity", severity))
	} else {
		chat := a.chat
		if severity == SeverityUrgent && a.urgentChat != "" {
			chat = a.urgentChat
		}
		if err := a.send(ctx, chat, label(severity)+text); err != nil {
			return fmt.Errorf("gửi cảnh báo %s: %w", key, err)
		}
	}
	err = a.d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		return store.New(tx).InsertAlertEvent(ctx, store.InsertAlertEventParams{
			Key: key, Severity: severity, Text: text, SentAt: sentAt,
		})
	})
	if err != nil {
		return fmt.Errorf("ghi alert_events %s: %w", key, err)
	}
	return nil
}

func label(severity string) string {
	if severity == SeverityUrgent {
		return "[GẤP] "
	}
	return "[CRM] "
}

// send gọi sendMessage. Lỗi không chứa URL (mang token bot).
func (a *telegramAlerter) send(ctx context.Context, chat, text string) error {
	body, err := json.Marshal(map[string]any{"chat_id": chat, "text": truncate(text, maxTextRunes), "disable_web_page_preview": true})
	if err != nil {
		return fmt.Errorf("mã hóa tin: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, telegramTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPI+"/bot"+a.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tạo yêu cầu Telegram: %w", urlerr.Strip(err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("gọi Telegram: %w", urlerr.Strip(err))
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		OK         bool `json:"ok"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &TelegramRetryError{After: time.Duration(max(out.Parameters.RetryAfter, 1)) * time.Second}
	case resp.StatusCode < 200 || resp.StatusCode > 299 || !out.OK:
		return fmt.Errorf("%w: mã %s", ErrTelegram, strconv.Itoa(resp.StatusCode))
	}
	return nil
}

// truncate cắt s còn tối đa n ký tự (không cắt giữa ký tự UTF-8).
func truncate(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// AlertSentWithin báo cảnh báo key đã được gửi (có dòng platform.alert_events) trong window vừa qua, theo đồng hồ
// database.
//
// Khi nào không dùng: chống lặp ngắn (10 phút thường, 30 phút gấp) Alerter đã tự làm; chỉ dùng cho nhắc việc cần
// khoảng lặp dài hơn (vd nhắc Data Use Checkup 7 ngày một lần, Task 179).
func AlertSentWithin(ctx context.Context, d *db.DB, key string, window time.Duration) (bool, error) {
	recent, err := store.New(d.Pool).AlertSentWithin(ctx, store.AlertSentWithinParams{Key: key, WindowS: window.Seconds()})
	if err != nil {
		return false, fmt.Errorf("đọc lần gửi trước của cảnh báo %s: %w", key, err)
	}
	return recent, nil
}
