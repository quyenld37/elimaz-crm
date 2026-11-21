package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// errCouponTotalsMoved: câu ghi tổng tiền sau khi dùng mã không gặp đơn hoặc dòng vừa chèn (không xảy ra trong cùng
// transaction); lùi để tổng không lệch.
var errCouponTotalsMoved = errors.New("order: không ghi được tổng tiền sau khi dùng mã")

// applyCoupon dùng mã p.coupon cho đơn orderID vừa chèn (Create bước 6; ADR-46 nhóm 10, sau đơn nhóm 7, trước tồn
// nhóm 11): promotion.Writer.Redeem với tạm tính sau giảm dòng và giảm tay cấp đơn, SĐT là danh tính SĐT của khách
// nếu đầu vào có, không thì SĐT người nhận; bị từ chối → lỗi bọc *promotion.RejectError. Phần giảm khác 0: tính lại
// bằng pricing.Compute với CouponDiscount, ghi tổng đơn (cod_amount = grand_total) và phần chia về dòng itemIDs (thứ
// tự nhập). Chỉ sửa dòng của chính đơn vừa chèn.
func (w Writer) applyCoupon(ctx context.Context, tx pgx.Tx, p prepared, d drafted, cust customer.OrderCustomer,
	orderID uuid.UUID, itemIDs []uuid.UUID, a Actor, now time.Time,
) error {
	t := d.totals
	phone := cust.Phone
	if phone == "" && p.addr != nil {
		phone = p.addr.Phone
	}
	r, err := promotion.Writer{Events: w.Events, Queue: w.Queue}.Redeem(ctx, tx, p.coupon,
		promotion.OrderRef{TenantID: a.TenantID, ID: orderID, Version: d.version},
		promotion.EvalInput{
			Base: t.Subtotal - t.LineDiscountTotal - t.OrderDiscount, Channel: p.channel, CustomerID: cust.CustomerID,
			CustomerRootID: cust.RootID, Phone: phone, SellerID: p.seller, Now: now,
		})
	if err != nil {
		return fmt.Errorf("dùng mã giảm giá: %w", err)
	}
	if r.Discount == 0 {
		return nil
	}
	in := d.pricing
	in.CouponDiscount = r.Discount
	t, err = pricing.Compute(in)
	if err != nil {
		return pricingError(err)
	}
	q := store.New(tx)
	n, err := q.SetOrderCouponTotals(ctx, store.SetOrderCouponTotalsParams{
		CouponDiscount: t.CouponDiscount, DiscountTotal: t.DiscountTotal, GrandTotal: t.GrandTotal, SellerID: p.seller,
		ID: orderID,
	})
	if err != nil {
		return fmt.Errorf("ghi tổng tiền sau khi dùng mã: %w", err)
	}
	if n != 1 {
		return errCouponTotalsMoved
	}
	alloc := store.SetOrderItemAllocationParams{
		Now: now, SellerID: p.seller, OrderID: orderID, Ids: itemIDs, Allocated: make([]int64, len(t.Lines)), Net: make([]int64, len(t.Lines)),
	}
	for i, l := range t.Lines {
		alloc.Allocated[i], alloc.Net[i] = l.Allocated, l.Net
	}
	n, err = q.SetOrderItemAllocation(ctx, alloc)
	if err != nil {
		return fmt.Errorf("ghi phần giảm chia về dòng: %w", err)
	}
	if n != int64(len(itemIDs)) {
		return errCouponTotalsMoved
	}
	return nil
}
