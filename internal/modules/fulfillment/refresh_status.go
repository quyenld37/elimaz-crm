package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh tra trạng thái vận đơn (kế hoạch M3, Task M3-11; B10): loại việc fulfillment.refresh_status của M1 giữ tên (việc
// đang chờ trong platform.jobs không mồ côi, A10). Execute đọc vận đơn và tài khoản hãng (đã giải mã) trong một
// transaction ngắn, gọi Track của adapter ngoài transaction (adapter không có Track: LookupByCode, GetStatus của M1, dựng
// hành trình một sự kiện); OnDone áp kết quả qua AttachTrackingCode, ApplyTracking. Tra là đọc: chạy lại không hại.

const (
	refreshExpires = 30 * time.Minute
	refreshTimeout = 20 * time.Second
	// closedAfter: vận đơn giao xong quá chừng này thì không tra nữa (hãng hết sửa trạng thái).
	closedAfter = 7 * 24 * time.Hour
	// markTimeout: hạn ghi mã lỗi đồng bộ sau một lần gọi hãng (ctx của lệnh có thể đã hết hạn).
	markTimeout = 5 * time.Second

	// Mã lỗi đồng bộ (shipments.last_sync_error, lý do của lệnh): chỉ mã ngắn, không thông điệp của hãng.
	syncTimeout          = "timeout"
	syncRateLimited      = "rate_limited"
	syncAuth             = "auth"
	syncManual           = "manual_account"
	syncNotSupported     = "not_supported"
	syncInconclusive     = "carrier_inconclusive"
	syncStockShort       = "stock_short"
	syncApplyFailed      = "apply_failed"
	syncTrackingConflict = "tracking_conflict"
	syncShipmentNotFound = "shipment_not_found"
	anomalyStockShort    = "stock_short"
)

var (
	errNotAtCarrier  = errors.New("fulfillment: hãng không có kiện theo mã vận đơn")
	errInconclusive  = errors.New("fulfillment: hãng không trả lời chắc chắn")
	errManualAccount = errors.New("fulfillment: tài khoản hãng nhập tay, không tra được")
)

// RefreshCommand là lệnh fulfillment.refresh_status (commands.Handler, Looker, DoneHook) của Service s.
type RefreshCommand struct{ s *Service }

// NewRefreshCommand tạo lệnh tra trạng thái vận đơn dùng adapter, khóa, đồng hồ, đường ghi của s.
func NewRefreshCommand(s *Service) *RefreshCommand { return &RefreshCommand{s: s} }

// Kind trả RefreshCommandKind().
func (*RefreshCommand) Kind() queue.Kind { return RefreshCommandKind() }

// AutoRetryNotSent trả true: đọc lại an toàn.
func (*RefreshCommand) AutoRetryNotSent() bool { return true }

// Lookup luôn not_sent: tra trạng thái chỉ đọc, chạy lại an toàn (lệnh in_doubt vì hết giữ chỗ về hàng chờ).
func (*RefreshCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// trackResult là Outcome.Result của lệnh tra: hành trình đã chuẩn hóa, mã hãng, tiền hãng báo, giờ tra. Chuỗi tự do của
// hãng đã qua logging.ScrubText; không tên, SĐT, địa chỉ.
type trackResult struct {
	TrackingCode string         `json:"trackingCode,omitempty"`
	Events       []trackedEvent `json:"events"`
	CODCollected *int64         `json:"codCollected,omitempty"`
	FeeActual    *int64         `json:"feeActual,omitempty"`
	SyncedAt     time.Time      `json:"syncedAt"`
}

type trackedEvent struct {
	Key        string    `json:"key"`
	RawStatus  string    `json:"rawStatus"`
	Canonical  Canonical `json:"canonical"`
	SubStatus  string    `json:"subStatus,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

func newTrackResult(tr Tracking, at time.Time) trackResult {
	out := trackResult{
		TrackingCode: tr.TrackingCode, Events: make([]trackedEvent, 0, len(tr.Events)), CODCollected: tr.CODCollected,
		FeeActual: tr.FeeActual, SyncedAt: at.UTC(),
	}
	for _, e := range tr.Events {
		out.Events = append(out.Events, trackedEvent{
			Key: e.Key, RawStatus: logging.ScrubText(e.RawStatus), Canonical: e.Canonical,
			SubStatus: logging.ScrubText(e.SubStatus), Reason: logging.ScrubText(e.Reason), OccurredAt: e.OccurredAt.UTC(),
		})
	}
	return out
}

func (r trackResult) tracking() Tracking {
	t := Tracking{TrackingCode: r.TrackingCode, CODCollected: r.CODCollected, FeeActual: r.FeeActual}
	for _, e := range r.Events {
		t.Events = append(t.Events, CarrierEvent(e))
	}
	return t
}

// trackTarget là thứ Execute đọc trước khi gọi hãng.
type trackTarget struct {
	row    store.TrackTargetRow
	acc    Account
	closed bool
}

// Execute tra vận đơn của lệnh j. Vận đơn đã kết thúc (cancelled, returned, delivered quá 7 ngày) → done không gọi hãng; tài khoản không
// active → rejected account_unavailable (mode manual: manual_account; khóa không giải mã được: decrypt_failed); ngược
// lại gọi hãng ngoài transaction. 429 → not_sent theo Retry-After; 401/403 → rejected auth + MarkAuthFailed; quá giờ,
// lỗi mạng, 5xx → not_sent; hãng từ chối khác → rejected. Mọi lần không đọc được ghi last_sync_error (mã), giữ
// last_synced_at.
func (r *RefreshCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, err := parsePayload(j)
	if err != nil {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil //nolint:nilerr // payload hỏng: dừng hẳn
	}
	tenant := *j.TenantID
	t, err := r.target(ctx, tenant, p.ShipmentID)
	switch {
	case errors.Is(err, ErrNotFound):
		return commands.Outcome{Status: commands.StatusRejected, Reason: syncShipmentNotFound}, nil
	case errors.Is(err, errManualAccount):
		return r.fail(ctx, tenant, t, commands.Outcome{Status: commands.StatusRejected, Reason: syncManual}, "")
	case errors.Is(err, ErrAccountUnavailable), errors.Is(err, ErrAccountNotFound):
		return r.fail(ctx, tenant, t, commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}, "")
	case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
		return r.fail(ctx, tenant, t, commands.Outcome{Status: commands.StatusRejected, Reason: reasonDecrypt}, "")
	case err != nil:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil
	case t.closed:
		return commands.Outcome{Status: commands.StatusDone}, nil
	}
	ad, ok := r.s.adapter(t.row.CarrierKey)
	if !ok {
		return r.fail(ctx, tenant, t, commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoAdapter}, "")
	}
	tr, err := r.read(ctx, ad, t)
	t.acc.Creds = nil
	if err != nil {
		out, authCode := classifyTrackError(err)
		return r.fail(ctx, tenant, t, out, authCode)
	}
	b, err := json.Marshal(newTrackResult(tr, r.s.d.Now()))
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả tra vận đơn: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// target đọc vận đơn, tài khoản (đã giải mã) trong một transaction ngắn. Lỗi: ErrNotFound; errManualAccount;
// ErrAccountUnavailable, ErrAccountNotFound, ErrKeysMissing, ErrCredentialsUnreadable (của ResolveAccount); lỗi đọc.
// t.row có giá trị với mọi lỗi trừ ErrNotFound và lỗi đọc.
func (r *RefreshCommand) target(ctx context.Context, tenant, id uuid.UUID) (trackTarget, error) {
	var t trackTarget
	var refused error
	err := tenancy.WithTenant(ctx, r.s.d.DB, tenant, func(tx pgx.Tx) error {
		t, refused = trackTarget{}, nil // RetryTx có thể chạy lại
		q := store.New(tx)
		row, err := q.TrackTarget(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			refused = ErrNotFound
			return nil
		}
		if err != nil {
			return fmt.Errorf("đọc vận đơn cần tra: %w", err)
		}
		t.row = row
		st := Status(row.Status)
		t.closed = st == StatusCancelled || st == StatusReturned ||
			(st == StatusDelivered && r.s.d.Now().Sub(row.ClosedAt) > closedAfter)
		if t.closed {
			return nil
		}
		if row.Mode != ModeAPI {
			refused = errManualAccount
			return nil
		}
		t.acc, err = r.s.ResolveAccount(ctx, tx, row.CarrierAccountID)
		switch {
		case errors.Is(err, ErrAccountUnavailable), errors.Is(err, ErrAccountNotFound), errors.Is(err, ErrKeysMissing),
			errors.Is(err, ErrCredentialsUnreadable):
			refused = err
			return nil
		case err != nil:
			return err
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return trackTarget{}, fmt.Errorf("đọc vận đơn của lệnh tra: %w", err)
	}
	return t, refused
}

// read gọi hãng (ngoài transaction). Vận đơn chưa có mã hãng: tra theo mã vận đơn của CRM khi adapter có LookupByRef
// (hay không có Track) để lấy mã hãng; adapter có Track → Track(mã hãng, mã vận đơn); không → GetStatus (hay kết quả
// tra theo mã) dựng hành trình một sự kiện như M1.
func (r *RefreshCommand) read(ctx context.Context, ad CarrierAdapter, t trackTarget) (Tracking, error) {
	tracking := t.row.TrackingCode.String
	caps := ad.Capabilities()
	if tracking == "" && (caps.LookupByRef || !caps.Track) {
		lr, err := ad.LookupByCode(ctx, t.acc, t.row.Code)
		switch {
		case err != nil:
			return Tracking{}, fmt.Errorf("tra vận đơn theo mã: %w", err)
		case lr.Inconclusive:
			return Tracking{}, errInconclusive
		case !lr.Found || lr.TrackingCode == "":
			return Tracking{}, errNotAtCarrier
		}
		if !caps.Track {
			return Tracking{TrackingCode: lr.TrackingCode, Events: []CarrierEvent{
				oneEvent(ad, "lookup:"+lr.RawStatus, lr.RawStatus, r.s.d.Now()),
			}}, nil
		}
		tracking = lr.TrackingCode
	}
	if !caps.Track {
		sr, err := ad.GetStatus(ctx, t.acc, tracking)
		if err != nil {
			return Tracking{}, fmt.Errorf("đọc trạng thái vận đơn: %w", err)
		}
		return Tracking{TrackingCode: tracking, Events: []CarrierEvent{
			oneEvent(ad, sr.EventKey, sr.RawStatus, sr.OccurredAt),
		}}, nil
	}
	tr, err := ad.Track(ctx, t.acc, tracking, t.row.Code)
	if err != nil {
		return Tracking{}, fmt.Errorf("đọc hành trình vận đơn: %w", err)
	}
	if tr.TrackingCode == "" {
		tr.TrackingCode = tracking
	}
	return tr, nil
}

// oneEvent dựng sự kiện từ trạng thái thô của đường M1 (mã lạ → CanonUnknown: ApplyTracking mở unmapped_status).
func oneEvent(ad CarrierAdapter, key, raw string, at time.Time) CarrierEvent {
	canon, _ := ad.MapCanonical(raw)
	return CarrierEvent{Key: key, RawStatus: raw, Canonical: canon, OccurredAt: at.UTC()}
}

// classifyTrackError đổi lỗi gọi hãng thành kết quả lệnh; authCode khác rỗng khi hãng từ chối khóa (mã lỗi của hãng
// cho MarkAuthFailed).
func classifyTrackError(err error) (commands.Outcome, string) {
	ce, isCE := errors.AsType[*CarrierError](err)
	switch {
	case errors.Is(err, errNotAtCarrier):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNotFound}, ""
	case errors.Is(err, errInconclusive):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: syncInconclusive}, ""
	case isCE && ce.Class == ClassRateLimited:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: syncRateLimited, RetryAfter: ce.RetryAfter}, ""
	case isCE && ce.Class == ClassAuth:
		code := ce.Code
		if code == "" {
			code = syncAuth
		}
		return commands.Outcome{Status: commands.StatusRejected, Reason: syncAuth}, code
	case isTimeout(err):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: syncTimeout}, ""
	case errors.Is(err, ErrNotSupported):
		return commands.Outcome{Status: commands.StatusRejected, Reason: syncNotSupported}, ""
	case isCE && (ce.Class == ClassRejected || ce.Class == ClassPermanent), !isCE && errors.Is(err, ErrRejected):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonRejected}, ""
	}
	return commands.Outcome{Status: commands.StatusNotSent, Reason: carrierErrorCode}, ""
}

// isTimeout: quá hạn của lệnh hay của client HTTP (lỗi có Timeout() = true).
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	te, ok := errors.AsType[interface {
		error
		Timeout() bool
	}](err)
	return ok && te.Timeout()
}

// fail ghi mã lỗi out.Reason vào last_sync_error của vận đơn (và MarkAuthFailed khi authCode khác rỗng) trong một
// transaction ngắn, hạn riêng (ctx của lệnh có thể đã hết hạn vì hãng treo); trả out. Ghi lỗi → not_sent (thử lại cả
// lần đọc, giữ RetryAfter) để tài khoản bị từ chối khóa luôn được đánh dấu.
func (r *RefreshCommand) fail(ctx context.Context, tenant uuid.UUID, t trackTarget, out commands.Outcome, authCode string,
) (commands.Outcome, error) {
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()
	err := tenancy.WithTenant(mctx, r.s.d.DB, tenant, func(tx pgx.Tx) error {
		if authCode != "" {
			if err := MarkAuthFailed(mctx, tx, t.row.CarrierAccountID, authCode); err != nil {
				return err
			}
		}
		if err := store.New(tx).SetSyncError(mctx, store.SetSyncErrorParams{Code: out.Reason, ID: t.row.ID}); err != nil {
			return fmt.Errorf("ghi lỗi đồng bộ vận đơn: %w", err)
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed, RetryAfter: out.RetryAfter}, nil //nolint:nilerr // database lỗi: đọc lại sau
	}
	return out, nil
}

// OnDone (trong transaction đổi lệnh sang done): trong một savepoint, gắn mã hãng khi vận đơn còn creating mà kết quả
// có mã (AttachTrackingCode, VC10; mã đã thuộc vận đơn khác → anomaly tracking_conflict, không áp hành trình), rồi
// ApplyTracking (nguồn carrier_api, mốc đồng bộ = giờ tra). Lỗi tạm của database → trả lên (Runner: in_doubt, tra lại
// rồi chạy lại). Lỗi lặp lại (tồn thực không đủ, kết quả sai dạng, ràng buộc…) → lùi savepoint, ghi last_sync_error
// (stock_short: thêm anomaly stock_short), ghi lệnh cảnh báo trong ngày, lệnh vẫn done: không tra lại vô ích (Q-T91-11);
// tra bù theo lịch thử lại sau.
func (r *RefreshCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if result == nil { // done không gọi hãng (vận đơn đã kết thúc) hay do tra lại: không có gì để áp
		return nil
	}
	p, err := parsePayload(j)
	if err != nil {
		return nil //nolint:nilerr // Execute đã từ chối payload hỏng; không có vận đơn để áp
	}
	var res trackResult
	if err := json.Unmarshal(result, &res); err != nil {
		return r.applyFailed(ctx, tx, *j.TenantID, p.ShipmentID, syncApplyFailed)
	}
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error { return r.apply(ctx, sp, p.ShipmentID, res) })
	switch {
	case err == nil:
		return nil
	case transientDBError(err):
		return fmt.Errorf("áp kết quả tra: %w", err)
	case errors.Is(err, ErrStockShort):
		return r.applyFailed(ctx, tx, *j.TenantID, p.ShipmentID, syncStockShort)
	}
	return r.applyFailed(ctx, tx, *j.TenantID, p.ShipmentID, syncApplyFailed)
}

func (r *RefreshCommand) apply(ctx context.Context, tx pgx.Tx, id uuid.UUID, res trackResult) error {
	if res.TrackingCode != "" {
		s, err := Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if s.TrackingCode == "" {
			ar, err := r.s.AttachTrackingCode(ctx, tx, id, res.TrackingCode, nil)
			if err != nil {
				return err
			}
			if ar.Conflict {
				return setSyncError(ctx, tx, id, syncTrackingConflict)
			}
		}
	}
	if _, err := r.s.ApplyTracking(ctx, tx, ApplyInput{
		ShipmentID: id, Tracking: res.tracking(), Source: SrcCarrierAPI, SyncedAt: res.SyncedAt,
	}); err != nil {
		return fmt.Errorf("áp kết quả tra: %w", err)
	}
	return nil
}

// applyFailed ghi mã code vào last_sync_error (stock_short: mở anomaly stock_short, needs_review) và lệnh cảnh báo
// trong ngày, trong tx của Runner (sau khi lùi savepoint; chỉ chạm dòng vận đơn rồi platform.jobs).
func (r *RefreshCommand) applyFailed(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID, code string) error {
	if code == syncStockShort {
		if _, err := openAnomaly(ctx, store.New(tx), id, anomalyStockShort, map[string]any{"reason": code}); err != nil {
			return err
		}
	}
	if err := setSyncError(ctx, tx, id, code); err != nil {
		return err
	}
	return enqueueTrackAlert(ctx, tx, r.s.queue(), tenant, code, r.s.d.Now())
}

func setSyncError(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string) error {
	if err := store.New(tx).SetSyncError(ctx, store.SetSyncErrorParams{Code: code, ID: id}); err != nil {
		return fmt.Errorf("ghi lỗi đồng bộ vận đơn: %w", err)
	}
	return nil
}

// transientDBError: lỗi database có thể hết khi chạy lại (40P01, 55P03, 40001; nhóm 08 kết nối, 53 thiếu tài nguyên,
// 57 quá hạn câu lệnh, đang tắt; lỗi lúc nối; ctx hết hạn hay bị hủy). Lỗi khác (sentinel nghiệp vụ, ràng buộc) chạy
// lại vẫn vậy. Mất kết nối không có SQLSTATE: lần ghi sau trong cùng tx cũng lỗi và được trả lên.
func transientDBError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
		return true
	}
	if _, connecting := errors.AsType[*pgconn.ConnectError](err); connecting {
		return true
	}
	pe, isPG := errors.AsType[*pgconn.PgError](err)
	if !isPG {
		return false
	}
	_, retryable := db.TransientCode(err)
	return retryable || slices.Contains([]string{"08", "53", "57"}, pe.Code[:min(len(pe.Code), 2)])
}
