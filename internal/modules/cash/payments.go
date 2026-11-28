package cash

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/refunds"
	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
)

// Phiếu thu, cọc trước khi gửi hãng (kế hoạch M3, Task M3-30; Q-17): tổng phiếu của đơn đẩy về đơn bằng
// order.Writer.ApplyPaymentTotals, COD còn thu tự tính lại; đơn đã gửi hãng thì từ chối (đổi COD phải qua hãng).

// Phương thức của phiếu (CHECK cash.payments.method). Nhân viên chỉ ghi ba phương thức đầu; exchange_credit do đổi hàng
// (Task M3-25), refund do PayRefund ghi.
const (
	MethodBankTransfer   = "bank_transfer"
	MethodDeposit        = "deposit"
	MethodCash           = "cash"
	MethodExchangeCredit = "exchange_credit"
	MethodRefund         = "refund"
)

// TopicPaymentReceived là sự kiện sau khi ghi một phiếu thu (aggregate payment, Seq 1; payload PaymentReceived).
const TopicPaymentReceived = "payment.received"

const (
	aggregatePayment = "payment"
	maxMoney         = 9007199254740991
	maxReferenceLen  = 200
)

// Lỗi của phiếu thu.
var (
	// ErrOrderNotFound: đơn không có hay của tenant khác.
	ErrOrderNotFound = errors.New("cash: không có đơn")
	// ErrCODLockedByShipment: đơn đã gửi hãng (có vận đơn hiện hành), COD không đổi được ở CRM (Q-17).
	// Cùng giá trị với lỗi của consumer cash_refunds khi cấn trừ đổi hàng (Task M3-25).
	ErrCODLockedByShipment = refunds.ErrCODLockedByShipment
	// ErrPaymentNotAllowed: đơn không còn nhận tiền (đã hủy, đang chờ hủy, đã giao, đã hoàn).
	ErrPaymentNotAllowed = errors.New("cash: đơn ở trạng thái không ghi phiếu thu")
	// ErrPaymentExceedsTotal: tổng phiếu của đơn sẽ vượt tổng đơn.
	ErrPaymentExceedsTotal = errors.New("cash: tổng phiếu vượt tổng đơn")
	// ErrExchangeCreditPending: đơn đổi chưa có phiếu cấn trừ tiền món trả, chưa gửi hãng được (COD khóa khi đã gửi;
	// Task 524). Bên gọi (gửi hãng) trả lỗi này khi ExchangeCreditPending báo true.
	ErrExchangeCreditPending = errors.New("cash: đơn đổi chưa cấn trừ tiền món trả")
)

// ExchangeCreditPending báo đơn orderID là đơn đổi còn phải cấn trừ tiền món trả (cùng luật với consumer
// cash_refunds: phiếu đổi có tiền hoàn, chưa có phiếu exchange_credit của phiếu đó, kho chưa chốt nghĩa vụ exchange,
// đơn chưa hủy, min(tiền hoàn, tổng − đã thu) > 0). Khóa đơn (ADR-46 nhóm 7) trong tx của bên gọi: gửi hãng gọi sau
// khi đã khóa đơn, nên nối tiếp với lần cấn trừ. Đơn thường hay không có → false. Không ghi gì.
func ExchangeCreditPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error) {
	return refunds.CreditPending(ctx, tx, orderID) //nolint:wrapcheck // refunds đã bọc
}

// PaymentInput là một phiếu thu nhân viên ghi: phương thức bank_transfer | deposit | cash, số tiền > 0, mã tham chiếu
// (mã giao dịch, tối đa 200 ký tự), lúc nhận (zero: lúc ghi).
type PaymentInput struct {
	OrderID    uuid.UUID
	Method     string
	Amount     int64
	Reference  string
	ReceivedAt time.Time
}

// PaymentView là một phiếu đã ghi (thu: Amount > 0; chi hoàn: Method refund, Amount < 0, RefundObligationID có).
type PaymentView struct {
	ID, OrderID        uuid.UUID
	Method             string
	Amount             int64
	Reference          string
	RefundObligationID *uuid.UUID
	ReceivedAt         time.Time
	ActorID            uuid.UUID
	CreatedAt          time.Time
}

