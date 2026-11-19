package promotion

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
)

// Lý do trả lượt khi Detach (sửa đơn bỏ mã, đổi mã).
const detachReason = "detached"

// lockRetries: số lần đọc lại lượt của đơn khi lượt đổi mã giữa lúc đọc và lúc khóa mã.
const lockRetries = 3

// errOrderHasCoupon: đơn đã có một lượt mã đang hiệu lực. Bên gọi (order) Detach trước khi dùng mã khác, nên đây là
// lỗi lập trình (HTTP 500), không phải lỗi nghiệp vụ cho người dùng.
var errOrderHasCoupon = errors.New("promotion: đơn đã có mã đang dùng")

// errRedemptionMoved: lượt của đơn đổi mã liên tục trong lúc khóa (bên gọi nên đã khóa đơn); lùi transaction.
var errRedemptionMoved = errors.New("promotion: lượt mã của đơn đổi trong lúc khóa")

// Redemption là kết quả dùng mã cho một đơn.
type Redemption struct {
	CouponID uuid.UUID
	Code     string
	Discount int64
}

// TopicCouponUsed là sự kiện công bố khi một đơn dùng mã (docs/views.md mục "Sự kiện công bố"; 04 mục 3: promotion
// phát). Aggregate order_coupon (mã đơn), Seq = version của đơn ở bên gọi (đơn đã khóa hoặc vừa chèn), nên hai lần
// dùng mã của cùng một đơn (sửa đơn đổi mã) không trùng Seq.
const TopicCouponUsed = "coupon.used"

const aggregateOrderCoupon = "order_coupon"

// errWriterNotWired: Redeem cần Writer có Events và Queue (lỗi nối dây, HTTP 500).
var errWriterNotWired = errors.New("promotion: Writer thiếu Events hoặc Queue")

// OrderRef là đơn dùng mã: tenant, mã đơn và version hiện tại của đơn (đơn đã chèn hoặc đã khóa trong tx của bên gọi;
// làm Seq của coupon.used).
type OrderRef struct {
	TenantID, ID uuid.UUID
	Version      int64
}

// Used là payload của coupon.used: chỉ mã và số (A16).
type Used struct {
	CouponID   uuid.UUID `json:"couponId"`
	OrderID    uuid.UUID `json:"orderId"`
	CustomerID uuid.UUID `json:"customerId"`
	Discount   int64     `json:"discount"`
}

// Redeem dùng mã code (không phân biệt hoa thường) cho đơn o trong tx của bên gọi: khóa dòng mã (ADR-46 nhóm 10:
// coupons → coupon_customer_uses → coupon_redemptions), Evaluate, tăng used_count có điều kiện (0 dòng → exhausted),
// tăng lượt của khách in.CustomerRootID có điều kiện (0 dòng → customer_limit), chèn lượt của đơn hoặc đổi dòng
// released cũ của cùng (mã, đơn) về active, rồi phát coupon.used. Từ chối → *RejectError (errors.Is
// ErrCouponRejected); bên gọi lùi cả transaction. Đơn đã có lượt active → lỗi (bên gọi Detach trước). Writer thiếu
// Events hoặc Queue → lỗi trước khi đọc gì.
func (w Writer) Redeem(ctx context.Context, tx pgx.Tx, code string, o OrderRef, in EvalInput) (Redemption, error) {
	if w.Events == nil || w.Queue == nil {
		return Redemption{}, errWriterNotWired
	}
	orderID := o.ID
	q := store.New(tx)
	row, err := q.CouponByCodeForUpdate(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, &RejectError{Reason: ReasonNotFound}
	}
	if err != nil {
		return Redemption{}, fmt.Errorf("khóa mã giảm giá: %w", err)
	}
	c := couponFrom(store.CouponByCodeRow(row))
	discount, reason := Evaluate(c, in)
	if reason != "" {
		return Redemption{}, &RejectError{Reason: reason}
	}
	if _, err := q.ActiveRedemptionByOrder(ctx, orderID); err == nil {
		return Redemption{}, errOrderHasCoupon
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, fmt.Errorf("đọc lượt mã của đơn: %w", err)
	}
	n, err := q.IncrementUsed(ctx, c.ID)
	if err != nil {
		return Redemption{}, fmt.Errorf("tăng lượt dùng mã: %w", err)
	}
	if n == 0 {
		return Redemption{}, &RejectError{Reason: ReasonExhausted}
	}
	limit := pgtype.Int4{}
	if c.PerCustomerLimit != nil {
		limit = pgtype.Int4{Int32: *c.PerCustomerLimit, Valid: true}
	}
	n, err = q.IncrementCustomerUse(ctx, store.IncrementCustomerUseParams{
		CouponID: c.ID, CustomerID: in.CustomerRootID, PerCustomerLimit: limit,
	})
	if err != nil {
		return Redemption{}, fmt.Errorf("tăng lượt dùng mã của khách: %w", err)
	}
	if n == 0 {
		return Redemption{}, &RejectError{Reason: ReasonCustomerLimit}
	}
	n, err = q.UpsertRedemption(ctx, store.UpsertRedemptionParams{
		CouponID: c.ID, OrderID: orderID, CustomerID: in.CustomerRootID, Discount: discount,
	})
	if err != nil {
		return Redemption{}, fmt.Errorf("ghi lượt dùng mã của đơn: %w", err)
	}
	if n == 0 {
		return Redemption{}, errOrderHasCoupon
	}
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: o.TenantID, AggregateType: aggregateOrderCoupon, AggregateID: orderID.String(), Seq: o.Version,
		Topic:   TopicCouponUsed,
		Payload: Used{CouponID: c.ID, OrderID: orderID, CustomerID: in.CustomerRootID, Discount: discount},
	}); err != nil {
		return Redemption{}, fmt.Errorf("phát %s: %w", TopicCouponUsed, err)
	}
	return Redemption{CouponID: c.ID, Code: c.Code, Discount: discount}, nil
}

