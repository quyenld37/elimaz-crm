package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Kiểm kết nối tài khoản hãng (kế hoạch M3, Task M3-09): lệnh carrier.check đọc tài khoản trong một transaction ngắn,
// giải mã khóa trong bộ nhớ, gọi CheckConnection của adapter ngoài transaction; OnDone ghi kết quả (active hay
// auth_failed). Kiểm chỉ đọc nên lỗi không rõ (quá giờ, mất kết nối) cũng là not_sent: thử lại an toàn.

const (
	// CheckKindName là tên loại lệnh kiểm kết nối tài khoản hãng.
	CheckKindName = "carrier.check"
	// AuthAlertKind là tên loại lệnh gửi cảnh báo "hãng từ chối khóa API" (key carrier-auth:<accountId>).
	AuthAlertKind    = "fulfillment.carrier_auth_alert"
	checkExpiry      = 10 * time.Minute
	checkTimeout     = 20 * time.Second
	authAlertTimeout = 10 * time.Second
	severityNormal   = "normal"

	// Mã kết quả kiểm (last_check_result) và lý do của lệnh; mã lỗi của hãng chỉ giữ ký tự [a-z0-9_].
	checkOK            = "ok"
	checkFailed        = "check_failed"
	checkNotSupported  = "check_not_supported"
	checkRejected      = "rejected"
	reasonDecrypt      = "decrypt_failed"
	reasonUnavailable  = "account_unavailable"
	reasonNoCarrier    = "capability_not_supported"
	reasonNoTenant     = "no_tenant"
	reasonAlerter      = "alerter_missing"
	reasonAlertFailed  = "alert_failed"
	carrierErrorCode   = "carrier_error"
	unknownOutcomeCode = "unknown_outcome"
)

// carrierCode: mã lỗi của hãng được ghi vào last_check_result, last_error (không thông điệp thô).
var carrierCode = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// CheckKind là Kind của lệnh kiểm kết nối: hết hạn 10 phút, mỗi lần gọi tối đa 20 s.
func CheckKind() queue.Kind {
	return commands.CommandKind(CheckKindName, checkExpiry, checkTimeout)
}

type checkPayload struct {
	AccountID uuid.UUID `json:"accountId"`
}

// checkResult là Outcome.Result của lệnh kiểm: chỉ mã, không thông điệp của hãng, không khóa.
type checkResult struct {
	OK                  bool       `json:"ok"`
	Code                string     `json:"code"`
	Auth                bool       `json:"auth,omitempty"`
	CredentialUpdatedAt *time.Time `json:"credentialUpdatedAt,omitempty"`
}

// checkCommand là lệnh carrier.check của Service s.
type checkCommand struct{ s *Service }

func (checkCommand) Kind() queue.Kind       { return CheckKind() }
func (checkCommand) AutoRetryNotSent() bool { return true }

// Lookup: kiểm chỉ đọc, không có gì "đã làm" ở hãng; lệnh in_doubt luôn được chạy lại.
func (checkCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// Execute đọc tài khoản (transaction ngắn), giải mã, gọi CheckConnection ngoài transaction và phân loại kết quả.
func (c checkCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p checkPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	if j.TenantID == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoTenant}, nil
	}
	var acc Account
	var row store.GetAccountRow
	var readErr error
	err := tenancy.WithTenant(ctx, c.s.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		acc, readErr = Account{}, nil // RetryTx có thể chạy lại
		q := store.New(tx)
		var err error
		if row, err = getAccount(ctx, q, p.AccountID); err != nil {
			return err
		}
		if row.Mode != ModeAPI || row.Status == AccountRevoked || len(row.CredentialEnc) == 0 {
			readErr = ErrAccountUnavailable
			return nil
		}
		acc, err = c.s.account(ctx, q, row)
		if errors.Is(err, ErrCredentialsUnreadable) || errors.Is(err, ErrKeysMissing) {
			readErr = err
			return nil
		}
		return err
	})
	switch {
	case errors.Is(err, ErrAccountNotFound), errors.Is(readErr, ErrAccountUnavailable):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}, nil
	case readErr != nil:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonDecrypt}, nil
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "read_failed"}, nil
	}
	ad, ok := c.s.adapter(row.CarrierKey)
	if !ok {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoCarrier}, nil
	}
	h, err := ad.CheckConnection(ctx, acc)
	acc.Creds = nil
	return checkOutcome(h, err, row.CredentialUpdatedAt)
}

