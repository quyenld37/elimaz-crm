package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// vnZone là giờ Việt Nam (UTC+7, không đổi giờ): hạn gửi trả là 23:59:59 của ngày hạn theo giờ này.
var vnZone = time.FixedZone("ICT", 7*3600) //nolint:gochecknoglobals // hằng múi giờ

// requestRefPattern: q của danh sách là mã yêu cầu.
const requestRefPrefix = "YC-"

// RequestFilter là bộ lọc danh sách yêu cầu cho nhân viên: trạng thái, loại (rỗng: mọi), q (mã yêu cầu YC-…, mã đơn
// DH…, SĐT người nhận hay đuôi, tên khách không dấu), con trỏ trang sau, số dòng (1–100, 0 → 50).
type RequestFilter struct {
	Status, Kind, Q string
	After           *Cursor
	Limit           int
}

// ListRequests trả một trang yêu cầu trên đơn của người bán sellerID (ADR-29), mới trước, và con trỏ trang sau (nil:
// hết). Trạng thái, loại lạ → ErrInvalidInput; q sai như danh sách đơn (ResolveListSearch).
func ListRequests(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, f RequestFilter) ([]RequestSummary, *Cursor, error) {
	limit := f.Limit
	switch {
	case limit == 0:
		limit = 50
	case limit < 1 || limit > MaxRequestListLimit:
		return nil, nil, fmt.Errorf("%w: limit", ErrInvalidInput)
	}
	p := store.ListOrderRequestsParams{SellerID: sellerID, Lim: int32(limit + 1)}
	if f.Status != "" {
		if _, ok := requestTransitions[f.Status]; !ok && !requestClosed(f.Status) {
			return nil, nil, fmt.Errorf("%w: trạng thái", ErrInvalidInput)
		}
		p.Status = pgtype.Text{String: f.Status, Valid: true}
	}
	if f.Kind != "" {
		switch f.Kind {
		case RequestCancel, RequestExchange, RequestReturn, RequestQuestion:
		default:
			return nil, nil, fmt.Errorf("%w: loại", ErrInvalidInput)
		}
		p.Kind = pgtype.Text{String: f.Kind, Valid: true}
	}
	if q := strings.TrimSpace(f.Q); strings.HasPrefix(strings.ToUpper(q), requestRefPrefix) {
		p.Ref = pgtype.Text{String: strings.ToUpper(q), Valid: true}
	} else if q != "" {
		s, err := ResolveListSearch(ctx, tx, q, nil)
		if err != nil {
			return nil, nil, err
		}
		p.OrderCode = pgtype.Text{String: s.Code, Valid: s.Code != ""}
		p.ShipPhone = pgtype.Text{String: s.Phone, Valid: s.Phone != ""}
		p.PhoneTail = pgtype.Text{String: s.PhoneTail, Valid: s.PhoneTail != ""}
		p.CustomerRoots = s.NameRoots
	}
	if f.After != nil {
		p.AfterAt = pgtype.Timestamptz{Time: f.After.CreatedAt, Valid: true}
		p.AfterID = pgtype.UUID{Bytes: f.After.ID, Valid: true}
	}
	rows, err := store.New(tx).ListOrderRequests(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách yêu cầu: %w", err)
	}
	var next *Cursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	out := make([]RequestSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, RequestSummary{
			Request: Request{
				ID: r.ID, OrderID: r.OrderID, Ref: r.Ref, Kind: r.Kind, Status: r.Status, Version: int(r.Version),
			},
			OrderCode: r.OrderCode, ReasonCode: r.ReasonCode, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		})
	}
	return out, next, nil
}

// lockRequest khóa yêu cầu id (nhóm 7). Không có → ErrRequestNotFound.
func lockRequest(ctx context.Context, q *store.Queries, id uuid.UUID) (Request, error) {
	r, err := q.LockOrderRequest(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrRequestNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("khóa yêu cầu: %w", err)
	}
	return Request{ID: r.ID, OrderID: r.OrderID, Ref: r.Ref, Kind: r.Kind, Status: r.Status, Version: int(r.Version)}, nil
}

