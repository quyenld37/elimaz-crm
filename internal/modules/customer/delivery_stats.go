package customer

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Mức "khách hay hoàn" khi lên đơn (kế hoạch M3, Task M3-21; DH03, B14, C3). Số đơn nhận, số đơn hoàn và tỷ lệ hoàn là
// của M2 (consumer customer_stats, StatsFor); M3 không tính lại, không ghi gì, chỉ phân mức theo ngưỡng của cài đặt
// vận chuyển mà bên gọi truyền vào (module customer không nhập fulfillment).

// Mức rủi ro hoàn của DeliveryStats.Risk.
const (
	RiskNone  = "none"
	RiskWatch = "watch"
	RiskHigh  = "high"
)

// Câu gợi ý hiện khi lên đơn, theo mức (none: rỗng).
const (
	HintWatch = "Khách có đơn hoàn"
	HintHigh  = "Khách hay hoàn: xin cọc hoặc gọi xác nhận trước khi gửi"
)

// ReturnRisk là ngưỡng mức hay hoàn (bản của customer; handler chép từ fulfillment.ReturnRisk): watch từ
// WatchMinReturned đơn hoàn; high từ HighMinReturned đơn hoàn và tỷ lệ hoàn ≥ HighRateBP phần vạn.
type ReturnRisk struct {
	WatchMinReturned int
	HighMinReturned  int
	HighRateBP       int
}

// DeliveryStats là số nhận/hoàn của gốc (M2) kèm mức rủi ro và câu gợi ý.
type DeliveryStats struct {
	Delivered, Returned int
	// ReturnRateBP: tỷ lệ hoàn phần vạn theo công thức của M2; 0 khi chưa có đơn kết thúc.
	ReturnRateBP int
	// Risk: RiskNone, RiskWatch hay RiskHigh.
	Risk string
	// Hint: rỗng, HintWatch hay HintHigh.
	Hint string
}

// DeliveryStatsFor đọc số nhận/hoàn mà M2 đã tính ở hồ sơ đại diện (gốc hiện tại) của customerID qua StatsFor, rồi
// phân mức: high khi Returned ≥ HighMinReturned và ReturnRateBP ≥ HighRateBP; watch khi Returned ≥ WatchMinReturned;
// ngược lại none (chưa có đơn hoàn luôn none). Chỉ đọc. Không có (hay của tenant khác) → ErrNotFound.
func DeliveryStatsFor(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, rr ReturnRisk) (DeliveryStats, error) {
	s, err := StatsFor(ctx, tx, customerID)
	if err != nil {
		return DeliveryStats{}, err
	}
	return classify(s, rr), nil
}

func classify(s Stats, rr ReturnRisk) DeliveryStats {
	out := DeliveryStats{Delivered: s.DeliveredCount, Returned: s.ReturnedCount, ReturnRateBP: s.ReturnRateBP, Risk: RiskNone}
	switch {
	case out.Returned == 0: // ngưỡng luôn > 0 (fulfillment kiểm); chặn thêm khi bên gọi quên truyền ngưỡng
	case out.Returned >= rr.HighMinReturned && out.ReturnRateBP >= rr.HighRateBP:
		out.Risk, out.Hint = RiskHigh, HintHigh
	case out.Returned >= rr.WatchMinReturned:
		out.Risk, out.Hint = RiskWatch, HintWatch
	}
	return out
}
