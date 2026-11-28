package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vận chuyển: sự kiện hãng của đơn ngoài hệ thống (MO07, Task M7-20 = 265).

const defaultUnmatchedLimit = 50

// ListUnmatchedCarrierEvents (GET /api/v1/fulfillment/unmatched-events, fulfillment.unmatched.read): sự kiện hãng
// của vận đơn không có trong CRM, mới thấy trước — mã vận đơn, hãng, tài khoản, số lần, mốc giờ; không dữ liệu khách.
// from, to là ngày giờ Việt Nam (dòng có sự kiện trong [from 00:00, to+1 00:00)); con trỏ là mã dòng cuối trang trước.
// Con trỏ sai, from > to → 400 invalid_input.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListUnmatchedCarrierEvents(ctx context.Context, params gen.ListUnmatchedCarrierEventsParams) (gen.ListUnmatchedCarrierEventsRes, error) {
	after, err := idCursor(params.Cursor, fulfillment.ErrInvalidPage)
	if err != nil {
		return nil, err
	}
	page := fulfillment.UnmatchedPage{
		Carrier: params.Carrier.Or(""), Limit: int(params.Limit.Or(defaultUnmatchedLimit)), After: after,
	}
	ict := vnZone()
	if d, ok := params.From.Get(); ok {
		page.Since = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, ict)
	}
	if d, ok := params.To.Get(); ok {
		page.Until = time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, ict)
	}
	var (
		rows []fulfillment.UnmatchedEvent
		next *uuid.UUID
	)
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, _ identity.AuthUser) (err error) {
		rows, next, err = fulfillment.ListUnmatched(ctx, tx, page)
		return err //nolint:wrapcheck // fulfillment đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc sự kiện hãng ngoài hệ thống: %w", err)
	}
	out := &gen.UnmatchedCarrierEventPage{Items: make([]gen.UnmatchedCarrierEvent, 0, len(rows))}
	for _, r := range rows {
		v := gen.UnmatchedCarrierEvent{
			ID: r.ID, CarrierKey: r.CarrierKey, TrackingCode: r.TrackingCode, EventCount: r.EventCount,
			FirstSeenAt: r.FirstSeenAt, LastSeenAt: r.LastSeenAt,
		}
		if r.CarrierAccountID != nil {
			v.CarrierAccountId = gen.NewOptUUID(*r.CarrierAccountID)
		}
		if r.LastRawStatus != "" {
			v.LastRawStatus = gen.NewOptString(r.LastRawStatus)
		}
		out.Items = append(out.Items, v)
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}