// ReplyRequest ghi tin của nhân viên a (5–1000 ký tự) vào yêu cầu id: visible hiện cho khách trên website, không thì là
// ghi chú nội bộ. Tin hiện cho khách khi yêu cầu đã đóng → ErrInvalidTransition; ghi chú nội bộ luôn được. Không đổi
// trạng thái, version (không làm lệch version người khác đang đổi trạng thái); báo hiệu màn hình.
func (w Writer) ReplyRequest(ctx context.Context, tx pgx.Tx, id uuid.UUID, body string, visible bool, a Actor,
	now time.Time,
) (RequestMessage, error) {
	body, err := cleanMessage(body)
	if err != nil {
		return RequestMessage{}, err
	}
	q := store.New(tx)
	r, err := lockRequest(ctx, q, id)
	if err != nil {
		return RequestMessage{}, err
	}
	if visible && requestClosed(r.Status) {
		return RequestMessage{}, ErrInvalidTransition
	}
	now = now.UTC()
	m, err := insertMessage(ctx, q, a.TenantID, id, AuthorStaff, a.UserID.String(), body, visible, now)
	if err != nil {
		return RequestMessage{}, err
	}
	if err := q.TouchOrderRequest(ctx, store.TouchOrderRequestParams{ID: id, Now: now}); err != nil {
		return RequestMessage{}, fmt.Errorf("ghi giờ đổi của yêu cầu: %w", err)
	}
	if err := w.publishRequest(ctx, tx, a.TenantID, id); err != nil {
		return RequestMessage{}, err
	}
	return m, nil
}

// StatusChange là lần đổi trạng thái của nhân viên: trạng thái mới, tin kèm (hiện cho khách; bắt buộc khi needs_info),
// hướng dẫn gửi trả (bắt buộc và chỉ khi confirmed: địa chỉ 10–300, mã gói 3–40, ngày hạn không trước hôm nay giờ Việt
// Nam), kết quả (chỉ khi resolved: mã vận đơn ≤ 60; rejected: lý do 1–500 bắt buộc).
type StatusChange struct {
	Status, Message string
	ShipBack        *ShipBackInput
	Outcome         *Outcome
}

// ShipBackInput là hướng dẫn gửi trả nhân viên nhập; Deadline là ngày (giờ bỏ qua, đọc theo giờ Việt Nam).
type ShipBackInput struct {
	Address, PackageCode string
	Deadline             time.Time
}

// SetRequestStatus đổi trạng thái yêu cầu id theo sơ đồ khi version khớp (khóa dòng; lệch → ErrVersionConflict). Cạnh
// không có, confirmed cho yêu cầu không phải đổi hàng → ErrInvalidTransition; thiếu tin khi needs_info, hướng dẫn gửi
// trả sai, trường không hợp trạng thái → ErrInvalidInput; từ chối không lý do → ErrReasonRequired. resolved, rejected
// ghi người a và giờ đóng. Tin kèm thành tin hiện cho khách. Báo hiệu; trả trước, sau cho audit.
func (w Writer) SetRequestStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, ch StatusChange, version int, a Actor,
	now time.Time,
) (RequestChange, error) {
	now = now.UTC()
	p, msg, err := statusParams(ch, now)
	if err != nil {
		return RequestChange{}, err
	}
	q := store.New(tx)
	cur, err := lockRequest(ctx, q, id)
	if err != nil {
		return RequestChange{}, err
	}
	if cur.Version != version {
		return RequestChange{}, ErrVersionConflict
	}
	if !CanTransitionRequest(cur.Status, ch.Status) || (ch.Status == RequestConfirmed && cur.Kind != RequestExchange) {
		return RequestChange{}, ErrInvalidTransition
	}
	if ch.Status == RequestResolved || ch.Status == RequestRejected {
		p.ResolvedAt = pgtype.Timestamptz{Time: now, Valid: true}
		p.ResolvedBy = pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil}
	}
	if msg != "" {
		if _, err := insertMessage(ctx, q, a.TenantID, id, AuthorStaff, a.UserID.String(), msg, true, now); err != nil {
			return RequestChange{}, err
		}
	}
	return w.moveRequest(ctx, q, tx, a.TenantID, cur, ch.Status, p, now)
}

