// Lệnh external-probe là bộ kiểm từ ngoài của CRM (spec 04 mục 8, ADR-38): chạy ở một dịch vụ theo lịch **không thuộc
// Cloudflare và ngoài VPS**, mỗi phút một lần. Mỗi lần chạy gọi HOOKS_HEALTH_URL và DEEP_HEALTH_URL (Bearer
// DEEP_HEALTH_TOKEN); đích nào hỏng thì chờ 20 s gọi lại, hỏng cả hai lượt thì nhắn Telegram (cùng nội dung không
// nhắn lại trong 10 phút, nhớ qua PROBE_STATE_FILE); cuối cùng gửi nhịp sống của chính nó tới PROBE_HEARTBEAT_URL.
// Chỉ dùng thư viện chuẩn. Cách cài: README.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	defaultGap      = 20 * time.Second
	defaultTimeout  = 10 * time.Second
	resendAfter     = 10 * time.Minute
	defaultTelegram = "https://api.telegram.org"
)

// Result là kết quả một lần gọi một đích.
type Result struct {
	Target string
	OK     bool
	Detail string
}

// Evaluate: alert khi có đích hỏng ở mọi lượt và có ít nhất hai lượt (hai lượt cách nhau Gap); failing là tên các đích
// đó, xếp tăng dần.
func Evaluate(rounds [][]Result) (alert bool, failing []string) {
	if len(rounds) < 2 {
		return false, nil
	}
	bad := map[string]int{}
	for _, round := range rounds {
		for _, r := range round {
			if !r.OK {
				bad[r.Target]++
			}
		}
	}
	for target, n := range bad {
		if n == len(rounds) {
			failing = append(failing, target)
		}
	}
	slices.Sort(failing)
	return len(failing) > 0, failing
}

// Config là cấu hình một lần chạy. Now, Sleep, Client để trống thì dùng đồng hồ, time.Sleep có ctx, http.Client hạn
// Timeout.
type Config struct {
	HooksHealthURL, DeepHealthURL, DeepHealthToken string
	TelegramAPI, TelegramBotToken, TelegramChatID  string
	HeartbeatURL                                   string
	StateFile                                      string
	Gap, Timeout                                   time.Duration

	Now    func() time.Time
	Sleep  func(ctx context.Context, d time.Duration) error
	Client *http.Client
}

// ErrConfig báo thiếu biến môi trường bắt buộc.
var ErrConfig = errors.New("thiếu biến môi trường")

// ConfigFromEnv đọc cấu hình qua getenv; lỗi nêu mọi biến bắt buộc còn thiếu.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		HooksHealthURL: getenv("HOOKS_HEALTH_URL"), DeepHealthURL: getenv("DEEP_HEALTH_URL"),
		DeepHealthToken: getenv("DEEP_HEALTH_TOKEN"), TelegramBotToken: getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID: getenv("TELEGRAM_CHAT_ID"), HeartbeatURL: getenv("PROBE_HEARTBEAT_URL"),
		TelegramAPI: getenv("TELEGRAM_API"), StateFile: getenv("PROBE_STATE_FILE"),
		Gap: defaultGap, Timeout: defaultTimeout,
	}
	if c.TelegramAPI == "" {
		c.TelegramAPI = defaultTelegram
	}
	if c.StateFile == "" {
		c.StateFile = filepath.Join(os.TempDir(), "crm-external-probe.json")
	}
	var missing []string
	for _, kv := range [][2]string{
		{"HOOKS_HEALTH_URL", c.HooksHealthURL},
		{"DEEP_HEALTH_URL", c.DeepHealthURL},
		{"DEEP_HEALTH_TOKEN", c.DeepHealthToken},
		{"TELEGRAM_BOT_TOKEN", c.TelegramBotToken},
		{"TELEGRAM_CHAT_ID", c.TelegramChatID},
	} {
		if kv[1] == "" {
			missing = append(missing, kv[0])
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("%w: %s", ErrConfig, strings.Join(missing, ", "))
	}
	return c, nil
}

