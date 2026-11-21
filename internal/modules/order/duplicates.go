package order

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Đơn trùng (kế hoạch M2, Task M2-23; Q7, DH09; 04 mục 18: "đánh dấu nghi trùng, không tự hủy"). Hai đơn nghi trùng khi
// cùng người bán, không đơn nào cancelled, tạo cách nhau không quá cửa sổ cài đặt (duplicate_window_minutes; 0 =
// tắt), cùng hồ sơ gốc của khách hoặc cùng SĐT giao (khác rỗng), và có ít nhất một biến thể chung. Không tự gộp dòng.
// Thứ tự khóa (ADR-46 nhóm 7): hai đơn theo id tăng dần → order_duplicate_links → (khi hủy) như Cancel.

// errDuplicatesNoClock: DuplicatesConsumer dựng không có đồng hồ (lỗi nối dây, không phải dữ liệu).
var errDuplicatesNoClock = errors.New("order: DuplicatesConsumer thiếu đồng hồ Now")

// DuplicatesConsumerName là tên consumer phát hiện đơn trùng (việc event.order_duplicates); đổi tên làm mất tiến độ.
const DuplicatesConsumerName = "order_duplicates"

// topicOrderCancelled là sự kiện order.<trạng thái> khi đơn sang cancelled (applyTransition phát; docs/views.md).
const topicOrderCancelled = "order." + StatusCancelled

// Cách khớp của một cặp (CHECK ord.order_duplicate_links.matched_by).
const (
	MatchedByCustomer = "customer"
	MatchedByPhone    = "phone"
)

// Hành động xử lý một cặp (ResolveOrderDuplicateRequest.action).
const (
	DuplicateNotDuplicate = "not_duplicate"
	DuplicateCancelOther  = "cancel_other"
	DuplicateCancelThis   = "cancel_this"
)

// Kết luận ghi ở cặp (CHECK ord.order_duplicate_links.resolution).
const (
	resolutionNotDuplicate = "not_duplicate"
	resolutionCancelled    = "cancelled"
)

const (
	defaultDuplicatePage = 50
	maxDuplicatePage     = 200
	maxCheckVariants     = 100
	// maxDuplicateCursorLen: 20 chữ số + 2 dấu chấm + 2 × 36 ký tự uuid.
	maxDuplicateCursorLen = 20 + 2 + 2*36
)

// Summary là tóm tắt một đơn cho danh sách (OrderListItem của hợp đồng): CustomerID là gốc nhóm khách,
// CustomerName tên của gốc; ShipPhone SĐT giao đã chuẩn hóa (rỗng khi không chuẩn hóa được); CareBy người phụ trách
// (nil khi chưa có; tên do bên gọi đọc).
type Summary struct {
	ID, CustomerID              uuid.UUID
	Code, Status, PendingReason string
	Label                       orderview.LabelKey
	CustomerName                string
	ShipPhone                   logging.Phone
	GrandTotal, CODAmount       int64
	ItemCount                   int32
	Channel                     string
	TrafficSourceID             *uuid.UUID
	CareBy                      *uuid.UUID
	CreatedAt                   time.Time
	DuplicateSuspect            bool
}

// Duplicate là một cặp nghi trùng: Order, OtherOrder (theo chiều bên gọi hỏi; ListDuplicates: đơn có mã nhỏ hơn
// trước), cách khớp, lúc phát hiện, kết luận (nil khi chưa xử lý).
type Duplicate struct {
	Order, OtherOrder Summary
	MatchedBy         string
	CreatedAt         time.Time
	Resolution        *string
	ResolvedAt        *time.Time
}

// DuplicateCursor là vị trí keyset của ListDuplicates (cặp cuối của trang trước).
type DuplicateCursor struct {
	At                    time.Time
	OrderID, OtherOrderID uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ: base64url của "<micro giây Unix>.<order_id>.<other_order_id>".
func (c DuplicateCursor) String() string {
	plain := strconv.FormatInt(c.At.UnixMicro(), 10) + "." + c.OrderID.String() + "." + c.OtherOrderID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(plain))
}

