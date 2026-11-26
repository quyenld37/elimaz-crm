// Package history là phần của module conversation: lấy 20 tin gần nhất khi có hội thoại mới, giữ hội thoại 24 tháng
// (Task 164); đọc comment định kỳ khi webhook feed không về (Task 150, backfill.go).
package history

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
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Lịch sử tin cũ (Q33): hội thoại hộp thư vừa tạo → lệnh chỉ đọc command.fb.fetch_history (ghi trong tx của tin vào);
// lệnh đọc tối đa 20 tin gần nhất qua channel.HistoryFetcher ngoài transaction, trả tin trong Outcome.Result (lưu ở
// jobs.result tới khi lịch dọn việc đã xong xóa); OnDone chèn mỗi tin origin = history (ON CONFLICT DO NOTHING: tin
// vừa kích hoạt hội thoại không bị nhân đôi), không đổi bộ đếm, mốc, đoạn trích của hội thoại, không gọi hook tự động,
// một báo hiệu cho cả lô. Payload lệnh chỉ có mã hội thoại (PSID, token đọc lúc chạy).

const (
	// FetchHistoryKind là lệnh đọc lịch sử tin của hội thoại hộp thư mới.
	FetchHistoryKind = "command.fb.fetch_history"
	// Limit là số tin gần nhất đọc lại (Q33).
	Limit = 20

	fetchExpiry  = time.Hour
	fetchTimeout = 30 * time.Second

	kindInbox     = "inbox"
	kindMessage   = "message"
	originHistory = "history"
	dirIn, dirOut = "in", "out"
	// permConversationRead là quyền báo hiệu hội thoại đòi (Q36: theo Page).
	permConversationRead = identity.Permission("conversation.read")
	// defaultRetentionMonths là hạn giữ khi cấu hình không đặt (vai trò khác worker).
	defaultRetentionMonths = 24

	// Lý do (last_error) của lệnh; không chứa token, nội dung.
	reasonBadPayload   = "bad_payload"
	reasonNotInbox     = "not_inbox"
	reasonNoAccount    = "account_missing"
	reasonNoCreds      = "no_credentials"
	reasonCredsBroken  = "credentials_unreadable"
	reasonNoFetcher    = "no_history_fetcher"
	reasonReadFailed   = "read_failed"
	reasonEncodeResult = "encode_result"
	// conversationMissing: hội thoại không còn (đã dọn theo hạn giữ) — lệnh xong, không ghi gì.
	conversationMissing = "conversation_missing"

	resultMessagesEmpty          = `{"messages":[]}`
	fetchHistoryDedupePrefix     = "fetch_history:"
	retentionContinueDedupPrefix = "conversation.retention:continue:"
)

// Parts là phần chạy nền của phần history: lịch đọc comment định kỳ (Task 150), lịch và việc dọn theo hạn giữ (luôn
// có); lệnh đọc lịch sử khi có adapter đọc được lịch sử (Task 164).
func Parts(d deps.Deps) deps.Parts {
	p := pollParts(d)
	p.Jobs = append(p.Jobs, NewRetentionHandler(d, retentionBatch))
	p.Schedules = append(p.Schedules, retentionSchedule())
	if anyFetcher(d.Registry) {
		p.Commands = []commands.Handler{fetchHistory{d: d, now: d.Clock()}}
	}
	return p
}

// anyFetcher cho biết bộ adapter có ít nhất một adapter đọc được lịch sử.
func anyFetcher(r *channel.Registry) bool {
	if r == nil {
		return false
	}
	for _, k := range r.Keys() {
		if fetcherOf(r, k) != nil {
			return true
		}
	}
	return false
}

// fetcherOf trả adapter đọc lịch sử của kênh key, nil khi không có.
func fetcherOf(r *channel.Registry, key string) channel.HistoryFetcher { //nolint:ireturn // năng lực tùy chọn của adapter
	f, _ := deps.AdapterAs[channel.HistoryFetcher](r, key)
	return f
}

// fetchPayload là payload của lệnh đọc lịch sử.
type fetchPayload struct {
	ConversationID uuid.UUID `json:"conversationId"`
}

// historyMessage là một tin đọc lại (Outcome.Result của lệnh: {"messages": [...]}).
type historyMessage struct {
	MID         string                  `json:"mid"`
	FromPage    bool                    `json:"fromPage,omitempty"`
	Text        string                  `json:"text,omitempty"`
	Attachments []channel.AttachmentRef `json:"attachments,omitempty"`
	SentAt      time.Time               `json:"sentAt"`
}

type fetchResult struct {
	Messages []historyMessage `json:"messages"`
}

func fetchKind() queue.Kind { return commands.CommandKind(FetchHistoryKind, fetchExpiry, fetchTimeout) }

