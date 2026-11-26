package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Sức khỏe kênh (kế hoạch M4, Task 150; HT15, HT16; 02 mục 7, 8): bốn lịch, ba lệnh chỉ đọc gọi kênh, các cảnh báo.
//   - channel.health (mỗi giờ, theo tenant): mỗi Page active, degraded một lệnh command.fb.health_check. Kết quả ghi
//     trong tx đổi lệnh sang done: token hỏng hay thiếu quyền → degraded + cảnh báo gấp channel-auth:<id>; thiếu
//     trường webhook → tự ghi subscribe_page + cảnh báo thường channel-fields:<id>; tốt lại → active, báo hiệu.
//   - channel.silence (mỗi 5 phút, theo tenant): trong giờ bán, Page active có webhook cuối cách 30 phút–3 giờ → cảnh
//     báo gấp webhook-silence:<id>, đăng ký lại, hỏi bù từ webhook cuối − 5 phút (một lần mỗi đợt im lặng).
//   - channel.backfill (mỗi 10 phút, theo tenant): mỗi Page active hỏi bù 30 phút gần nhất (04 mục 5.2).
//   - channel.graph_version (mỗi ngày): hạn phiên bản Graph còn dưới 180 ngày → cảnh báo thường graph-version, một
//     lần mỗi 7 ngày.
// Lệnh đọc comment định kỳ (command.fb.comment_poll) do conversation ghi bằng EnqueueCommentPoll (chế độ theo Page nằm
// ở page_settings của conversation, Q4).

// Tên lịch.
const (
	HealthSchedule       = "channel.health"
	SilenceSchedule      = "channel.silence"
	BackfillSchedule     = "channel.backfill"
	GraphVersionSchedule = "channel.graph_version"
)

// Lệnh chỉ đọc gọi kênh.
const (
	// HealthCheckKind kiểm token, quyền, đăng ký webhook của một Page.
	HealthCheckKind = "command.fb.health_check"
	// BackfillKind hỏi bù sự kiện từ một mốc (payload accountId, since).
	BackfillKind = "command.fb.backfill"
	// CommentPollKind đọc comment mới trên các bài gần nhất từ một mốc (payload accountId, since).
	CommentPollKind = "command.fb.comment_poll"
)

// Lệnh cảnh báo (gửi Telegram sau commit, cmd/crm đăng ký bằng HealthAlertCommands).
const (
	AuthAlertKind         = "channel.auth_alert"          // channel-auth:<id>, gấp
	FieldsAlertKind       = "channel.fields_alert"        // channel-fields:<id>, thường
	SilenceAlertKind      = "channel.silence_alert"       // webhook-silence:<id>, gấp
	PolicyAlertKind       = "channel.policy_alert"        // meta-policy:<id>, gấp
	GraphVersionAlertKind = "channel.graph_version_alert" // graph-version, thường
)

const (
	healthEvery       = time.Hour
	silenceEvery      = 5 * time.Minute
	backfillEvery     = 10 * time.Minute
	graphVersionEvery = 24 * time.Hour
	commentPollExpiry = 2 * time.Minute

	silenceFrom      = 30 * time.Minute
	silenceTo        = 3 * time.Hour
	silenceOverlap   = 5 * time.Minute
	backfillWindow   = 30 * time.Minute
	graphWarnBefore  = 180 * 24 * time.Hour
	graphAlertRepeat = 7 * 24 * time.Hour
	// enqueueBatch: số sự kiện ghi trong một transaction ngắn của lệnh đọc.
	enqueueBatch = 100

	// Giờ bán theo giờ Việt Nam: [08:00, 23:00).
	salesFromHour = 8
	salesToHour   = 23

	// status_reason của Page degraded do kênh báo mất quyền; policyReasonPrefix + hành động khi Meta áp chế tài.
	reasonTokenInvalid       = "token_invalid"
	reasonMissingPermissions = "missing_permissions"
	reasonAuthFailed         = "auth_failed"
	policyReasonPrefix       = "meta_policy:"
	policyUnblock            = "unblock"
	policyOther              = "other"
	reasonNoAdapter          = "no_adapter"
	reasonNoPoller           = "no_comment_poller"
)