// PaymentReceived là payload payment.received: chỉ mã, phương thức, số tiền.
type PaymentReceived struct {
	PaymentID uuid.UUID `json:"paymentId"`
	OrderID   uuid.UUID `json:"orderId"`
	Method    string    `json:"method"`
	Amount    int64     `json:"amount"`
}

// orderWriter dựng order.Writer từ sổ sự kiện, hàng đợi, bus của module (Events, Queue nil → order báo lỗi dựng).
func (s *Service) orderWriter() order.Writer {
	return order.Writer{Events: s.d.Events, Queue: s.d.Queue, Realtime: s.d.Bus}
}

// lockPaymentOrder đọc người bán của đơn id qua ord.orders_v rồi khóa đơn (order.LockSnapshot, ADR-46 nhóm 7). Không
// có → ErrOrderNotFound.
func lockPaymentOrder(ctx context.Context, tx pgx.Tx, id uuid.UUID) (order.Snapshot, error) {
	o, err := store.New(tx).OrderForPayment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return order.Snapshot{}, ErrOrderNotFound
	}
	if err != nil {
		return order.Snapshot{}, fmt.Errorf("đọc đơn của phiếu: %w", err)
	}
	snap, err := order.LockSnapshot(ctx, tx, o.SellerID, id)
	if errors.Is(err, order.ErrNotFound) {
		return order.Snapshot{}, ErrOrderNotFound
	}
	if err != nil {
		return order.Snapshot{}, fmt.Errorf("khóa đơn của phiếu: %w", err)
	}
	return snap, nil
}

// checkPaymentInput kiểm phương thức, số tiền, mã tham chiếu của in → ErrInvalidInput.
func checkPaymentInput(in PaymentInput) error {
	switch {
	case in.Method != MethodBankTransfer && in.Method != MethodDeposit && in.Method != MethodCash:
		return fmt.Errorf("%w: phương thức %q", ErrInvalidInput, in.Method)
	case in.Amount <= 0 || in.Amount > maxMoney:
		return fmt.Errorf("%w: số tiền %d", ErrInvalidInput, in.Amount)
	case utf8.RuneCountInString(in.Reference) > maxReferenceLen:
		return fmt.Errorf("%w: mã tham chiếu quá %d ký tự", ErrInvalidInput, maxReferenceLen)
	}
	return nil
}

// RecordPayment ghi phiếu thu in của đơn in.OrderID trong tx (WriteTx của tenantID), người ghi a: khóa đơn (nhóm 7) →
// đơn có vận đơn hiện hành → ErrCODLockedByShipment; đơn không ở draft, new, confirmed → ErrPaymentNotAllowed; tổng
// phiếu sau khi ghi vượt grand_total → ErrPaymentExceedsTotal → chèn phiếu → order.ApplyPaymentTotals(tổng phiếu)
// (prepaid_total, cod_amount, payment_status của đơn) → phát payment.received. Đầu vào sai → ErrInvalidInput; đơn
// không có → ErrOrderNotFound. Chạy lại được trong RetryTx.
func (s *Service) RecordPayment(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PaymentInput, a Actor) (PaymentView, error) {
	if err := checkPaymentInput(in); err != nil {
		return PaymentView{}, err
	}
	snap, err := lockPaymentOrder(ctx, tx, in.OrderID)
	if err != nil {
		return PaymentView{}, err
	}
	switch {
	case snap.CurrentShipmentID != nil:
		return PaymentView{}, ErrCODLockedByShipment
	case snap.Status != order.StatusDraft && snap.Status != order.StatusNew && snap.Status != order.StatusConfirmed:
		return PaymentView{}, fmt.Errorf("%w: đơn %s", ErrPaymentNotAllowed, snap.Status)
	}
	q := store.New(tx)
	total, err := q.PaymentsTotal(ctx, in.OrderID)
	if err != nil {
		return PaymentView{}, fmt.Errorf("đọc tổng phiếu của đơn: %w", err)
	}
	if total+in.Amount > snap.GrandTotal {
		return PaymentView{}, fmt.Errorf("%w: %d + %d > %d", ErrPaymentExceedsTotal, total, in.Amount, snap.GrandTotal)
	}
	now := s.d.Now().UTC()
	received := in.ReceivedAt.UTC()
	if in.ReceivedAt.IsZero() {
		received = now
	}
	p, err := q.InsertPayment(ctx, store.InsertPaymentParams{
		OrderID: in.OrderID, Method: in.Method, Amount: in.Amount, Reference: in.Reference, ReceivedAt: received,
		ActorID: a.ID,
	})
	if err != nil {
		return PaymentView{}, fmt.Errorf("ghi phiếu thu: %w", err)
	}
	if err := s.applyTotals(ctx, tx, tenantID, snap, total+in.Amount, a, now); err != nil {
		return PaymentView{}, err
	}
	if err := s.emit(ctx, tx, events.Event{
		TenantID: tenantID, AggregateType: aggregatePayment, AggregateID: p.ID.String(), Seq: 1, Topic: TopicPaymentReceived,
		Payload: PaymentReceived{PaymentID: p.ID, OrderID: p.OrderID, Method: p.Method, Amount: p.Amount},
	}); err != nil {
		return PaymentView{}, err
	}
	return paymentView(p), nil
}

