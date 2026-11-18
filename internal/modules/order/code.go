package order

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// nextCode là chỗ duy nhất sinh mã đơn (Q26): 'DH' || lpad(nextval('ord.order_code_seq'), 7, '0'). Sequence không
// lùi theo transaction nên mã có thể nhảy số; không bao giờ trùng.
func nextCode(ctx context.Context, tx pgx.Tx) (string, error) {
	code, err := store.New(tx).NextOrderCode(ctx)
	if err != nil {
		return "", fmt.Errorf("sinh mã đơn: %w", err)
	}
	return code, nil
}

// nextReturnCode là chỗ duy nhất sinh mã phiếu trả (kế hoạch M2, Task M2-05): 'TH' || lpad(nextval(
// 'ord.return_code_seq'), 7, '0'). Cùng cách của nextCode: không khóa dòng, có thể nhảy số, không trùng.
func nextReturnCode(ctx context.Context, tx pgx.Tx) (string, error) {
	code, err := store.New(tx).NextReturnCode(ctx)
	if err != nil {
		return "", fmt.Errorf("sinh mã phiếu trả: %w", err)
	}
	return code, nil
}

// Codes trả mã đơn của các đơn ids thuộc người bán sellerID (ADR-29); id không có, của người bán hay tenant khác bị bỏ.
// Dùng cho danh sách lượt dùng mã giảm giá (Task M2-14): chỉ mã đơn, không nội dung.
func Codes(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := store.New(tx).OrderCodes(ctx, store.OrderCodesParams{SellerID: sellerID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("đọc mã đơn: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}