// vietnamOffset là độ lệch giờ Việt Nam (UTC+7, không đổi giờ mùa hè), giây.
const vietnamOffset = 7 * 3600

// policyActionRe: hành động chính sách ghi được vào status_reason, cảnh báo; khác → "other".
var policyActionRe = regexp.MustCompile(`^[a-z_]{1,32}$`)

// InSalesHours báo t nằm trong giờ bán 08:00–23:00 giờ Việt Nam (cảnh báo im lặng, đọc comment định kỳ chế độ auto).
func InSalesHours(t time.Time) bool {
	h := t.In(time.FixedZone("ICT", vietnamOffset)).Hour()
	return h >= salesFromHour && h < salesToHour
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

// healthParts: lịch, lệnh của sức khỏe kênh (Task 150).
func healthParts(d Deps) parts {
	sch := []scheduleJob{
		{d: d, name: HealthSchedule, every: healthEvery, perTenant: true, run: runHealth},
		{d: d, name: SilenceSchedule, every: silenceEvery, perTenant: true, run: runSilence},
		{d: d, name: BackfillSchedule, every: backfillEvery, perTenant: true, run: runBackfill},
		{d: d, name: GraphVersionSchedule, every: graphVersionEvery, run: runGraphVersion},
	}
	p := parts{Commands: []commands.Handler{
		healthCommand{d: d},
		readCommand{d: d, name: BackfillKind, via: webhooks.ViaBackfill, read: readBackfill},
		readCommand{d: d, name: CommentPollKind, via: webhooks.ViaCommentPoll, read: readComments},
	}}
	for _, s := range sch {
		p.Jobs = append(p.Jobs, s)
		p.Schedules = append(p.Schedules, schedules.Schedule{Name: s.name, Every: s.every, PerTenant: s.perTenant, Kind: s.Kind()})
	}
	return p
}

// scheduleJob là việc của một lịch: chạy run trong tx của việc (theo tenant: WithTenant của tenant của việc).
type scheduleJob struct {
	d         Deps
	name      string
	every     time.Duration
	perTenant bool
	run       func(ctx context.Context, d Deps, tx pgx.Tx, now time.Time) (int, error)
}

// Kind: lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func (s scheduleJob) Kind() queue.Kind {
	return queue.Kind{Name: "task." + s.name, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: s.every}}
}

// Handle chạy lịch; kết quả {"enqueued": số lệnh đã ghi}.
func (s scheduleJob) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if s.perTenant && j.TenantID == nil {
		return nil, fmt.Errorf("lịch %s: việc thiếu tenant: %w", s.name, queue.ErrPermanent)
	}
	n, err := s.run(ctx, s.d, tx, s.d.now())
	if err != nil {
		return nil, fmt.Errorf("lịch %s: %w", s.name, err)
	}
	return mustJSON(map[string]int{"enqueued": n}), nil
}

// readPayload là payload của lệnh đọc: tài khoản và mốc đọc từ đó.
type readPayload struct {
	AccountID uuid.UUID `json:"accountId"`
	Since     time.Time `json:"since"`
}

// served trả tài khoản của shop mà kênh có adapter ở tiến trình này (lịch không ghi lệnh chắc chắn bị từ chối).
func served(ctx context.Context, d Deps, tx pgx.Tx) ([]ChannelAccount, error) {
	as, err := ListAccounts(ctx, tx, nil)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(as, func(a ChannelAccount) bool {
		_, ok := d.Registry.Lookup(a.Channel)
		return !ok
	}), nil
}

// runHealth ghi mỗi Page active, degraded một lệnh kiểm sức khỏe, khóa <id>:<giờ> (chạy lại trong giờ không thêm).
func runHealth(ctx context.Context, d Deps, tx pgx.Tx, now time.Time) (int, error) {
	hour := now.Truncate(time.Hour).Format(time.RFC3339)
	return perAccount(ctx, d, tx, func(a ChannelAccount) bool { return a.Status != StatusDisconnected },
		func(a ChannelAccount) error {
			return enqueueKeyed(ctx, tx, a, HealthCheckKind, a.ID.String()+":"+hour, accountPayload{AccountID: a.ID})
		})
}

