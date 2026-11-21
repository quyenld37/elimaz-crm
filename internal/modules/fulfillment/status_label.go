package fulfillment

import (
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
)

// StatusLabel là nhãn tiếng Việt của vận đơn hãng carrierKey ở trạng thái status, trạng thái con subStatus — cùng nhãn
// với ShipmentView.Label của getShipment (vận đơn tự giao chờ giao: "Tự giao"; mã lạ giữ nguyên). Cho lớp ngoài module
// hiện vận đơn đọc từ view fulfillment.shipments_v (danh sách đơn, Task M3-28).
func StatusLabel(carrierKey, status, subStatus string) string {
	st := shipstate.Status(status)
	return manual.Label(carrierKey, st, shipstate.Display(st, subStatus, ""))
}