// ParseDuplicateCursor đọc chuỗi do DuplicateCursor.String tạo. Hỏng → ErrInvalidInput.
func ParseDuplicateCursor(s string) (DuplicateCursor, error) {
	bad := fmt.Errorf("%w: con trỏ danh sách đơn trùng", ErrInvalidInput)
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) > maxDuplicateCursorLen {
		return DuplicateCursor{}, bad
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return DuplicateCursor{}, bad
	}
	micros, errAt := strconv.ParseInt(parts[0], 10, 64)
	lo, errLo := uuid.Parse(parts[1])
	hi, errHi := uuid.Parse(parts[2])
	if errAt != nil || errLo != nil || errHi != nil {
		return DuplicateCursor{}, bad
	}
	return DuplicateCursor{At: time.UnixMicro(micros).UTC(), OrderID: lo, OtherOrderID: hi}, nil
}

// DuplicatesConsumer phát hiện đơn trùng sau commit (events.Consumer; module.go đăng ký): với đơn X của sự kiện
// order.created, order.updated (bỏ qua khi cửa sổ 0), mỗi đơn O khớp (luật ở đầu tệp) mà cặp (X, O)
// chưa có dòng: khóa hai đơn theo id tăng dần, kiểm lại cả hai chưa cancelled, chèn dòng (ON CONFLICT DO NOTHING) và
// chỉ khi chèn được thì bật duplicate_suspect của hai đơn (không tăng version, không phát order.updated nên không
// lặp). Cặp đã có dòng (mở hay đã xử lý) không đụng lại. Đơn X đã cancelled (sự kiện order.cancelled của mọi đường hủy:
// hủy tay, nháp hết hạn, hãng xác nhận hủy, xử lý đơn trùng; review M2 B-04) → gỡ cờ của X và của các đơn ghép cặp với
// X không còn cặp mở (clearCancelledPairs). Chạy lại được; payload hỏng → queue.ErrPermanent. Now là đồng hồ
// cho mốc của cặp (bắt buộc; Background giao background.Deps.Clock): nil → lỗi, không ghi gì.
type DuplicatesConsumer struct {
	Now func() time.Time
}

// Name trả DuplicatesConsumerName.
func (DuplicatesConsumer) Name() string { return DuplicatesConsumerName }

// Topics trả order.created, order.updated, order.cancelled.
func (DuplicatesConsumer) Topics() []string {
	return []string{TopicOrderCreated, TopicOrderUpdated, topicOrderCancelled}
}

// Handle tìm và ghi cặp nghi trùng của đơn trong sự kiện e, trong tx của worker.
func (c DuplicatesConsumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	if c.Now == nil {
		return errDuplicatesNoClock
	}
	var p Changed
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.OrderID == uuid.Nil || p.SellerID == uuid.Nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	x, err := q.DuplicateSource(ctx, store.DuplicateSourceParams{SellerID: p.SellerID, ID: p.OrderID})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("đơn %s không có: %w", p.OrderID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("đọc đơn của sự kiện: %w", err)
	}
	if x.Status == StatusCancelled {
		return clearCancelledPairs(ctx, q, p.SellerID, x.ID)
	}
	window, err := duplicateWindow(ctx, tx)
	if err != nil || window == 0 {
		return err
	}
	variants, err := q.OrderVariantIDs(ctx, store.OrderVariantIDsParams{SellerID: p.SellerID, OrderID: x.ID})
	if err != nil {
		return fmt.Errorf("đọc biến thể của đơn: %w", err)
	}
	if len(variants) == 0 {
		return nil
	}
	found, err := q.FindDuplicateCandidates(ctx, store.FindDuplicateCandidatesParams{
		RootID: pgtype.UUID{Bytes: x.RootID, Valid: true}, SellerID: p.SellerID, ExcludeID: x.ID,
		FromAt: x.CreatedAt.Add(-window), ToAt: x.CreatedAt.Add(window), ShipPhone: x.ShipPhone, VariantIds: variants,
	})
	if err != nil {
		return fmt.Errorf("tìm đơn trùng: %w", err)
	}
	return linkAll(ctx, q, p.SellerID, x.ID, found, e, c.Now().UTC())
}

