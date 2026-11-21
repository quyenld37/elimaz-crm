package fulfillment

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

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/cancel"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh hủy vận đơn ở hãng (kế hoạch M3, Task M3-14).

const (
	cancelCommandName = "command.shipment.cancel"
	// cancelTimeout là hạn của một lần gọi hãng hủy.
	cancelTimeout = 20 * time.Second

	reasonNotCancelRequested = "shipment_not_cancel_requested"
	reasonNoTracking         = "no_tracking_code"
	reasonCancelOutcome      = "cancel_outcome_unknown"
)

// CancelCommandKind là loại lệnh command.shipment.cancel: không hết hạn khi chờ, mỗi lần gọi hãng tối đa 20 s.
func CancelCommandKind() queue.Kind {
	return commands.CommandKind(cancelCommandName, 0, cancelTimeout)
}

// CancelCommand là lệnh hủy vận đơn ở hãng (commands.Handler, Looker, DoneHook, queue.TerminalHook). Kết quả hãng chỉ
// đổi vận đơn, đơn, tồn qua ApplyTracking (đường EffCancelConfirmed, EffCancelRejected của Task M3-08): không có đường
// thứ hai nhả hàng.
type CancelCommand struct {
	s *Service
}

// NewCancelCommand tạo lệnh hủy dùng adapter, database, đường ghi của s.
func NewCancelCommand(s *Service) *CancelCommand { return &CancelCommand{s: s} }

// Kind trả CancelCommandKind().
func (*CancelCommand) Kind() queue.Kind { return CancelCommandKind() }

// AutoRetryNotSent (Q-24): chắc chưa gửi thì tự thử lại chỉ khi mọi hãng hủy qua API đã khai CancelIdempotent (một
// handler cho mọi hãng: một hãng chưa kiểm chứng là không); không thì dead, việc manual_cancel.
func (c *CancelCommand) AutoRetryNotSent() bool {
	if c.s.d.Carriers == nil {
		return false
	}
	found := false
	for _, k := range c.s.d.Carriers.Keys() {
		ad, _ := c.s.d.Carriers.Get(k)
		caps := ad.Capabilities()
		if !caps.Cancel {
			continue
		}
		if !caps.CancelIdempotent {
			return false
		}
		found = true
	}
	return found
}

type cancelResult struct {
	Outcome string `json:"outcome"`
}

// cancelTarget là vận đơn của lệnh đã đọc kèm tài khoản (giải mã), adapter; refused khác nil: không gọi hãng được.
type cancelTarget struct {
	row     store.CancelTargetRow
	acc     Account
	ad      CarrierAdapter
	refused *commands.Outcome
}

// target đọc vận đơn và tài khoản trong một transaction ngắn (ngoài transaction của lệnh).
func (c *CancelCommand) target(ctx context.Context, j queue.Job, id uuid.UUID) (cancelTarget, error) {
	var t cancelTarget
	err := tenancy.WithTenant(ctx, c.s.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		t = cancelTarget{} // RetryTx có thể chạy lại
		row, err := store.New(tx).CancelTarget(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("đọc vận đơn của lệnh hủy: %w", err)
		}
		t.row = row
		ad, ok := c.s.adapter(row.CarrierKey)
		switch {
		case !ok:
			t.refused = &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoAdapter}
			return nil
		case row.Mode != ModeAPI || !ad.Capabilities().Cancel:
			t.refused = &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNotSupported}
			return nil
		case !row.TrackingCode.Valid:
			t.refused = &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoTracking}
			return nil
		}
		t.ad = ad
		t.acc, err = c.s.ResolveAccount(ctx, tx, row.CarrierAccountID)
		switch {
		case errors.Is(err, ErrAccountUnavailable), errors.Is(err, ErrAccountNotFound):
			t.refused = &commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}
		case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
			t.refused = &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonKeys}
		case err != nil:
			return err
		}
		return nil
	}, db.TxWorker)
	return t, err //nolint:wrapcheck // lỗi đã bọc trong thân
}

