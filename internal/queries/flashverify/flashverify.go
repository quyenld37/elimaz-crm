// Package flashverify kiểm một đợt flash sale sau thử tải hay khi nghi lệch (kế hoạch M5, Task 219 / M5-36; lệnh
// `crm verify-flash --sale <id>`). Chỉ đọc view _v đã công bố (docs/views.md), không nhập module nào (tests/arch luật
// f), không ghi gì. Phép kiểm (g) — held của số dư bằng giữ hàng held + đơn vị suất còn trống — là inventory.Reconcile,
// lệnh gọi riêng. Khi nào không dùng: đường bán hàng (chỉ để kiểm, chạy khi tải đã dừng; đọc cả đợt).
package flashverify

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries/flashverify/internal/store"
)

// ErrSaleNotFound báo không có đợt với mã này ở tenant của transaction.
var ErrSaleNotFound = errors.New("flashverify: không có đợt flash sale")

// Check là tên một phép kiểm (chữ cái theo kế hoạch M5-36).
type Check string

// Các phép kiểm. "Đơn còn hiệu lực" là đơn chưa hủy.
const (
	// CheckActiveLines (a): tổng số lượng dòng flash của đơn còn hiệu lực = số đơn vị held + sold của các đơn đó; đơn đã
	// hủy không còn đơn vị held, và chỉ còn đơn vị sold khi đợt đã đóng (05 mục 7.4).
	CheckActiveLines Check = "a_active_lines"
	// CheckLineUnits (b): không dòng đơn nào giữ (held/sold) nhiều đơn vị hơn số lượng của dòng.
	CheckLineUnits Check = "b_line_units"
	// CheckClaims (c): tổng số đã lấy (claims) = held + sold; đợt đã đóng thì ≥ (đơn hủy sau khi đóng không trừ lại).
	CheckClaims Check = "c_claims"
	// CheckCustomerLimit (d): không khách nào vượt giới hạn mỗi khách (theo claims hay theo dòng đơn còn hiệu lực).
	CheckCustomerLimit Check = "d_customer_limit"
	// CheckNegative (e): không số nào âm: claims, on_hand, held, available của số dư của món.
	CheckNegative Check = "e_negative"
	// CheckClosedAvailable (f): đợt đã đóng không còn đơn vị available.
	CheckClosedAvailable Check = "f_closed_available"
	// CheckHeld (g): held của số dư = giữ hàng held + đơn vị suất còn trống (inventory.Reconcile; lệnh gọi).
	CheckHeld Check = "g_held"
	// CheckExternalRef (h): không hai đơn có dòng flash của đợt cùng external_ref.
	CheckExternalRef Check = "h_external_ref"
)

// Failure là một mục sai: món (uuid.Nil với CheckExternalRef), số đúng, số thấy, mô tả (không dữ liệu cá nhân).
type Failure struct {
	Check            Check
	ItemID           uuid.UUID
	Expected, Actual int64
	Detail           string
}

// ItemCount là số đếm của một món: số suất, đơn vị theo trạng thái, tổng đã lấy, tổng dòng flash của đơn còn hiệu lực.
type ItemCount struct {
	ItemID, VariantID, WarehouseID  uuid.UUID
	Quantity, PerCustomerLimit      int64
	Available, Held, Sold, Released int64
	Claimed, ActiveQty              int64
}

// Report là kết quả kiểm một đợt.
type Report struct {
	SaleID   uuid.UUID
	Status   string
	Items    []ItemCount
	Failures []Failure
}

// OK báo không có mục sai.
func (r Report) OK() bool { return len(r.Failures) == 0 }

// Verify chạy các phép kiểm a–f, h của đợt saleID trong tx của bên gọi (tenancy.WithTenant; nên REPEATABLE READ để
// mọi câu cùng một ảnh chụp). Đợt không có → ErrSaleNotFound.
func Verify(ctx context.Context, tx pgx.Tx, saleID uuid.UUID) (Report, error) {
	q := store.New(tx)
	sale, err := q.SaleStatus(ctx, saleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, fmt.Errorf("%s: %w", saleID, ErrSaleNotFound)
	}
	if err != nil {
		return Report{}, fmt.Errorf("đọc đợt flash sale: %w", err)
	}
	r := Report{SaleID: sale.ID, Status: sale.Status, Items: []ItemCount{}, Failures: []Failure{}}
	closed := sale.Status == "closed"
	items, err := q.ItemCounts(ctx, saleID)
	if err != nil {
		return Report{}, fmt.Errorf("đếm đơn vị suất của đợt: %w", err)
	}
	for _, it := range items {
		r.Items = append(r.Items, ItemCount{
			ItemID: it.ItemID, VariantID: it.VariantID, WarehouseID: it.WarehouseID, Quantity: it.Quantity,
			PerCustomerLimit: it.PerCustomerLimit, Available: it.Available, Held: it.Held, Sold: it.Sold,
			Released: it.Released, Claimed: it.Claimed, ActiveQty: it.ActiveQty,
		})
		r.Failures = append(r.Failures, itemFailures(it, closed)...)
	}
	if err := r.addQueried(ctx, q, saleID); err != nil {
		return Report{}, err
	}
	return r, nil
}