func (c *Config) defaults() {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: c.Timeout}
	}
	if c.Sleep == nil {
		c.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return fmt.Errorf("chờ lượt hai: %w", ctx.Err())
			case <-t.C:
				return nil
			}
		}
	}
}

// Run làm một lần kiểm: lượt một; có đích hỏng thì chờ Gap rồi lượt hai; Evaluate; nhắn Telegram khi cần; gửi nhịp
// sống. Lỗi: không nhắn được Telegram, không ghi được tệp trạng thái, không gửi được nhịp sống.
func Run(ctx context.Context, c Config) error {
	c.defaults()
	rounds := [][]Result{check(ctx, c)}
	if slices.ContainsFunc(rounds[0], func(r Result) bool { return !r.OK }) {
		if err := c.Sleep(ctx, c.Gap); err != nil {
			return err
		}
		rounds = append(rounds, check(ctx, c))
	}
	var errs []error
	if alert, failing := Evaluate(rounds); alert {
		errs = append(errs, notify(ctx, c, "[GẤP] CRM kiểm từ ngoài: hỏng hai lượt liền: "+strings.Join(failing, ", ")))
	}
	if c.HeartbeatURL != "" {
		if _, err := get(ctx, c, c.HeartbeatURL, ""); err != nil {
			errs = append(errs, fmt.Errorf("gửi nhịp sống: %w", err))
		}
	}
	return errors.Join(errs...)
}

// check gọi hai đích; đạt khi trả 200.
func check(ctx context.Context, c Config) []Result {
	targets := []struct{ name, url, token string }{
		{"hooks", c.HooksHealthURL, ""},
		{"deep", c.DeepHealthURL, c.DeepHealthToken},
	}
	out := make([]Result, 0, len(targets))
	for _, t := range targets {
		status, err := get(ctx, c, t.url, t.token)
		switch {
		case err != nil:
			out = append(out, Result{Target: t.name, Detail: err.Error()})
		case status != http.StatusOK:
			out = append(out, Result{Target: t.name, Detail: fmt.Sprintf("mã %d", status)})
		default:
			out = append(out, Result{Target: t.name, OK: true})
		}
	}
	return out
}

// get gọi GET u (kèm Bearer token khi có) và trả mã. Lỗi không chứa URL (có thể mang mã bí mật).
func get(ctx context.Context, c Config, u, token string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("tạo yêu cầu: %w", stripURL(err))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("gọi: %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, nil
}

func stripURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}

// state là lần nhắn cuối, để không nhắn lại cùng nội dung trong 10 phút.
type state struct {
	Text   string    `json:"text"`
	SentAt time.Time `json:"sentAt"`
}

// ErrTelegram báo Telegram không nhận tin.
var ErrTelegram = errors.New("telegram không nhận tin")

func notify(ctx context.Context, c Config, text string) error {
	now := c.Now()
	var last state
	if b, err := os.ReadFile(c.StateFile); err == nil {
		_ = json.Unmarshal(b, &last)
	}
	if last.Text == text && now.Sub(last.SentAt) < resendAfter {
		return nil
	}
	form := url.Values{"chat_id": {c.TelegramChatID}, "text": {text}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TelegramAPI+"/bot"+c.TelegramBotToken+"/sendMessage",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("tạo yêu cầu Telegram: %w", stripURL(err))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("gọi Telegram: %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: mã %d", ErrTelegram, resp.StatusCode)
	}
	b, err := json.Marshal(state{Text: text, SentAt: now})
	if err != nil {
		return fmt.Errorf("mã hóa trạng thái: %w", err)
	}
	if err := os.WriteFile(c.StateFile, b, 0o600); err != nil {
		return fmt.Errorf("ghi tệp trạng thái: %w", err)
	}
	return nil
}

func main() { os.Exit(run()) }

func run() int {
	cfg, err := ConfigFromEnv(os.Getenv) //nolint:forbidigo // tệp chạy riêng ngoài CRM, không dùng gói config
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "external-probe:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	if err := Run(ctx, cfg); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "external-probe:", err)
		return 1
	}
	return 0
}