// OnNewConversation (hook của đường tin vào, chạy trong tx của sự kiện vào khi hội thoại hộp thư conversationID của
// Page acc vừa được tạo) xếp lệnh đọc lịch sử nếu adapter của kênh đọc được lịch sử; kênh khác → không làm gì.
func OnNewConversation(ctx context.Context, tx pgx.Tx, d deps.Deps, acc channel.ChannelAccount, conversationID uuid.UUID) error {
	if fetcherOf(d.Registry, acc.Channel) == nil {
		return nil
	}
	tenant := acc.TenantID
	if _, _, err := d.Queue.Enqueue(ctx, tx, fetchKind(), queue.NewJob{
		TenantID: &tenant, Payload: fetchPayload{ConversationID: conversationID},
		DedupeKey: fetchHistoryDedupePrefix + conversationID.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh đọc lịch sử tin: %w", err)
	}
	return nil
}

// fetchHistory là lệnh chỉ đọc command.fb.fetch_history.
type fetchHistory struct {
	d   deps.Deps
	now func() time.Time
}

func (fetchHistory) Kind() queue.Kind       { return fetchKind() }
func (fetchHistory) AutoRetryNotSent() bool { return true }

// target là mọi thứ cần để đọc lịch sử, đọc trong một transaction ngắn.
type target struct {
	acc  channel.ChannelAccount
	cred channel.Credentials
	psid string
}

// load đọc hội thoại (PSID), tài khoản, token của lệnh j; out khác nil là kết quả cuối Execute trả ngay.
func (c fetchHistory) load(ctx context.Context, j queue.Job) (target, *commands.Outcome) {
	var p fetchPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.ConversationID == uuid.Nil || j.TenantID == nil {
		return target{}, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}
	}
	var tg target
	var reason string
	err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		tg, reason, err = readTarget(ctx, tx, c.d, p.ConversationID) // gán lại mỗi lần chạy
		return err
	})
	switch {
	case errors.Is(err, channel.ErrAccountNotFound):
		return target{}, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoAccount}
	case err != nil:
		return target{}, &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}
	case reason == conversationMissing:
		// Hội thoại đã bị xóa (dọn theo hạn giữ): không còn gì để ghi.
		return target{}, &commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(resultMessagesEmpty)}
	case reason != "":
		return target{}, &commands.Outcome{Status: commands.StatusRejected, Reason: reason}
	}
	return tg, nil
}

// readTarget đọc hội thoại id, tài khoản Page và token trong tx; reason khác rỗng: không đọc được lịch sử.
func readTarget(ctx context.Context, tx pgx.Tx, d deps.Deps, id uuid.UUID) (target, string, error) {
	conv, err := store.New(tx).GetConversationForSend(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return target{}, conversationMissing, nil
	case err != nil:
		return target{}, "", fmt.Errorf("đọc hội thoại cần lấy lịch sử: %w", err)
	case conv.Kind != kindInbox:
		return target{}, reasonNotInbox, nil
	}
	acc, err := channel.Account(ctx, tx, conv.ChannelAccountID)
	if err != nil {
		return target{}, "", err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	cred, err := channel.CredentialsFor(ctx, tx, d.Keys, acc.ID)
	switch {
	case errors.Is(err, channel.ErrNoCredentials), errors.Is(err, channel.ErrNoKeyring):
		return target{}, reasonNoCreds, nil
	case err != nil:
		return target{}, reasonCredsBroken, nil
	}
	return target{acc: acc, cred: cred, psid: conv.ExternalThreadRef}, "", nil
}

// Execute đọc tối đa Limit tin gần nhất qua adapter của kênh, ngoài transaction. Kênh báo không còn hội thoại (lỗi
// vĩnh viễn) → done rỗng; chưa gửi, quá tải → not_sent (tự thử lại trong 1 giờ); token, quyền, chính sách → rejected;
// còn lại → unknown (in_doubt; Lookup trả not_sent vì lệnh chỉ đọc).
func (c fetchHistory) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	tg, out := c.load(ctx, j)
	if out != nil {
		return *out, nil
	}
	f := fetcherOf(c.d.Registry, tg.acc.Channel)
	if f == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoFetcher}, nil
	}
	ad, _ := c.d.Registry.Get(tg.acc.Channel) // có fetcher nên bộ adapter, kênh đều có
	evs, err := f.FetchHistory(ctx, tg.acc, tg.cred, tg.psid, Limit)
	if err != nil {
		return errorOutcome(ad.ClassifyError(err)), nil
	}
	b, err := json.Marshal(fetchResult{Messages: toMessages(evs)})
	if err != nil {
		return commands.Outcome{Status: commands.StatusUnknown, Reason: reasonEncodeResult}, nil //nolint:nilerr // kiểu cố định; phòng thủ
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// errorOutcome đổi lớp lỗi của kênh thành kết quả của lệnh chỉ đọc lịch sử.
func errorOutcome(class channel.ErrorClass) commands.Outcome {
	out := commands.Outcome{Status: commands.StatusUnknown, Reason: string(class)}
	switch class {
	case channel.ErrorPermanent:
		out = commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(resultMessagesEmpty)}
	case channel.ErrorNotSentRetry, channel.ErrorRateLimited:
		out.Status = commands.StatusNotSent
	case channel.ErrorAuth, channel.ErrorPolicy:
		out.Status = commands.StatusRejected
	default:
	}
	return out
}