// linkAll ghi các cặp (x, ứng viên) chưa có: khóa một lần x và mọi ứng viên theo id tăng dần (ADR-46: không khóa
// ngược chiều khi x nằm giữa các ứng viên), bỏ cặp có đơn vừa bị hủy, chèn dòng; dòng chèn được thì bật cờ hai đơn.
func linkAll(ctx context.Context, q *store.Queries, sellerID, x uuid.UUID, found []store.FindDuplicateCandidatesRow,
	e events.StoredEvent, now time.Time,
) error {
	ids := []uuid.UUID{x}
	for _, o := range found {
		ids = append(ids, o.ID)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	cancelled := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		o, err := lockOrder(ctx, q, sellerID, id)
		if err != nil {
			return fmt.Errorf("khóa đơn của cặp nghi trùng: %w", err)
		}
		cancelled[id] = o.Status == StatusCancelled
	}
	if cancelled[x] {
		return nil
	}
	for _, o := range found {
		if cancelled[o.ID] {
			continue
		}
		lo, hi := orderedPair(x, o.ID)
		n, err := q.InsertDuplicateLink(ctx, store.InsertDuplicateLinkParams{
			OrderID: lo, OtherOrderID: hi, MatchedBy: o.MatchedBy, Now: now,
		})
		if err != nil {
			return fmt.Errorf("ghi cặp nghi trùng (sự kiện %s): %w", e.ID, err)
		}
		if n == 0 {
			continue
		}
		if err := q.SetDuplicateSuspect(ctx, store.SetDuplicateSuspectParams{
			SellerID: sellerID, Suspect: true, Ids: []uuid.UUID{lo, hi},
		}); err != nil {
			return fmt.Errorf("đánh dấu nghi trùng: %w", err)
		}
	}
	return nil
}