// perAccount gọi fn cho mỗi tài khoản phục vụ được (served) mà keep đúng; trả số lần gọi xong.
func perAccount(ctx context.Context, d Deps, tx pgx.Tx, keep func(ChannelAccount) bool, fn func(ChannelAccount) error) (int, error) {
	as, err := served(ctx, d, tx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range as {
		if !keep(a) {
			continue
		}
		if err := fn(a); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func isActive(a ChannelAccount) bool { return a.Status == StatusActive }

// runSilence: trong giờ bán, Page active có webhook cuối cách [30 phút, 3 giờ] → cảnh báo gấp, đăng ký lại, hỏi bù từ
// webhook cuối − 5 phút. Mọi lệnh khóa theo mốc webhook cuối: một đợt im lặng chỉ một bộ lệnh. Trả số Page im lặng.
func runSilence(ctx context.Context, d Deps, tx pgx.Tx, now time.Time) (int, error) {
	if !InSalesHours(now) {
		return 0, nil
	}
	silent := func(a ChannelAccount) bool {
		if a.Status != StatusActive || a.LastWebhookAt == nil {
			return false
		}
		age := now.Sub(*a.LastWebhookAt)
		return age >= silenceFrom && age <= silenceTo
	}
	return perAccount(ctx, d, tx, silent, func(a ChannelAccount) error {
		last := a.LastWebhookAt.UTC()
		episode := a.ID.String() + ":" + last.Format(time.RFC3339Nano)
		if err := enqueueAlert(ctx, tx, SilenceAlertKind, a, "webhook-silence:"+episode); err != nil {
			return err
		}
		if err := enqueueKeyed(ctx, tx, a, SubscribePageKind, "silence:"+episode, accountPayload{AccountID: a.ID}); err != nil {
			return err
		}
		return enqueueKeyed(ctx, tx, a, BackfillKind, "silence:"+episode, readPayload{AccountID: a.ID, Since: last.Add(-silenceOverlap)})
	})
}

// runBackfill ghi mỗi Page active một lệnh hỏi bù 30 phút gần nhất, khóa theo kỳ 10 phút.
func runBackfill(ctx context.Context, d Deps, tx pgx.Tx, now time.Time) (int, error) {
	period := now.Truncate(backfillEvery).Format(time.RFC3339)
	return perAccount(ctx, d, tx, isActive, func(a ChannelAccount) error {
		return enqueueKeyed(ctx, tx, a, BackfillKind, a.ID.String()+":"+period,
			readPayload{AccountID: a.ID, Since: now.Add(-backfillWindow)})
	})
}

// graphPayload là payload của cảnh báo phiên bản Graph (không theo tenant).
type graphPayload struct {
	Version string `json:"version"`
	Expires string `json:"expires"`
	Days    int    `json:"days"`
}

// runGraphVersion: hạn phiên bản Graph (META_GRAPH_VERSION_EXPIRES) còn dưới 180 ngày → một lệnh cảnh báo thường,
// khóa theo đợt 7 ngày tính từ lúc bắt đầu vào khoảng cảnh báo (lịch chạy mỗi ngày nên mỗi đợt gửi một lần). Không có
// hạn (vai trò không cấu hình Graph) → không làm gì.
func runGraphVersion(ctx context.Context, d Deps, tx pgx.Tx, now time.Time) (int, error) {
	exp := d.Config.Meta.GraphVersionExpires
	if exp.IsZero() {
		return 0, nil
	}
	left := exp.Sub(now)
	if left >= graphWarnBefore {
		return 0, nil
	}
	round := int(now.Sub(exp.Add(-graphWarnBefore)) / graphAlertRepeat)
	date := exp.UTC().Format(time.DateOnly)
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, alertKind(GraphVersionAlertKind), queue.NewJob{
		Payload:   graphPayload{Version: d.Config.Meta.GraphVersion, Expires: date, Days: int(left / (24 * time.Hour))},
		DedupeKey: "graph-version:" + d.Config.Meta.GraphVersion + ":" + date + ":" + strconv.Itoa(round),
	}); err != nil {
		return 0, fmt.Errorf("ghi lệnh cảnh báo phiên bản Graph: %w", err)
	}
	return 1, nil
}

// healthResult là Outcome.Result của lệnh kiểm sức khỏe.
type healthResult struct {
	TokenValid        bool     `json:"tokenValid"`
	PermissionsOK     bool     `json:"permissionsOk"`
	WebhookSubscribed bool     `json:"webhookSubscribed"`
	MissingFields     []string `json:"missingFields,omitempty"`
	MissingPerms      []string `json:"missingPermissions,omitempty"`
	Skipped           bool     `json:"skipped,omitempty"`
}

// healthCommand là lệnh command.fb.health_check: gọi HealthCheck của adapter ngoài tx; kết quả ghi ở OnDone.
type healthCommand struct{ d Deps }

func (healthCommand) Kind() queue.Kind       { return accountCommandKind(HealthCheckKind) }
func (healthCommand) AutoRetryNotSent() bool { return true }

func notDisconnected(a ChannelAccount) bool { return a.Status != StatusDisconnected }

// Execute: Page đã ngắt → done bỏ qua; chưa có token hay không giải mã được → kết quả token hỏng; HealthCheck lỗi (mạng,
// Meta tạm lỗi, quá tải, không rõ) → not_sent (chỉ đọc: chạy lại vô hại); yêu cầu sai → rejected.
func (c healthCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	id, out := accountOf(j)
	if out != nil {
		return *out, nil
	}
	t, out := loadTarget(ctx, c.d, j, id, notDisconnected)
	switch {
	case out != nil:
		return *out, nil
	case t.skip:
		return commands.Outcome{Status: commands.StatusDone, Result: mustJSON(healthResult{Skipped: true})}, nil
	}
	ad, ok := c.d.Registry.Lookup(t.acc.Channel)
	switch {
	case !ok:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoAdapter}, nil
	case t.credErr != nil:
		return commands.Outcome{Status: commands.StatusDone, Result: mustJSON(healthResult{})}, nil
	}
	h, err := ad.HealthCheck(ctx, t.acc, t.cred)
	t.cred = Credentials{}
	if err != nil {
		return readFailure(ad.ClassifyError(err)), nil
	}
	return commands.Outcome{Status: commands.StatusDone, Result: mustJSON(healthResult{
		TokenValid: h.TokenValid, PermissionsOK: h.PermissionsOK, WebhookSubscribed: h.WebhookSubscribed,
		MissingFields: h.MissingFields, MissingPerms: h.MissingPermissions,
	})}, nil
}

// readFailure đổi lớp lỗi của một lời gọi chỉ đọc thành kết quả: token hỏng, chính sách, yêu cầu sai → rejected (lý do
// là lớp); còn lại → not_sent (đọc lại vô hại, kể cả không rõ).
func readFailure(class ErrorClass) commands.Outcome {
	switch class {
	case ErrorAuth, ErrorPolicy, ErrorPermanent:
		return commands.Outcome{Status: commands.StatusRejected, Reason: string(class)}
	default:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: string(class)}
	}
}

