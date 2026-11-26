package send

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Chỉ số của lệnh gửi.
const (
	// metricClickToAPI: từ lúc nhân viên bấm gửi (sent_at của tin queued, đồng hồ của API) tới lúc bắt đầu gọi kênh;
	// nhãn là khóa kênh (04 mục 1: p95 dưới 1 giây).
	metricClickToAPI = "send_click_to_api_ms"
	// minBucketWait: chờ tối thiểu khi thùng lượt gửi của Page đã hết (tránh hẹn 0 ms).
	minBucketWait = 50 * time.Millisecond
	// Mã lý do của outcome không đổi trạng thái tin (last_error của việc, không có nội dung).
	outcomeBadPayload = "bad_payload"
	outcomeNotQueued  = "not_queued"
	outcomePrepare    = "prepare_failed"
	outcomeBucket     = "send_bucket"
	outcomeBucketWait = "send_bucket_empty"
)

// result là kết quả lệnh đã gửi (lưu ở jobs.result, truyền cho OnDone): mã tin của kênh, người nhận kênh báo (chỉ lệnh
// nhắn riêng: PSID để gắn danh tính, Task 158) và thời gian kênh xin giãn (mức dùng của Page cao, tệp task-156, Quyết
// định 5). Không nội dung.
type result struct {
	ExternalID  string `json:"externalId,omitempty"`
	RecipientID string `json:"recipientId,omitempty"`
	SlowDownMs  int64  `json:"slowDownMs,omitempty"`
}

// sendCommand là lệnh command.fb.send_message: Execute (ngoài transaction), OnDone, OnTerminal (merge.go).
type sendCommand struct {
	d   deps.Deps
	now func() time.Time
}

func newSendCommand(d deps.Deps) sendCommand {
	return sendCommand{d: d, now: d.Clock()}
}

func (sendCommand) Kind() queue.Kind { return CommandKind() }

// AutoRetryNotSent: tin chắc chắn chưa tới kênh thì tự gửi lại trong hạn 3 phút.
func (sendCommand) AutoRetryNotSent() bool { return true }

// prepared là mọi thứ cần để gọi kênh, đọc trong một transaction chỉ đọc rồi dùng ngoài transaction.
type prepared struct {
	acc       channel.ChannelAccount
	adapter   channel.MessagingAdapter
	cred      channel.Credentials
	cmd       channel.OutboundCommand
	clickedAt time.Time
}

// Execute gửi tin của lệnh j: (1) transaction chỉ đọc: tin còn queued (không thì rejected, không đổi tin), dựng lại
// trạng thái hội thoại và hỏi lại SendPolicy (không cho → ghi mã lý do vào tin, rejected), giải mã token, chọn biến thể
// JPEG lớn nhất của ảnh; (2) lấy lượt gửi của Page (hết → not_sent chờ đúng thời gian nạp, không tiêu lượt);
// (3) gọi kênh. Lỗi của chính CRM trước khi gọi kênh → not_sent (chưa gửi gì, tự thử lại), không bao giờ in_doubt.
func (c sendCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	return c.run(ctx, j, func(ctx context.Context, tx pgx.Tx, p payload, now time.Time) (prepared, string, error) {
		return c.prepare(ctx, tx, p.MessageID, now)
	})
}

// prepareFunc đọc mọi thứ cần gọi kênh cho payload p trong transaction chỉ đọc tx (xem sendCommand.prepare).
type prepareFunc func(ctx context.Context, tx pgx.Tx, p payload, now time.Time) (prepared, string, error)

// run là thân chung của lệnh ra kênh (gửi tin, trả lời comment, nhắn riêng, ẩn/hiện): prep trong transaction chỉ đọc
// (lỗi → not_sent; outcomeNotQueued → rejected không đổi tin; mã khác → ghi vào tin, rejected), lấy lượt gửi của Page,
// gọi kênh, đổi kết quả bằng outcome.
func (c sendCommand) run(ctx context.Context, j queue.Job, prep prepareFunc) (commands.Outcome, error) {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.MessageID == uuid.Nil || j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: outcomeBadPayload}, nil
	}
	tenant, now := *j.TenantID, c.now().UTC()
	var pr prepared
	var refusal string
	err := tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
		var err error
		pr, refusal, err = prep(ctx, tx, p, now) // gán lại mỗi lần chạy
		return err
	})
	switch {
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: outcomePrepare}, nil
	case refusal == outcomeNotQueued:
		return commands.Outcome{Status: commands.StatusRejected, Reason: refusal}, nil
	case refusal != "":
		return c.refuse(ctx, tenant, p.MessageID, refusal), nil
	}
	ok, wait, err := channel.AcquireSendToken(ctx, c.d.DB, tenant, pr.acc.ID, now)
	switch {
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: outcomeBucket}, nil
	case !ok:
		return commands.Outcome{
			Status: commands.StatusNotSent, Reason: outcomeBucketWait, RetryAfter: max(wait, minBucketWait), KeepAttempt: true,
		}, nil
	}
	if c.d.Metrics != nil {
		c.d.Metrics.Observe(metricClickToAPI, float64(c.now().Sub(pr.clickedAt).Milliseconds()), pr.acc.Channel)
	}
	out, sendErr := pr.adapter.Send(ctx, pr.acc, pr.cred, pr.cmd)
	if pr.cmd.Purpose != channel.PurposePrivateReply {
		out.RecipientID = "" // PSID chỉ cần cho nhắn riêng (gắn danh tính, OnDone); không lưu thừa ở jobs.result
	}
	return c.outcome(ctx, tenant, p.MessageID, pr.adapter, out, sendErr), nil
}

