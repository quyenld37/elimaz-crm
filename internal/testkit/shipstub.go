package testkit

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// stubShipmentSQL chèn vận đơn shipmentID (đã hủy, loại manual: không chặn unique "một vận đơn chưa hủy mỗi đơn")
// cho đơn orderID bằng tài khoản hãng giả "stub:<người bán>:<kho>" của người bán, kho của đơn (tạo nếu chưa có; có
// rồi thì chỉ đọc, không ghi lại dòng ở mỗi lần gọi), mã 'E' + yyMMdd giờ Việt Nam + suffix. Đơn không có, vận đơn đã
// có, mã đã có vận đơn khác giữ (chỉ mục unique shipments_code_key bắt, kể cả của transaction song song, không phụ
// thuộc RLS) hay tài khoản vừa do transaction song song tạo (câu này không thấy dòng đó) → không chèn gì; bên gọi thử
// lại bằng câu mới.
const stubShipmentSQL = `
WITH o AS (
    SELECT tenant_id, seller_id, warehouse_id, id FROM ord.orders
    WHERE id = @order_id AND NOT EXISTS (SELECT 1 FROM fulfillment.shipments WHERE id = @shipment_id)
), old_acc AS (
    SELECT a.id FROM fulfillment.carrier_accounts a, o
    WHERE a.tenant_id = o.tenant_id AND a.carrier_key = 'stub'
      AND a.external_account_ref = 'stub:' || o.seller_id || ':' || o.warehouse_id
), new_acc AS (
    INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id)
    SELECT tenant_id, seller_id, 'stub', 'Hãng giả (test)', 'stub:' || seller_id || ':' || warehouse_id, warehouse_id
    FROM o WHERE NOT EXISTS (SELECT 1 FROM old_acc)
    ON CONFLICT (tenant_id, carrier_key, external_account_ref) DO NOTHING
    RETURNING id
), acc AS (
    SELECT id FROM old_acc UNION ALL SELECT id FROM new_acc
)
INSERT INTO fulfillment.shipments
    (id, tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, status, cod_amount, cancel_kind)
SELECT @shipment_id, o.tenant_id, o.seller_id,
       'E' || to_char(now() AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYMMDD') || @suffix,
       o.id, 'stub', acc.id, 'cancelled', 0, 'manual'
FROM o, acc
ON CONFLICT (tenant_id, code) DO NOTHING`

// stubPendingSQL: còn phải chèn vận đơn shipmentID cho đơn orderID (đơn có, vận đơn chưa có) — lần chèn vừa rồi không
// ra dòng nào nghĩa là mã đã có vận đơn khác giữ (hay tài khoản hãng giả vừa được transaction song song tạo).
const stubPendingSQL = `SELECT EXISTS (SELECT 1 FROM ord.orders WHERE id = @order_id)
    AND NOT EXISTS (SELECT 1 FROM fulfillment.shipments WHERE id = @shipment_id)`

const (
	// stubCodeAlphabet là bảng chữ base32 Crockford của mã vận đơn thật (fulfillment.NewShipmentCode), nằm trong
	// [0-9A-Z] của CHECK ở 0118.
	stubCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// stubCodeAttempts: số mã ứng viên thử lần lượt. Mỗi mã 30 bit; với 20.000 vận đơn giả trong một database và một
	// ngày, xác suất cả 8 ứng viên đều đã bị giữ cỡ (2·10⁻⁵)⁸.
	stubCodeAttempts = 8
)

// stubCodeSuffix trả 6 ký tự Crockford base32 (30 bit đầu của SHA-256 của 16 byte id nối số attempt viết thập phân) — ứng viên thứ attempt của mã vận
// đơn giả id. Cố định theo (id, attempt): gọi lại StubShipment cùng mã ra cùng ứng viên.
func stubCodeSuffix(id uuid.UUID, attempt int) string {
	h := sha256.Sum256(strconv.AppendInt(id[:], int64(attempt), 10))
	v := binary.BigEndian.Uint32(h[:4])
	out := make([]byte, 6)
	for i := range out {
		out[i] = stubCodeAlphabet[(v>>(5*(len(out)-1-i)))&31]
	}
	return string(out)
}

// StubShipment tạo một dòng fulfillment.shipments có mã shipmentID cho đơn orderID, để test của order (và module đọc
// đơn) gắn vận đơn hiện hành mà không qua fulfillment: ord.orders.current_shipment_id có khóa ngoại tới vận đơn thật
// (0300, M3). tx là của chủ bảng trong ngữ cảnh tenant của đơn (Staff.Exec). Gọi lại với cùng mã không đổi gì; đơn
// không có thì không chèn (gắn vận đơn vào đơn không có vẫn ra ErrNotFound như trước). Mã vận đơn (code) là ứng viên
// đầu tiên trong stubCodeAttempts ứng viên chưa có vận đơn nào giữ, nên nhiều vận đơn giả trong một database không
// trùng shipments_code_key.
//
// Khi nào không dùng: test của fulfillment, cash hay đầu cuối cần vận đơn thật có trạng thái, tiền COD (dùng
// shiptest.Shop.Shipment); dòng này là vận đơn đã hủy, chỉ để thỏa khóa ngoại.
func StubShipment(t testing.TB, tx pgx.Tx, orderID, shipmentID uuid.UUID) {
	t.Helper()
	args := pgx.NamedArgs{"order_id": orderID, "shipment_id": shipmentID}
	for attempt := range stubCodeAttempts {
		args["suffix"] = stubCodeSuffix(shipmentID, attempt)
		tag, err := tx.Exec(t.Context(), stubShipmentSQL, args)
		if err != nil {
			t.Fatalf("testkit: vận đơn giả %s: %v", shipmentID, err)
		}
		if tag.RowsAffected() == 1 {
			return
		}
		var pending bool
		if err := tx.QueryRow(t.Context(), stubPendingSQL, args).Scan(&pending); err != nil {
			t.Fatalf("testkit: kiểm vận đơn giả %s: %v", shipmentID, err)
		}
		if !pending {
			return
		}
	}
	t.Fatalf("testkit: vận đơn giả %s: cả %d mã ứng viên đều đã có vận đơn khác giữ", shipmentID, stubCodeAttempts)
}
