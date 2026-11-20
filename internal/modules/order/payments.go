package order

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Trạng thái thanh toán của đơn (05: payment_status). refunded do cash đặt ở M3 bằng MarkRefunded.
const (
	PaymentUnpaid   = "unpaid"
	PaymentPartial  = "partial"
	PaymentPaid     = "paid"
	PaymentRefunded = "refunded"
)

// ApplyPaymentTotals ghi tổng đã thanh toán paidTotal (đồng, do cash của M3 tính từ phiếu thu) của đơn orderID của
// người bán sellerID, trong tx của bên gọi (kế hoạch M2, Task M2-18): prepaid_total = paidTotal, cod_amount =
// max(grand_total − paidTotal, 0), payment_status unpaid (0) / partial (< grand_total) / paid; version + 1, sự kiện
// order.updated (seq = version mới), báo hiệu. Khóa đơn trước (ADR-46 nhóm 7). Cả ba giá trị như hiện tại → không đổi
// gì (gọi lại an toàn). Lỗi: paidTotal âm hay vượt 9007199254740991 → ErrInvalidInput; đơn có vận đơn hiện hành mà
// cod_amount sẽ đổi → ErrLockedByShipment (đổi COD phải qua hãng, M3); không có đơn → ErrNotFound.
func (w Writer) ApplyPaymentTotals(ctx context.Context, tx pgx.Tx, sellerID, orderID uuid.UUID, paidTotal int64,
	_ Actor, now time.Time,
) error {
	if paidTotal < 0 || paidTotal > maxMoney {
		return fmt.Errorf("%w: tổng đã thanh toán %d", ErrInvalidInput, paidTotal)
	}
	q, o, err := w.lockForPayment(ctx, tx, sellerID, orderID)
	if err != nil {
		return err
	}
	cod, status := max(o.GrandTotal-paidTotal, 0), PaymentPartial
	switch {
	case paidTotal == 0:
		status = PaymentUnpaid
	case paidTotal >= o.GrandTotal:
		status = PaymentPaid
	}
	if paidTotal == o.PrepaidTotal && cod == o.CodAmount && status == o.PaymentStatus {
		return nil
	}
	if o.CurrentShipmentID.Valid && cod != o.CodAmount {
		return fmt.Errorf("%w: COD %d → %d", ErrLockedByShipment, o.CodAmount, cod)
	}
	version, err := q.SetPaymentTotals(ctx, store.SetPaymentTotalsParams{
		PrepaidTotal: paidTotal, CodAmount: cod, PaymentStatus: status, Now: now, SellerID: sellerID, ID: orderID,
		Version: o.Version,
	})
	if err != nil {
		return fmt.Errorf("ghi tổng đã thanh toán: %w", err)
	}
	return w.emitUpdated(ctx, tx, o, version)
}

// MarkRefunded đặt payment_status = refunded cho đơn orderID của người bán sellerID khi cash đã chi hoàn hết tiền khách
// trả (kế hoạch M3, Task M3-30; TI09), trong tx của bên gọi: khóa đơn trước (ADR-46 nhóm 7); tổng đã thanh toán
// (prepaid_total) phải đã về 0 qua ApplyPaymentTotals, không thì ErrInvalidTransition và không đổi gì; đã refunded →
// không đổi gì (gọi lại an toàn); version + 1, sự kiện order.updated (seq = version mới), báo hiệu. Không có đơn →
// ErrNotFound.
func (w Writer) MarkRefunded(ctx context.Context, tx pgx.Tx, sellerID, orderID uuid.UUID, _ Actor, now time.Time) error {
	q, o, err := w.lockForPayment(ctx, tx, sellerID, orderID)
	if err != nil {
		return err
	}
	switch {
	case o.PaymentStatus == PaymentRefunded:
		return nil
	case o.PrepaidTotal != 0:
		return fmt.Errorf("%w: đơn còn %d đã thanh toán, chưa hoàn hết", ErrInvalidTransition, o.PrepaidTotal)
	}
	version, err := q.SetPaymentRefunded(ctx, store.SetPaymentRefundedParams{
		Now: now, SellerID: sellerID, ID: orderID, Version: o.Version,
	})
	if err != nil {
		return fmt.Errorf("ghi đơn đã hoàn tiền: %w", err)
	}
	return w.emitUpdated(ctx, tx, o, version)
}

// lockForPayment kiểm w đã nối rồi khóa đơn orderID của người bán sellerID (ADR-46 nhóm 7) cho một lần ghi tiền của
// đơn (ApplyPaymentTotals, MarkRefunded). Không có đơn → ErrNotFound.
func (w Writer) lockForPayment(ctx context.Context, tx pgx.Tx, sellerID, orderID uuid.UUID) (
	*store.Queries, store.LockOrderRow, error,
) {
	if err := w.wired(); err != nil {
		return nil, store.LockOrderRow{}, err
	}
	q := store.New(tx)
	o, err := lockOrder(ctx, q, sellerID, orderID)
	return q, o, err
}