// itemFailures là các mục sai a, c, e (claims), f của một món.
func itemFailures(it store.ItemCountsRow, closed bool) []Failure {
	var out []Failure
	add := func(c Check, exp, act int64, detail string) {
		out = append(out, Failure{Check: c, ItemID: it.ItemID, Expected: exp, Actual: act, Detail: detail})
	}
	if it.ActiveQty != it.ActiveUnits {
		add(CheckActiveLines, it.ActiveQty, it.ActiveUnits,
			"tổng số lượng dòng flash của đơn còn hiệu lực khác số đơn vị held + sold của các đơn đó")
	}
	if it.CancelledHeld != 0 {
		add(CheckActiveLines, 0, it.CancelledHeld, "đơn đã hủy còn giữ đơn vị held")
	}
	if !closed && it.CancelledSold != 0 {
		add(CheckActiveLines, 0, it.CancelledSold, "đợt chưa đóng mà đơn đã hủy còn đơn vị sold")
	}
	switch hs := it.Held + it.Sold; {
	case !closed && it.Claimed != hs:
		add(CheckClaims, hs, it.Claimed, "tổng số đã lấy của khách khác số đơn vị held + sold")
	case closed && it.Claimed < hs:
		add(CheckClaims, hs, it.Claimed, "đợt đã đóng: tổng số đã lấy của khách nhỏ hơn số đơn vị held + sold")
	}
	if it.MinClaim < 0 {
		add(CheckNegative, 0, it.MinClaim, "số đã lấy của một khách âm")
	}
	if closed && it.Available != 0 {
		add(CheckClosedAvailable, 0, it.Available, "đợt đã đóng còn đơn vị available")
	}
	return out
}

// addQueried thêm các mục sai b, d, e (số dư), h (mỗi phép một câu).
func (r *Report) addQueried(ctx context.Context, q *store.Queries, saleID uuid.UUID) error {
	lines, err := q.LineUnitsOver(ctx, saleID)
	if err != nil {
		return fmt.Errorf("đối chiếu đơn vị với dòng đơn: %w", err)
	}
	for _, l := range lines {
		r.Failures = append(r.Failures, Failure{
			Check: CheckLineUnits, ItemID: l.FlashSaleItemID, Expected: l.Quantity, Actual: l.Units,
			Detail: "dòng đơn " + l.OrderItemID.String() + " giữ nhiều đơn vị hơn số lượng của dòng",
		})
	}
	over, err := q.CustomersOverLimit(ctx, saleID)
	if err != nil {
		return fmt.Errorf("đối chiếu giới hạn mỗi khách: %w", err)
	}
	for _, c := range over {
		r.Failures = append(r.Failures, Failure{
			Check: CheckCustomerLimit, ItemID: c.FlashSaleItemID, Expected: c.PerCustomerLimit, Actual: c.Qty,
			Detail: "khách " + c.CustomerID.String() + " vượt giới hạn mỗi khách",
		})
	}
	bals, err := q.ItemBalances(ctx, saleID)
	if err != nil {
		return fmt.Errorf("đọc số dư của món: %w", err)
	}
	for _, b := range bals {
		for _, n := range []struct {
			name string
			v    int64
		}{{"on_hand", b.OnHand}, {"held", b.Held}, {"available", b.Available}} {
			if n.v < 0 {
				r.Failures = append(r.Failures, Failure{
					Check: CheckNegative, ItemID: b.ItemID, Actual: n.v, Detail: "số dư " + n.name + " của món âm",
				})
			}
		}
	}
	dups, err := q.DuplicateExternalRefs(ctx, saleID)
	if err != nil {
		return fmt.Errorf("tìm mã đơn website trùng: %w", err)
	}
	for _, d := range dups {
		r.Failures = append(r.Failures, Failure{
			Check: CheckExternalRef, Expected: 1, Actual: d.Orders, Detail: "mã đơn website " + d.ExternalRef + " có nhiều đơn",
		})
	}
	return nil
}