// OrderCouponCode trả mã (đúng chữ đã lưu) mà đơn orderID đang dùng, cho dấu vân tay lần gửi lại tạo đơn: mã của lượt
// đang hiệu lực; đơn đã hủy (lượt đã trả) → mã của lượt đã trả gần nhất; lượt bị bỏ khi sửa đơn (Detach) không tính.
// Đơn không dùng mã → "".
func OrderCouponCode(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (string, error) {
	code, err := store.New(tx).OrderCouponCode(ctx, store.OrderCouponCodeParams{OrderID: orderID, DetachReason: detachReason})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("đọc mã của đơn: %w", err)
	}
	return code, nil
}

// Release trả lượt mã của đơn orderID khi đơn hủy (reason: lý do hủy, 1–100 ký tự). Không có lượt active, hoặc mã có
// release_on_cancel = false → false, không đổi gì. Ngược lại: lượt của khách −1, dòng lượt của đơn → released,
// used_count −1; trả true. Gọi lần hai → false.
func Release(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, reason string) (released bool, err error) {
	return release(ctx, tx, orderID, reason, true)
}

// Detach bỏ mã khỏi đơn khi sửa đơn: luôn trả lượt (không xét release_on_cancel). Đơn không có lượt active → nil.
func Detach(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) error {
	_, err := release(ctx, tx, orderID, detachReason, false)
	return err
}

func release(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, reason string, honorFlag bool) (bool, error) {
	q := store.New(tx)
	c, r, ok, err := lockActive(ctx, q, orderID)
	if err != nil || !ok {
		return false, err
	}
	if honorFlag && !c.ReleaseOnCancel {
		return false, nil
	}
	if _, err := q.DecrementCustomerUse(ctx, store.DecrementCustomerUseParams{
		CouponID: c.ID, CustomerID: r.CustomerID,
	}); err != nil {
		return false, fmt.Errorf("trả lượt dùng mã của khách: %w", err)
	}
	n, err := q.ReleaseRedemption(ctx, store.ReleaseRedemptionParams{ReleaseReason: reason, ID: r.ID})
	if err != nil {
		return false, fmt.Errorf("trả lượt dùng mã của đơn: %w", err)
	}
	if n == 0 {
		return false, errRedemptionMoved // không xảy ra khi giữ khóa dòng mã; lùi để lượt của khách không lệch
	}
	if _, err := q.DecrementUsed(ctx, c.ID); err != nil {
		return false, fmt.Errorf("trả lượt dùng mã: %w", err)
	}
	return true, nil
}

