package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Tra đơn web, khách hủy từ website (kế hoạch M5, Task 196 / M5-13). Chỉ đơn kênh web; bản xem cho khách không có địa
// chỉ, SĐT, tên người nhận, ghi chú nội bộ, tên nhân viên (QĐ25). Phần vận đơn, nhãn gộp có vận đơn và luồng thay đổi
// đọc ở internal/queries/weborders (ghép view của ord và fulfillment): order không gọi fulfillment (04 mục 3).

// maxWebCancelNoteRunes: lời nhắn của khách khi hủy (giới hạn cancel_note, maxCancelNoteRunes).
const maxWebCancelNoteRunes = maxCancelNoteRunes

// WebViewLine là một dòng đơn cho khách; ExchangeableQty là số còn gửi yêu cầu đổi, trả được (0 khi đơn chưa giao hay
// quá hạn đổi).
type WebViewLine struct {
	VariantID       uuid.UUID
	SKU, Name       string
	Qty             int
	UnitPrice, Net  int64
	ExchangeableQty int
}

// WebOrderView là phần của đơn web cho khách tra (WB03). Status, PendingReason, PackingStatus là ba trục để bên gọi
// tính nhãn gộp (orderview.CustomerLabel) cùng trạng thái vận đơn hiện hành CurrentShipmentID (nil: chưa có).
// DeliveredAt là lần cuối đơn chuyển delivered, ExchangeUntil = DeliveredAt + ExchangeWindowDays ngày. Cancellable: đơn
// new, confirmed.
type WebOrderView struct {
	ExternalRef, Code, Status, PendingReason, PackingStatus string
	CurrentShipmentID                                       *uuid.UUID
	Lines                                                   []WebViewLine
	Merchandise, Discount, ShippingFee, GrandTotal          int64
	CreatedAt                                               time.Time
	DeliveredAt, ExchangeUntil                              *time.Time
	Cancellable, PriceReview                                bool
}

// GetWebOrder trả đơn kênh web có mã ngoài externalRef của người bán sellerID (đọc không khóa) lúc now (tính số còn
// đổi được). Không có, không phải kênh web, của tenant khác → ErrNotFound.
func GetWebOrder(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, externalRef string, now time.Time) (WebOrderView, error) {
	q := store.New(tx)
	ref, found, err := webOrderByRef(ctx, tx, sellerID, strings.TrimSpace(externalRef))
	switch {
	case err != nil:
		return WebOrderView{}, err
	case !found:
		return WebOrderView{}, ErrNotFound
	}
	o, err := Get(ctx, tx, sellerID, ref.OrderID)
	if err != nil {
		return WebOrderView{}, err
	}
	v := WebOrderView{
		ExternalRef: strings.TrimSpace(externalRef), Code: o.Code, Status: o.Status, Merchandise: o.Subtotal,
		Discount: o.DiscountTotal, ShippingFee: o.ShippingFee, GrandTotal: o.GrandTotal, CreatedAt: o.CreatedAt,
		Cancellable: o.Status == StatusNew || o.Status == StatusConfirmed, PriceReview: ref.PriceReview,
		PendingReason: o.PendingReason, PackingStatus: o.PackingStatus, CurrentShipmentID: o.CurrentShipmentID,
	}
	remaining, err := webExchangeable(ctx, tx, q, &v, o, now)
	if err != nil {
		return WebOrderView{}, err
	}
	v.Lines = make([]WebViewLine, 0, len(o.Items))
	for _, it := range o.Items {
		ex := min(max(remaining[it.VariantID], 0), int(it.Qty))
		remaining[it.VariantID] -= ex
		v.Lines = append(v.Lines, WebViewLine{
			VariantID: it.VariantID, SKU: it.SKU, Name: it.Name, Qty: int(it.Qty), UnitPrice: it.UnitPrice,
			Net: it.NetAmount, ExchangeableQty: ex,
		})
	}
	return v, nil
}