// applyTotals đẩy tổng đã thanh toán paid về đơn snap (ApplyPaymentTotals); order.ErrLockedByShipment →
// ErrCODLockedByShipment.
func (s *Service) applyTotals(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, snap order.Snapshot, paid int64, a Actor,
	now time.Time,
) error {
	err := s.orderWriter().ApplyPaymentTotals(ctx, tx, snap.SellerID, snap.ID, paid, orderActor(tenantID, a), now)
	if errors.Is(err, order.ErrLockedByShipment) {
		return ErrCODLockedByShipment
	}
	if err != nil {
		return fmt.Errorf("đẩy tổng đã thanh toán về đơn: %w", err)
	}
	return nil
}

// orderActor là tác nhân của order cho người ghi a của tenantID.
func orderActor(tenantID uuid.UUID, a Actor) order.Actor {
	return order.Actor{TenantID: tenantID, UserID: a.ID, Type: a.Type}
}

// ListOrderPayments đọc phiếu của đơn orderID mới nhất trước, limit dòng sau mã before (nil: từ đầu); next là mã của
// dòng cuối khi còn trang sau. Đơn không có → ErrOrderNotFound.
func (*Service) ListOrderPayments(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, before *uuid.UUID, limit int) (
	items []PaymentView, next *uuid.UUID, err error,
) {
	q := store.New(tx)
	if _, err := q.OrderForPayment(ctx, orderID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrOrderNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("đọc đơn của phiếu: %w", err)
	}
	limit = min(max(limit, 1), maxListLimit)
	rows, err := q.ListOrderPayments(ctx, store.ListOrderPaymentsParams{
		OrderID: orderID, Before: db.NullUUID(before), Lim: int32(limit + 1),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc phiếu của đơn: %w", err)
	}
	items = make([]PaymentView, 0, min(len(rows), limit))
	for _, r := range rows[:min(len(rows), limit)] {
		items = append(items, paymentView(r))
	}
	if len(rows) > limit {
		id := items[limit-1].ID
		next = &id
	}
	return items, next, nil
}

// maxListLimit là số dòng tối đa một trang (hợp đồng Limit).
const maxListLimit = 100

func paymentView(p store.CashPayment) PaymentView {
	v := PaymentView{
		ID: p.ID, OrderID: p.OrderID, Method: p.Method, Amount: p.Amount, Reference: p.Reference, ReceivedAt: p.ReceivedAt,
		ActorID: p.ActorID, CreatedAt: p.CreatedAt,
	}
	if p.RefundObligationID.Valid {
		id := uuid.UUID(p.RefundObligationID.Bytes)
		v.RefundObligationID = &id
	}
	return v
}
