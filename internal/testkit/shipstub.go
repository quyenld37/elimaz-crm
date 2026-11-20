package testkit

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// stubShipmentSQL chèn vận đơn shipmentID (đã hủy, loại manual: không chặn unique "một vận đơn chưa hủy mỗi đơn")
// cho đơn orderID bằng tài khoản hãng giả "stub:<người bán>:<kho>" của người bán, kho của đơn (tạo nếu chưa có). Đơn
// không có hay vận đơn đã có → không chèn gì.
const stubShipmentSQL = `
WITH o AS (
    SELECT tenant_id, seller_id, warehouse_id, id FROM ord.orders
    WHERE id = @order_id AND NOT EXISTS (SELECT 1 FROM fulfillment.shipments WHERE id = @shipment_id)
), acc AS (
    INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id)
    SELECT tenant_id, seller_id, 'stub', 'Hãng giả (test)', 'stub:' || seller_id || ':' || warehouse_id, warehouse_id
    FROM o
    ON CONFLICT (tenant_id, carrier_key, external_account_ref) DO UPDATE SET name = EXCLUDED.name
    RETURNING id
)
INSERT INTO fulfillment.shipments
    (id, tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, status, cod_amount, cancel_kind)
SELECT @shipment_id, o.tenant_id, o.seller_id,
       'E' || to_char(now() AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYMMDD') || upper(substr(md5(@shipment_id::text), 1, 6)),
       o.id, 'stub', acc.id, 'cancelled', 0, 'manual'
FROM o, acc`

// StubShipment tạo một dòng fulfillment.shipments có mã shipmentID cho đơn orderID, để test của order (và module đọc
// đơn) gắn vận đơn hiện hành mà không qua fulfillment: ord.orders.current_shipment_id có khóa ngoại tới vận đơn thật
// (0300, M3). tx là của chủ bảng trong ngữ cảnh tenant của đơn (Staff.Exec). Gọi lại với cùng mã không đổi gì; đơn
// không có thì không chèn (gắn vận đơn vào đơn không có vẫn ra ErrNotFound như trước).
//
// Khi nào không dùng: test của fulfillment, cash hay đầu cuối cần vận đơn thật có trạng thái, tiền COD (dùng
// shiptest.Shop.Shipment); dòng này là vận đơn đã hủy, chỉ để thỏa khóa ngoại.
func StubShipment(t testing.TB, tx pgx.Tx, orderID, shipmentID uuid.UUID) {
	t.Helper()
	if _, err := tx.Exec(t.Context(), stubShipmentSQL, pgx.NamedArgs{"order_id": orderID, "shipment_id": shipmentID}); err != nil {
		t.Fatalf("testkit: vận đơn giả %s: %v", shipmentID, err)
	}
}