// outcome đổi kết quả của kênh sang kết quả lệnh; lý do bị từ chối, chưa gửi được ghi vào tin để OnTerminal dùng.
func (c sendCommand) outcome(ctx context.Context, tenant, msgID uuid.UUID, ad channel.MessagingAdapter,
	out channel.SendOutcome, sendErr error,
) commands.Outcome {
	switch out.Status {
	case channel.OutcomeSent:
		raw, err := json.Marshal(result{
			ExternalID: out.ExternalID, RecipientID: out.RecipientID, SlowDownMs: out.RetryAfter.Milliseconds(),
		})
		if err != nil { // chuỗi và số, không lỗi; phòng hờ thì không rõ đã gửi
			return commands.Outcome{Status: commands.StatusUnknown, Reason: "encode_result"}
		}
		return commands.Outcome{Status: commands.StatusDone, Result: raw}
	case channel.OutcomeRejected:
		code := string(out.Reason)
		switch {
		case sendErr != nil && ad.ClassifyError(sendErr) == channel.ErrorAuth:
			code = reasonAuth
		case code == "":
			code = reasonRejected
		}
		return c.refuse(ctx, tenant, msgID, code)
	case channel.OutcomeNotSent:
		code := reasonNotSent
		if out.RetryAfter > 0 {
			code = reasonRateLimited
		}
		c.note(ctx, tenant, msgID, code)
		return commands.Outcome{Status: commands.StatusNotSent, Reason: code, RetryAfter: out.RetryAfter}
	case channel.OutcomeUnknown:
	}
	return commands.Outcome{Status: commands.StatusUnknown, Reason: reasonInDoubt}
}

// refuse ghi mã lý do code vào tin rồi trả rejected (Runner ghi dead, OnTerminal đổi tin sang failed).
func (c sendCommand) refuse(ctx context.Context, tenant, msgID uuid.UUID, code string) commands.Outcome {
	c.note(ctx, tenant, msgID, code)
	return commands.Outcome{Status: commands.StatusRejected, Reason: code}
}

// note ghi mã lý do vào tin còn queued, trong transaction ngắn riêng. Ghi lỗi thì bỏ qua: OnTerminal vẫn đổi tin với
// lý do chung (mã lý do chỉ để hiện câu cho nhân viên).
func (c sendCommand) note(ctx context.Context, tenant, msgID uuid.UUID, code string) {
	_ = tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
		_, err := store.New(tx).NoteSendRejection(ctx, store.NoteSendRejectionParams{
			FailReason: pgtype.Text{String: code, Valid: true}, ID: msgID,
		})
		return err //nolint:wrapcheck // bỏ qua ở trên
	})
}

// prepare đọc tin msgID và mọi thứ cần gửi. refusal khác rỗng: không gửi (outcomeNotQueued: tin đã đổi trạng thái;
// mã khác: lý do ghi vào tin). err: lỗi tạm của database.
func (c sendCommand) prepare(ctx context.Context, tx pgx.Tx, msgID uuid.UUID, now time.Time) (prepared, string, error) {
	q := store.New(tx)
	m, err := q.GetOutgoingMessage(ctx, msgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return prepared{}, outcomeNotQueued, nil
	case err != nil:
		return prepared{}, "", fmt.Errorf("đọc tin cần gửi: %w", err)
	case m.SendStatus.String != statusQueued:
		return prepared{}, outcomeNotQueued, nil
	}
	conv, err := q.GetConversationForSend(ctx, m.ConversationID)
	if err != nil {
		return prepared{}, "", fmt.Errorf("đọc hội thoại cần gửi: %w", err)
	}
	pr, pol, refusal, err := c.channelFor(ctx, tx, m.ChannelAccountID, func(acc channel.ChannelAccount) channel.ConversationState {
		return state(conv, acc, m.Origin == originAuto)
	}, now)
	if err != nil || refusal != "" {
		return prepared{}, refusal, err
	}
	cmd := channel.OutboundCommand{
		Purpose: channel.PurposeMessage, RecipientID: conv.ExternalThreadRef, Text: m.Body.String, Mode: pol.Mode,
		ClientRef: uuid.UUID(m.ClientRef.Bytes).String(),
	}
	if cmd.ImageURL, err = c.imageURL(ctx, tx, m.Attachments); err != nil {
		return prepared{}, "", err
	}
	if cmd.Text == "" && cmd.ImageURL == "" {
		return prepared{}, reasonNoAttachment, nil
	}
	pr.cmd, pr.clickedAt = cmd, m.SentAt
	return pr, "", nil
}

// imageURL trả đường dẫn công khai của biến thể JPEG lớn nhất (bộ kích thước mới nhất) của ảnh đầu tiên trong cột
// attachments; tin chữ hay ảnh không còn biến thể → rỗng (M1 không trả tệp private, R10).
func (c sendCommand) imageURL(ctx context.Context, tx pgx.Tx, raw []byte) (string, error) {
	var atts []Attachment
	if err := json.Unmarshal(raw, &atts); err != nil || len(atts) == 0 || atts[0].FileID == nil {
		return "", nil //nolint:nilerr // cột hỏng hay rỗng = không có ảnh
	}
	id := *atts[0].FileID
	vs, err := files.PublicVariants(ctx, tx, []uuid.UUID{id}, c.d.Config.FilesPublicBaseURL)
	if err != nil {
		return "", fmt.Errorf("đọc biến thể ảnh gửi: %w", err)
	}
	jpegs := slices.DeleteFunc(vs[id], func(v files.VariantURL) bool { return v.Format != "jpeg" })
	if len(jpegs) == 0 {
		return "", nil
	}
	best := slices.MaxFunc(jpegs, func(a, b files.VariantURL) int {
		return cmp.Or(cmp.Compare(a.Preset, b.Preset), cmp.Compare(a.Width, b.Width))
	})
	return best.URL, nil
}
