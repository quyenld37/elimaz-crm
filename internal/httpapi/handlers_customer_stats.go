package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Vùng customer_stats (kế hoạch M3, Task M3-21): mức "khách hay hoàn". Số nhận/hoàn là của M2 (customer.StatsFor);
// ngưỡng đọc từ cài đặt vận chuyển ở đây rồi chép sang customer.ReturnRisk (customer không nhập fulfillment).

// GetCustomerDeliveryStats (GET /api/v1/customers/{id}/delivery-stats, customer.read): số đơn giao xong, hoàn, tỷ lệ
// hoàn và mức rủi ro của gốc hiện tại của khách (CustomerPicker, khối gửi hãng). Cài đặt chưa lưu → ngưỡng mặc định.
// Khách không có hay của tenant khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerDeliveryStats(ctx context.Context, params gen.GetCustomerDeliveryStatsParams) (gen.GetCustomerDeliveryStatsRes, error) {
	var s customer.DeliveryStats
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		set, _, err := fulfillment.LoadSettings(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // fulfillment đã bọc
		}
		rr := set.ReturnRisk
		s, err = customer.DeliveryStatsFor(ctx, tx, params.ID, customer.ReturnRisk{
			WatchMinReturned: rr.WatchMinReturned, HighMinReturned: rr.HighMinReturned, HighRateBP: rr.HighRateBP,
		})
		return err //nolint:wrapcheck // sentinel của customer
	}); err != nil {
		return nil, fmt.Errorf("đọc mức hay hoàn của khách: %w", err)
	}
	return deliveryStatsOut(s), nil
}

// deliveryStatsOut đổi customer.DeliveryStats sang gen.DeliveryStats (hint bỏ trống khi rỗng).
func deliveryStatsOut(s customer.DeliveryStats) *gen.DeliveryStats {
	out := &gen.DeliveryStats{
		Delivered: int32(s.Delivered), Returned: int32(s.Returned), //nolint:gosec // số đơn của một khách, xa trần int32
		ReturnRateBp: int32(s.ReturnRateBP), Risk: gen.DeliveryStatsRisk(s.Risk), //nolint:gosec // 0–10.000
	}
	if s.Hint != "" {
		out.Hint = gen.NewOptString(s.Hint)
	}
	return out
}
