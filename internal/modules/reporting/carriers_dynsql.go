package reporting

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// carrierCounts: các cột số của một nhóm vận đơn (bảng hằng; đủ chín trạng thái vận đơn của M3, G12). Vận đơn hủy chỉ
// vào cột cancelled; phí, COD chỉ cộng vận đơn chưa hủy; tập rỗng → 0.
const carrierCounts = `count(*) FILTER (WHERE sh.status <> 'cancelled') AS created,
		count(*) FILTER (WHERE sh.status = 'cancelled') AS cancelled,
		count(*) FILTER (WHERE sh.status IN ('creating', 'awaiting_pickup', 'shipping', 'cancel_requested')) AS in_progress,
		count(*) FILTER (WHERE sh.status = 'returning') AS returning,
		count(*) FILTER (WHERE sh.status = 'delivery_failed') AS delivery_failed,
		count(*) FILTER (WHERE sh.status = 'delivered') AS delivered,
		count(*) FILTER (WHERE sh.status = 'returned') AS returned,
		coalesce(sum(coalesce(sh.fee_actual, sh.fee, 0)) FILTER (WHERE sh.status <> 'cancelled'), 0)::bigint AS fee_total,
		coalesce(sum(sh.cod_amount) FILTER (WHERE sh.status <> 'cancelled'), 0)::bigint AS cod_total`

// carrierSet dựng phần FROM … WHERE của tập vận đơn của báo cáo hãng: vận đơn của người bán s.SellerID tạo trong kỳ
// (khoảng UTC trên created_at, dùng chỉ mục shipments_created), nối đơn cùng người bán (ADR-29); không AllPages thì
// chỉ đơn của các Page được xem (đơn không gắn Page bị loại; không Page nào → tập rỗng). seller là chỗ giữ $n của
// người bán để câu ngoài dùng lại.
func carrierSet(s DataScope, period Period, b *sqlBuilder) (set, seller string, err error) {
	if s.SellerID == uuid.Nil {
		return "", "", fmt.Errorf("tập vận đơn thiếu người bán: %w", ErrBadParams)
	}
	if period.IsZero() || !period.Start.Before(period.End) {
		return "", "", fmt.Errorf("tập vận đơn thiếu kỳ: %w", ErrBadParams)
	}
	seller = b.Arg(s.SellerID)
	q := "fulfillment.shipments_v sh\n\tJOIN ord.orders_v o ON o.id = sh.order_id AND o.seller_id = " + seller +
		"\n\tWHERE sh.seller_id = " + seller +
		" AND sh.created_at >= " + b.Arg(period.Start) + " AND sh.created_at < " + b.Arg(period.End)
	if !s.AllPages {
		q += " AND o.channel_account_id = ANY(" + b.Arg(append([]uuid.UUID{}, s.PageIDs...)) + "::uuid[])" // nil → mảng rỗng, không NULL
	}
	return q, seller, nil
}

// carrierRows đọc một dòng mỗi tài khoản hãng có vận đơn trong tập (số đếm, phí, COD; chưa tính tỷ lệ, trung bình), sắp
// theo khóa hãng, tên tài khoản, mã tài khoản. Tên tài khoản đọc từ fulfillment.carrier_accounts_v cùng người bán.
func carrierRows(ctx context.Context, tx pgx.Tx, s DataScope, period Period) ([]CarrierRow, error) {
	b := &sqlBuilder{}
	set, seller, err := carrierSet(s, period, b)
	if err != nil {
		return nil, err
	}
	q := `SELECT g.carrier_key, g.carrier_account_id, coalesce(ca.name, ''), g.created, g.cancelled, g.in_progress,
	g.returning, g.delivery_failed, g.delivered, g.returned, g.fee_total, g.cod_total
FROM (
	SELECT sh.carrier_key, sh.carrier_account_id, ` + carrierCounts + `
	FROM ` + set + `
	GROUP BY sh.carrier_key, sh.carrier_account_id
) AS g
LEFT JOIN fulfillment.carrier_accounts_v ca ON ca.id = g.carrier_account_id AND ca.seller_id = ` + seller + `
ORDER BY g.carrier_key, ca.name, g.carrier_account_id`
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc báo cáo hãng: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CarrierRow, error) {
		var c CarrierRow
		err := r.Scan(&c.CarrierKey, &c.CarrierAccountID, &c.AccountName, &c.Created, &c.Cancelled, &c.InProgress,
			&c.Returning, &c.DeliveryFailed, &c.Delivered, &c.Returned, &c.FeeTotal, &c.CODTotal)
		return c, err //nolint:wrapcheck // CollectRows trả lỗi, bên ngoài bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng báo cáo hãng: %w", err)
	}
	return out, nil
}