// Lookup: lệnh chỉ đọc không có hiệu lực ở kênh → not_sent (chạy lại).
func (healthCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// OnDone (tx đổi lệnh sang done): khóa tài khoản (ADR-46 nhóm 3), ghi mốc kiểm; Page đã ngắt → chỉ vậy. Token hỏng,
// thiếu quyền → degraded + cảnh báo gấp (chỉ khi mới chuyển); token tốt → gỡ degraded do lỗi token, đăng ký (không gỡ
// chế tài của Meta); thiếu trường webhook → ghi subscribe_page và cảnh báo thường.
func (c healthCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var res healthResult
	if len(result) > 0 {
		if err := json.Unmarshal(result, &res); err != nil {
			return fmt.Errorf("đọc kết quả kiểm sức khỏe: %w", err)
		}
	} // done do Lookup: không có (Lookup không bao giờ trả done)
	if res.Skipped {
		return nil
	}
	a, ok, err := lockFor(ctx, tx, j)
	if err != nil || !ok {
		return err
	}
	if _, err := store.New(tx).SetTokenChecked(ctx, store.SetTokenCheckedParams{CheckedAt: new(c.d.now()), ID: a.ID}); err != nil {
		return fmt.Errorf("ghi mốc kiểm token: %w", err)
	}
	if a.Status == StatusDisconnected {
		return nil
	}
	w := Writer{Realtime: c.d.Bus}
	switch {
	case !res.TokenValid:
		return w.degradeAuth(ctx, tx, a, reasonTokenInvalid)
	case !res.PermissionsOK:
		return w.degradeAuth(ctx, tx, a, reasonMissingPermissions)
	}
	if a.Status == StatusDegraded && healthOwned(a.StatusReason) {
		if _, err := MarkStatus(ctx, tx, a.ID, StatusActive, ""); err != nil {
			return err
		}
		if err := w.signal(ctx, tx, a); err != nil {
			return err
		}
	}
	if res.WebhookSubscribed {
		return nil
	}
	if err := enqueueKeyed(ctx, tx, a, SubscribePageKind, "health:"+j.ID.String(), accountPayload{AccountID: a.ID}); err != nil {
		return err
	}
	return enqueueAlert(ctx, tx, FieldsAlertKind, a, "channel-fields:"+a.ID.String()+":"+j.ID.String())
}

// healthOwned: lý do degraded mà lịch sức khỏe gỡ được khi token, quyền tốt lại (không gồm chế tài của Meta).
func healthOwned(reason string) bool {
	switch reason {
	case "", reasonTokenInvalid, reasonMissingPermissions, reasonAuthFailed, statusReasonSubscribe:
		return true
	}
	return false
}

func authReason(reason string) bool {
	return reason == reasonTokenInvalid || reason == reasonMissingPermissions || reason == reasonAuthFailed
}

// degradeAuth đặt tài khoản a (đã khóa) sang degraded lý do reason và ghi cảnh báo gấp channel-auth:<id>, báo hiệu;
// đã degraded vì mất quyền (lý do nào) → không làm gì (một cảnh báo cho mỗi lần mất quyền).
func (w Writer) degradeAuth(ctx context.Context, tx pgx.Tx, a ChannelAccount, reason string) error {
	if a.Status == StatusDisconnected || (a.Status == StatusDegraded && authReason(a.StatusReason)) {
		return nil
	}
	changed, err := MarkStatus(ctx, tx, a.ID, StatusDegraded, reason)
	if err != nil || !changed {
		return err
	}
	n, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("sinh khóa cảnh báo: %w", err)
	}
	if err := enqueueAlert(ctx, tx, AuthAlertKind, a, "channel-auth:"+a.ID.String()+":"+n.String()); err != nil {
		return err
	}
	return w.signal(ctx, tx, a)
}