// Execute (ngoài transaction): vận đơn còn cancel_requested → adapter.Cancel theo mã hãng. CancelDone → done
// cancelled; CancelRejectedPickedUp → done rejected; CancelPending → done pending; kết quả lạ, quá giờ, lỗi không rõ →
// lỗi (in_doubt, không gửi lại); lỗi chắc chắn → như lệnh tạo (classifyCreateError: chưa gửi → not_sent, từ chối →
// rejected). Vận đơn không còn cancel_requested, không có → rejected, không gọi hãng.
func (c *CancelCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	t, stop, ok := c.executeTarget(ctx, j)
	if !ok {
		return stop, nil
	}
	switch {
	case t.row.Status != string(StatusCancelRequested):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNotCancelRequested}, nil
	case t.refused != nil:
		return *t.refused, nil
	}
	res, err := t.ad.Cancel(ctx, t.acc, t.row.TrackingCode.String)
	if err != nil {
		out, _, known := classifyCreateError(err)
		if !known {
			return commands.Outcome{}, fmt.Errorf("hủy vận đơn ở hãng: %w", err)
		}
		return out, nil
	}
	outcome, ok := cancel.Outcome(string(res))
	if !ok {
		return commands.Outcome{}, fmt.Errorf("hủy vận đơn ở hãng: %s", reasonCancelOutcome) //nolint:err113 // in_doubt
	}
	b, err := json.Marshal(cancelResult{Outcome: outcome})
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả hủy: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// executeTarget đọc vận đơn của lệnh j cho Execute; ok = false: không gọi hãng, kết thúc lệnh bằng stop (payload hỏng,
// không có vận đơn → rejected; lỗi đọc → not_sent).
func (c *CancelCommand) executeTarget(ctx context.Context, j queue.Job) (cancelTarget, commands.Outcome, bool) {
	p, err := parsePayload(j)
	if err != nil {
		return cancelTarget{}, commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, false
	}
	t, err := c.target(ctx, j, p.ShipmentID)
	stop, failed := targetFailed(err)
	return t, stop, !failed
}

// Lookup (commands.Looker, ResolveInDoubt) tra trạng thái kiện ở hãng: đã hủy hay hàng đã đi → done (OnDone ghi lệnh
// tra trạng thái để áp kết quả thật); còn chờ thu gom → not_sent; không tra được → unknown. Vận đơn không còn
// cancel_requested → done (đã có kết quả theo đường khác).
func (c *CancelCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	var t cancelTarget
	p, err := parsePayload(j)
	if err == nil {
		t, err = c.target(ctx, j, p.ShipmentID)
	}
	switch {
	case err != nil:
		return commands.StatusUnknown, err
	case t.row.Status != string(StatusCancelRequested):
		return commands.StatusDone, nil
	case t.refused != nil || t.ad == nil:
		return commands.StatusUnknown, nil
	}
	canon, err := c.latest(ctx, t)
	if err != nil {
		return commands.StatusUnknown, err
	}
	return cancel.LookStatus(canon), nil
}

// latest là trạng thái chuẩn mới nhất của kiện: Track (sự kiện mới nhất) khi adapter có, không thì GetStatus.
func (*CancelCommand) latest(ctx context.Context, t cancelTarget) (Canonical, error) {
	tracking := t.row.TrackingCode.String
	if t.ad.Capabilities().Track {
		tr, err := t.ad.Track(ctx, t.acc, tracking, t.row.Code)
		if err != nil {
			return CanonUnknown, fmt.Errorf("tra hành trình: %w", err)
		}
		if len(tr.Events) == 0 {
			return CanonUnknown, nil
		}
		last := slices.MaxFunc(tr.Events, func(a, b CarrierEvent) int { return a.OccurredAt.Compare(b.OccurredAt) })
		return last.Canonical, nil
	}
	st, err := t.ad.GetStatus(ctx, t.acc, tracking)
	if err != nil {
		return CanonUnknown, fmt.Errorf("đọc trạng thái: %w", err)
	}
	canon, _ := t.ad.MapCanonical(st.RawStatus)
	return canon, nil
}

