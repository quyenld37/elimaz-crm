package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Tác động của máy trạng thái (shipstate.Effect) trong transaction của ApplyTracking: đổi đơn và tồn chỉ qua public
// API của order (G-M2-3); việc gọi lại, bất thường là bảng của fulfillment. Tiền COD do cash ghi khi nghe
// shipment.status_changed, không ở đây.

const (
	reasonCarrierCorrected  = "carrier_corrected" // lý do đảo "đã giao" khi hãng sửa lại (order.ReverseDelivery)
	reasonStaffCorrected    = "staff_corrected"   // lý do đảo "đã giao" khi nhân viên sửa nhầm (Task M3-27)
	outcomeDelivered        = "delivered"
	outcomeCarrierReturning = "carrier_returning"
	anomalyCancelConflict   = "cancel_conflict"
	anomalyTrackingConflict = "tracking_conflict"
	cancelIntentReplace     = "replace"
)

// effect chạy một tác động ef của bước tới trạng thái next (sự kiện e, dòng lịch sử eventID).
func (r *applyRun) effect(ctx context.Context, ef shipstate.Effect, next shipstate.Status, eventID uuid.UUID,
	e CarrierEvent,
) error {
	var err error
	switch ef {
	case shipstate.EffPickedUp:
		_, err = r.w.OnShipmentEvent(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, order.StepPickedUp, r.actor, r.now)
	case shipstate.EffDelivered:
		_, err = r.w.OnShipmentEvent(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, order.StepDelivered, r.actor, r.now)
	case shipstate.EffDeliveryFailed:
		err = r.openCallback(ctx, eventID, e)
	case shipstate.EffReturning:
		err = r.returning(ctx)
	case shipstate.EffCloseCallbacks:
		outcome := outcomeCarrierReturning
		if next == shipstate.Delivered {
			outcome = outcomeDelivered
		}
		err = r.closeCallbacks(ctx, outcome)
	case shipstate.EffCarrierReturned: // chỉ mốc carrier_reported_returned_at (đã ghi khi cập nhật vận đơn, Q-9)
	case shipstate.EffCancelConfirmed:
		err = r.cancelConfirmed(ctx)
	case shipstate.EffCancelRejected:
		if r.sh.CancelIntent.String != cancelIntentReplace { // hủy để thay: đơn vẫn confirmed, không có gì để trả lời
			_, err = r.w.OnShipmentCancelRejected(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, r.actor, r.now)
		}
	case shipstate.EffDeliveryReversed:
		err = r.reverseDelivery(ctx, eventID)
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, order.ErrStockShort) {
		return fmt.Errorf("tác động %s: %w: %w", ef, ErrStockShort, err)
	}
	return fmt.Errorf("tác động %s: %w", ef, err)
}

// returning: đơn còn cancel_requested (vận đơn chưa nhận yêu cầu hủy mà hãng báo hoàn) về confirmed trước (Q-T91-2),
// rồi phiếu hoàn của hãng (order.OnShipmentReturning), rồi đóng việc gọi lại còn mở (carrier_returning).
func (r *applyRun) returning(ctx context.Context) error {
	snap, err := order.LockSnapshot(ctx, r.tx, r.sh.SellerID, r.sh.OrderID)
	if err != nil {
		return fmt.Errorf("đọc đơn: %w", err)
	}
	if snap.Status == order.StatusCancelRequested {
		if _, err := r.w.OnShipmentCancelRejected(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, r.actor,
			r.now); err != nil {
			return fmt.Errorf("đơn đang yêu cầu hủy về confirmed: %w", err)
		}
	}
	if _, err := r.w.OnShipmentReturning(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, r.actor, r.now); err != nil {
		return fmt.Errorf("phiếu hoàn của hãng: %w", err)
	}
	return r.closeCallbacks(ctx, outcomeCarrierReturning)
}

