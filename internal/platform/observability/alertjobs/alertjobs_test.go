package alertjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Lệnh lịch: mode command, giữ chỗ hết hạn thì chạy lại, hạn sống đúng một kỳ, tự chạy lại khi not_sent.
func TestScheduledKind(t *testing.T) {
	for _, c := range []struct {
		h     commands.Handler
		name  string
		every time.Duration
	}{
		{NewAlertsCommand(nil, nil, observability.RuleOptions{}, nil), "task." + AlertsSchedule, AlertsEvery},
		{NewDigestCommand(nil, nil, nil), "task." + DigestSchedule, DigestEvery},
	} {
		k := c.h.Kind()
		if k.Name != c.name || k.Policy.Mode != queue.ModeCommand || k.Policy.OnLeaseExpired != queue.LeaseRetry ||
			k.Policy.ExpiresAfter != c.every || k.Policy.MaxAttempts != 3 {
			t.Fatalf("Kind %+v", k)
		}
		if r, ok := c.h.(interface{ AutoRetryNotSent() bool }); !ok || !r.AutoRetryNotSent() {
			t.Fatalf("%s: không tự chạy lại not_sent", c.name)
		}
	}
}

// Lỗi → not_sent với lý do cố định; Telegram 429 → chờ đúng thời gian Telegram yêu cầu; đạt → done kèm kết quả JSON.
func TestScheduledExecute(t *testing.T) {
	ctx := context.Background()
	fail := scheduled{name: "x", run: func(context.Context, time.Time) (any, error) { return nil, errors.New("hỏng") }}
	out, err := fail.Execute(ctx, queue.Job{})
	if err != nil || out.Status != commands.StatusNotSent || out.Reason != reasonFailed || out.RetryAfter != 0 {
		t.Fatalf("lỗi thường: %+v %v", out, err)
	}
	busy := scheduled{name: "x", run: func(context.Context, time.Time) (any, error) {
		return nil, &observability.TelegramRetryError{After: 7 * time.Second}
	}}
	if out, _ = busy.Execute(ctx, queue.Job{}); out.Status != commands.StatusNotSent || out.RetryAfter != 7*time.Second {
		t.Fatalf("429: %+v", out)
	}
	wrapped := scheduled{name: "x", run: func(context.Context, time.Time) (any, error) {
		return nil, fmt.Errorf("chạy luật cảnh báo: %w", &observability.TelegramRetryError{After: 3 * time.Second})
	}}
	if out, _ = wrapped.Execute(ctx, queue.Job{}); out.Status != commands.StatusNotSent || out.RetryAfter != 3*time.Second {
		t.Fatalf("429 đã bọc: %+v", out)
	}
	ok := scheduled{name: "x", run: func(context.Context, time.Time) (any, error) { return map[string]int{"n": 2}, nil }}
	out, err = ok.Execute(ctx, queue.Job{})
	var got map[string]int
	if err != nil || out.Status != commands.StatusDone || json.Unmarshal(out.Result, &got) != nil || got["n"] != 2 {
		t.Fatalf("đạt: %+v %v", out, err)
	}
}
