package channel_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

type recNotifier struct {
	err            error
	key, sev, text string
	calls          int
}

func (r *recNotifier) Notify(_ context.Context, key, severity, text string) error {
	r.calls++
	r.key, r.sev, r.text = key, severity, text
	return r.err
}

// Lệnh cảnh báo chế độ gửi: gửi key send-mode:<id>, mức normal, văn bản chỉ có mã tài khoản và kênh; Notify lỗi →
// not_sent (tự gửi lại); không có Alerter → rejected; payload hỏng → rejected.
func TestSendModeAlertCommand(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	payload := []byte(`{"accountId":"` + id.String() + `","channel":"fakechat"}`)
	n := &recNotifier{}
	h := channel.NewSendModeAlertCommand(n, nil)
	if h.Kind().Name != channel.SendModeAlertKind || !h.AutoRetryNotSent() {
		t.Fatalf("kind = %+v", h.Kind())
	}
	out, err := h.Execute(t.Context(), queue.Job{Payload: payload})
	if err != nil || out.Status != commands.StatusDone || n.key != "send-mode:"+id.String() || n.sev != "normal" ||
		!strings.Contains(n.text, id.String()) {
		t.Fatalf("gửi: %+v %v %+v", out, err, n)
	}
	n.err = errors.New("telegram hỏng")
	if out, _ := h.Execute(t.Context(), queue.Job{Payload: payload}); out.Status != commands.StatusNotSent {
		t.Fatalf("Notify lỗi: %+v", out)
	}
	if out, _ := channel.NewSendModeAlertCommand(nil, nil).Execute(t.Context(), queue.Job{Payload: payload}); out.Status != commands.StatusRejected {
		t.Fatalf("không Alerter: %+v", out)
	}
	if out, _ := h.Execute(t.Context(), queue.Job{Payload: []byte(`{}`)}); out.Status != commands.StatusRejected {
		t.Fatalf("payload hỏng: %+v", out)
	}
}
