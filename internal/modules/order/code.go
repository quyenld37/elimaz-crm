package order

import (
	"context"
	"fmt"

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