// clearCancelledPairs gỡ cờ nghi trùng của đơn cancelled x và của mọi đơn ghép cặp với x không còn cặp mở: khóa các
// đơn còn cờ (x và đơn ghép cặp) theo id tăng dần (ADR-46 nhóm 7, như linkAll; không còn cặp mới nào với x vì linkAll bỏ
// đơn cancelled dưới khóa), rồi clearSuspectIfClosed từng đơn.
func clearCancelledPairs(ctx context.Context, q *store.Queries, sellerID, x uuid.UUID) error {
	ids, err := q.SuspectPairedOrders(ctx, store.SuspectPairedOrdersParams{SellerID: sellerID, ID: x})
	if err != nil {
		return fmt.Errorf("đọc đơn còn cờ ghép cặp với đơn hủy: %w", err)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	for _, id := range ids {
		if _, err := lockOrder(ctx, q, sellerID, id); err != nil {
			return fmt.Errorf("khóa đơn để gỡ cờ nghi trùng: %w", err)
		}
	}
	for _, id := range ids {
		if err := clearSuspectIfClosed(ctx, q, sellerID, id); err != nil {
			return err
		}
	}
	return nil
}

// CheckDuplicates (chỉ đọc; cảnh báo ở form trước khi tạo) trả các đơn chưa hủy của người bán sellerID tạo trong cửa
// sổ cài đặt tính tới now, cùng nhóm khách của customerID (hồ sơ gốc hay con) hoặc cùng SĐT giao phone (chuẩn hóa như
// cột ship_phone; không chuẩn hóa được thì bỏ qua), có ít nhất một biến thể trong variantIDs; mới nhất trước, tối đa 100.
// Cửa sổ 0, không khách và không SĐT → rỗng. variantIDs rỗng hay quá 100 → ErrInvalidInput; khách không có →
// ErrNotFound.
func CheckDuplicates(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, customerID *uuid.UUID, phone string,
	variantIDs []uuid.UUID, now time.Time,
) ([]Summary, error) {
	if len(variantIDs) == 0 || len(variantIDs) > maxCheckVariants {
		return nil, fmt.Errorf("%w: cần 1–%d biến thể", ErrInvalidInput, maxCheckVariants)
	}
	q := store.New(tx)
	root := pgtype.UUID{}
	if customerID != nil {
		r, err := q.CustomerRoot(ctx, *customerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: khách của kiểm đơn trùng", ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("đọc gốc khách: %w", err)
		}
		root = pgtype.UUID{Bytes: r, Valid: true}
	}
	norm, _ := normalizedShipPhone(logging.Phone(phone))
	if !root.Valid && norm == "" {
		return []Summary{}, nil
	}
	window, err := duplicateWindow(ctx, tx)
	if err != nil || window == 0 {
		return []Summary{}, err
	}
	now = now.UTC()
	found, err := q.FindDuplicateCandidates(ctx, store.FindDuplicateCandidatesParams{
		RootID: root, SellerID: sellerID, ExcludeID: uuid.Nil, FromAt: now.Add(-window), ToAt: now,
		ShipPhone: string(norm), VariantIds: variantIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("kiểm đơn trùng: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(found))
	for _, f := range found {
		ids = append(ids, f.ID)
	}
	byID, err := summaries(ctx, q, sellerID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(ids))
	for _, id := range ids {
		if s, ok := byID[id]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// ListDuplicates trả một trang cặp nghi trùng chưa xử lý của người bán sellerID mà hai đơn đều chưa hủy, mới phát hiện
// trước, keyset (after nil: trang đầu), và con trỏ trang sau (nil khi hết). limit ≤ 0: 50; tối đa 200.
func ListDuplicates(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, after *DuplicateCursor, limit int,
) ([]Duplicate, *DuplicateCursor, error) {
	if limit <= 0 {
		limit = defaultDuplicatePage
	}
	limit = min(limit, maxDuplicatePage)
	p := store.ListOpenDuplicateLinksParams{SellerID: sellerID, MaxRows: int32(limit + 1)}
	if after != nil {
		p.AfterAt = pgtype.Timestamptz{Time: after.At, Valid: true}
		p.AfterOrderID, p.AfterOtherOrderID = after.OrderID, after.OtherOrderID
	}
	q := store.New(tx)
	rows, err := q.ListOpenDuplicateLinks(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc cặp nghi trùng: %w", err)
	}
	var next *DuplicateCursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		next = &DuplicateCursor{At: last.CreatedAt, OrderID: last.OrderID, OtherOrderID: last.OtherOrderID}
	}
	ids := make([]uuid.UUID, 0, 2*len(rows))
	for _, r := range rows {
		ids = append(ids, r.OrderID, r.OtherOrderID)
	}
	byID, err := summaries(ctx, q, sellerID, ids)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Duplicate, 0, len(rows))
	for _, r := range rows {
		out = append(out, Duplicate{
			Order: byID[r.OrderID], OtherOrder: byID[r.OtherOrderID], MatchedBy: r.MatchedBy, CreatedAt: r.CreatedAt,
			Resolution: textPtr(r.Resolution), ResolvedAt: timePtr(r.ResolvedAt),
		})
	}
	return out, next, nil
}

// GetDuplicate trả cặp (orderID, otherOrderID) của người bán sellerID theo chiều đó (mọi kết luận). Không có cặp →
// ErrNotFound.
func GetDuplicate(ctx context.Context, tx pgx.Tx, sellerID, orderID, otherOrderID uuid.UUID) (Duplicate, error) {
	lo, hi := orderedPair(orderID, otherOrderID)
	q := store.New(tx)
	r, err := q.GetDuplicateLink(ctx, store.GetDuplicateLinkParams{SellerID: sellerID, OrderID: lo, OtherOrderID: hi})
	if errors.Is(err, pgx.ErrNoRows) {
		return Duplicate{}, fmt.Errorf("%w: cặp nghi trùng", ErrNotFound)
	}
	if err != nil {
		return Duplicate{}, fmt.Errorf("đọc cặp nghi trùng: %w", err)
	}
	byID, err := summaries(ctx, q, sellerID, []uuid.UUID{orderID, otherOrderID})
	if err != nil {
		return Duplicate{}, err
	}
	return Duplicate{
		Order: byID[orderID], OtherOrder: byID[otherOrderID], MatchedBy: r.MatchedBy, CreatedAt: r.CreatedAt,
		Resolution: textPtr(r.Resolution), ResolvedAt: timePtr(r.ResolvedAt),
	}, nil
}

// ResolveDuplicate xử lý cặp (orderID, otherOrderID) của người bán sellerID trong tx của bên gọi (không bao giờ tự
// hủy; nhân viên chọn):
//
//   - not_duplicate: ghi kết luận not_duplicate (version không dùng).
//   - cancel_other / cancel_this: Cancel đơn otherOrderID / orderID ở version (version của đơn bị hủy mà nhân viên đã
//     xem, bắt buộc; khác version hiện tại → ErrVersionConflict, không đổi gì) với lý do duplicate, như hủy tay: nhả
//     hàng, trả lượt mã, rồi ghi kết luận cancelled. Đơn đã gửi hãng thì Cancel chỉ đưa sang cancel_requested: cặp giữ
//     mở, giữ cờ (review M2 B-03); hãng xác nhận hủy thì cặp rời danh sách và DuplicatesConsumer gỡ cờ, hãng từ chối
//     thì đơn về confirmed và cặp vẫn chờ nhân viên.
//
// Sau khi ghi kết luận, đơn nào của cặp không còn cặp mở (chưa xử lý, đơn kia chưa hủy) thì gỡ duplicate_suspect. Khóa
// hai đơn theo id tăng dần rồi dòng cặp. Hành động lạ, hai mã trùng, hủy mà thiếu version → ErrInvalidInput; không có
// cặp (hay đơn của người bán khác) → ErrNotFound; cặp đã xử lý → ErrInvalidTransition; lỗi của Cancel trả nguyên.
// Audit do bên gọi ghi.
func (w Writer) ResolveDuplicate(ctx context.Context, tx pgx.Tx, sellerID, orderID, otherOrderID uuid.UUID,
	action string, version *int64, a Actor, now time.Time,
) error {
	if err := w.wired(); err != nil {
		return err
	}
	if orderID == otherOrderID ||
		!slices.Contains([]string{DuplicateNotDuplicate, DuplicateCancelOther, DuplicateCancelThis}, action) {
		return fmt.Errorf("%w: hành động xử lý đơn trùng", ErrInvalidInput)
	}
	if action != DuplicateNotDuplicate && version == nil {
		return fmt.Errorf("%w: hủy đơn trùng cần version của đơn bị hủy", ErrInvalidInput)
	}
	now = now.UTC()
	q := store.New(tx)
	lo, hi := orderedPair(orderID, otherOrderID)
	if err := lockOpenPair(ctx, q, sellerID, lo, hi); err != nil {
		return err
	}
	resolution := resolutionNotDuplicate
	if action != DuplicateNotDuplicate {
		target := otherOrderID
		if action == DuplicateCancelThis {
			target = orderID
		}
		got, err := w.Cancel(ctx, tx, sellerID, target, *version, CancelDuplicate, "", a, now)
		if err != nil {
			return err
		}
		if got.Status != StatusCancelled { // cancel_requested: chờ hãng, cặp giữ mở
			return nil
		}
		resolution = resolutionCancelled
	}
	n, err := q.ResolveDuplicateLink(ctx, store.ResolveDuplicateLinkParams{
		Resolution: resolution, Now: now, ResolvedBy: a.id(), OrderID: lo, OtherOrderID: hi,
	})
	if err != nil {
		return fmt.Errorf("ghi kết luận cặp nghi trùng: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("%w: ghi kết luận cặp nghi trùng không đúng một dòng", ErrStatusConflict)
	}
	for _, id := range []uuid.UUID{lo, hi} {
		if err := clearSuspectIfClosed(ctx, q, sellerID, id); err != nil {
			return err
		}
	}
	return nil
}

// lockOpenPair khóa hai đơn lo < hi của người bán (theo id tăng dần) rồi dòng cặp. Đơn không có → ErrNotFound; không
// có cặp → ErrNotFound; cặp đã xử lý → ErrInvalidTransition.
func lockOpenPair(ctx context.Context, q *store.Queries, sellerID, lo, hi uuid.UUID) error {
	for _, id := range []uuid.UUID{lo, hi} {
		if _, err := lockOrder(ctx, q, sellerID, id); err != nil {
			return err
		}
	}
	link, err := q.LockDuplicateLink(ctx, store.LockDuplicateLinkParams{OrderID: lo, OtherOrderID: hi})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: cặp nghi trùng", ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("khóa cặp nghi trùng: %w", err)
	}
	if link.Resolution.Valid {
		return fmt.Errorf("%w: cặp nghi trùng đã xử lý", ErrInvalidTransition)
	}
	return nil
}

// clearSuspectIfClosed gỡ duplicate_suspect của đơn id (đã khóa) khi đơn không còn cặp mở nào.
func clearSuspectIfClosed(ctx context.Context, q *store.Queries, sellerID, id uuid.UUID) error {
	open, err := q.HasOpenDuplicateLink(ctx, store.HasOpenDuplicateLinkParams{ID: id, SellerID: sellerID})
	if err != nil {
		return fmt.Errorf("đọc cặp mở của đơn: %w", err)
	}
	if open {
		return nil
	}
	if err := q.SetDuplicateSuspect(ctx, store.SetDuplicateSuspectParams{SellerID: sellerID, Suspect: false, Ids: []uuid.UUID{id}}); err != nil {
		return fmt.Errorf("gỡ cờ nghi trùng: %w", err)
	}
	return nil
}

// duplicateWindow là cửa sổ đơn trùng của tenant (0: tắt).
func duplicateWindow(ctx context.Context, tx pgx.Tx) (time.Duration, error) {
	s, err := LoadSettings(ctx, tx)
	if err != nil {
		return 0, err
	}
	return s.DuplicateWindow, nil
}

// orderedPair trả hai mã theo thứ tự tăng dần (dòng cặp: order_id < other_order_id; thứ tự khóa).
func orderedPair(a, b uuid.UUID) (lo, hi uuid.UUID) {
	if slices.Compare(a[:], b[:]) > 0 {
		return b, a
	}
	return a, b
}

// summaries đọc tóm tắt các đơn ids của người bán sellerID theo mã.
func summaries(ctx context.Context, q *store.Queries, sellerID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Summary, error) {
	out := make(map[uuid.UUID]Summary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.OrderSummaries(ctx, store.OrderSummariesParams{SellerID: sellerID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("đọc tóm tắt đơn: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = Summary{
			ID: r.ID, CustomerID: r.RootID, Code: r.Code, Status: r.Status, PendingReason: r.PendingReason,
			Label: orderview.Label(orderview.Axes{
				Status: r.Status, PendingReason: r.PendingReason, PackingStatus: r.PackingStatus,
			}),
			CustomerName: r.RootName, ShipPhone: logging.Phone(r.ShipPhone), GrandTotal: r.GrandTotal,
			CODAmount: r.CodAmount, ItemCount: r.ItemCount, Channel: r.Channel, TrafficSourceID: uuidPtr(r.TrafficSourceID),
			CareBy: uuidPtr(r.CareBy), CreatedAt: r.CreatedAt, DuplicateSuspect: r.DuplicateSuspect,
		}
	}
	return out, nil
}