// Reprice tính lại phần giảm của lượt mã đang hiệu lực của đơn orderID theo in (sửa đơn) và ghi lại. Đơn không có
// lượt active → (0, nil). Điều kiện của mã không còn đạt → *RejectError (bên gọi Detach hoặc lùi). Không đổi lượt.
func Reprice(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, in EvalInput) (discount int64, err error) {
	q := store.New(tx)
	c, r, ok, err := lockActive(ctx, q, orderID)
	if err != nil || !ok {
		return 0, err
	}
	discount, reason := Evaluate(c, in)
	if reason != "" {
		return 0, &RejectError{Reason: reason}
	}
	if _, err := q.SetRedemptionDiscount(ctx, store.SetRedemptionDiscountParams{Discount: discount, ID: r.ID}); err != nil {
		return 0, fmt.Errorf("ghi lại phần giảm của mã: %w", err)
	}
	return discount, nil
}

// lockActive tìm lượt active của đơn, khóa dòng mã của lượt (FOR UPDATE, trước mọi bảng lượt), rồi đọc lại lượt sau
// khi giữ khóa (mọi đường ghi lượt của một mã đều khóa dòng mã trước nên lượt đọc lại không đổi tới cuối tx). Không có
// lượt → ok = false.
func lockActive(ctx context.Context, q *store.Queries, orderID uuid.UUID) (
	c Coupon, r store.ActiveRedemptionByOrderRow, ok bool, err error,
) {
	r, err = q.ActiveRedemptionByOrder(ctx, orderID)
	for range lockRetries {
		if errors.Is(err, pgx.ErrNoRows) {
			return Coupon{}, r, false, nil
		}
		if err != nil {
			return Coupon{}, r, false, fmt.Errorf("đọc lượt mã của đơn: %w", err)
		}
		row, lerr := q.CouponByIDForUpdate(ctx, r.CouponID)
		if lerr != nil {
			return Coupon{}, r, false, fmt.Errorf("khóa mã giảm giá: %w", lerr)
		}
		var again store.ActiveRedemptionByOrderRow
		again, err = q.ActiveRedemptionByOrder(ctx, orderID)
		if err == nil && again.CouponID == r.CouponID {
			return couponFrom(store.CouponByCodeRow(row)), again, true, nil
		}
		r = again
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Coupon{}, r, false, nil // lượt vừa được trả ở lần đọc lại cuối
	}
	return Coupon{}, r, false, errRedemptionMoved
}

// CheckResult là kết quả kiểm mã cho form lên đơn (CouponCheck của hợp đồng API).
type CheckResult struct {
	OK            bool
	CouponID      uuid.UUID // uuid.Nil khi không có mã
	Discount      int64     // 0 khi bị từ chối
	Reason        Reason    // rỗng khi OK
	RemainingUses *int32    // lượt còn của mã (nil: không giới hạn, hoặc mã bị từ chối trước bước kiểm lượt)
}

// Check kiểm mã code cho đơn in mà không đổi gì, không khóa: Evaluate, rồi lượt còn lại của mã (hết → exhausted) và
// lượt của khách in.CustomerRootID (đủ → customer_limit; Nil → bỏ qua). Mã bị từ chối là kết quả (OK = false), không
// phải lỗi; kết quả có thể khác lúc Redeem nếu người khác dùng mã giữa hai lần.
func Check(ctx context.Context, tx pgx.Tx, code string, in EvalInput) (CheckResult, error) {
	q := store.New(tx)
	row, err := q.CouponByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckResult{Reason: ReasonNotFound}, nil
	}
	if err != nil {
		return CheckResult{}, fmt.Errorf("đọc mã giảm giá: %w", err)
	}
	c := couponFrom(row)
	res := CheckResult{CouponID: c.ID}
	discount, reason := Evaluate(c, in)
	if reason != "" {
		res.Reason = reason
		return res, nil
	}
	if c.MaxUses != nil {
		rem := max(*c.MaxUses-c.UsedCount, 0)
		res.RemainingUses = &rem
		if rem == 0 {
			res.Reason = ReasonExhausted
			return res, nil
		}
	}
	if c.PerCustomerLimit != nil && in.CustomerRootID != uuid.Nil {
		used, err := q.CustomerUsed(ctx, store.CustomerUsedParams{CouponID: c.ID, CustomerID: in.CustomerRootID})
		if err != nil {
			return CheckResult{}, fmt.Errorf("đọc lượt dùng mã của khách: %w", err)
		}
		if used >= *c.PerCustomerLimit {
			res.Reason = ReasonCustomerLimit
			return res, nil
		}
	}
	res.OK, res.Discount = true, discount
	return res, nil
}