// cancelConfirmed: hãng xác nhận hủy → order.OnShipmentCancelConfirmed trong savepoint. Hàng đã rời kho
// (order.ErrInvalidTransition) → lùi phần đơn, mở cancel_conflict; vận đơn vẫn cancelled theo hãng.
func (r *applyRun) cancelConfirmed(ctx context.Context) error {
	var applied bool
	err := db.Savepoint(ctx, r.tx, func(sp pgx.Tx) (err error) {
		applied, err = r.w.OnShipmentCancelConfirmed(ctx, sp, r.sh.SellerID, r.sh.OrderID, r.sh.ID, r.actor, r.now)
		return err //nolint:wrapcheck // bọc ở effect
	})
	switch {
	case errors.Is(err, order.ErrInvalidTransition):
		return r.openAnomaly(ctx, anomalyCancelConflict, map[string]any{"status": r.sh.Status})
	case err != nil:
		return fmt.Errorf("đơn khi hãng xác nhận hủy: %w", err)
	}
	if applied { // đơn đã gỡ vận đơn này (hủy đơn, hay hủy để thay)
		r.isCurrent = false
	}
	return nil
}

// openCallback chèn việc gọi lại lần kế tiếp của vận đơn (hạn = lúc thất bại + CallbackDeadlineHours); đã có → bỏ
// qua. Lý do của hãng qua logging.ScrubText.
func (r *applyRun) openCallback(ctx context.Context, eventID uuid.UUID, e CarrierEvent) error {
	if r.callbackH == 0 {
		st, _, err := LoadSettings(ctx, r.tx)
		if err != nil {
			return err
		}
		r.callbackH = st.CallbackDeadlineHours
	}
	attempt, err := r.q.NextCallbackAttempt(ctx, r.sh.ID)
	if err != nil {
		return fmt.Errorf("đọc lần gọi lại kế tiếp: %w", err)
	}
	id, err := r.q.InsertCallbackTask(ctx, store.InsertCallbackTaskParams{
		ShipmentID: r.sh.ID, OrderID: r.sh.OrderID, AttemptNo: attempt,
		FailedEventID: pgtype.UUID{Bytes: eventID, Valid: true}, FailedReason: logging.ScrubText(e.Reason),
		FailedAt: e.OccurredAt, DueAt: e.OccurredAt.Add(time.Duration(r.callbackH) * time.Hour),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ghi việc gọi lại: %w", err)
	}
	return r.s.publishCallback(ctx, r.tx, r.sh.TenantID, id)
}

// closeCallbacks đóng mọi việc gọi lại còn mở của vận đơn với kết quả outcome.
func (r *applyRun) closeCallbacks(ctx context.Context, outcome string) error {
	ids, err := r.q.CloseOpenCallbacks(ctx, store.CloseOpenCallbacksParams{
		Outcome: pgtype.Text{String: outcome, Valid: true}, ShipmentID: r.sh.ID,
	})
	if err != nil {
		return fmt.Errorf("đóng việc gọi lại: %w", err)
	}
	for _, id := range ids {
		if err := r.s.publishCallback(ctx, r.tx, r.sh.TenantID, id); err != nil {
			return err
		}
	}
	return nil
}

// publishCallback báo hiệu callback_task.changed (fulfillment.callbacks.work) cho việc gọi lại id của tenant trong tx
// của bên gọi; Bus nil → bỏ qua.
func (s *Service) publishCallback(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) error {
	if s.d.Bus == nil {
		return nil
	}
	sig := realtime.Signal{
		TenantID: tenantID, Topic: signalCallbackChanged, Requires: permCallbacksWork,
		Entity: realtime.Entity{Type: "callback_task", ID: id.String()},
	}
	if err := s.d.Bus.Publish(ctx, tx, sig); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signalCallbackChanged, err)
	}
	return nil
}

// reverseDelivery đảo "đã giao" của đơn (order.ReverseDelivery, chứng từ = dòng lịch sử eventID): lý do
// staff_corrected khi nhân viên sửa nhầm, carrier_corrected khi hãng sửa lại; hãng sửa lại mà đơn thật sự được đảo thì
// ghi thêm lệnh cảnh báo carrier-corrected (Task M3-27).
func (r *applyRun) reverseDelivery(ctx context.Context, eventID uuid.UUID) error {
	reason := reasonCarrierCorrected
	if r.in.Source == SrcStaff {
		reason = reasonStaffCorrected
	}
	applied, err := r.w.ReverseDelivery(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, eventID, reason, r.actor, r.now)
	if err != nil || !applied || r.in.Source != SrcCarrierAPI {
		return err //nolint:wrapcheck // effect bọc tên tác động
	}
	return r.correctedAlert(ctx, eventID)
}