// lockAccount khóa tài khoản id (ADR-46 nhóm 3); không có → ErrAccountNotFound.
func lockAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ChannelAccount, error) {
	r, err := store.New(tx).LockAccount(ctx, id)
	if err != nil {
		return ChannelAccount{}, notFound(err, "khóa tài khoản kênh")
	}
	return accountFrom(accountRow(r)), nil
}

// ReportAuthFailure ghi kênh từ chối token của tài khoản id (lệnh gặp lớp auth, trong tx của bên gọi): degraded lý do
// auth_failed, cảnh báo gấp channel-auth:<id> và báo hiệu channel.status — chỉ khi chưa degraded vì mất quyền (gọi
// nhiều lần không cảnh báo thêm). Page đã ngắt → không làm gì. Không có tài khoản → ErrAccountNotFound.
func (w Writer) ReportAuthFailure(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	a, err := lockAccount(ctx, tx, id)
	if err != nil {
		return err
	}
	return w.degradeAuth(ctx, tx, a, reasonAuthFailed)
}

// ReportPolicyEnforcement ghi Meta áp chế tài chính sách action lên tài khoản id (sự kiện policy.enforcement, trong tx
// của bên gọi): action "unblock" → gỡ degraded do chế tài; hành động khác → degraded lý do meta_policy:<action> (hành
// động lạ ghi "other"), cảnh báo gấp meta-policy:<id>, báo hiệu — chỉ khi trạng thái, lý do thật sự đổi. Page đã ngắt →
// không làm gì. Không có tài khoản → ErrAccountNotFound.
func (w Writer) ReportPolicyEnforcement(ctx context.Context, tx pgx.Tx, id uuid.UUID, action string) error {
	a, err := lockAccount(ctx, tx, id)
	if err != nil || a.Status == StatusDisconnected {
		return err
	}
	act := strings.ToLower(strings.TrimSpace(action))
	if !policyActionRe.MatchString(act) {
		act = policyOther
	}
	if act == policyUnblock {
		if a.Status != StatusDegraded || !strings.HasPrefix(a.StatusReason, policyReasonPrefix) {
			return nil
		}
		if _, err := MarkStatus(ctx, tx, a.ID, StatusActive, ""); err != nil {
			return err
		}
		return w.signal(ctx, tx, a)
	}
	changed, err := MarkStatus(ctx, tx, a.ID, StatusDegraded, policyReasonPrefix+act)
	if err != nil || !changed {
		return err
	}
	n, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("sinh khóa cảnh báo: %w", err)
	}
	tenant := a.TenantID
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, alertKind(PolicyAlertKind), queue.NewJob{
		TenantID: &tenant, Payload: alertPayload{AccountID: a.ID, Channel: a.Channel, Name: a.Name, Action: act},
		DedupeKey: "meta-policy:" + a.ID.String() + ":" + n.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh %s: %w", PolicyAlertKind, err)
	}
	return w.signal(ctx, tx, a)
}

