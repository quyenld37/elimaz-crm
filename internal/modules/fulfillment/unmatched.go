package fulfillment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/inbound"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
)

// Sự kiện hãng của vận đơn ngoài hệ thống (kế hoạch M7, Task 265 / M7-20; MO07, Q17): đơn cũ còn đang giao sau ngày mở
// vẫn có webhook hãng. CRM không đọc các đơn đó (ADR-11); handler vào ghi một dòng mỗi (hãng, mã vận đơn) và đếm số
// lần, không tra hãng, không cảnh báo. Không dữ liệu khách.

// MetricCarrierEventUnmatched là chỉ số tín hiệu hãng ghi "ngoài hệ thống" (nhãn: khóa hãng).
const MetricCarrierEventUnmatched = inbound.MetricUnmatched

// maxUnmatchedPage là cỡ trang lớn nhất của ListUnmatched (tham số limit của hợp đồng: 1–100).
const maxUnmatchedPage = 100

// UnmatchedEvent là một (hãng, mã vận đơn) ngoài hệ thống. CarrierAccountID nil khi không rõ tài khoản;
// LastRawStatus rỗng khi chưa sự kiện nào mang trạng thái.
type UnmatchedEvent struct {
	ID               uuid.UUID
	CarrierKey       string
	CarrierAccountID *uuid.UUID
	TrackingCode     string
	LastRawStatus    string
	EventCount       int32
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
}

// UnmatchedPage là một trang của ListUnmatched. Carrier rỗng: mọi hãng; Since, Until (UTC, zero: không giới hạn) chọn
// dòng có sự kiện trong [Since, Until); After là mã dòng cuối trang trước; Limit 1–100.
type UnmatchedPage struct {
	Carrier      string
	Since, Until time.Time
	After        *uuid.UUID
	Limit        int
}

// RecordUnmatched ghi một sự kiện hãng của vận đơn ngoài hệ thống trong tx của bên gọi (ngữ cảnh tenant): dòng mới
// event_count 1, đã có (cùng hãng, mã) thì +1, last_seen_at = now (không lùi), last_raw_status = rawStatus khi khác
// rỗng. Chỉ ghi database; bên gọi đếm chỉ số. Thứ tự khóa: nhóm 8 ADR-46, bảng cuối nhóm.
func RecordUnmatched(ctx context.Context, tx pgx.Tx, carrierKey string, accountID *uuid.UUID, trackingCode, rawStatus string,
	now time.Time,
) error {
	arg := store.RecordUnmatchedEventParams{
		CarrierKey: carrierKey, TrackingCode: trackingCode, SeenAt: now.UTC(),
		RawStatus: pgtype.Text{String: rawStatus, Valid: rawStatus != ""},
	}
	if accountID != nil {
		arg.CarrierAccountID = pgtype.UUID{Bytes: *accountID, Valid: true}
	}
	if err := store.New(tx).RecordUnmatchedEvent(ctx, arg); err != nil {
		return fmt.Errorf("ghi sự kiện hãng ngoài hệ thống: %w", err)
	}
	return nil
}

// ListUnmatched đọc một trang sự kiện ngoài hệ thống, mới thấy trước (first_seen_at giảm dần); trả mã dòng cuối khi còn
// trang sau. Cỡ trang ngoài 1–100, khoảng rỗng hay ngược, con trỏ không phải dòng của tenant → ErrInvalidPage.
func ListUnmatched(ctx context.Context, tx pgx.Tx, p UnmatchedPage) ([]UnmatchedEvent, *uuid.UUID, error) {
	if p.Limit < 1 || p.Limit > maxUnmatchedPage {
		return nil, nil, fmt.Errorf("%w: cỡ trang %d", ErrInvalidPage, p.Limit)
	}
	if !p.Since.IsZero() && !p.Until.IsZero() && !p.Until.After(p.Since) {
		return nil, nil, fmt.Errorf("%w: khoảng thời gian rỗng hay ngược", ErrInvalidPage)
	}
	q := store.New(tx)
	arg := store.ListUnmatchedEventsParams{Carrier: p.Carrier, Lim: int32(p.Limit) + 1}
	if !p.Since.IsZero() {
		arg.Since = pgtype.Timestamptz{Time: p.Since.UTC(), Valid: true}
	}
	if !p.Until.IsZero() {
		arg.Until = pgtype.Timestamptz{Time: p.Until.UTC(), Valid: true}
	}
	if p.After != nil {
		ok, err := q.UnmatchedEventExists(ctx, *p.After)
		if err != nil {
			return nil, nil, fmt.Errorf("kiểm con trỏ trang sự kiện ngoài hệ thống: %w", err)
		}
		if !ok {
			return nil, nil, fmt.Errorf("%w: con trỏ không phải dòng sự kiện ngoài hệ thống", ErrInvalidPage)
		}
		arg.AfterID = pgtype.UUID{Bytes: *p.After, Valid: true}
	}
	rows, err := q.ListUnmatchedEvents(ctx, arg)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc sự kiện hãng ngoài hệ thống: %w", err)
	}
	var next *uuid.UUID
	if len(rows) > p.Limit {
		last := rows[p.Limit-1].ID
		next, rows = &last, rows[:p.Limit]
	}
	out := make([]UnmatchedEvent, 0, len(rows))
	for _, r := range rows {
		v := UnmatchedEvent{
			ID: r.ID, CarrierKey: r.CarrierKey, TrackingCode: r.TrackingCode, LastRawStatus: r.LastRawStatus.String,
			EventCount: r.EventCount, FirstSeenAt: r.FirstSeenAt.UTC(), LastSeenAt: r.LastSeenAt.UTC(),
		}
		if r.CarrierAccountID.Valid {
			id := uuid.UUID(r.CarrierAccountID.Bytes)
			v.CarrierAccountID = &id
		}
		out = append(out, v)
	}
	return out, next, nil
}
