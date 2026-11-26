package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Đăng ký và hủy đăng ký webhook của Page (kế hoạch M4, Task 149): hai lệnh ra ngoài ghi trong transaction của
// Writer.Connect, Writer.Disconnect. Payload chỉ có mã tài khoản: token đọc từ dòng tài khoản lúc chạy (không bao giờ
// nằm trong platform.jobs). Đua với nối lại/ngắt lại được chặn ở hai chỗ: Execute bỏ qua (done, không gọi kênh) khi
// trạng thái tài khoản không còn đúng loại lệnh; hook khóa dòng rồi kiểm lại trạng thái trước khi ghi.

const (
	// SubscribePageKind là lệnh đăng ký webhook (11 trường) cho Page vừa nối.
	SubscribePageKind = "command.fb.subscribe_page"
	// UnsubscribePageKind là lệnh hủy đăng ký webhook của Page vừa ngắt; khi lệnh kết thúc (done, dead, expired) token
	// bị xóa.
	UnsubscribePageKind = "command.fb.unsubscribe_page"

	pageCommandExpiry  = time.Hour
	pageCommandTimeout = 30 * time.Second

	// Lý do (last_error) của lệnh; không chứa token, nội dung.
	reasonNoTenant      = "no_tenant"
	reasonNoAccount     = "account_missing"
	reasonNoCredentials = "no_credentials"
	reasonNoConnector   = "no_connector"
	reasonReadFailed    = "read_failed"
	reasonUnreadable    = "credentials_unreadable"
	// statusReasonSubscribe là status_reason của tài khoản khi đăng ký webhook thất bại hẳn.
	statusReasonSubscribe = "subscribe_failed"
)

// subscribeParts: lệnh command.fb.subscribe_page, command.fb.unsubscribe_page (Task 149).
func subscribeParts(d Deps) parts {
	return parts{Commands: []commands.Handler{pageCommand{d: d, subscribe: true}, pageCommand{d: d}}}
}

type accountPayload struct {
	AccountID uuid.UUID `json:"accountId"`
}

// subscribeResult là Outcome.Result của lệnh: trường đã đăng ký, hoặc Skipped khi bỏ qua vì trạng thái đã đổi.
type subscribeResult struct {
	Fields  []string `json:"fields,omitempty"`
	Skipped bool     `json:"skipped,omitempty"`
}

// accountCommandKind là Kind của lệnh theo tài khoản của channel: đọc comment định kỳ sống 2 phút (kỳ sau có lệnh mới),
// lệnh khác 1 giờ; hạn một lần chạy 30 s.
func accountCommandKind(name string) queue.Kind {
	expiry := pageCommandExpiry
	if name == CommentPollKind {
		expiry = commentPollExpiry
	}
	return commands.CommandKind(name, expiry, pageCommandTimeout)
}

// enqueueAccountCommand ghi lệnh kind (SubscribePageKind, UnsubscribePageKind) cho tài khoản a trong tx (mỗi lần một
// lệnh mới).
func enqueueAccountCommand(ctx context.Context, tx pgx.Tx, a ChannelAccount, kind string) error {
	n, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("sinh khóa lệnh %s: %w", kind, err)
	}
	return enqueueKeyed(ctx, tx, a, kind, a.ID.String()+":"+n.String(), accountPayload{AccountID: a.ID})
}

// enqueueKeyed ghi lệnh kind của channel cho tài khoản a trong tx với khóa chống trùng dedupe (đã có → không thêm).
func enqueueKeyed(ctx context.Context, tx pgx.Tx, a ChannelAccount, kind, dedupe string, payload any) error {
	tenant := a.TenantID
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, accountCommandKind(kind), queue.NewJob{
		TenantID: &tenant, Payload: payload, DedupeKey: dedupe,
	}); err != nil {
		return fmt.Errorf("ghi lệnh %s: %w", kind, err)
	}
	return nil
}

// pageCommand là lệnh đăng ký (subscribe) hay hủy đăng ký webhook của một tài khoản kênh.
type pageCommand struct {
	d         Deps
	subscribe bool
}

func (c pageCommand) name() string {
	if c.subscribe {
		return SubscribePageKind
	}
	return UnsubscribePageKind
}

func (c pageCommand) Kind() queue.Kind     { return accountCommandKind(c.name()) }
func (pageCommand) AutoRetryNotSent() bool { return true }

// current: lệnh chỉ còn ý nghĩa khi tài khoản đang ở trạng thái này (subscribe: chưa ngắt; unsubscribe: đã ngắt).
func (c pageCommand) current(a ChannelAccount) bool {
	return (a.Status == StatusDisconnected) != c.subscribe
}

