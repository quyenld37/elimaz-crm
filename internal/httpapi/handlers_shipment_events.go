package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng shipment_events (kế hoạch M3, Task M3-08): lịch sử sự kiện của vận đơn.

const defaultShipmentEventLimit = 50

// ListShipmentEvents (GET /api/v1/shipments/{id}/events, fulfillment.read ở kho của tài khoản hãng của vận đơn):
// mới nhất trước, không có thân thô của hãng. Thiếu quyền ở kho → 404 (không lộ vận đơn có hay không); con trỏ sai
// → 400.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListShipmentEvents(ctx context.Context, params gen.ListShipmentEventsParams) (gen.ListShipmentEventsRes, error) {
	page := fulfillment.EventPage{Limit: int(params.Limit.Or(defaultShipmentEventLimit))}
	if c, ok := params.Cursor.Get(); ok {
		id, err := uuid.Parse(c)
		if err != nil {
			return nil, fmt.Errorf("con trỏ trang: %w", fulfillment.ErrInvalidPage)
		}
		page.After = &id
	}
	var (
		evs  []fulfillment.EventView
		next *uuid.UUID
	)
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) (err error) {
		s, err := fulfillment.Get(ctx, tx, params.ID)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		if canAt(u, s.WarehouseID, permShipRead) != nil {
			return fulfillment.ErrNotFound
		}
		evs, next, err = fulfillment.ShipmentEvents(ctx, tx, s.ID, page)
		return err //nolint:wrapcheck // fulfillment đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc lịch sử vận đơn: %w", err)
	}
	out := &gen.ShipmentEventPage{Items: make([]gen.ShipmentEvent, 0, len(evs))}
	for _, e := range evs {
		out.Items = append(out.Items, shipmentEventOut(e))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

func shipmentEventOut(e fulfillment.EventView) gen.ShipmentEvent {
	out := gen.ShipmentEvent{
		ID: gen.NewOptUUID(e.ID), OccurredAt: e.OccurredAt, RecordedAt: e.RecordedAt,
		Source: gen.ShipmentEventSource(e.Source), RawStatus: e.RawStatus,
	}
	if e.Status != "" {
		out.Status = gen.NewOptShipmentStatus(gen.ShipmentStatus(e.Status))
		out.Label = gen.NewOptString(e.Label)
	}
	if e.Applied != nil {
		out.Applied = gen.NewOptBool(*e.Applied)
	}
	if e.IgnoredReason != "" {
		out.IgnoredReason = gen.NewOptString(e.IgnoredReason)
	}
	return out
}
