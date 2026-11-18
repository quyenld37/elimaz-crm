package order

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// NextReturnCode cho test gói order_test gọi hàm sinh mã phiếu trả (chỉ có trong test).
func NextReturnCode(ctx context.Context, tx pgx.Tx) (string, error) { return nextReturnCode(ctx, tx) }
