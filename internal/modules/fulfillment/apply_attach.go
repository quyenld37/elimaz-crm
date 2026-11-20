package fulfillment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
)

const (
	maxTrackingCode       = 64 // CHECK của shipments.tracking_code
	trackingKeyConstraint = "shipments_tracking_key"
)

// AttachResult là kết quả gắn mã hãng: Already khi vận đơn đã có đúng mã đó (không làm gì); Conflict khi không gắn
// được (vận đơn có mã khác, không còn creating, hay mã đã thuộc vận đơn khác cùng hãng) — đã mở anomaly
// tracking_conflict, needs_review; Version là version của vận đơn sau khi gắn (0 khi không gắn).
type AttachResult struct {
	Already, Conflict bool
	Version           int64
}

// AttachTrackingCode gắn mã hãng trackingCode (và phí fee khi còn trống) cho vận đơn shipmentID trong tx của bên gọi
// (Task M3-11, M3-12): khóa đơn rồi vận đơn (ADR-46 nhóm 7 → 8); vận đơn creating → mã, create_state created,
// awaiting_pickup, version + 2, phát shipment.created (Seq v+1) và shipment.status_changed (Seq v+2), báo hiệu
// shipment.changed. Đã có đúng mã → Already. Có mã khác, không còn creating, hay mã đã thuộc vận đơn khác cùng hãng
// (unique shipments_tracking_key, bắt trong savepoint) → Conflict, anomaly tracking_conflict, needs_review, vận đơn
// giữ nguyên. Lỗi: mã rỗng hay dài quá 64 → ErrInvalidTracking; không có vận đơn, đơn → ErrNotFound.
func (s *Service) AttachTrackingCode(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID, trackingCode string,
	fee *int64,
) (AttachResult, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return AttachResult{}, errNotWired
	}
	if trackingCode == "" || len(trackingCode) > maxTrackingCode {
		return AttachResult{}, fmt.Errorf("%w: mã hãng dài %d", ErrInvalidTracking, len(trackingCode))
	}
	q := store.New(tx)
	ref, err := q.ShipmentOrderRef(ctx, shipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttachResult{}, ErrNotFound
	}
	if err != nil {
		return AttachResult{}, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	if err := order.Lock(ctx, tx, ref.SellerID, ref.OrderID); errors.Is(err, order.ErrNotFound) {
		return AttachResult{}, ErrNotFound
	} else if err != nil {
		return AttachResult{}, fmt.Errorf("khóa đơn của vận đơn: %w", err)
	}
	sh, err := q.LockShipmentForApply(ctx, shipmentID)
	if err != nil {
		return AttachResult{}, fmt.Errorf("khóa vận đơn: %w", err)
	}
	if sh.TrackingCode.Valid && sh.TrackingCode.String == trackingCode {
		return AttachResult{Already: true}, nil
	}
	if sh.TrackingCode.Valid || sh.Status != string(StatusCreating) {
		return conflict(ctx, q, sh, trackingCode, "shipment_not_creating")
	}
	var row store.AttachShipmentTrackingRow
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) (err error) {
		row, err = store.New(sp).AttachShipmentTracking(ctx, store.AttachShipmentTrackingParams{
			TrackingCode: pgtype.Text{String: trackingCode, Valid: true}, Fee: optInt8(fee), ID: sh.ID,
		})
		return err //nolint:wrapcheck // bọc dưới
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" &&
		pgErr.ConstraintName == trackingKeyConstraint {
		return conflict(ctx, q, sh, trackingCode, "tracking_code_taken")
	}
	if err != nil {
		return AttachResult{}, fmt.Errorf("gắn mã hãng: %w", err)
	}
	var feeOut *int64
	if row.Fee.Valid {
		feeOut = &row.Fee.Int64
	}
	if _, _, err := s.d.Events.Emit(ctx, tx, s.d.Queue, events.Event{
		TenantID: sh.TenantID, AggregateType: aggregateShipment, AggregateID: sh.ID.String(), Seq: row.Version - 1,
		Topic:   TopicShipmentCreated,
		Payload: ShipmentCreated{ShipmentID: sh.ID, OrderID: sh.OrderID, TrackingCode: trackingCode, Fee: feeOut},
	}); err != nil {
		return AttachResult{}, fmt.Errorf("phát %s: %w", TopicShipmentCreated, err)
	}
	if _, _, err := s.d.Events.Emit(ctx, tx, s.d.Queue, events.Event{
		TenantID: sh.TenantID, AggregateType: aggregateShipment, AggregateID: sh.ID.String(), Seq: row.Version,
		Topic: TopicShipmentStatusChanged,
		Payload: StatusChanged{
			ShipmentID: sh.ID, Status: StatusAwaitingPickup, CODAmount: sh.CodAmount, OrderID: sh.OrderID,
			From: StatusCreating,
		},
	}); err != nil {
		return AttachResult{}, fmt.Errorf("phát %s: %w", TopicShipmentStatusChanged, err)
	}
	if err := s.publishShipment(ctx, tx, sh.TenantID, sh.ID, sh.WarehouseID); err != nil {
		return AttachResult{}, err
	}
	return AttachResult{Version: row.Version}, nil
}

// conflict mở anomaly tracking_conflict (chi tiết: mã hãng mới, lý do) và đặt needs_review của vận đơn sh.
func conflict(ctx context.Context, q *store.Queries, sh store.LockShipmentForApplyRow, trackingCode, reason string,
) (AttachResult, error) {
	if _, err := openAnomaly(ctx, q, sh.ID, anomalyTrackingConflict, map[string]any{
		"trackingCode": trackingCode, "reason": reason, "status": sh.Status,
	}); err != nil {
		return AttachResult{}, err
	}
	return AttachResult{Conflict: true}, nil
}
