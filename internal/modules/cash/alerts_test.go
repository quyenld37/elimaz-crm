package cash_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// stubAlerter trả err và ghi key của lần gửi cuối (fakeAlerter của postings_test.go không trả lỗi được).
type stubAlerter struct {
	err error
	key string
}

func (a *stubAlerter) Notify(_ context.Context, key, _, _ string) error {
	a.key = key
	return a.err
}

// alertCase là một lệnh cảnh báo của cash cùng payload đúng, payload hỏng và key mong đợi.
type alertCase struct {
	name      string
	command   func(observability.Alerter) commands.Handler
	good, bad string
	key       string
}

func alertCases() []alertCase {
	id := uuid.New()
	return []alertCase{{
		name: "remitted_returned", command: cash.NewRemittedReturnedAlertCommand,
		good: `{"shipmentId":"` + id.String() + `","code":"VD0001"}`, bad: `{"code":"VD0001"}`,
		key: "cod-remitted-returned:VD0001",
	}}
}

// Lệnh cảnh báo của cash: payload hỏng → rejected bad_payload; không có Alerter → rejected alerter_missing; gửi lỗi →
// not_sent alert_failed (tự gửi lại); Telegram 429 → not_sent chờ đúng Retry-After; gửi được → done với key chống lặp.
func TestAlertCommands_Outcomes(t *testing.T) {
	t.Parallel()
	for _, c := range alertCases() {
		run := func(n observability.Alerter, payload string) commands.Outcome {
			out, err := c.command(n).Execute(t.Context(), queue.Job{Payload: []byte(payload)})
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			return out
		}
		if !c.command(nil).AutoRetryNotSent() {
			t.Errorf("%s: không tự gửi lại khi chưa gửi", c.name)
		}
		if out := run(&stubAlerter{}, c.bad); out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
			t.Errorf("%s payload hỏng: %+v", c.name, out)
		}
		if out := run(nil, c.good); out.Status != commands.StatusRejected || out.Reason != "alerter_missing" {
			t.Errorf("%s không Alerter: %+v", c.name, out)
		}
		if out := run(&stubAlerter{err: errors.New("mạng")}, c.good); out.Status != commands.StatusNotSent ||
			out.Reason != "alert_failed" || out.RetryAfter != 0 {
			t.Errorf("%s gửi lỗi: %+v", c.name, out)
		}
		busy := &stubAlerter{err: &observability.TelegramRetryError{After: 7 * time.Second}}
		if out := run(busy, c.good); out.Status != commands.StatusNotSent || out.RetryAfter != 7*time.Second {
			t.Errorf("%s Telegram 429: %+v", c.name, out)
		}
		ok := &stubAlerter{}
		if out := run(ok, c.good); out.Status != commands.StatusDone || ok.key != c.key {
			t.Errorf("%s gửi được: %+v key %q", c.name, out, ok.key)
		}
	}
}
