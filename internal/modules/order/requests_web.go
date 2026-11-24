package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/files"
)

// requestOrder là đơn kênh web đã khóa (nhóm 7 ADR-46) cho việc tạo yêu cầu.
type requestOrder struct {
	seller, id  uuid.UUID
	status      string
	hasShipment bool
}

// CreateRequestFromWeb tạo yêu cầu in của khách cho đơn kênh web có mã ngoài externalRef (người bán sellerID) trong tx
// của bên gọi; a là khóa dịch vụ của website. Thứ tự: kiểm đầu vào không database (loại, lý do, lời nhắn 5–1000 ký tự,
// dòng, quá 5 ảnh → ErrTooManyFiles, đổi/trả có dòng DEFECT, WRONG_ITEM mà không ảnh → ErrPhotoRequired) → khóa đơn
// (không có → ErrNotFound) → quá MaxRequestsPerOrder24 yêu cầu trong 24 giờ → ErrRequestLimit → theo loại: hủy chỉ khi
// đơn new, confirmed, cancel_requested (khác → ErrRequestNotAllowed) và chưa có yêu cầu hủy đang mở (→ ErrDuplicateOpen); đổi, trả chỉ
// khi đơn delivered (khác → ErrRequestNotAllowed) trong ExchangeWindowDays ngày từ lần giao (quá → ErrWindowClosed),
// mỗi biến thể có trong đơn và số ≤ số đã mua trừ số đang nằm trong yêu cầu đổi, trả chưa bị từ chối hay rút
// (→ ErrQtyExceeded) → ảnh: tệp của tenant, mục đích order_request_photo, chưa xóa, chưa gắn yêu cầu khác (khác →
// ErrFileNotFound), imager đã từ chối → ErrFileRejected → chèn yêu cầu received, gắn ảnh, báo hiệu màn hình. Chạy lại
// được trong db.RetryTx.
func (w Writer) CreateRequestFromWeb(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, externalRef string,
	in CreateRequestInput, a Actor, now time.Time,
) (Request, error) {
	in, err := validateRequestInput(in)
	if err != nil {
		return Request{}, err
	}
	q := store.New(tx)
	r, err := q.LockWebOrderForRequest(ctx, store.LockWebOrderForRequestParams{
		SellerID: sellerID, ExternalRef: strings.TrimSpace(externalRef),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("khóa đơn web của yêu cầu: %w", err)
	}
	return w.createRequest(ctx, tx, requestOrder{
		seller: sellerID, id: r.ID, status: r.Status, hasShipment: r.CurrentShipmentID.Valid,
	}, in, a, now.UTC())
}

// CreateCancelRequest tạo yêu cầu hủy (lý do OTHER, lời nhắn note tùy chọn ≤ 1000 ký tự) cho đơn kênh web orderID khi
// khách bấm hủy mà đơn đã gửi hãng (M5-13, QĐ18): không đổi trạng thái đơn, nhân viên xử lý. Luật như
// CreateRequestFromWeb loại cancel (đã có yêu cầu hủy đang mở → ErrDuplicateOpen).
func (w Writer) CreateCancelRequest(ctx context.Context, tx pgx.Tx, sellerID, orderID uuid.UUID, note string, a Actor,
	now time.Time,
) (Request, error) {
	note = strings.TrimSpace(note)
	if !runesBetween(note, 0, MaxMessageRunes) {
		return Request{}, fmt.Errorf("%w: lời nhắn", ErrInvalidInput)
	}
	r, err := store.New(tx).LockOrderForRequest(ctx, store.LockOrderForRequestParams{SellerID: sellerID, ID: orderID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("khóa đơn của yêu cầu hủy: %w", err)
	}
	in := CreateRequestInput{Kind: RequestCancel, ReasonCode: "OTHER", Note: note}
	return w.createRequest(ctx, tx, requestOrder{
		seller: sellerID, id: r.ID, status: r.Status, hasShipment: r.CurrentShipmentID.Valid,
	}, in, a, now.UTC())
}

// validateRequestInput kiểm phần không cần database và chuẩn hóa (cắt khoảng trắng).
func validateRequestInput(in CreateRequestInput) (CreateRequestInput, error) {
	in.Note = strings.TrimSpace(in.Note)
	if !runesBetween(in.Note, MinMessageRunes, MaxMessageRunes) {
		return in, fmt.Errorf("%w: lời nhắn 5–1000 ký tự", ErrInvalidInput)
	}
	switch in.Kind {
	case RequestCancel:
		if !slices.Contains(cancelReasons, in.ReasonCode) || len(in.Lines) != 0 {
			return in, fmt.Errorf("%w: yêu cầu hủy", ErrInvalidInput)
		}
	case RequestQuestion:
		if (!slices.Contains(cancelReasons, in.ReasonCode) && !slices.Contains(exchangeReasons, in.ReasonCode)) ||
			len(in.Lines) != 0 {
			return in, fmt.Errorf("%w: câu hỏi", ErrInvalidInput)
		}
	case RequestExchange, RequestReturn:
		if err := validateExchangeLines(&in); err != nil {
			return in, err
		}
	default:
		return in, fmt.Errorf("%w: loại yêu cầu", ErrInvalidInput)
	}
	if len(in.FileIDs) > MaxRequestFiles {
		return in, ErrTooManyFiles
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.FileIDs {
		if id == uuid.Nil || seen[id] {
			return in, fmt.Errorf("%w: ảnh trùng", ErrInvalidInput)
		}
		seen[id] = true
	}
	if needsPhoto(in) && len(in.FileIDs) == 0 {
		return in, ErrPhotoRequired
	}
	return in, nil
}

func validateExchangeLines(in *CreateRequestInput) error {
	if !slices.Contains(exchangeReasons, in.ReasonCode) || !in.ConditionConfirmed || len(in.Lines) == 0 ||
		len(in.Lines) > MaxRequestLines {
		return fmt.Errorf("%w: yêu cầu đổi, trả", ErrInvalidInput)
	}
	seen := map[uuid.UUID]bool{}
	for i := range in.Lines {
		l := &in.Lines[i]
		l.Note = strings.TrimSpace(l.Note)
		if l.VariantID == uuid.Nil || seen[l.VariantID] || l.Qty < 1 || l.Qty > MaxRequestLineQty ||
			!slices.Contains(lineReasons, l.Reason) || !runesBetween(l.Note, 0, MaxLineNoteRunes) {
			return fmt.Errorf("%w: dòng đổi, trả", ErrInvalidInput)
		}
		seen[l.VariantID] = true
	}
	return nil
}

// needsPhoto: đổi, trả có lý do hàng lỗi hay giao nhầm (cấp yêu cầu hay dòng) phải kèm ít nhất một ảnh.
func needsPhoto(in CreateRequestInput) bool {
	if in.Kind != RequestExchange && in.Kind != RequestReturn {
		return false
	}
	if slices.Contains(photoReasons, in.ReasonCode) {
		return true
	}
	return slices.ContainsFunc(in.Lines, func(l RequestLine) bool { return slices.Contains(photoReasons, l.Reason) })
}

// createRequest kiểm theo đơn đã khóa o rồi chèn yêu cầu, gắn ảnh, báo hiệu.
func (w Writer) createRequest(ctx context.Context, tx pgx.Tx, o requestOrder, in CreateRequestInput, a Actor,
	now time.Time,
) (Request, error) {
	q := store.New(tx)
	n, err := q.CountRecentOrderRequests(ctx, store.CountRecentOrderRequestsParams{OrderID: o.id, Since: now.Add(-24 * time.Hour)})
	if err != nil {
		return Request{}, fmt.Errorf("đếm yêu cầu của đơn: %w", err)
	}
	if n >= MaxRequestsPerOrder24 {
		return Request{}, ErrRequestLimit
	}
	if err := checkOrderForRequest(ctx, tx, q, o, in, now); err != nil {
		return Request{}, err
	}
	if err := checkRequestFiles(ctx, tx, q, in.FileIDs); err != nil {
		return Request{}, err
	}
	lines := in.Lines
	if lines == nil {
		lines = []RequestLine{}
	}
	raw, err := json.Marshal(lines)
	if err != nil {
		return Request{}, fmt.Errorf("mã hóa dòng của yêu cầu: %w", err)
	}
	var id uuid.UUID
	var ref string
	for range 3 { // trùng mã YC- (40 bit ngẫu nhiên) thì sinh lại
		ref = newRequestRef()
		id, err = q.InsertOrderRequest(ctx, store.InsertOrderRequestParams{
			TenantID: a.TenantID, OrderID: o.id, Ref: ref, Kind: in.Kind, ReasonCode: in.ReasonCode, Lines: raw,
			Source: RequestSourceWeb, CustomerNote: pgtype.Text{String: in.Note, Valid: in.Note != ""},
			ConditionConfirmed: in.ConditionConfirmed, Now: now,
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			break
		}
	}
	if errors.Is(err, pgx.ErrNoRows) && in.Kind == RequestCancel {
		return Request{}, ErrDuplicateOpen // index một yêu cầu hủy đang mở (đã kiểm dưới khóa đơn; giữ để an toàn)
	}
	if err != nil {
		return Request{}, fmt.Errorf("ghi yêu cầu về đơn: %w", err)
	}
	for i, f := range in.FileIDs {
		rows, err := q.InsertOrderRequestFile(ctx, store.InsertOrderRequestFileParams{
			TenantID: a.TenantID, RequestID: id, FileID: f, Position: int32(i + 1),
		})
		if err != nil {
			return Request{}, fmt.Errorf("gắn ảnh vào yêu cầu: %w", err)
		}
		if rows != 1 {
			return Request{}, ErrFileNotFound // vừa được yêu cầu khác gắn
		}
	}
	if err := w.publishRequest(ctx, tx, a.TenantID, id); err != nil {
		return Request{}, err
	}
	return Request{ID: id, OrderID: o.id, Ref: ref, Kind: in.Kind, Status: RequestReceived, Version: 1}, nil
}

// checkOrderForRequest kiểm luật theo loại trên đơn đã khóa.
func checkOrderForRequest(ctx context.Context, tx pgx.Tx, q *store.Queries, o requestOrder, in CreateRequestInput,
	now time.Time,
) error {
	switch in.Kind {
	case RequestCancel: // cancel_requested: nhân viên đã yêu cầu hãng hủy, khách vẫn gửi được (Task 196, Q4)
		if o.status != StatusNew && o.status != StatusConfirmed && o.status != StatusCancelRequested {
			return ErrRequestNotAllowed
		}
		open, err := q.OpenCancelRequestExists(ctx, o.id)
		if err != nil {
			return fmt.Errorf("đọc yêu cầu hủy đang mở: %w", err)
		}
		if open {
			return ErrDuplicateOpen
		}
	case RequestExchange, RequestReturn:
		if o.status != StatusDelivered {
			return ErrRequestNotAllowed
		}
		if err := checkExchangeWindow(ctx, tx, q, o.id, now); err != nil {
			return err
		}
		return checkExchangeQty(ctx, q, o.seller, o.id, in.Lines)
	}
	return nil
}

func checkExchangeWindow(ctx context.Context, tx pgx.Tx, q *store.Queries, orderID uuid.UUID, now time.Time) error {
	at, err := q.OrderDeliveredAt(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRequestNotAllowed
	}
	if err != nil {
		return fmt.Errorf("đọc lúc giao đơn: %w", err)
	}
	s, err := LoadSettings(ctx, tx)
	if err != nil {
		return err
	}
	if now.After(at.Add(time.Duration(s.ExchangeWindowDays) * 24 * time.Hour)) {
		return ErrWindowClosed
	}
	return nil
}

// checkExchangeQty: mỗi dòng ≤ số đã mua − số đang nằm trong yêu cầu đổi, trả chưa bị từ chối hay rút.
func checkExchangeQty(ctx context.Context, q *store.Queries, sellerID, orderID uuid.UUID, lines []RequestLine) error {
	remaining, err := exchangeRemaining(ctx, q, sellerID, orderID)
	if err != nil {
		return err
	}
	for _, l := range lines {
		r, ok := remaining[l.VariantID]
		if !ok {
			return fmt.Errorf("%w: biến thể không có trong đơn", ErrInvalidInput)
		}
		if l.Qty > r {
			return ErrQtyExceeded
		}
	}
	return nil
}

// exchangeRemaining trả số còn đổi, trả được của từng biến thể trong đơn orderID: số đã mua − số đang nằm trong yêu
// cầu đổi, trả chưa bị từ chối hay rút (có thể âm nếu dữ liệu cũ lệch). Biến thể không có trong đơn không có khóa.
func exchangeRemaining(ctx context.Context, q *store.Queries, sellerID, orderID uuid.UUID) (map[uuid.UUID]int, error) {
	items, err := q.OrderItemQuantities(ctx, store.OrderItemQuantitiesParams{SellerID: sellerID, OrderID: orderID})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng đơn: %w", err)
	}
	remaining := map[uuid.UUID]int{}
	for _, it := range items {
		remaining[it.VariantID] = int(it.Quantity)
	}
	held, err := q.HeldRequestLines(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("đọc yêu cầu đổi đang mở: %w", err)
	}
	for _, raw := range held {
		var ls []RequestLine
		if err := json.Unmarshal(raw, &ls); err != nil {
			return nil, fmt.Errorf("đọc dòng yêu cầu đang mở: %w", err)
		}
		for _, l := range ls {
			if _, ok := remaining[l.VariantID]; ok {
				remaining[l.VariantID] -= l.Qty
			}
		}
	}
	return remaining, nil
}

// checkRequestFiles: mỗi tệp thuộc tenant, đúng mục đích, chưa xóa, chưa gắn yêu cầu nào, không bị imager từ chối.
func checkRequestFiles(ctx context.Context, tx pgx.Tx, q *store.Queries, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	attached, err := q.AttachedFiles(ctx, ids)
	if err != nil {
		return fmt.Errorf("đọc ảnh đã gắn: %w", err)
	}
	if len(attached) != 0 {
		return ErrFileNotFound
	}
	for _, id := range ids {
		st, err := files.Status(ctx, tx, id)
		if errors.Is(err, files.ErrNotFound) {
			return ErrFileNotFound
		}
		if err != nil {
			return fmt.Errorf("đọc trạng thái ảnh: %w", err)
		}
		switch {
		case st.Purpose != RequestPhotoPurposeKey || st.Deleted:
			return ErrFileNotFound
		case st.Status == files.StatusRejected:
			return ErrFileRejected
		case st.Status != files.StatusUploaded && st.Status != files.StatusProcessing && st.Status != files.StatusReady:
			return ErrFileNotFound
		}
	}
	return nil
}

// lockWebRequest khóa yêu cầu ref của đơn web externalRef (người bán sellerID). Không có → ErrRequestNotFound.
func lockWebRequest(ctx context.Context, q *store.Queries, sellerID uuid.UUID, externalRef, ref string,
) (store.LockOrderRequestByRefRow, error) {
	oid, err := q.WebOrderIDByRef(ctx, store.WebOrderIDByRefParams{SellerID: sellerID, ExternalRef: strings.TrimSpace(externalRef)})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.LockOrderRequestByRefRow{}, ErrRequestNotFound
	}
	if err != nil {
		return store.LockOrderRequestByRefRow{}, fmt.Errorf("đọc đơn web của yêu cầu: %w", err)
	}
	r, err := q.LockOrderRequestByRef(ctx, store.LockOrderRequestByRefParams{OrderID: oid, Ref: strings.TrimSpace(ref)})
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrRequestNotFound
	}
	if err != nil {
		return r, fmt.Errorf("khóa yêu cầu: %w", err)
	}
	return r, nil
}

// CustomerReplyRequest ghi trả lời của khách (5–1000 ký tự) cho yêu cầu ref đang needs_info (khác →
// ErrRequestNotAllowed) và đưa về in_review, version + 1, báo hiệu. Trả trước, sau để bên gọi ghi audit.
func (w Writer) CustomerReplyRequest(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, externalRef, ref, body string,
	a Actor, now time.Time,
) (RequestChange, error) {
	body, err := cleanMessage(body)
	if err != nil {
		return RequestChange{}, err
	}
	q := store.New(tx)
	r, err := lockWebRequest(ctx, q, sellerID, externalRef, ref)
	if err != nil {
		return RequestChange{}, err
	}
	if r.Status != RequestNeedsInfo {
		return RequestChange{}, ErrRequestNotAllowed
	}
	now = now.UTC()
	if _, err := insertMessage(ctx, q, a.TenantID, r.ID, AuthorCustomer, a.UserID.String(), body, true, now); err != nil {
		return RequestChange{}, err
	}
	return w.moveRequest(ctx, q, tx, a.TenantID, Request{
		ID: r.ID, OrderID: r.OrderID, Ref: r.Ref, Kind: r.Kind, Status: r.Status, Version: int(r.Version),
	}, RequestInReview, store.UpdateOrderRequestStatusParams{}, AuthorCustomer, a.UserID.String(), now)
}

// CustomerCancelRequest cho khách rút yêu cầu ref khi nhân viên chưa bắt đầu (received → cancelled). Đã cancelled → trả
// như cũ (gửi lại); trạng thái khác → ErrRequestNotAllowed.
func (w Writer) CustomerCancelRequest(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, externalRef, ref string,
	a Actor, now time.Time,
) (RequestChange, error) {
	q := store.New(tx)
	r, err := lockWebRequest(ctx, q, sellerID, externalRef, ref)
	if err != nil {
		return RequestChange{}, err
	}
	cur := Request{ID: r.ID, OrderID: r.OrderID, Ref: r.Ref, Kind: r.Kind, Status: r.Status, Version: int(r.Version)}
	switch r.Status {
	case RequestCancelled:
		return RequestChange{Before: cur, After: cur}, nil
	case RequestReceived:
	default:
		return RequestChange{}, ErrRequestNotAllowed
	}
	now = now.UTC()
	return w.moveRequest(ctx, q, tx, a.TenantID, cur, RequestCancelled, store.UpdateOrderRequestStatusParams{
		ResolvedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}, AuthorCustomer, a.UserID.String(), now)
}

// moveRequest ghi trạng thái to (p mang ship_back, outcome, resolved_*) cho yêu cầu cur đã khóa, version + 1, một dòng
// lịch sử (người làm actorType AuthorStaff/AuthorCustomer, mã actorID), báo hiệu.
func (w Writer) moveRequest(ctx context.Context, q *store.Queries, tx pgx.Tx, tenant uuid.UUID, cur Request, to string,
	p store.UpdateOrderRequestStatusParams, actorType, actorID string, now time.Time,
) (RequestChange, error) {
	p.Status, p.Now, p.ID, p.Version = to, now, cur.ID, int32(cur.Version) //nolint:gosec // version int4 của bảng
	v, err := q.UpdateOrderRequestStatus(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestChange{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng
	}
	if err != nil {
		return RequestChange{}, fmt.Errorf("đổi trạng thái yêu cầu: %w", err)
	}
	if err := q.InsertOrderRequestEvent(ctx, store.InsertOrderRequestEventParams{
		TenantID: tenant, RequestID: cur.ID, FromStatus: cur.Status, ToStatus: to, ActorType: actorType,
		ActorID: pgtype.Text{String: actorID, Valid: actorID != ""}, Now: now,
	}); err != nil {
		return RequestChange{}, fmt.Errorf("ghi lịch sử yêu cầu: %w", err)
	}
	after := cur
	after.Status, after.Version = to, int(v)
	if err := w.publishRequest(ctx, tx, tenant, cur.ID); err != nil {
		return RequestChange{}, err
	}
	return RequestChange{Before: cur, After: after}, nil
}

// ListWebRequests trả các yêu cầu (mới trước) của đơn kênh web externalRef cho website: chỉ tin hiện cho khách. Không
// có đơn → ErrNotFound.
func ListWebRequests(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, externalRef string) ([]RequestDetail, error) {
	q := store.New(tx)
	oid, err := q.WebOrderIDByRef(ctx, store.WebOrderIDByRefParams{SellerID: sellerID, ExternalRef: strings.TrimSpace(externalRef)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc đơn web: %w", err)
	}
	ids, err := q.ListOrderRequestsOfOrder(ctx, oid)
	if err != nil {
		return nil, fmt.Errorf("đọc yêu cầu của đơn: %w", err)
	}
	out := make([]RequestDetail, 0, len(ids))
	for _, id := range ids {
		d, err := getRequest(ctx, tx, sellerID, id, false)
		if err != nil {
			return nil, err
		}
		d.Messages = slices.DeleteFunc(d.Messages, func(m RequestMessage) bool { return !m.VisibleToCustomer })
		out = append(out, d)
	}
	return out, nil
}