// checkOutcome phân loại kết quả CheckConnection: OK → done ok; 401/403 → done auth (cảnh báo); hãng từ chối khác
// (kể cả ErrRejected không phân lớp), Health.OK = false, không hỗ trợ → done (auth_failed, không cảnh báo); 429 →
// not_sent theo RetryAfter; còn lại (503, ErrNotSent, lỗi mạng, quá giờ) → not_sent.
func checkOutcome(h Health, err error, credAt pgtype.Timestamptz) (commands.Outcome, error) {
	res := checkResult{}
	if credAt.Valid {
		t := credAt.Time.UTC()
		res.CredentialUpdatedAt = &t
	}
	ce, isCE := errors.AsType[*CarrierError](err)
	switch {
	case err == nil && h.OK:
		res.OK, res.Code = true, checkOK
	case err == nil:
		res.Code = checkFailed
	case errors.Is(err, ErrNotSupported):
		res.Code = checkNotSupported
	case isCE && ce.Class == ClassAuth:
		res.Auth, res.Code = true, safeCode(ce.Code)
	case isCE && (ce.Class == ClassRejected || ce.Class == ClassPermanent):
		res.Code = safeCode(ce.Code)
	case isCE && ce.Class == ClassRateLimited:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: safeCode(ce.Code), RetryAfter: ce.RetryAfter}, nil
	case isCE:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: safeCode(ce.Code)}, nil
	case errors.Is(err, ErrRejected): // lỗi kiểu M1 không phân lớp: hãng từ chối
		res.Code = checkRejected
	default:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: unknownOutcomeCode}, nil
	}
	b, mErr := json.Marshal(res)
	if mErr != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả kiểm: %w", mErr)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

func safeCode(code string) string {
	code = strings.ToLower(code)
	if carrierCode.MatchString(code) {
		return code
	}
	return carrierErrorCode
}

// OnDone ghi kết quả kiểm (trong transaction đổi lệnh sang done): chỉ khi khóa chưa đổi từ lúc kiểm; ok → active,
// còn lại → auth_failed (lỗi xác thực: thêm lệnh cảnh báo khi tài khoản vừa sang auth_failed).
func (checkCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var p checkPayload
	var r checkResult
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return fmt.Errorf("đọc lệnh kiểm: %w", err)
	}
	if result == nil { // done do tra lại (Lookup luôn not_sent nên không xảy ra): không có kết quả để ghi
		return nil
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return fmt.Errorf("đọc kết quả kiểm: %w", err)
	}
	status := AccountAuthFailed
	if r.OK {
		status = AccountActive
	}
	at := pgtype.Timestamptz{}
	if r.CredentialUpdatedAt != nil {
		at = pgtype.Timestamptz{Time: *r.CredentialUpdatedAt, Valid: true}
	}
	row, err := store.New(tx).ApplyCheckResult(ctx, store.ApplyCheckResultParams{
		Status: status, Result: pgtype.Text{String: r.Code, Valid: true}, CredentialUpdatedAt: at, ID: p.AccountID,
	})
	if errors.Is(err, pgx.ErrNoRows) { // khóa đã đổi (lệnh kiểm mới đã ghi) hoặc tài khoản đã thu hồi
		return nil
	}
	if err != nil {
		return fmt.Errorf("ghi kết quả kiểm: %w", err)
	}
	if r.Auth && row.Status == AccountAuthFailed && row.PrevStatus != AccountAuthFailed {
		return enqueueAuthAlert(ctx, tx, row.TenantID, p.AccountID, row.CarrierKey)
	}
	return nil
}

