package order

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// Nhả suất flash sale theo chuyển trạng thái của đơn (kế hoạch M5, Task 202 / M5-19; 05 mục 7.4). Mọi đường hủy (hủy
// tay, hết hạn nháp, hãng xác nhận hủy, đơn trùng, phiếu đổi) đi qua cancelAll; đơn có dòng flash (đúng một dòng, QĐ6)
// trả suất về đợt khi đợt còn mở (trừ lại claims, giữ hàng released, số dư không đổi), hay về tồn thường khi đợt đã
// đóng (ReleaseHeldByItems: held −; đơn vị held → released, sold giữ nguyên).

// flashLine là dòng flash của một đơn sẽ hủy: dòng đơn, món, đợt, số lượng, khách đã lấy suất.
type flashLine struct {
	orderItemID, itemID, saleID, customerID uuid.UUID
	qty                                     int
}

// flashLineOf đọc dòng flash của đơn o đã khóa (nil khi không có). Nhiều hơn một dòng flash → ErrInvalidTransition
// (QĐ6: thứ tự khóa của đường hủy chỉ đúng với một dòng).
func flashLineOf(ctx context.Context, tx pgx.Tx, o *Order) (*flashLine, error) {
	rows, err := store.New(tx).FlashLinesOfOrder(ctx, store.FlashLinesOfOrderParams{SellerID: o.SellerID, OrderID: o.ID})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng flash của đơn: %w", err)
	}
	switch len(rows) {
	case 0:
		return nil, nil
	case 1:
	default:
		return nil, fmt.Errorf("%w: đơn có %d dòng flash sale", ErrInvalidTransition, len(rows))
	}
	it, err := promotion.GetFlashItemForBuy(ctx, tx, rows[0].FlashSaleItemID)
	if err != nil {
		return nil, fmt.Errorf("món flash sale của đơn: %w", err)
	}
	return &flashLine{
		orderItemID: rows[0].ID, itemID: it.ItemID, saleID: it.SaleID, customerID: o.CustomerID, qty: int(rows[0].Quantity),
	}, nil
}

// flashRelease là phần flash của một lần cancelAll: đợt nào còn mở (khóa chia sẻ) và các dòng flash.
type flashRelease struct {
	open  map[uuid.UUID]bool
	lines []*flashLine
}

// lockFlashSales khóa chia sẻ đợt của mọi dòng flash của plans theo mã đợt tăng dần (ADR-46 nhóm 10, sau coupons) và
// trả đợt nào còn mở; rồi trừ lại claims của các dòng thuộc đợt còn mở theo (món, khách) tăng dần (nhóm 10, sau
// flash_sales).
func lockFlashSales(ctx context.Context, tx pgx.Tx, plans []cancelPlan) (flashRelease, error) {
	fr := flashRelease{open: map[uuid.UUID]bool{}}
	var sales []uuid.UUID
	for _, p := range plans {
		if p.flash != nil {
			fr.lines = append(fr.lines, p.flash)
			sales = append(sales, p.flash.saleID)
		}
	}
	if len(fr.lines) == 0 {
		return fr, nil
	}
	slices.SortFunc(sales, compareID)
	for _, s := range slices.Compact(sales) {
		open, err := promotion.FlashSaleOpenForRelease(ctx, tx, s)
		if err != nil {
			return flashRelease{}, fmt.Errorf("đợt flash sale của đơn hủy: %w", err)
		}
		fr.open[s] = open
	}
	claims := slices.Clone(fr.lines)
	slices.SortFunc(claims, func(a, b *flashLine) int {
		if c := compareID(a.itemID, b.itemID); c != 0 {
			return c
		}
		return compareID(a.customerID, b.customerID)
	})
	for _, l := range claims {
		if !fr.open[l.saleID] {
			continue
		}
		if err := promotion.Unclaim(ctx, tx, l.itemID, l.customerID, l.qty); err != nil {
			return flashRelease{}, fmt.Errorf("trừ lại suất của khách: %w", err)
		}
	}
	return fr, nil
}

// reopened báo dòng đơn item là dòng flash của đợt còn mở (giữ hàng của nó không đi qua ReleaseHeldByItems).
func (fr flashRelease) reopened(item uuid.UUID) bool {
	return slices.ContainsFunc(fr.lines, func(l *flashLine) bool { return l.orderItemID == item && fr.open[l.saleID] })
}

// releaseUnits trả đơn vị suất của mọi dòng flash theo mã dòng đơn tăng dần (ADR-46 nhóm 11, sau số dư và giữ hàng).
func (fr flashRelease) releaseUnits(ctx context.Context, tx pgx.Tx, reason string) error {
	lines := slices.Clone(fr.lines)
	slices.SortFunc(lines, func(a, b *flashLine) int { return compareID(a.orderItemID, b.orderItemID) })
	for _, l := range lines {
		if _, err := inventory.ReleaseFlashUnits(ctx, tx, l.orderItemID, reason, fr.open[l.saleID]); err != nil {
			return fmt.Errorf("trả suất flash sale của đơn hủy: %w", err)
		}
	}
	return nil
}

// FlashItemOf trả món flash sale của đơn orderID (dòng flash đầu tiên) trong tx tenant của bên gọi; đơn không có dòng
// flash hay không có đơn → ok false. Dùng ở consumer webcache (order.cancelled của đơn flash → báo hiệu flash_item).
func FlashItemOf(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (itemID uuid.UUID, ok bool, err error) {
	id, err := store.New(tx).FlashItemOfOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("đọc món flash sale của đơn: %w", err)
	}
	return id, true, nil
}

func compareID(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }
