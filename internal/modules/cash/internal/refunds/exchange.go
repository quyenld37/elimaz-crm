package refunds

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đổi hàng (kế hoạch M3, Task M3-25; TD06, AT14, Q-26): tiền hoàn của món trả (refund_amount của phiếu exchange) cấn
// trừ vào đơn thay thế bằng một phiếu exchange_credit (mã tham chiếu exchange:<mã phiếu>, người ghi hệ thống), COD của
// đơn thay thế tự tính lại qua order.ApplyPaymentTotals; đơn gốc không đổi. Phần dư (tiền hoàn cuối lúc kho nhận − số
// đã cấn trừ) thành nghĩa vụ hoàn exchange của đơn gốc khi kho nhận phiếu đổi.

// ErrCODLockedByShipment báo đơn đã gửi hãng (có vận đơn hiện hành), COD không đổi được ở CRM (Q-17). cash công bố cùng
// giá trị (cash.ErrCODLockedByShipment).
var ErrCODLockedByShipment = errors.New("cash: đơn đã gửi hãng, không ghi phiếu làm đổi COD")

// methodExchangeCredit là phương thức của phiếu cấn trừ (cash.MethodExchangeCredit).
const methodExchangeCredit = "exchange_credit"

// systemActor là loại tác nhân hệ thống của order cho lần đẩy tổng đã thanh toán (consumer không có người dùng).
const systemActor = "system"

// creditAmount là số cấn trừ vào đơn có tổng grand đã thu paid khi tiền hoàn của món trả là refund: min(refund, grand −
// paid), không âm.
func creditAmount(refund, grand, paid int64) int64 {
	return max(min(refund, grand-paid), 0)
}

// surplus là phần tiền hoàn cuối final chưa cấn trừ (credit) phải trả lại khách: final − credit, không âm.
func surplus(final, credit int64) int64 {
	return max(final-credit, 0)
}

