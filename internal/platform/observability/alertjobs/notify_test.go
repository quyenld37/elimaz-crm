package alertjobs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/observability/alertjobs"
)

type recAlerter struct {
	err            error
	key, sev, text string
}

func (r *recAlerter) Notify(_ context.Context, key, severity, text string) error {
	r.key, r.sev, r.text = key, severity, text
	return r.err
}

// Notify: gửi được → done với đúng key, mức, văn bản; Alerter lỗi → not_sent alert_failed; Telegram 429 (đã bọc) →
// not_sent chờ đúng Retry-After; không có Alerter → rejected.
func TestNotify(t *testing.T) {
	t.Parallel()
	a := &recAlerter{}
	if out := alertjobs.Notify(t.Context(), a, nil, "k:1", "normal", "chữ"); out.Status != commands.StatusDone ||
		a.key != "k:1" || a.sev != "normal" || a.text != "chữ" {
		t.Fatalf("gửi được: %+v %+v", out, a)
	}
	a.err = errors.New("hỏng")
	if out := alertjobs.Notify(t.Context(), a, nil, "k:1", "normal", "chữ"); out.Status != commands.StatusNotSent || out.Reason != "alert_failed" {
		t.Fatalf("Alerter lỗi: %+v", out)
	}
	a.err = fmt.Errorf("gửi: %w", &observability.TelegramRetryError{After: 7 * time.Second})
	if out := alertjobs.Notify(t.Context(), a, nil, "k:1", "normal", "chữ"); out.Status != commands.StatusNotSent ||
		out.RetryAfter != 7*time.Second {
		t.Fatalf("Telegram 429: %+v", out)
	}
	if out := alertjobs.Notify(t.Context(), nil, nil, "k:1", "normal", "chữ"); out.Status != commands.StatusRejected ||
		out.Reason != alertjobs.ReasonAlerterMissing {
		t.Fatalf("không Alerter: %+v", out)
	}
}