// OnDone (trong transaction đổi lệnh sang done): vận đơn còn cancel_requested → cancelled: ApplyTracking với sự kiện
// tổng hợp Cancelled (EffCancelConfirmed: order_cancel → đơn cancelled, nhả hàng; replace → gỡ vận đơn, giữ hàng);
// rejected: sự kiện PickedUp (EffPickedUp + EffCancelRejected: đơn về confirmed, hàng in_transit); pending hay không
// có kết quả (done do tra lại) → ghi lệnh tra trạng thái (khóa cancel-check:<lệnh>) để áp kết quả thật. Mọi lần: đóng
// việc in_doubt_cancel còn mở.
func (c *CancelCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	p, err := parsePayload(j)
	if err != nil {
		return err
	}
	var r cancelResult
	if len(result) > 0 && string(result) != "null" {
		if err := json.Unmarshal(result, &r); err != nil {
			return fmt.Errorf("lệnh %s: kết quả hủy không đọc được: %w", j.ID, queue.ErrPermanent)
		}
	}
	q := store.New(tx)
	l, found, err := lockJobShipment(ctx, tx, q, p.ShipmentID)
	if !found || err != nil {
		return err
	}
	if err := closeInDoubtCancel(ctx, q, l.ID, cmp.Or(r.Outcome, "carrier_checked")); err != nil {
		return err
	}
	if l.Status != string(StatusCancelRequested) {
		return nil
	}
	switch r.Outcome {
	case cancel.OutcomeCancelled:
		return c.apply(ctx, tx, q, j, p.ShipmentID, CanonCancelled)
	case cancel.OutcomeRejected:
		return c.apply(ctx, tx, q, j, p.ShipmentID, CanonPickedUp)
	}
	return enqueueRefresh(ctx, tx, c.s.d.Queue, *j.TenantID, p.ShipmentID, "cancel-check:"+j.ID.String())
}

// apply áp sự kiện tổng hợp canon của lệnh j (nguồn carrier_api, giờ không cũ hơn mốc sự kiện đã xét của vận đơn).
func (c *CancelCommand) apply(ctx context.Context, tx pgx.Tx, q *store.Queries, j queue.Job, id uuid.UUID,
	canon Canonical,
) error {
	now := c.s.d.Now().UTC()
	at := now
	last, err := q.ShipmentLastEvent(ctx, id)
	if err != nil {
		return fmt.Errorf("đọc mốc sự kiện của vận đơn: %w", err)
	}
	if last.Valid && last.Time.After(at) {
		at = last.Time.UTC()
	}
	if _, err := c.s.ApplyTracking(ctx, tx, ApplyInput{
		ShipmentID: id, Source: SrcCarrierAPI, SyncedAt: now,
		Tracking: Tracking{Events: []CarrierEvent{{
			Key: "cancel:" + j.ID.String(), RawStatus: "cancel_" + string(canon), Canonical: canon, OccurredAt: at,
		}}},
	}); err != nil {
		return fmt.Errorf("áp kết quả hủy: %w", err)
	}
	return nil
}

// lockJobShipment khóa đơn rồi vận đơn id của lệnh hủy (trong hook); không có → found = false, nil.
func lockJobShipment(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID) (lockedCancel, bool, error) {
	l, err := lockForCancel(ctx, tx, q, id, nil)
	if errors.Is(err, ErrNotFound) {
		return l, false, nil
	}
	return l, err == nil, err
}

// closeInDoubtCancel đóng việc in_doubt_cancel còn mở của vận đơn id với kết quả resolution.
func closeInDoubtCancel(ctx context.Context, q *store.Queries, id uuid.UUID, resolution string) error {
	if err := q.ResolveCancelAnomaly(ctx, store.ResolveCancelAnomalyParams{
		ShipmentID: id, Resolution: pgtype.Text{String: resolution, Valid: true},
	}); err != nil {
		return fmt.Errorf("đóng việc không rõ đã hủy: %w", err)
	}
	return nil
}

// OnTerminal (queue.TerminalHook, trong transaction đổi lệnh sang trạng thái kết thúc): vận đơn còn cancel_requested →
// in_doubt: việc in_doubt_cancel (không rõ hãng đã hủy chưa; tra lại hay người xem); dead, expired: việc manual_cancel
// (hãng chưa hủy: hủy trên trang hãng rồi ghi tay), đóng việc in_doubt_cancel nếu còn. Khác → không đổi gì.
func (c *CancelCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	p, err := parsePayload(j)
	if err != nil {
		return nil //nolint:nilerr // payload hỏng: không có vận đơn để đổi, lệnh vẫn kết thúc
	}
	q := store.New(tx)
	l, found, err := lockJobShipment(ctx, tx, q, p.ShipmentID)
	if !found || err != nil {
		return err
	}
	if status != queue.StatusInDoubt { // đã có kết quả chắc chắn (chưa hủy được): việc "không rõ" không còn đúng
		if err := closeInDoubtCancel(ctx, q, l.ID, status); err != nil {
			return err
		}
	}
	kind := cancel.TerminalAnomaly(l.Status, status)
	if kind == "" {
		return nil
	}
	if _, err := openAnomaly(ctx, q, l.ID, kind, map[string]any{"jobId": j.ID.String(), "jobStatus": status}); err != nil {
		return err
	}
	return c.s.publishShipment(ctx, tx, *j.TenantID, l.ID, l.WarehouseID)
}