// creditReplacement là order.created của đơn thay thế orderID: cấn trừ tiền hoàn của phiếu đổi đã tạo đơn đó. Không có
// phiếu → queue.ErrPermanent; đơn đã gửi hãng → ErrCODLockedByShipment bọc queue.ErrPermanent (việc dead ngay, cảnh báo
// dead thường; màn đổi hàng chỉ cho gửi hãng sau khi có phiếu cấn trừ).
func (c Consumer) creditReplacement(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, now time.Time) error {
	r, err := store.New(tx).ExchangeReturnOfReplacement(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("đơn thay thế %s không có phiếu đổi: %w", orderID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("đọc phiếu đổi của đơn thay thế: %w", err)
	}
	if _, err := c.applyCredit(ctx, tx, tenantID, exchangeRef{id: r.ID, orig: r.OrderID}, orderID, r.RefundAmount,
		now); err != nil {
		if errors.Is(err, ErrCODLockedByShipment) {
			return fmt.Errorf("cấn trừ phiếu đổi %s vào đơn %s: %w: %w", r.ID, orderID, err, queue.ErrPermanent)
		}
		return err
	}
	return nil
}

// receivedExchange là kho nhận phiếu đổi p.ReturnID (tiền hoàn cuối p.RefundAmount): chưa cấn trừ thì cấn trừ ngay bằng
// tiền hoàn cuối (sự kiện tạo đơn thay thế là đối tượng khác, không có thứ tự với sự kiện này); đơn thay thế đã gửi hãng
// mà chưa cấn trừ → không cấn trừ (khách trả đủ COD); phần dư > 0 → nghĩa vụ exchange của đơn gốc gắn phiếu. Tiền hoàn
// cuối nhỏ hơn số đã cấn trừ (kho nhận thiếu): không nghĩa vụ (QĐ-116-3). Phiếu không có → queue.ErrPermanent.
func (c Consumer) receivedExchange(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, p order.ReturnReceived,
	now time.Time,
) error {
	q := store.New(tx)
	r, err := q.ExchangeReturn(ctx, p.ReturnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("phiếu đổi %s không có: %w", p.ReturnID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("đọc phiếu đổi: %w", err)
	}
	var credit int64
	if r.ReplacementOrderID.Valid {
		credit, err = c.applyCredit(ctx, tx, tenantID, exchangeRef{id: r.ID, orig: r.OrderID}, r.ReplacementOrderID.Bytes,
			p.RefundAmount, now)
		if err != nil && !errors.Is(err, ErrCODLockedByShipment) {
			return err
		}
	}
	rest := surplus(p.RefundAmount, credit)
	if rest <= 0 {
		return nil
	}
	return open(ctx, q, r.OrderID, &r.ID, SourceExchange, rest)
}

// exchangeRef là phiếu đổi (id) và đơn gốc của nó (orig).
type exchangeRef struct{ id, orig uuid.UUID }

// creditRef là mã tham chiếu của phiếu cấn trừ của phiếu đổi id.
func creditRef(id uuid.UUID) string { return "exchange:" + id.String() }

// due là kết quả creditDue: have là số đã cấn trừ (phiếu đã có), amount là số còn phải cấn trừ (> 0 chỉ khi chưa có
// phiếu), paid là tổng mọi phiếu của đơn (chỉ đọc khi cần tính amount).
type due struct{ have, amount, paid int64 }

// creditDue là luật cấn trừ của phiếu đổi ret (tiền hoàn refund) trên đơn thay thế orderID có ảnh chụp snap (bên gọi
// đã khóa đơn): đơn đã hủy → không gì; đã có phiếu exchange:<phiếu> → have; kho đã nhận phiếu đổi (đã có nghĩa vụ
// exchange) → không gì; đơn không còn ở draft, new, confirmed → không gì; ngược lại amount = min(refund, tổng − đã thu),
// không âm. Không xét vận đơn (bên gọi quyết). Một nguồn cho consumer (applyCredit) và kiểm lúc gửi hãng (CreditPending).
func creditDue(ctx context.Context, q *store.Queries, ret exchangeRef, orderID uuid.UUID, refund int64,
	snap order.Snapshot,
) (due, error) {
	if snap.Status == order.StatusCancelled {
		return due{}, nil
	}
	have, err := q.ExchangeCredit(ctx, store.ExchangeCreditParams{OrderID: orderID, Reference: creditRef(ret.id)})
	if err != nil {
		return due{}, fmt.Errorf("đọc phiếu cấn trừ: %w", err)
	}
	if have > 0 {
		return due{have: have}, nil
	}
	settled, err := q.ExchangeObligationExists(ctx, store.ExchangeObligationExistsParams{
		OrderID: ret.orig, ReturnID: ret.id,
	})
	if err != nil {
		return due{}, fmt.Errorf("đọc nghĩa vụ hoàn của phiếu đổi: %w", err)
	}
	if settled || (snap.Status != order.StatusDraft && snap.Status != order.StatusNew &&
		snap.Status != order.StatusConfirmed) {
		return due{}, nil
	}
	paid, err := q.PaymentsTotal(ctx, orderID)
	if err != nil {
		return due{}, fmt.Errorf("đọc tổng phiếu của đơn thay thế: %w", err)
	}
	return due{amount: creditAmount(refund, snap.GrandTotal, paid), paid: paid}, nil
}

// CreditPending báo đơn orderID là đơn đổi còn phải cấn trừ tiền món trả (creditDue > 0): đơn không phải đơn thay
// thế của phiếu đổi nào → false. Khóa đơn (order.LockSnapshot, ADR-46 nhóm 7; bên gọi đã khóa thì khóa lại không chờ)
// nên đọc nối tiếp với lần cấn trừ của consumer. Đơn không có → false (bên gọi đã báo lỗi của nó).
func CreditPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error) {
	q := store.New(tx)
	r, err := q.ExchangeReturnOfReplacement(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc phiếu đổi của đơn: %w", err)
	}
	o, err := q.OrderForPayment(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc đơn: %w", err)
	}
	snap, err := order.LockSnapshot(ctx, tx, o.SellerID, orderID)
	if errors.Is(err, order.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("khóa đơn đổi: %w", err)
	}
	d, err := creditDue(ctx, q, exchangeRef{id: r.ID, orig: r.OrderID}, orderID, r.RefundAmount, snap)
	if err != nil {
		return false, err
	}
	return d.have == 0 && d.amount > 0, nil
}

// applyCredit khóa đơn thay thế orderID (ADR-46 nhóm 7) rồi ghi phiếu cấn trừ của phiếu đổi ret (tiền hoàn refund) nếu
// chưa có; trả số đã cấn trừ còn tính cho khách. Đơn đã hủy → 0 (phiếu cấn trừ cũ không còn tính, không ghi gì); đã có
// phiếu → số của phiếu đó (giao lại không ghi thêm); kho đã nhận phiếu đổi (đã có nghĩa vụ exchange: phần tiền đã chốt
// lúc nhận, vd. đơn thay thế khi đó đang gửi hãng rồi vận đơn bị gỡ) → 0, không ghi gì (không trả tiền hoàn hai lần);
// đơn không còn ở draft, new, confirmed → 0; đơn có vận đơn hiện hành → ErrCODLockedByShipment, không ghi gì. Lần nhận
// giữ khóa đơn thay thế tới khi commit nghĩa vụ, nên lần cấn trừ chạy song song thấy nghĩa vụ sau khi có khóa. Chạy lại
// được trong tx mới.
func (c Consumer) applyCredit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, ret exchangeRef, orderID uuid.UUID,
	refund int64, now time.Time,
) (int64, error) {
	q := store.New(tx)
	o, err := orderOf(ctx, q, orderID)
	if err != nil {
		return 0, err
	}
	snap, err := order.LockSnapshot(ctx, tx, o.SellerID, orderID)
	if err != nil {
		return 0, fmt.Errorf("khóa đơn thay thế: %w", err)
	}
	d, err := creditDue(ctx, q, ret, orderID, refund, snap)
	switch {
	case err != nil:
		return 0, err
	case d.have > 0:
		return d.have, nil
	case d.amount <= 0:
		return 0, nil
	case snap.CurrentShipmentID != nil:
		return 0, fmt.Errorf("đơn thay thế %s: %w", orderID, ErrCODLockedByShipment)
	}
	amount, ref, total := d.amount, creditRef(ret.id), d.paid
	if _, err := q.InsertPayment(ctx, store.InsertPaymentParams{
		OrderID: orderID, Method: methodExchangeCredit, Amount: amount, Reference: ref, ReceivedAt: now, ActorID: uuid.Nil,
	}); err != nil {
		return 0, fmt.Errorf("ghi phiếu cấn trừ: %w", err)
	}
	// Vận đơn hiện hành đã loại ở trên dưới cùng khóa, nên ErrLockedByShipment ở đây không xảy ra; nếu có thì giữ lỗi
	// của order (không phải ErrCODLockedByShipment) để receivedExchange không bỏ qua khi phiếu vừa chèn còn trong tx.
	if err := c.Orders.ApplyPaymentTotals(ctx, tx, snap.SellerID, orderID, total+amount,
		order.Actor{TenantID: tenantID, Type: systemActor}, now); err != nil {
		return 0, fmt.Errorf("đẩy tổng đã thanh toán về đơn thay thế: %w", err)
	}
	return amount, nil
}