// webExchangeable đặt DeliveredAt, ExchangeUntil của v (đơn o đã giao) và trả số còn đổi được theo biến thể; đơn chưa
// giao hay quá hạn lúc now → bản đồ rỗng (mọi dòng 0).
func webExchangeable(ctx context.Context, tx pgx.Tx, q *store.Queries, v *WebOrderView, o Order, now time.Time,
) (map[uuid.UUID]int, error) {
	if o.Status != StatusDelivered {
		return map[uuid.UUID]int{}, nil
	}
	at, err := q.OrderDeliveredAt(ctx, o.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[uuid.UUID]int{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("đọc lúc giao đơn: %w", err)
	}
	s, err := LoadSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	until := at.Add(time.Duration(s.ExchangeWindowDays) * 24 * time.Hour)
	v.DeliveredAt, v.ExchangeUntil = &at, &until
	if now.After(until) {
		return map[uuid.UUID]int{}, nil
	}
	return exchangeRemaining(ctx, q, o.SellerID, o.ID)
}

// CancelOutcome là kết quả khách bấm hủy: Cancelled (Before, After là đơn trước và sau hủy, cho audit), hoặc yêu cầu
// hủy RequestRef (Request khác nil khi vừa tạo, nil khi trả lại yêu cầu hủy đang mở).
type CancelOutcome struct {
	Cancelled     bool
	RequestRef    string
	Request       *Request
	Before, After Order
}

// CancelFromWeb là khách bấm hủy đơn web externalRef (người bán sellerID, khóa dịch vụ a) trong tx của bên gọi (QĐ18).
// Khóa đơn trước (LockSnapshot, nhóm 7 ADR-46 — cùng khóa Gửi hãng giữ trước khi tạo vận đơn), rồi theo ảnh chụp dưới
// khóa:
//
//   - new, confirmed chưa có vận đơn hiện hành → Writer.Cancel lý do CancelCustomerChangedMind, ghi chú note (nhả hàng,
//     trả lượt mã theo KM03): Cancelled.
//   - confirmed có vận đơn hiện hành, cancel_requested → yêu cầu hủy cho nhân viên (CreateCancelRequest, lời nhắn note);
//     đã có yêu cầu hủy đang mở → trả lại mã đó, không tạo thêm.
//   - trạng thái khác (draft, delivered, returned, cancelled) → ErrCannotCancel.
//
// Lời nhắn quá 500 ký tự → ErrInvalidInput; không có đơn web → ErrNotFound. Chạy lại được trong db.RetryTx.
func (w Writer) CancelFromWeb(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, externalRef, note string, a Actor,
	now time.Time,
) (CancelOutcome, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxWebCancelNoteRunes {
		return CancelOutcome{}, fmt.Errorf("%w: lời nhắn hủy", ErrInvalidInput)
	}
	ref, found, err := webOrderByRef(ctx, tx, sellerID, strings.TrimSpace(externalRef))
	switch {
	case err != nil:
		return CancelOutcome{}, err
	case !found:
		return CancelOutcome{}, ErrNotFound
	}
	q := store.New(tx)
	snap, err := LockSnapshot(ctx, tx, sellerID, ref.OrderID)
	if err != nil {
		return CancelOutcome{}, err
	}
	switch {
	case (snap.Status == StatusNew || snap.Status == StatusConfirmed) && snap.CurrentShipmentID == nil:
		before, err := Get(ctx, tx, sellerID, snap.ID)
		if err != nil {
			return CancelOutcome{}, err
		}
		after, err := w.Cancel(ctx, tx, sellerID, snap.ID, snap.Version, CancelCustomerChangedMind, note, a, now)
		if err != nil {
			return CancelOutcome{}, err
		}
		return CancelOutcome{Cancelled: true, Before: before, After: after}, nil
	case snap.Status == StatusConfirmed || snap.Status == StatusCancelRequested:
		return w.cancelRequestFromWeb(ctx, tx, q, sellerID, snap.ID, note, a, now)
	}
	return CancelOutcome{}, ErrCannotCancel
}

// cancelRequestFromWeb trả yêu cầu hủy đang mở của đơn orderID (đã khóa), chưa có thì tạo.
func (w Writer) cancelRequestFromWeb(ctx context.Context, tx pgx.Tx, q *store.Queries, sellerID, orderID uuid.UUID,
	note string, a Actor, now time.Time,
) (CancelOutcome, error) {
	open, err := q.OpenCancelRequestRef(ctx, orderID)
	switch {
	case err == nil:
		return CancelOutcome{RequestRef: open}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return CancelOutcome{}, fmt.Errorf("đọc yêu cầu hủy đang mở: %w", err)
	}
	r, err := w.CreateCancelRequest(ctx, tx, sellerID, orderID, note, a, now)
	if err != nil {
		return CancelOutcome{}, err
	}
	return CancelOutcome{RequestRef: r.Ref, Request: &r}, nil
}