// target là tài khoản, token và adapter của một lần chạy; skip = trạng thái đã đổi, không làm gì; credErr là lỗi đọc
// token (ErrNoCredentials, giải mã hỏng) để bên gọi tự quyết.
type target struct {
	acc     ChannelAccount
	cred    Credentials
	ad      MessagingAdapter
	conn    Connector
	skip    bool
	credErr error
}

// accountOf đọc mã tài khoản của payload j; payload hỏng hay thiếu tenant → kết quả từ chối.
func accountOf(j queue.Job) (uuid.UUID, *commands.Outcome) {
	var p accountPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil {
		return uuid.Nil, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}
	}
	if j.TenantID == nil {
		return uuid.Nil, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoTenant}
	}
	return p.AccountID, nil
}

// loadTarget đọc tài khoản id và token của nó trong một transaction ngắn của tenant của j (ngoài mọi lời gọi mạng).
// current sai → skip, không đọc token. out khác rỗng: không đọc được tài khoản (từ chối, chưa gửi).
func loadTarget(ctx context.Context, d Deps, j queue.Job, id uuid.UUID, current func(ChannelAccount) bool) (target, *commands.Outcome) {
	var t target
	err := tenancy.WithTenant(ctx, d.DB, *j.TenantID, func(tx pgx.Tx) (err error) {
		t = target{} // RetryTx có thể chạy lại
		if t.acc, err = Account(ctx, tx, id); err != nil {
			return err
		}
		if !current(t.acc) {
			t.skip = true
			return nil
		}
		t.cred, t.credErr = CredentialsFor(ctx, tx, d.Keys, id)
		return nil
	})
	switch {
	case errors.Is(err, ErrAccountNotFound):
		return target{}, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoAccount}
	case err != nil:
		return target{}, &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}
	}
	return t, nil
}

// load đọc tài khoản và token của j (loadTarget) cùng Connector của kênh. out khác rỗng: kết quả cuối (bỏ qua, từ chối,
// chưa gửi) mà Execute trả ngay.
func (c pageCommand) load(ctx context.Context, j queue.Job) (target, *commands.Outcome) {
	id, out := accountOf(j)
	if out != nil {
		return target{}, out
	}
	t, out := loadTarget(ctx, c.d, j, id, c.current)
	switch {
	case out != nil:
		return t, out
	case t.skip:
		return t, &commands.Outcome{Status: commands.StatusDone, Result: mustJSON(subscribeResult{Skipped: true})}
	case errors.Is(t.credErr, ErrNoCredentials) && !c.subscribe:
		// Đã ngắt mà không còn token: không còn gì để hủy bằng token này.
		return t, &commands.Outcome{Status: commands.StatusDone}
	case errors.Is(t.credErr, ErrNoCredentials):
		return t, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoCredentials}
	case t.credErr != nil:
		return t, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnreadable}
	}
	ad, conn, err := connector(c.d.Registry, t.acc.Channel)
	if err != nil {
		return t, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoConnector}
	}
	t.ad, t.conn = ad, conn
	return t, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v) //nolint:errchkjson // kiểu cố định, không lỗi
	return b
}

// Execute đăng ký (11 trường) hay hủy đăng ký webhook của Page ngoài transaction. Kết quả theo lớp lỗi của adapter:
// chưa gửi, quá tải → not_sent (tự thử lại trong 1 giờ); token hỏng, chính sách, yêu cầu sai → rejected (dead + hook);
// không rõ → unknown (in_doubt, Lookup tra lại).
func (c pageCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	t, out := c.load(ctx, j)
	if out != nil {
		return *out, nil
	}
	fields := WebhookFields()
	var err error
	if c.subscribe {
		err = t.conn.Subscribe(ctx, t.acc, t.cred, fields)
	} else {
		err = t.conn.Unsubscribe(ctx, t.acc, t.cred)
	}
	t.cred = Credentials{}
	if err == nil {
		if !c.subscribe {
			fields = nil
		}
		return commands.Outcome{Status: commands.StatusDone, Result: mustJSON(subscribeResult{Fields: fields})}, nil
	}
	class := t.ad.ClassifyError(err)
	switch class {
	case ErrorNotSentRetry, ErrorRateLimited:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: string(class)}, nil
	case ErrorAuth, ErrorPolicy, ErrorPermanent:
		return commands.Outcome{Status: commands.StatusRejected, Reason: string(class)}, nil
	default:
		return commands.Outcome{Status: commands.StatusUnknown, Reason: string(class)}, nil
	}
}

