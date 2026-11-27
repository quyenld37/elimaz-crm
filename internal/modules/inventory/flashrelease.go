package inventory

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Đơn vị suất flash sale theo chuyển trạng thái của đơn (kế hoạch M5, Task 202 / M5-19; 05 mục 7.4). Chạy trong tx
// tenant của bên gọi (order đã khóa đơn, ADR-46 nhóm 7). Không phát báo hiệu: order.cancelled của đơn đã đủ cho
// consumer webcache.

// ReleaseFlashUnits trả các đơn vị suất của dòng đơn orderItemID khi đơn bị hủy, trả số đơn vị đã đổi:
//
//   - reopen (đợt còn mở): khóa các dòng giữ hàng held của dòng đơn theo id, chúng → released lý do reason mà số dư
//     không đổi (suất quay lại phần held của đợt); đơn vị held/sold → available, bỏ dòng đơn.
//   - !reopen (đợt đã đóng): bên gọi đã nhả dòng giữ hàng bằng ReleaseHeldByItems (held −, cùng lần khóa số dư theo
//     biến thể tăng dần với các dòng khác của lô); ở đây chỉ đơn vị held → released, bỏ dòng đơn. Đơn vị sold giữ
//     nguyên.
//
// reason sai mẫu → ErrInvalidInput.
func ReleaseFlashUnits(ctx context.Context, tx pgx.Tx, orderItemID uuid.UUID, reason string, reopen bool) (int, error) {
	if orderItemID == uuid.Nil || !reasonRe.MatchString(reason) {
		return 0, fmt.Errorf("%w: nhả đơn vị suất", ErrInvalidInput)
	}
	q := store.New(tx)
	if !reopen {
		n, err := q.ReleaseHeldFlashUnits(ctx, orderItemID)
		if err != nil {
			return 0, fmt.Errorf("trả đơn vị suất về tồn: %w", err)
		}
		return int(n), nil
	}
	ids, err := q.LockHeldReservationsOfItem(ctx, orderItemID)
	if err != nil {
		return 0, fmt.Errorf("khóa giữ hàng của dòng đơn flash: %w", err)
	}
	if len(ids) > 0 {
		n, err := q.ReleaseReservationsKeepBalance(ctx, store.ReleaseReservationsKeepBalanceParams{Reason: reason, Ids: ids})
		if err != nil {
			return 0, fmt.Errorf("nhả giữ hàng của dòng đơn flash: %w", err)
		}
		if n != int64(len(ids)) {
			return 0, fmt.Errorf("%w: nhả %d/%d giữ hàng flash", ErrStateConflict, n, len(ids))
		}
	}
	n, err := q.ReopenFlashUnits(ctx, orderItemID)
	if err != nil {
		return 0, fmt.Errorf("trả đơn vị suất về đợt: %w", err)
	}
	return int(n), nil
}

// MarkFlashSold đổi đơn vị held của dòng đơn orderItemID sang sold (đơn nháp hoàn tất; giữ hàng, số dư không đổi).
// Không đơn vị held nào → ErrStateConflict.
func MarkFlashSold(ctx context.Context, tx pgx.Tx, orderItemID uuid.UUID) error {
	n, err := store.New(tx).MarkFlashUnitsSold(ctx, orderItemID)
	if err != nil {
		return fmt.Errorf("bán đơn vị suất: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: dòng đơn không có đơn vị suất đang giữ", ErrStateConflict)
	}
	return nil
}