// RequestHealthCheck ghi ngay một lệnh kiểm sức khỏe cho tài khoản id (nút "Kiểm ngay"; mỗi lần một lệnh, chống bấm
// trùng do khóa chống trùng của API). Trả tài khoản hiện tại; không có → ErrAccountNotFound.
func (w Writer) RequestHealthCheck(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ChannelAccount, error) {
	a, err := Account(ctx, tx, id)
	if err != nil {
		return ChannelAccount{}, err
	}
	n, err := uuid.NewV7()
	if err != nil {
		return ChannelAccount{}, fmt.Errorf("sinh khóa lệnh kiểm sức khỏe: %w", err)
	}
	if err := enqueueKeyed(ctx, tx, a, HealthCheckKind, a.ID.String()+":now:"+n.String(), accountPayload{AccountID: a.ID}); err != nil {
		return ChannelAccount{}, err
	}
	return a, nil
}

// EnqueueCommentPoll ghi lệnh đọc comment mới của tài khoản a từ since (lịch đọc comment định kỳ của conversation, Q4),
// trong tx của bên gọi; khóa chống trùng theo kỳ 30 giây của now (lịch chạy chồng không thêm lệnh).
func EnqueueCommentPoll(ctx context.Context, tx pgx.Tx, a ChannelAccount, since, now time.Time) error {
	period := now.UTC().Truncate(commentPollPeriod).Format(time.RFC3339)
	return enqueueKeyed(ctx, tx, a, CommentPollKind, a.ID.String()+":"+period, readPayload{AccountID: a.ID, Since: since.UTC()})
}

// commentPollPeriod là kỳ của lịch đọc comment định kỳ (khóa chống trùng của lệnh).
const commentPollPeriod = 30 * time.Second

// readResult là Outcome.Result của lệnh đọc: số sự kiện đọc được, số việc vào mới tạo.
type readResult struct {
	Read    int  `json:"read"`
	Created int  `json:"created"`
	Skipped bool `json:"skipped,omitempty"`
}

// readCommand là lệnh chỉ đọc sự kiện từ kênh rồi ghi bằng webhooks.EnqueueInbound (via): hỏi bù, đọc comment.
type readCommand struct {
	d    Deps
	name string
	via  string
	read func(ctx context.Context, ad MessagingAdapter, t target, since time.Time) ([]webhooks.InboundEvent, bool, error)
}

func (c readCommand) Kind() queue.Kind     { return accountCommandKind(c.name) }
func (readCommand) AutoRetryNotSent() bool { return true }