// toMessages đổi sự kiện tin (message.received của khách, message.echo của Page) thành tin lịch sử; bỏ loại khác và
// payload hỏng.
func toMessages(evs []webhooks.InboundEvent) []historyMessage {
	out := make([]historyMessage, 0, len(evs))
	for _, ev := range evs {
		if ev.Type != channel.EventMessageReceived && ev.Type != channel.EventMessageEcho {
			continue
		}
		var p channel.MessagePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil || p.MID == "" || p.SentAt.IsZero() {
			continue
		}
		out = append(out, historyMessage{
			MID: p.MID, FromPage: ev.Type == channel.EventMessageEcho, Text: p.Text, Attachments: p.Attachments,
			SentAt: p.SentAt.UTC(),
		})
	}
	return out
}

// Lookup: lệnh chỉ đọc, chạy lại không có tác dụng phụ ngoài → luôn not_sent (về hàng chờ).
func (fetchHistory) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// OnDone (trong tx đổi lệnh sang done) chèn các tin đọc lại vào hội thoại, origin = history; tin đã có (cùng mã ngoài)
// và tin đã quá hạn giữ thì bỏ. Có tin mới → một báo hiệu hội thoại đổi. Hội thoại không còn, không có kết quả (done do
// tra lại) → không làm gì.
func (c fetchHistory) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if len(result) == 0 || j.TenantID == nil {
		return nil
	}
	var p fetchPayload
	var res fetchResult
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return fmt.Errorf("đọc payload lệnh đọc lịch sử: %w", err)
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return fmt.Errorf("đọc kết quả lệnh đọc lịch sử: %w", err)
	}
	q := store.New(tx)
	conv, err := q.GetConversationForSend(ctx, p.ConversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đọc hội thoại lấy lịch sử: %w", err)
	}
	cutoff := retentionCutoff(c.d, c.now())
	inserted := 0
	for _, m := range res.Messages {
		ok, err := insertHistory(ctx, q, conv, m, cutoff)
		if err != nil {
			return err
		}
		if ok {
			inserted++
		}
	}
	if inserted == 0 {
		return nil
	}
	if err := c.d.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: *j.TenantID, Topic: deps.TopicConversationChanged, Requires: permConversationRead,
		Scope: access.PageScope(conv.ChannelAccountID), Entity: realtime.Entity{Type: deps.EntityConversation, ID: conv.ID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu hội thoại có lịch sử tin: %w", err)
	}
	return nil
}

// insertHistory chèn tin m vào hội thoại conv; ok = false khi tin đã có hay đã quá hạn giữ (trước cutoff).
func insertHistory(ctx context.Context, q *store.Queries, conv store.GetConversationForSendRow, m historyMessage,
	cutoff time.Time,
) (bool, error) {
	if m.MID == "" || m.SentAt.Before(cutoff) {
		return false, nil
	}
	// Tin lịch sử chưa xếp lệnh tải tệp (Task 155 chỉ nối đường tin vào): không giữ đường dẫn có chữ ký của kênh.
	atts, err := deps.PendingAttachments(deps.WithoutURLs(m.Attachments))
	if err != nil {
		return false, err //nolint:wrapcheck // deps đã bọc việc đang làm
	}
	dir := dirIn
	if m.FromPage {
		dir = dirOut
	}
	_, err = q.InsertInboundMessage(ctx, store.InsertInboundMessageParams{
		ConversationID: conv.ID, ChannelAccountID: conv.ChannelAccountID, ExternalID: text(m.MID), Direction: dir,
		Kind: kindMessage, Origin: originHistory, Body: text(m.Text), Attachments: atts, SentAt: m.SentAt.UTC(),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil // tin đã có
	case err != nil:
		return false, fmt.Errorf("ghi tin lịch sử: %w", err)
	}
	return true, nil
}

// retentionCutoff là mốc hạn giữ lúc now: tin gửi trước mốc này bị dọn (CONVERSATION_RETENTION_MONTHS, mặc định 24).
func retentionCutoff(d deps.Deps, now time.Time) time.Time {
	m := d.Config.ConversationRetentionMonths
	if m <= 0 {
		m = defaultRetentionMonths
	}
	return now.UTC().AddDate(0, -m, 0)
}

// text là pgtype.Text, chuỗi rỗng → NULL.
func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