// statusParams kiểm phần không cần database của ch và dựng ship_back, outcome; trả tin kèm đã cắt.
func statusParams(ch StatusChange, now time.Time) (store.UpdateOrderRequestStatusParams, string, error) {
	var p store.UpdateOrderRequestStatusParams
	msg := strings.TrimSpace(ch.Message)
	if msg != "" || ch.Status == RequestNeedsInfo {
		var err error
		if msg, err = cleanMessage(msg); err != nil {
			return p, "", err
		}
	}
	if (ch.ShipBack != nil) != (ch.Status == RequestConfirmed) {
		return p, "", fmt.Errorf("%w: hướng dẫn gửi trả chỉ khi xác nhận đổi", ErrInvalidInput)
	}
	if ch.ShipBack != nil {
		sb, err := shipBackOf(*ch.ShipBack, now)
		if err != nil {
			return p, "", err
		}
		if p.ShipBack, err = json.Marshal(sb); err != nil {
			return p, "", fmt.Errorf("mã hóa hướng dẫn gửi trả: %w", err)
		}
	}
	out, err := outcomeOf(ch)
	if err != nil {
		return p, "", err
	}
	if out != (Outcome{}) {
		if p.Outcome, err = json.Marshal(out); err != nil {
			return p, "", fmt.Errorf("mã hóa kết quả: %w", err)
		}
	}
	return p, msg, nil
}

// outcomeOf kiểm kết quả theo trạng thái đích: rejected cần lý do 1–500 (thiếu → ErrReasonRequired), resolved có mã
// vận đơn ≤ 60; trạng thái khác không có kết quả.
func outcomeOf(ch StatusChange) (Outcome, error) {
	if ch.Outcome == nil {
		if ch.Status == RequestRejected {
			return Outcome{}, ErrReasonRequired
		}
		return Outcome{}, nil
	}
	out := Outcome{TrackingCode: strings.TrimSpace(ch.Outcome.TrackingCode), Reason: strings.TrimSpace(ch.Outcome.Reason)}
	switch ch.Status {
	case RequestRejected:
		if out.Reason == "" {
			return Outcome{}, ErrReasonRequired
		}
		if !runesBetween(out.Reason, 1, MaxRejectReasonRunes) || out.TrackingCode != "" {
			return Outcome{}, fmt.Errorf("%w: lý do từ chối", ErrInvalidInput)
		}
	case RequestResolved:
		if !runesBetween(out.TrackingCode, 0, MaxTrackingCodeRunes) || out.Reason != "" {
			return Outcome{}, fmt.Errorf("%w: mã vận đơn", ErrInvalidInput)
		}
	default:
		return Outcome{}, fmt.Errorf("%w: kết quả chỉ khi đóng yêu cầu", ErrInvalidInput)
	}
	return out, nil
}

// shipBackOf kiểm hướng dẫn gửi trả: địa chỉ 10–300, mã gói 3–40 ký tự, ngày hạn không trước ngày của now theo giờ Việt
// Nam; hạn lưu là 23:59:59 giờ Việt Nam của ngày đó.
func shipBackOf(in ShipBackInput, now time.Time) (ShipBack, error) {
	sb := ShipBack{Address: strings.TrimSpace(in.Address), PackageCode: strings.TrimSpace(in.PackageCode)}
	y, m, d := in.Deadline.Date()
	sb.Deadline = time.Date(y, m, d, 23, 59, 59, 0, vnZone).UTC()
	if !runesBetween(sb.Address, MinShipAddressRunes, MaxShipAddressRunes) ||
		!runesBetween(sb.PackageCode, MinPackageCodeRunes, MaxPackageCodeRunes) || in.Deadline.IsZero() ||
		sb.Deadline.Before(now) {
		return ShipBack{}, fmt.Errorf("%w: hướng dẫn gửi trả", ErrInvalidInput)
	}
	return sb, nil
}
