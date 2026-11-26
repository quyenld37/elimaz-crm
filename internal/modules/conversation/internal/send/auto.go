package send

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// KindAutoSend là lệnh gửi một tin do quy tắc tự động ghi (tin chào Task 167, nhắn lại Task 168; tên theo kế hoạch M4).
const KindAutoSend = "command.fb.auto_send"

// Hạn của lệnh gửi tự động (04 mục 5.3): còn chờ quá 5 phút thì bỏ; mỗi lần Execute tối đa 30 giây.
const autoExpiresAfter = 5 * time.Minute

// AutoCommandKind là Kind của lệnh gửi tự động.
func AutoCommandKind() queue.Kind {
	return commands.CommandKind(KindAutoSend, autoExpiresAfter, sendTimeout)
}

// AutoInput là tin tự động cần gửi: chữ (bên gọi đã kiểm độ dài theo cấu hình quy tắc) và nút trả lời nhanh (có thể rỗng).
type AutoInput struct {
	Text         string
	QuickReplies []channel.QuickReply
}

// quickReply là dạng lưu của một nút trong cột messages.quick_replies.
type quickReply struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
}

// EnqueueAuto chèn tin tự động in (origin automation, queued, không người gửi) vào hội thoại hộp thư convID của Page
// acc lúc now, cập nhật mốc tin cuối của hội thoại (không đổi mốc tin nhân viên), ghi lệnh command.fb.auto_send (DedupeKey = mã tin), gắn lệnh vào tin, báo hiệu; trong
// tx của bên gọi, sau khi hội thoại đã được khóa (đường vào: upsert hội thoại; ADR-46 nhóm 6). Trả mã lệnh.
func EnqueueAuto(ctx context.Context, tx pgx.Tx, d deps.Deps, acc channel.ChannelAccount, convID uuid.UUID, in AutoInput,
	now time.Time,
) (uuid.UUID, error) {
	now = now.UTC()
	q := store.New(tx)
	var qrs []byte
	if len(in.QuickReplies) > 0 {
		out := make([]quickReply, 0, len(in.QuickReplies))
		for _, r := range in.QuickReplies {
			out = append(out, quickReply(r))
		}
		var err error
		if qrs, err = json.Marshal(out); err != nil {
			return uuid.Nil, fmt.Errorf("mã hóa nút trả lời nhanh: %w", err)
		}
	}
	ref, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("tạo client_ref: %w", err)
	}
	id, err := q.InsertAutomatedMessage(ctx, store.InsertAutomatedMessageParams{
		ConversationID: convID, ChannelAccountID: acc.ID, ClientRef: pgtype.UUID{Bytes: ref, Valid: true}, Body: in.Text,
		QuickReplies: qrs, SentAt: now,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi tin tự động: %w", err)
	}
	snip := deps.Snippet(in.Text, false)
	if err := q.BumpConversationAutomated(ctx, store.BumpConversationAutomatedParams{
		At: now, Snippet: pgtype.Text{String: snip, Valid: snip != ""}, ID: convID,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("cập nhật hội thoại sau tin tự động: %w", err)
	}
	tenant := acc.TenantID
	jobID, _, err := d.Queue.Enqueue(ctx, tx, AutoCommandKind(), queue.NewJob{
		TenantID: &tenant, Payload: payload{MessageID: id}, DedupeKey: id.String(),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi lệnh gửi tin tự động: %w", err)
	}
	if err := q.SetMessageCommandJob(ctx, store.SetMessageCommandJobParams{
		CommandJobID: pgtype.UUID{Bytes: jobID, Valid: true}, ID: id,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("gắn lệnh gửi vào tin tự động: %w", err)
	}
	if err := publish(ctx, tx, d.Bus, tenant, acc.ID, convID); err != nil {
		return uuid.Nil, err
	}
	return jobID, nil
}

// quickReplies đọc cột messages.quick_replies (NULL → không có nút).
func quickReplies(raw []byte) ([]channel.QuickReply, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var in []quickReply
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("đọc nút trả lời nhanh của tin: %w", err)
	}
	out := make([]channel.QuickReply, 0, len(in))
	for _, r := range in {
		out = append(out, channel.QuickReply(r))
	}
	return out, nil
}

// Settled là hàm bên gọi nhận kết quả cuối của lệnh gửi tự động jobID (commands.StatusDone, queue.StatusDead, StatusExpired,
// StatusInDoubt), trong cùng tx của Runner, sau khi tin đã đổi trạng thái (automation ghi kết quả lượt chạy).
type Settled func(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, status string) error

// AutoCommand là lệnh command.fb.auto_send: gửi như lệnh của nhân viên (prepare hỏi SendPolicy với Automated, lượt
// gửi của Page, OnDone gộp echo) nhưng không có Looker; không rõ hay quá hạn → tin discarded (không hỏi nhân viên, không
// tự gửi lại; 04 mục 5.3). Mọi kết quả cuối báo cho settled.
type AutoCommand struct {
	base    sendCommand
	settled Settled
}

// NewAutoCommand dựng lệnh gửi tự động; settled nhận kết quả cuối (nil: không báo).
func NewAutoCommand(d deps.Deps, settled Settled) AutoCommand {
	return AutoCommand{base: newSendCommand(d), settled: settled}
}

// Kind là Kind của lệnh gửi tự động.
func (AutoCommand) Kind() queue.Kind { return AutoCommandKind() }

// AutoRetryNotSent cho biết tin chắc chắn chưa tới kênh thì được tự gửi lại trong hạn 5 phút.
func (AutoCommand) AutoRetryNotSent() bool { return true }

// Execute gửi tin như lệnh gửi của nhân viên (ngoài transaction).
func (c AutoCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.base.Execute(ctx, j)
}

// OnDone đặt tin sent (như lệnh của nhân viên) rồi báo kết quả done.
func (c AutoCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	if err := c.base.OnDone(ctx, j, raw, tx); err != nil {
		return err
	}
	return c.report(ctx, tx, j.ID, commands.StatusDone)
}

// OnTerminal đổi tin khi lệnh kết thúc không thành: dead → failed; expired, in_doubt → tin discarded; rồi báo kết quả.
func (c AutoCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if err := c.base.settle(ctx, j, status, tx, true); err != nil {
		return err
	}
	return c.report(ctx, tx, j.ID, status)
}

func (c AutoCommand) report(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, status string) error {
	if c.settled == nil {
		return nil
	}
	return c.settled(ctx, tx, jobID, status)
}
