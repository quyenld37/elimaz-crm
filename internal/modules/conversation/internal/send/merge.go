package send

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// target là tin của lệnh j đọc không khóa (mã hội thoại, Page, mã lý do đã ghi); ok = false khi payload hỏng hay tin
// không còn (không có gì để đổi).
func target(ctx context.Context, tx pgx.Tx, j queue.Job) (store.GetOutgoingMessageRow, bool, error) {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.MessageID == uuid.Nil {
		return store.GetOutgoingMessageRow{}, false, nil
	}
	m, err := store.New(tx).GetOutgoingMessage(ctx, p.MessageID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return m, false, nil
	case err != nil:
		return m, false, fmt.Errorf("đọc tin của lệnh gửi: %w", err)
	}
	return m, true, nil
}

// lock khóa hội thoại rồi tin của lệnh (ADR-46 nhóm 6: conversations → messages).
func lock(ctx context.Context, tx pgx.Tx, m store.GetOutgoingMessageRow) (store.LockOutgoingMessageRow, error) {
	q := store.New(tx)
	if _, err := q.LockConversationForSend(ctx, m.ConversationID); err != nil {
		return store.LockOutgoingMessageRow{}, fmt.Errorf("khóa hội thoại của tin gửi: %w", err)
	}
	row, err := q.LockOutgoingMessage(ctx, m.ID)
	if err != nil {
		return row, fmt.Errorf("khóa tin gửi: %w", err)
	}
	return row, nil
}

// OnDone (trong transaction đổi lệnh sang done): kênh xin giãn → NoteBlockedUntil (nhóm 3, trước mọi khóa nhóm 6);
// khóa hội thoại, tin; xóa dòng own_echo_unmatched cùng mã tin của kênh (echo không metadata tới trước, Q23; cả dòng
// page_external của echo đọc qua hỏi bù, đổi nhãn trước, Task 157) và chép
// mốc của kênh, đã nhận, đã xem, cảm xúc của nó; đặt tin sent kèm mã ngoài; báo hiệu. result nil (done do tra lại hay
// người quyết) → tin sent, giữ mã ngoài đã có.
func (c sendCommand) OnDone(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx) error {
	return c.done(ctx, j, raw, tx, c.settleSent)
}

// settleFunc đặt tin m của lệnh j sent với mã ngoài ext (rỗng: giữ mã đã có).
type settleFunc func(ctx context.Context, tx pgx.Tx, j queue.Job, m store.GetOutgoingMessageRow, ext string) error

// done là OnDone của lệnh có tin: begin rồi settle với mã ngoài của kết quả.
func (c sendCommand) done(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx, settle settleFunc) error {
	res, m, ok, err := c.begin(ctx, j, raw, tx)
	if err != nil || !ok {
		return err
	}
	return settle(ctx, tx, j, m, res.ExternalID)
}

// begin là phần đầu của OnDone mọi lệnh ra kênh: đọc kết quả, tin của lệnh (ok = false: không còn gì để đổi); kênh
// xin giãn → NoteBlockedUntil (nhóm 3, trước mọi khóa nhóm 5, 6).
func (c sendCommand) begin(ctx context.Context, j queue.Job, raw json.RawMessage, tx pgx.Tx,
) (result, store.GetOutgoingMessageRow, bool, error) {
	var res result
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &res); err != nil {
			return res, store.GetOutgoingMessageRow{}, false, fmt.Errorf("đọc kết quả lệnh gửi: %w", err)
		}
	}
	m, ok, err := target(ctx, tx, j)
	if err != nil || !ok {
		return res, m, false, err
	}
	if res.SlowDownMs > 0 {
		until := c.now().UTC().Add(time.Duration(res.SlowDownMs) * time.Millisecond)
		if err := channel.NoteBlockedUntil(ctx, tx, m.ChannelAccountID, until); err != nil {
			return res, m, false, err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
	}
	return res, m, true, nil
}

// settleSent khóa hội thoại, tin m; đổi nhãn echo hỏi bù cùng mã ext (Task 157); xóa dòng own_echo_unmatched cùng
// mã ext (echo không metadata tới trước) và chép mốc, đã nhận, đã xem, cảm xúc của nó; đặt tin sent kèm mã ngoài
// (ext rỗng: giữ mã đã có); báo hiệu.
func (c sendCommand) settleSent(ctx context.Context, tx pgx.Tx, j queue.Job, m store.GetOutgoingMessageRow, ext string) error {
	if _, err := lock(ctx, tx, m); err != nil {
		return err
	}
	q := store.New(tx)
	mark := store.MarkOwnMessageSentParams{SentAt: c.now().UTC(), ID: m.ID}
	if ext != "" {
		mark.ExternalID = pgtype.Text{String: ext, Valid: true}
		// Echo đọc qua hỏi bù (page_external, không metadata) cùng mid kênh vừa trả là echo của chính tin này (Task 157).
		if _, err := q.RelabelExternalEcho(ctx, store.RelabelExternalEchoParams{
			ChannelAccountID: m.ChannelAccountID, ExternalID: mark.ExternalID,
		}); err != nil {
			return fmt.Errorf("nhận echo hỏi bù của tin gửi: %w", err)
		}
		echoes, err := q.DeleteUnmatchedEcho(ctx, store.DeleteUnmatchedEchoParams{
			ChannelAccountID: m.ChannelAccountID, ExternalID: mark.ExternalID,
		})
		if err != nil {
			return fmt.Errorf("gộp echo của tin gửi: %w", err)
		}
		for _, e := range echoes { // tối đa một dòng (unique theo mã ngoài)
			mark.SentAt, mark.DeliveredAt, mark.ReadAt, mark.Reactions = e.SentAt.UTC(), e.DeliveredAt, e.ReadAt, e.Reactions
		}
	}
	return c.markSent(ctx, tx, j, m, mark)
}

