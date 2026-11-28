package lists

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Overdue là một vận đơn giao xong quá hạn mà hãng chưa trả tiền COD: Days = số ngày tròn từ lúc giao xong.
type Overdue struct {
	ShipmentID, OrderID                 uuid.UUID
	OrderCode, CarrierKey, TrackingCode string
	DeliveredAt                         time.Time
	CarrierHolding                      int64
	Days                                int32
}

// OverdueList đọc tối đa limit vận đơn của người bán seller delivered trước now − days ngày còn carrier_holding > 0, theo mã vận đơn tăng
// dần sau mã after (nil: trang đầu).
func OverdueList(ctx context.Context, tx pgx.Tx, seller uuid.UUID, now time.Time, days int, after *uuid.UUID, limit int,
) ([]Overdue, error) {
	rows, err := store.New(tx).ListOverdue(ctx, store.ListOverdueParams{
		SellerID: seller, Cutoff: now.UTC().Add(-time.Duration(days) * day), AfterID: db.NullUUID(after),
		Lim: int32(min(max(limit, 1), maxLimit)),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc quá hạn chưa có tiền: %w", err)
	}
	out := make([]Overdue, 0, len(rows))
	for _, r := range rows {
		out = append(out, Overdue{
			ShipmentID: r.ID, OrderID: r.OrderID, OrderCode: r.OrderCode, CarrierKey: r.CarrierKey,
			TrackingCode: r.TrackingCode.String, DeliveredAt: r.DeliveredAt, CarrierHolding: r.CarrierHolding,
			Days: int32(min(now.Sub(r.DeliveredAt)/day, 1<<31-1)), //nolint:gosec // đã chặn
		})
	}
	return out, nil
}