// MarkAuthFailed đánh dấu tài khoản accountID auth_failed khi hãng từ chối khóa API ở một lệnh bất kỳ của tài khoản
// (tạo, hủy, tra, báo phí…), trong tx của bên gọi (ngữ cảnh tenant của lệnh); code là mã lỗi của hãng (lọc
// [a-z0-9_]). Tài khoản active, checking, draft vừa sang auth_failed → ghi lệnh cảnh báo carrier-auth:<accountId>;
// paused, revoked → không đổi.
func MarkAuthFailed(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, code string) error {
	row, err := store.New(tx).MarkAuthFailed(ctx, store.MarkAuthFailedParams{
		Result: pgtype.Text{String: safeCode(code), Valid: true}, ID: accountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đánh dấu tài khoản hãng auth_failed: %w", err)
	}
	if row.PrevStatus == AccountAuthFailed {
		return nil
	}
	return enqueueAuthAlert(ctx, tx, row.TenantID, accountID, row.CarrierKey)
}

type authAlertPayload struct {
	AccountID  uuid.UUID `json:"accountId"`
	CarrierKey string    `json:"carrierKey"`
}

func enqueueAuthAlert(ctx context.Context, tx pgx.Tx, tenantID, accountID uuid.UUID, carrierKey string) error {
	n, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("sinh khóa lệnh cảnh báo: %w", err)
	}
	if _, _, err := queue.NewQueue().Enqueue(ctx, tx, authAlertKind(), queue.NewJob{
		TenantID: &tenantID, Payload: authAlertPayload{AccountID: accountID, CarrierKey: carrierKey},
		DedupeKey: "auth:" + accountID.String() + ":" + n.String(),
	}); err != nil {
		return fmt.Errorf("ghi lệnh cảnh báo khóa API: %w", err)
	}
	return nil
}

func authAlertKind() queue.Kind { return commands.CommandKind(AuthAlertKind, 0, authAlertTimeout) }

// Notifier gửi một cảnh báo vận hành (observability.Alerter thỏa interface này; cmd/crm nối).
type Notifier interface {
	Notify(ctx context.Context, key, severity, text string) error
}

// NewAuthAlertCommand trả lệnh AuthAlertKind: Notify(key "carrier-auth:<accountId>", normal, văn bản chỉ có khóa hãng
// và mã tài khoản). Notify lỗi → log một lần ở đây (logger nil: không log), not_sent và tự gửi lại; n nil → rejected
// alerter_missing. cmd/crm đăng ký cùng các lệnh cảnh báo khác (dùng chung Alerter).
func NewAuthAlertCommand(n Notifier, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return authAlert{n: n, log: logger}
}

type authAlert struct {
	n   Notifier
	log *slog.Logger
}

func (authAlert) Kind() queue.Kind       { return authAlertKind() }
func (authAlert) AutoRetryNotSent() bool { return true }

func (a authAlert) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	var p authAlertPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
	}
	if a.n == nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonAlerter}, nil
	}
	text := fmt.Sprintf("Hãng %s từ chối khóa API của tài khoản %s: cần nhập lại khóa rồi kiểm kết nối", p.CarrierKey, p.AccountID)
	if err := a.n.Notify(ctx, "carrier-auth:"+p.AccountID.String(), severityNormal, text); err != nil {
		if a.log != nil {
			a.log.WarnContext(ctx, "gửi cảnh báo khóa API hãng lỗi", slog.String("account_id", p.AccountID.String()),
				slog.String("error", queue.ErrorText(err)))
		}
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonAlertFailed}, nil
	}
	return commands.Outcome{Status: commands.StatusDone}, nil
}