// markSent đặt tin m (đã khóa) sent theo mark rồi báo hiệu; không đổi dòng nào (echo có metadata đã đặt sent trước) →
// không báo.
func (c sendCommand) markSent(ctx context.Context, tx pgx.Tx, j queue.Job, m store.GetOutgoingMessageRow,
	mark store.MarkOwnMessageSentParams,
) error {
	n, err := store.New(tx).MarkOwnMessageSent(ctx, mark)
	if err != nil {
		return fmt.Errorf("đặt tin đã gửi: %w", err)
	}
	if n == 0 {
		return nil
	}
	return publish(ctx, tx, c.d.Bus, *j.TenantID, m.ChannelAccountID, m.ConversationID)
}

// OnTerminal (trong transaction đổi lệnh sang dead, expired, in_doubt), chỉ đổi tin còn queued: dead → failed (giữ mã
// lý do lệnh đã ghi; blocked_by_user → hội thoại bị chặn; auth → channel.Writer.ReportTokenInvalid, nhóm 3 nên gọi trước khi
// khóa hội thoại); expired → expired; in_doubt → checking, send_checking_since = now (lịch send_reveal của Task 157 hiện in_doubt sau 60 giây). Báo hiệu khi đổi.
func (c sendCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	return c.settle(ctx, j, status, tx, false)
}

// settleParams là trạng thái mới của tin m khi lệnh kết thúc với status lúc now; ok = false: không đổi.
func settleParams(m store.GetOutgoingMessageRow, status string, auto bool, now time.Time,
) (store.SettleOutgoingMessageParams, bool) {
	set := store.SettleOutgoingMessageParams{ID: m.ID}
	switch {
	case auto && (status == queue.StatusExpired || status == queue.StatusInDoubt):
		set.SendStatus = pgtype.Text{String: statusDiscarded, Valid: true}
		set.FailReason = pgtype.Text{String: reasonAutoDropped, Valid: true}
	case status == queue.StatusDead:
		set.SendStatus = pgtype.Text{String: statusFailed, Valid: true}
		if !m.FailReason.Valid {
			set.FailReason = pgtype.Text{String: reasonRejected, Valid: true}
		}
	case status == queue.StatusExpired:
		set.SendStatus = pgtype.Text{String: statusExpired, Valid: true}
		if !m.FailReason.Valid {
			set.FailReason = pgtype.Text{String: reasonExpiredQueued, Valid: true}
		}
	case status == queue.StatusInDoubt:
		set.SendStatus = pgtype.Text{String: statusChecking, Valid: true}
		set.SendCheckingSince = pgtype.Timestamptz{Time: now, Valid: true}
	default:
		return set, false
	}
	return set, true
}

// settle là thân của OnTerminal; auto (lệnh command.fb.auto_send, command.fb.auto_reply_comment,
// command.fb.auto_private_reply): expired, in_doubt → discarded với lý do
// reasonAutoDropped (04 mục 5.3: tin tự động không rõ hay quá hạn thì bỏ, không hỏi nhân viên, không gửi lại).
func (c sendCommand) settle(ctx context.Context, j queue.Job, status string, tx pgx.Tx, auto bool) error {
	m, ok, err := target(ctx, tx, j)
	if err != nil || !ok || m.SendStatus.String != statusQueued {
		return err
	}
	set, ok := settleParams(m, status, auto, c.now().UTC())
	if !ok {
		return nil
	}
	if status == queue.StatusDead && m.FailReason.String == reasonAuth {
		if err := (channel.Writer{Realtime: c.d.Bus}).ReportTokenInvalid(ctx, tx, m.ChannelAccountID); err != nil {
			return err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
	}
	if _, err := lock(ctx, tx, m); err != nil {
		return err
	}
	q := store.New(tx)
	n, err := q.SettleOutgoingMessage(ctx, set)
	if err != nil {
		return fmt.Errorf("đổi trạng thái tin gửi: %w", err)
	}
	if n == 0 {
		return nil
	}
	if status == queue.StatusDead && m.FailReason.String == string(channel.DenyBlockedByUser) {
		if err := q.MarkConversationBlocked(ctx, m.ConversationID); err != nil {
			return fmt.Errorf("ghi khách chặn Page: %w", err)
		}
	}
	return publish(ctx, tx, c.d.Bus, *j.TenantID, m.ChannelAccountID, m.ConversationID)
}
