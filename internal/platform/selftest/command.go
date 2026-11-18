package selftest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Lệnh thử command.selftest (kế hoạch M0 Task 18): hết hạn sau 3 phút chờ, mỗi lần gửi tối đa 30 s.
const (
	commandName    = "command.selftest"
	commandExpires = 3 * time.Minute
	commandTimeout = 30 * time.Second
)

// CommandKind là loại lệnh command.selftest (commands.CommandKind: mode command, giữ chỗ hết hạn thì in_doubt).
func CommandKind() queue.Kind {
	return commands.CommandKind(commandName, commandExpires, commandTimeout)
}

// External là "bên ngoài" của lệnh thử: Send gửi ref; Has báo bên ngoài đã ghi nhận ref chưa (nói thật).
// testkit.FakeExternal hiện thực cho test; Loopback cho worker máy dev.
type External interface {
	Send(ctx context.Context, ref string) error
	Has(ref string) bool
}

// Command là handler lệnh command.selftest (commands.Handler, commands.Looker), ref là mã lệnh. Chỉ đăng ký khi
// CRM_ENV khác production (cmd/crm).
type Command struct {
	ext External
}

// NewCommand tạo handler gửi qua ext.
func NewCommand(ext External) *Command { return &Command{ext: ext} }

// Kind trả CommandKind().
func (*Command) Kind() queue.Kind { return CommandKind() }

// AutoRetryNotSent trả false: Execute không bao giờ báo not_sent.
func (*Command) AutoRetryNotSent() bool { return false }

// Execute gửi ref = mã lệnh: nhận → done, result {"ref"}. Mọi lỗi của bên ngoài trả lên nguyên dạng lỗi, Runner hiểu
// là không biết đã gửi chưa (in_doubt): lệnh thử không phân biệt "chắc chưa gửi", nên không bao giờ tự gửi lại.
func (c *Command) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	ref := j.ID.String()
	if err := c.ext.Send(ctx, ref); err != nil {
		return commands.Outcome{}, fmt.Errorf("gửi lệnh selftest: %w", err)
	}
	res, err := json.Marshal(map[string]string{"ref": ref})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả lệnh selftest: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: res}, nil
}

// Lookup trả done khi bên ngoài đã có ref, not_sent khi chưa (External.Has nói thật).
func (c *Command) Lookup(_ context.Context, j queue.Job) (string, error) {
	if c.ext.Has(j.ID.String()) {
		return commands.StatusDone, nil
	}
	return commands.StatusNotSent, nil
}

// Loopback là bên ngoài tại chỗ của worker máy dev: luôn nhận, luôn báo đã có (không có gì thật được gửi đi).
type Loopback struct{}

// Send luôn nhận.
func (Loopback) Send(context.Context, string) error { return nil }

// Has luôn đúng.
func (Loopback) Has(string) bool { return true }
