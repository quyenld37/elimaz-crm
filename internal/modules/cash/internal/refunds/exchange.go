package refunds

import (
	"context"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/events"
)

// handleExchange xử lý tiền của đổi hàng (phiếu exchange nhận xong, đơn thay thế order_type exchange vừa tạo): bù
// chênh lệch giữa đơn gốc và đơn thay thế. Task M3-30 chỉ tạo chỗ gọi; Task M3-25 viết thân hàm.
func handleExchange(context.Context, pgx.Tx, events.StoredEvent) error {
	return nil
}
