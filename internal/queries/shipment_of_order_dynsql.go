package queries

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// trackingOfOrderSQL: mã vận đơn của vận đơn hiện hành của đơn $1 (ord.orders_v.current_shipment_id →
// fulfillment.shipments_v.id); vận đơn chưa có mã (đang tạo) không tính.
const trackingOfOrderSQL = `SELECT s.tracking_code
FROM ord.orders_v AS o
JOIN fulfillment.shipments_v AS s ON s.id = o.current_shipment_id
WHERE o.id = $1
  AND s.tracking_code IS NOT NULL`

// TrackingCodeOfOrder trả mã vận đơn của vận đơn hiện hành của đơn orderID (Task 162, biến {ma_van_don} của mẫu trả
// lời; conversation không được gọi fulfillment nên đọc qua view ở đây). ok = false khi đơn không có (hay của tenant
// khác), đơn chưa có vận đơn hiện hành hoặc vận đơn hiện hành chưa có mã. Chạy trong tenancy.WithTenant.
func TrackingCodeOfOrder(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (code string, ok bool, err error) {
	err = tx.QueryRow(ctx, trackingOfOrderSQL, orderID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("đọc mã vận đơn của đơn: %w", err)
	}
	return code, true, nil
}
