package cash

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/cash/internal/refunds"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Nghĩa vụ hoàn tiền cho khách (kế hoạch M3, Task M3-30; Q-2, TI09/AT17): consumer RefundsConsumerName mở nghĩa vụ
// khi đơn có tiền đã thu bị hủy hay kho nhận phiếu khách trả; nghĩa vụ chỉ đóng khi đã chi đủ (PayRefund) hoặc người
// có quyền cash.refunds.waive miễn kèm lý do (WaiveRefund). Không mã hóa chính sách mất cọc nào (Q07 còn mở).

// Nguồn và trạng thái của nghĩa vụ (CHECK cash.refund_obligations).
const (
	RefundOrderCancel     = refunds.SourceOrderCancel
	RefundCustomerReturn  = refunds.SourceCustomerReturn
	RefundExchange        = "exchange"
	RefundPartialDelivery = "partial_delivery"

	RefundOpen   = "open"
	RefundPaid   = "paid"
	RefundWaived = "waived"
)

// RefundsConsumerName là tên consumer mở nghĩa vụ hoàn (việc event.cash_refunds); đổi tên làm mất tiến độ.
const RefundsConsumerName = refunds.ConsumerName

const maxWaiveReasonLen = 1000

// Lỗi của nghĩa vụ hoàn.
var (
	// ErrObligationNotFound: nghĩa vụ không có hay của tenant khác.
	ErrObligationNotFound = errors.New("cash: không có nghĩa vụ hoàn")
	// ErrObligationClosed: nghĩa vụ đã chi đủ hay đã miễn.
	ErrObligationClosed = errors.New("cash: nghĩa vụ hoàn đã đóng")
	// ErrRefundExceedsRemaining: số chi vượt số còn phải hoàn.
	ErrRefundExceedsRemaining = errors.New("cash: số chi vượt số còn phải hoàn")
	// ErrReasonRequired: miễn hoàn không có lý do.
	ErrReasonRequired = errors.New("cash: miễn hoàn cần lý do")
)

// RefundObligation là một nghĩa vụ hoàn tiền kèm mã đơn.
type RefundObligation struct {
	ID, OrderID        uuid.UUID
	OrderCode          string
	ReturnID           *uuid.UUID
	Source, Status     string
	Amount, PaidAmount int64
	WaiveReason        string
	CreatedAt          time.Time
	Version            int64
}

// lockObligation khóa đơn của nghĩa vụ id (nhóm 7) rồi nghĩa vụ (nhóm 9) và trả nghĩa vụ dưới khóa. Không có →
// ErrObligationNotFound.
func lockObligation(ctx context.Context, tx pgx.Tx, id uuid.UUID) (store.CashRefundObligation, error) {
	q := store.New(tx)
	head, err := q.RefundObligationOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.CashRefundObligation{}, ErrObligationNotFound
	}
	if err != nil {
		return store.CashRefundObligation{}, fmt.Errorf("đọc nghĩa vụ hoàn: %w", err)
	}
	if _, err := lockPaymentOrder(ctx, tx, head.OrderID); err != nil {
		return store.CashRefundObligation{}, err
	}
	ob, err := q.LockRefundObligation(ctx, id)
	if err != nil {
		return store.CashRefundObligation{}, fmt.Errorf("khóa nghĩa vụ hoàn: %w", err)
	}
	return ob, nil
}

