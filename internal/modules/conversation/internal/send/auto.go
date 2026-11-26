package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Tin tự động vào hội thoại hộp thư (Task 168 nhắn lại; dùng chung cho tin chào Task 167): cùng đường gửi với tin
// nhân viên (chèn tin queued, lệnh gọi kênh ngoài transaction, OnDone), khác ở nguồn automation, không người gửi,
// không bao giờ dùng thẻ nhân viên, hạn 5 phút và không rõ đã gửi thì bỏ (04 mục 5.3).

// KindAutoSend là lệnh gửi một tin tự động ra kênh.
const KindAutoSend = "command.fb.auto_send"

// autoExpiresAfter: tin tự động còn chờ quá 5 phút thì bỏ (expired).
const autoExpiresAfter = 5 * time.Minute

// AutoCommandKind là Kind của lệnh gửi tin tự động.
func AutoCommandKind() queue.Kind {
	return commands.CommandKind(KindAutoSend, autoExpiresAfter, sendTimeout)
}

// Automated chèn tin tự động chữ text vào hội thoại hộp thư conversationID lúc now và ghi lệnh
// command.fb.auto_send, trong tx tenant của bên gọi; trả tin và mã lệnh. Khóa hội thoại (ADR-46 nhóm 6; bên gọi đã
// khóa thì khóa lại không đổi gì); hội thoại không có → access.ErrNotFound; không phải hộp thư, chữ rỗng hay quá dài →
// ErrInvalidInput; Page chỉ xem → ErrReadOnly; SendPolicy không cho tin tự động (hết cửa sổ, khách chặn, chỉ còn thẻ
// nhân viên) → *NotAllowedError. Không kiểm quyền người: bên gọi là quy tắc tự động.
func Automated(ctx context.Context, tx pgx.Tx, env Env, conversationID uuid.UUID, text string, now time.Time,
) (Message, uuid.UUID, error) {
	if err := (Input{Text: text}).validate(); err != nil {
		return Message{}, uuid.Nil, err
	}
	now = now.UTC()
	c, err := store.New(tx).LockConversationForSend(ctx, conversationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Message{}, uuid.Nil, access.ErrNotFound
	case err != nil:
		return Message{}, uuid.Nil, fmt.Errorf("khóa hội thoại để gửi tin tự động: %w", err)
	case c.Kind != kindInbox:
		return Message{}, uuid.Nil, ErrInvalidInput
	}
	acc, err := channel.Account(ctx, tx, c.ChannelAccountID)
	if err != nil {
		return Message{}, uuid.Nil, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	if err := allowed(env.Registry, conversationRow(c), acc, false, true, now); err != nil {
		return Message{}, uuid.Nil, err
	}
	return insert(ctx, tx, env, uuid.Nil, originAuto, c.ID, acc, text, []Attachment{}, now)
}

// AutoCommand là lệnh command.fb.auto_send: Execute, OnDone như lệnh gửi tin nhân viên; OnTerminal đổi tin (in_doubt →
// discarded, không hỏi nhân viên); mỗi kết quả cuối (done, dead, expired, in_doubt) còn báo cho Finish của bên dùng
// (quy tắc tự động ghi kết quả lượt) trong cùng transaction. Không có Looker: không rõ đã gửi thì bỏ ngay.
type AutoCommand struct {
	inner sendCommand
	// Finish ghi kết quả cuối status (commands.StatusDone, queue.StatusDead, StatusExpired, StatusInDoubt) của lệnh jobID.
	finish func(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, status string) error
}

// NewAutoCommand dựng lệnh gửi tin tự động; finish nhận kết quả cuối của mỗi lệnh.
func NewAutoCommand(d deps.Deps, finish func(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, status string) error) AutoCommand {
	return AutoCommand{inner: newSendCommand(d), finish: finish}
}

// Kind là command.fb.auto_send (hạn 5 phút).
func (AutoCommand) Kind() queue.Kind { return AutoCommandKind() }

// AutoRetryNotSent trả true: chắc chắn chưa tới kênh thì tự gửi lại trong hạn 5 phút.
func (AutoCommand) AutoRetryNotSent() bool { return true }

// Execute gửi tin như lệnh gửi tin nhân viên (SendPolicy hỏi lại với cờ tự động theo nguồn của tin).
func (c AutoCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.inner.Execute(ctx, j)
}

// OnDone đặt tin sent như lệnh gửi tin nhân viên rồi báo kết quả done.
func (c AutoCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	if err := c.inner.OnDone(ctx, j, raw, tx); err != nil {
		return err
	}
	return c.finish(ctx, tx, j.ID, commands.StatusDone)
}

// OnTerminal đổi tin (dead → failed, expired → expired, in_doubt → discarded lý do in_doubt) rồi báo kết quả.
func (c AutoCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if err := c.inner.settle(ctx, j, status, tx, true); err != nil {
		return err
	}
	return c.finish(ctx, tx, j.ID, status)
}
