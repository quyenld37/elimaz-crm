package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PickingLine là một dòng phiếu nhặt hàng: một biến thể gộp qua mọi đơn được chọn (Task M3-16, VC04).
type PickingLine struct {
	SKU, Name, Variant string
	Qty                int
	OrderCodes         []string
}

// PickingOrder là một vận đơn được chọn kèm đơn của nó: không tên, SĐT, địa chỉ người nhận. WarehouseID là kho của đơn
// (phạm vi quyền của bên gọi, không trả cho client).
type PickingOrder struct {
	ShipmentID, OrderID, WarehouseID uuid.UUID
	Code, ShipmentCode, TrackingCode string
	Carrier                          string
	ItemCount                        int
	CODAmount                        int64
}

// PickingList là phiếu nhặt hàng của các vận đơn được chọn.
type PickingList struct {
	Lines  []PickingLine
	Orders []PickingOrder
}

// pickingOrdersSQL, pickingLinesSQL: câu cố định (không dựng lúc chạy); nằm ở đây vì gói queries không có khối sqlc.
// Dòng đơn gộp theo đơn khác nhau của các vận đơn (hai vận đơn cùng đơn không đếm hàng hai lần), theo biến thể.
const (
	pickingOrdersSQL = `
SELECT s.id, o.id, o.warehouse_id, o.code, s.code, coalesce(s.tracking_code, ''), s.carrier_key, s.cod_amount,
	(SELECT coalesce(sum(i.quantity), 0) FROM ord.order_items_v AS i WHERE i.order_id = o.id)::int
FROM fulfillment.shipments_v AS s
JOIN ord.orders_v AS o ON o.id = s.order_id
WHERE s.id = ANY($1::uuid[])
ORDER BY o.code, s.code`
	pickingLinesSQL = `
SELECT coalesce(v.sku, min(i.sku_snapshot)), coalesce(v.product_name, min(i.name_snapshot)),
	concat_ws(' / ', nullif(v.color, ''), nullif(v.size, '')), sum(i.quantity)::int,
	array_agg(DISTINCT o.code ORDER BY o.code)
FROM ord.order_items_v AS i
JOIN ord.orders_v AS o ON o.id = i.order_id
LEFT JOIN catalog.variants_v AS v ON v.variant_id = i.variant_id
WHERE i.order_id IN (SELECT s.order_id FROM fulfillment.shipments_v AS s WHERE s.id = ANY($1::uuid[]))
GROUP BY i.variant_id, v.sku, v.product_name, v.color, v.size
ORDER BY 1, 2`
	orderWarehousesSQL = `
SELECT o.id, o.warehouse_id
FROM ord.orders_v AS o
WHERE o.id = ANY($1::uuid[])`
)

// Picking trả phiếu nhặt hàng của shipmentIDs, đọc qua fulfillment.shipments_v, ord.orders_v, ord.order_items_v,
// catalog.variants_v trong tenant của tx (tenancy.WithTenant): dòng hàng gộp theo biến thể (SKU, tên mẫu, màu / size
// hiện tại; biến thể không còn → bản chụp của dòng đơn), theo SKU, kèm mã các đơn có nó; một dòng mỗi vận đơn (mã đơn,
// mã vận đơn, mã hãng, hãng, số món, COD, kho của đơn), theo mã đơn. Mã không có (hay tenant khác) không có trong
// Orders: bên gọi so số dòng với số mã để biết thiếu.
func Picking(ctx context.Context, tx pgx.Tx, shipmentIDs []uuid.UUID) (PickingList, error) {
	out := PickingList{Lines: []PickingLine{}, Orders: []PickingOrder{}}
	if len(shipmentIDs) == 0 {
		return out, nil
	}
	if err := scanEach(ctx, tx, pickingOrdersSQL, shipmentIDs, func(r pgx.Rows) error {
		var o PickingOrder
		var items int32
		if err := r.Scan(&o.ShipmentID, &o.OrderID, &o.WarehouseID, &o.Code, &o.ShipmentCode, &o.TrackingCode,
			&o.Carrier, &o.CODAmount, &items); err != nil {
			return err //nolint:wrapcheck // bọc ở bên gọi
		}
		o.ItemCount = int(items)
		out.Orders = append(out.Orders, o)
		return nil
	}); err != nil {
		return PickingList{}, fmt.Errorf("đọc vận đơn của phiếu nhặt: %w", err)
	}
	if err := scanEach(ctx, tx, pickingLinesSQL, shipmentIDs, func(r pgx.Rows) error {
		var l PickingLine
		var sku, name pgtype.Text
		var qty int32
		if err := r.Scan(&sku, &name, &l.Variant, &qty, &l.OrderCodes); err != nil {
			return err //nolint:wrapcheck // bọc ở bên gọi
		}
		l.SKU, l.Name, l.Qty = sku.String, name.String, int(qty)
		out.Lines = append(out.Lines, l)
		return nil
	}); err != nil {
		return PickingList{}, fmt.Errorf("đọc dòng hàng của phiếu nhặt: %w", err)
	}
	return out, nil
}

// OrderWarehouses trả kho của từng đơn trong orderIDs (ord.orders_v, tenant của tx); mã không có không có trong kết
// quả.
func OrderWarehouses(ctx context.Context, tx pgx.Tx, orderIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := make(map[uuid.UUID]uuid.UUID, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	if err := scanEach(ctx, tx, orderWarehousesSQL, orderIDs, func(r pgx.Rows) error {
		var id, wh uuid.UUID
		if err := r.Scan(&id, &wh); err != nil {
			return err //nolint:wrapcheck // bọc ở bên gọi
		}
		out[id] = wh
		return nil
	}); err != nil {
		return nil, fmt.Errorf("đọc kho của đơn: %w", err)
	}
	return out, nil
}