// Lookup tra kênh (HealthCheck) lệnh in_doubt đã có hiệu lực chưa: subscribe → done khi đủ trường, không thì not_sent;
// unsubscribe → done khi không còn trường nào (hay token đã hỏng), không thì not_sent. Trạng thái tài khoản đã đổi, hay
// đã ngắt mà không còn token → done (Execute cũng trả done). Không đọc được, tra lỗi → unknown.
func (c pageCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	t, out := c.load(ctx, j)
	switch {
	case out != nil && out.Status == commands.StatusDone:
		return commands.StatusDone, nil
	case out != nil:
		return commands.StatusUnknown, nil
	}
	h, err := t.ad.HealthCheck(ctx, t.acc, t.cred)
	if err != nil {
		return commands.StatusUnknown, nil //nolint:nilerr // tra lỗi: chưa biết, giữ in_doubt
	}
	var ok bool
	if c.subscribe {
		ok = h.TokenValid && h.WebhookSubscribed
	} else {
		ok = !h.TokenValid || len(h.MissingFields) >= len(WebhookFields())
	}
	if ok {
		return commands.StatusDone, nil
	}
	return commands.StatusNotSent, nil
}

// lockFor khóa tài khoản của j (ADR-46 nhóm 3, trước câu đổi platform.jobs của runner); không có → ok = false.
func lockFor(ctx context.Context, tx pgx.Tx, j queue.Job) (ChannelAccount, bool, error) {
	var p accountPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil {
		return ChannelAccount{}, false, nil //nolint:nilerr // payload hỏng: Execute đã rejected, hook không làm gì
	}
	r, err := store.New(tx).LockAccount(ctx, p.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChannelAccount{}, false, nil
	}
	if err != nil {
		return ChannelAccount{}, false, fmt.Errorf("khóa tài khoản kênh: %w", err)
	}
	return accountFrom(accountRow(r)), true, nil
}

// OnDone (trong tx đổi lệnh sang done): subscribe ghi trường đã đăng ký nếu tài khoản chưa ngắt; unsubscribe xóa token
// và trường đăng ký nếu tài khoản vẫn đã ngắt (nối lại trong lúc chờ → giữ token mới). Bỏ qua → không làm gì.
func (c pageCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var res subscribeResult
	if len(result) > 0 {
		if err := json.Unmarshal(result, &res); err != nil {
			return fmt.Errorf("đọc kết quả lệnh %s: %w", c.name(), err)
		}
	}
	if res.Skipped {
		return nil
	}
	a, more, err := c.settle(ctx, tx, j)
	if err != nil || !more {
		return err
	}
	if len(res.Fields) == 0 {
		res.Fields = WebhookFields() // done do Lookup tra thấy đủ trường
	}
	if _, err := store.New(tx).SetSubscribedFields(ctx, store.SetSubscribedFieldsParams{SubscribedFields: res.Fields, ID: a.ID}); err != nil {
		return fmt.Errorf("ghi trường webhook đã đăng ký: %w", err)
	}
	return c.signal(ctx, tx, a.ID)
}

// OnTerminal (dead, expired; in_doubt không làm gì: Lookup còn cần token): unsubscribe xóa token nếu tài khoản vẫn đã
// ngắt; subscribe đánh dấu degraded và ghi lệnh cảnh báo gấp channel-subscribe:<id> nếu tài khoản chưa ngắt.
func (c pageCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status == queue.StatusInDoubt {
		return nil
	}
	a, more, err := c.settle(ctx, tx, j)
	if err != nil || !more {
		return err
	}
	if _, err := MarkStatus(ctx, tx, a.ID, StatusDegraded, statusReasonSubscribe); err != nil {
		return err
	}
	if err := enqueueSubscribeAlert(ctx, tx, a, j.ID); err != nil {
		return err
	}
	return c.signal(ctx, tx, a.ID)
}

// settle là phần chung của hai hook: khóa tài khoản của j; không có hay trạng thái đã đổi → không làm gì; lệnh hủy đăng
// ký → xóa token (xong). more = true: lệnh đăng ký, bên gọi làm tiếp với tài khoản a.
func (c pageCommand) settle(ctx context.Context, tx pgx.Tx, j queue.Job) (a ChannelAccount, more bool, err error) {
	a, ok, err := lockFor(ctx, tx, j)
	switch {
	case err != nil || !ok || !c.current(a):
		return a, false, err
	case !c.subscribe:
		return a, false, c.clear(ctx, tx, a)
	}
	return a, true, nil
}

// clear xóa token và trường đăng ký của tài khoản đã ngắt a.
func (c pageCommand) clear(ctx context.Context, tx pgx.Tx, a ChannelAccount) error {
	if err := ClearCredentials(ctx, tx, a.ID); err != nil {
		return err
	}
	if _, err := store.New(tx).SetSubscribedFields(ctx, store.SetSubscribedFieldsParams{SubscribedFields: []string{}, ID: a.ID}); err != nil {
		return fmt.Errorf("xóa trường webhook đã đăng ký: %w", err)
	}
	return c.signal(ctx, tx, a.ID)
}

func (c pageCommand) signal(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	a, err := Account(ctx, tx, id)
	if err != nil {
		return err
	}
	return Writer{Realtime: c.d.Bus}.signal(ctx, tx, a)
}