// PayRefund ghi một lần chi amount (> 0) cho nghĩa vụ obligationID trong tx (WriteTx của tenantID), người chi a: khóa
// đơn (nhóm 7) rồi nghĩa vụ (nhóm 9); nghĩa vụ không open → ErrObligationClosed; vượt số còn phải hoàn →
// ErrRefundExceedsRemaining (câu UPDATE cũng có điều kiện paid_amount + amount ≤ amount, status open); chi đủ → paid;
// chèn phiếu refund số âm gắn nghĩa vụ. Nghĩa vụ order_cancel (đơn đã hủy): tổng phiếu của đơn đẩy về đơn
// (ApplyPaymentTotals, max(tổng, 0)) và khi chi đủ mà tổng về 0 thì đơn refunded (MarkRefunded). Nguồn khác (đơn đã
// giao): tiền của đơn giữ nguyên (Quyết định 1 của Task M3-30). Đầu vào sai → ErrInvalidInput; không có →
// ErrObligationNotFound. Trả nghĩa vụ sau khi chi.
func (s *Service) PayRefund(ctx context.Context, tx pgx.Tx, tenantID, obligationID uuid.UUID, amount int64, reference string,
	a Actor,
) (RefundObligation, error) {
	switch {
	case amount <= 0 || amount > maxMoney:
		return RefundObligation{}, fmt.Errorf("%w: số chi %d", ErrInvalidInput, amount)
	case utf8.RuneCountInString(reference) > maxReferenceLen:
		return RefundObligation{}, fmt.Errorf("%w: mã tham chiếu quá %d ký tự", ErrInvalidInput, maxReferenceLen)
	}
	ob, err := lockObligation(ctx, tx, obligationID)
	if err != nil {
		return RefundObligation{}, err
	}
	switch {
	case ob.Status != RefundOpen:
		return RefundObligation{}, fmt.Errorf("%w: %s", ErrObligationClosed, ob.Status)
	case ob.PaidAmount+amount > ob.Amount:
		return RefundObligation{}, fmt.Errorf("%w: còn %d, chi %d", ErrRefundExceedsRemaining, ob.Amount-ob.PaidAmount, amount)
	}
	q, now := store.New(tx), s.d.Now().UTC()
	paid, err := q.PayRefundObligation(ctx, store.PayRefundObligationParams{Pay: amount, Now: now, ID: obligationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return RefundObligation{}, ErrRefundExceedsRemaining
	}
	if err != nil {
		return RefundObligation{}, fmt.Errorf("ghi số đã chi của nghĩa vụ: %w", err)
	}
	if _, err := q.InsertPayment(ctx, store.InsertPaymentParams{
		OrderID: ob.OrderID, Method: MethodRefund, Amount: -amount, Reference: reference, ReceivedAt: now,
		RefundObligationID: pgtype.UUID{Bytes: obligationID, Valid: true}, ActorID: a.ID,
	}); err != nil {
		return RefundObligation{}, fmt.Errorf("ghi phiếu chi hoàn: %w", err)
	}
	if ob.Source == RefundOrderCancel {
		if err := s.settleCancelled(ctx, tx, tenantID, ob.OrderID, paid.Status == RefundPaid, a, now); err != nil {
			return RefundObligation{}, err
		}
	}
	return getObligation(ctx, q, obligationID)
}

// settleCancelled đẩy tổng phiếu của đơn đã hủy orderID về đơn; done (nghĩa vụ hủy vừa chi đủ) và tổng ≤ 0 → đơn
// refunded.
func (s *Service) settleCancelled(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, done bool, a Actor,
	now time.Time,
) error {
	total, err := store.New(tx).PaymentsTotal(ctx, orderID)
	if err != nil {
		return fmt.Errorf("đọc tổng phiếu của đơn: %w", err)
	}
	snap, err := lockPaymentOrder(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if err := s.applyTotals(ctx, tx, tenantID, snap, max(total, 0), a, now); err != nil {
		return err
	}
	if !done || total > 0 {
		return nil
	}
	if err := s.orderWriter().MarkRefunded(ctx, tx, snap.SellerID, orderID, orderActor(tenantID, a), now); err != nil {
		return fmt.Errorf("ghi đơn đã hoàn tiền: %w", err)
	}
	return nil
}

// WaiveRefund đóng nghĩa vụ obligationID là "không hoàn" (waived) kèm lý do reason (bắt buộc, tối đa 1000 ký tự; bỏ
// khoảng trắng đầu cuối) trong tx (WriteTx của tenantID); quyền cash.refunds.waive kiểm ở API, audit do bên gọi ghi.
// Khóa đơn rồi nghĩa vụ; nghĩa vụ không open → ErrObligationClosed. Tiền của đơn giữ nguyên. Lý do rỗng →
// ErrReasonRequired; dài quá → ErrInvalidInput; không có → ErrObligationNotFound.
func (s *Service) WaiveRefund(ctx context.Context, tx pgx.Tx, _ uuid.UUID, obligationID uuid.UUID, reason string, _ Actor,
) (RefundObligation, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return RefundObligation{}, ErrReasonRequired
	case utf8.RuneCountInString(reason) > maxWaiveReasonLen:
		return RefundObligation{}, fmt.Errorf("%w: lý do quá %d ký tự", ErrInvalidInput, maxWaiveReasonLen)
	}
	ob, err := lockObligation(ctx, tx, obligationID)
	if err != nil {
		return RefundObligation{}, err
	}
	if ob.Status != RefundOpen {
		return RefundObligation{}, fmt.Errorf("%w: %s", ErrObligationClosed, ob.Status)
	}
	q := store.New(tx)
	if _, err := q.WaiveRefundObligation(ctx, store.WaiveRefundObligationParams{
		Reason: reason, Now: s.d.Now().UTC(), ID: obligationID,
	}); err != nil {
		return RefundObligation{}, fmt.Errorf("ghi miễn hoàn: %w", err)
	}
	return getObligation(ctx, q, obligationID)
}

// RefundFilter là bộ lọc danh sách nghĩa vụ hoàn: Status rỗng là mọi trạng thái; OrderID nil là mọi đơn (Task 524:
// thẻ Nghĩa vụ hoàn trên chi tiết đơn).
type RefundFilter struct {
	Status  string
	OrderID *uuid.UUID
}

// ListRefundObligations đọc nghĩa vụ mới nhất trước theo bộ lọc f (trạng thái khác open, paid, waived →
// ErrInvalidInput), limit dòng sau mã before; next là mã của dòng cuối khi còn trang sau.
func (*Service) ListRefundObligations(ctx context.Context, tx pgx.Tx, f RefundFilter, before *uuid.UUID, limit int) (
	items []RefundObligation, next *uuid.UUID, err error,
) {
	status := f.Status
	st := pgtype.Text{String: status, Valid: status != ""}
	switch status {
	case "", RefundOpen, RefundPaid, RefundWaived:
	default:
		return nil, nil, fmt.Errorf("%w: trạng thái %q", ErrInvalidInput, status)
	}
	limit = min(max(limit, 1), maxListLimit)
	rows, err := store.New(tx).ListRefundObligations(ctx, store.ListRefundObligationsParams{
		Status: st, OrderID: db.NullUUID(f.OrderID), Before: db.NullUUID(before), Lim: int32(limit + 1),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc nghĩa vụ hoàn: %w", err)
	}
	items = make([]RefundObligation, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		items = append(items, obligationView(store.GetRefundObligationRow(r)))
	}
	if len(rows) > limit {
		id := items[limit-1].ID
		next = &id
	}
	return items, next, nil
}

func getObligation(ctx context.Context, q *store.Queries, id uuid.UUID) (RefundObligation, error) {
	r, err := q.GetRefundObligation(ctx, id)
	if err != nil {
		return RefundObligation{}, fmt.Errorf("đọc nghĩa vụ hoàn: %w", err)
	}
	return obligationView(r), nil
}

func obligationView(r store.GetRefundObligationRow) RefundObligation {
	v := RefundObligation{
		ID: r.ID, OrderID: r.OrderID, OrderCode: r.OrderCode, Source: r.Source, Status: r.Status, Amount: r.Amount,
		PaidAmount: r.PaidAmount, WaiveReason: r.WaiveReason.String, CreatedAt: r.CreatedAt, Version: r.Version,
	}
	if r.ReturnID.Valid {
		id := uuid.UUID(r.ReturnID.Bytes)
		v.ReturnID = &id
	}
	return v
}
