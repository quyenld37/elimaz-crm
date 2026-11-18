package order

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/state"
)

// NextReturnCode cho test gói order_test gọi hàm sinh mã phiếu trả (chỉ có trong test).
func NextReturnCode(ctx context.Context, tx pgx.Tx) (string, error) { return nextReturnCode(ctx, tx) }

// ApplyTransition cho test gói order_test gọi đường đổi trạng thái duy nhất (Task M2-16; task sau gọi qua hàm nghiệp
// vụ của mình).
func ApplyTransition(ctx context.Context, tx pgx.Tx, w Writer, o *Order, to, pending, reason string, a Actor,
	now time.Time,
) error {
	return w.applyTransition(ctx, tx, o, state.Status(to), pending, reason, a, now)
}