// Lookup: lệnh chỉ đọc → not_sent (chạy lại; sự kiện đã ghi không ghi trùng).
func (readCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// readBackfill đọc mọi sự kiện từ since bằng Backfill của adapter (dừng ở lỗi đầu tiên, giữ sự kiện đã đọc).
func readBackfill(ctx context.Context, ad MessagingAdapter, t target, since time.Time) ([]webhooks.InboundEvent, bool, error) {
	var out []webhooks.InboundEvent
	for e, err := range ad.Backfill(ctx, t.acc, t.cred, since) {
		if err != nil {
			return out, true, err
		}
		out = append(out, e)
	}
	return out, true, nil
}

// readComments đọc comment mới từ since bằng CommentPoller của adapter; adapter không có → ok = false.
func readComments(ctx context.Context, ad MessagingAdapter, t target, since time.Time) ([]webhooks.InboundEvent, bool, error) {
	p, ok := ad.(CommentPoller)
	if !ok {
		return nil, false, nil
	}
	evs, err := p.PollPageComments(ctx, t.acc, t.cred, since)
	if err != nil {
		return nil, true, fmt.Errorf("đọc comment định kỳ: %w", err)
	}
	return evs, true, nil
}

// Execute: Page đã ngắt → done bỏ qua; không token → rejected; đọc ngoài tx, rồi ghi sự kiện đã đọc theo lô 100 trong
// transaction ngắn (khóa chống trùng như webhook: sự kiện đã có không tạo việc). Lỗi đọc: token hỏng → ReportAuthFailure
// (tx riêng) rồi rejected; chính sách, yêu cầu sai → rejected; còn lại → not_sent (tự đọc lại).
func (c readCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p readPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil || p.Since.IsZero() {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	if j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoTenant}, nil
	}
	t, out := loadTarget(ctx, c.d, j, p.AccountID, notDisconnected)
	switch {
	case out != nil:
		return *out, nil
	case t.skip:
		return commands.Outcome{Status: commands.StatusDone, Result: mustJSON(readResult{Skipped: true})}, nil
	case errors.Is(t.credErr, ErrNoCredentials):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoCredentials}, nil
	case t.credErr != nil:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnreadable}, nil
	}
	ad, ok := c.d.Registry.Lookup(t.acc.Channel)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoAdapter}, nil
	}
	evs, ok, readErr := c.read(ctx, ad, t, p.Since)
	t.cred = Credentials{}
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoPoller}, nil
	}
	created, err := c.store(ctx, *j.TenantID, t.acc.Channel, evs)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // ghi lỗi: đọc lại vô hại
	}
	if readErr == nil {
		return commands.Outcome{Status: commands.StatusDone, Result: mustJSON(readResult{Read: len(evs), Created: created})}, nil
	}
	class := ad.ClassifyError(readErr)
	if class == ErrorAuth {
		if err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) error {
			return Writer{Realtime: c.d.Bus}.ReportAuthFailure(ctx, tx, t.acc.ID)
		}); err != nil && !errors.Is(err, ErrAccountNotFound) {
			return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chưa ghi được: thử lại
		}
	}
	return readFailure(class), nil
}

// store ghi evs cho tenant theo lô enqueueBatch, mỗi lô một transaction ngắn; trả số việc vào mới tạo.
func (c readCommand) store(ctx context.Context, tenant uuid.UUID, source string, evs []webhooks.InboundEvent) (int, error) {
	created := 0
	now := c.d.now()
	for start := 0; start < len(evs); start += enqueueBatch {
		batch := evs[start:min(start+enqueueBatch, len(evs))]
		n := 0
		if err := tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
			n = 0 // RetryTx có thể chạy lại
			for _, e := range batch {
				ok, err := webhooks.EnqueueInbound(ctx, tx, queue.NewQueue(), tenant, source, e, now, c.via)
				if err != nil {
					return fmt.Errorf("ghi sự kiện %s: %w", c.via, err)
				}
				if ok {
					n++
				}
			}
			return nil
		}); err != nil {
			return created, err //nolint:wrapcheck // đã bọc trong hàm
		}
		created += n
	}
	return created, nil
}
