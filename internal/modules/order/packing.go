package order

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// Trạng thái đóng gói của đơn (CHECK ord.orders.packing_status) — chỉ đi tới: none → picking → packed.
const (
	PackingNone    = "none"
	PackingPicking = "picking"
	PackingPacked  = "packed"
)

// Kết quả đóng gói của từng đơn (PackingResult.Result).
const (
	// PackingUpdated: đã đổi sang trạng thái mới.
	PackingUpdated = "updated"
	// PackingAlreadyFurther: đơn đã ở trạng thái này hay xa hơn (in lại, chọn lại không lùi; VC04, AT08).
	PackingAlreadyFurther = "already_further"
	// PackingNotAllowed: đơn không confirmed (chưa chốt, đã hủy, chờ hủy, đã giao).
	PackingNotAllowed = "not_allowed"
)

// PackingResult là kết quả đóng gói của một đơn.
type PackingResult struct {
	OrderID uuid.UUID
	Result  string
}

// packingRank là thứ tự đi tới của trạng thái đóng gói.
func packingRank(s string) int {
	return slices.Index([]string{PackingNone, PackingPicking, PackingPacked}, s)
}

// SetPackingStatus đưa các đơn orderIDs của người bán sellerID sang trạng thái đóng gói to (picking hay packed), trong
// tx của bên gọi (VC04). Khóa mọi đơn bằng một câu theo id tăng dần (ADR-46 nhóm 7; hai lệnh song song khác thứ tự không
// deadlock). Mỗi đơn (mã trùng tính một lần, kết quả theo id tăng dần): không confirmed → not_allowed; đã ở to hay xa
// hơn → already_further, không lỗi, không đổi; còn lại → updated: packing_status = to, version + 1, lịch sử confirmed
// → confirmed lý do packing_<to> với người đóng gói a, order.updated (seq = version mới), báo hiệu.
//
// Lỗi (cả lệnh không đổi gì): không đơn, to không phải picking/packed → ErrInvalidInput; một mã không có (hay của người
// bán, tenant khác) → ErrNotFound.
func (w Writer) SetPackingStatus(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, orderIDs []uuid.UUID, to string,
	a Actor, now time.Time,
) ([]PackingResult, error) {
	if len(orderIDs) == 0 || (to != PackingPicking && to != PackingPacked) {
		return nil, fmt.Errorf("%w: đóng gói %d đơn sang %q", ErrInvalidInput, len(orderIDs), to)
	}
	if err := w.wired(); err != nil {
		return nil, err
	}
	ids := slices.Compact(slices.SortedFunc(slices.Values(orderIDs), func(x, y uuid.UUID) int {
		return slices.Compare(x[:], y[:])
	}))
	q := store.New(tx)
	rows, err := q.LockOrdersForPacking(ctx, store.LockOrdersForPackingParams{SellerID: sellerID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("khóa đơn đóng gói: %w", err)
	}
	if len(rows) != len(ids) {
		return nil, ErrNotFound
	}
	out := make([]PackingResult, len(rows))
	for i, r := range rows {
		res, err := w.pack(ctx, tx, q, r, to, a, now)
		if err != nil {
			return nil, err
		}
		out[i] = PackingResult{OrderID: r.ID, Result: res}
	}
	return out, nil
}

// pack áp trạng thái đóng gói to lên đơn đã khóa r; trả kết quả của đơn.
func (w Writer) pack(ctx context.Context, tx pgx.Tx, q *store.Queries, r store.LockOrdersForPackingRow, to string,
	a Actor, now time.Time,
) (string, error) {
	switch {
	case r.Status != StatusConfirmed:
		return PackingNotAllowed, nil
	case packingRank(r.PackingStatus) >= packingRank(to):
		return PackingAlreadyFurther, nil
	}
	version, err := q.SetPackingStatus(ctx, store.SetPackingStatusParams{
		PackingStatus: to, Now: now, SellerID: r.SellerID, ID: r.ID, Version: r.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrVersionConflict // đã khóa: không xảy ra
	}
	if err != nil {
		return "", fmt.Errorf("ghi trạng thái đóng gói: %w", err)
	}
	a.TenantID = r.TenantID
	if err := insertHistory(ctx, tx, r.ID, r.Status, r.PendingReason, r.Status, r.PendingReason, "packing_"+to, a,
		now); err != nil {
		return "", err
	}
	o := Order{
		ID: r.ID, SellerID: r.SellerID, CustomerID: r.CustomerID, Status: r.Status, PendingReason: r.PendingReason,
		CurrentShipmentID: uuidPtr(r.CurrentShipmentID), AddressComplete: r.AddressComplete, Version: version,
	}
	if err := w.emit(ctx, tx, r.TenantID, o, TopicOrderUpdated, version); err != nil {
		return "", err
	}
	if err := w.signal(ctx, tx, r.TenantID, o); err != nil {
		return "", err
	}
	return PackingUpdated, nil
}
