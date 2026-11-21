// Package partial là quyết định thuần của lần ghi giao một phần (kế hoạch M3, Task M3-26; TD09, Q-15): vận đơn có được
// ghi không, dòng đơn nào đã giao bao nhiêu, phần nào còn lại đi luồng hoàn, tiền thu có trong khoảng COD không.
// Không database; fulfillment gọi dưới khóa đơn → vận đơn.
package partial

import (
	"slices"

	"github.com/google/uuid"
)

// Refusal là lý do từ chối (OK: nhận).
type Refusal string

// Các lý do từ chối, xét theo thứ tự: trạng thái, số lượng, tiền.
const (
	OK Refusal = ""
	// RefuseStatus: vận đơn không ở trạng thái ghi được (xem Decide).
	RefuseStatus Refusal = "status"
	// RefuseQty: dòng thiếu, lạ, trùng, số âm hay lớn hơn số mua, hoặc tổng giao 0 hay bằng tổng mua (giao đủ đi đường
	// thường).
	RefuseQty Refusal = "qty"
	// RefuseCOD: tiền thu âm hay lớn hơn COD của vận đơn.
	RefuseCOD Refusal = "cod"
)

// Shipment là phần vận đơn quyết định cần: trạng thái, có anomaly partial_reported mở, COD.
type Shipment struct {
	Status          string
	PartialReported bool
	COD             int64
}

// Item là một dòng đơn: mã và số mua.
type Item struct {
	ID  uuid.UUID
	Qty int32
}

// Line là một dòng đơn và một số cái (số giao ở yêu cầu; số giao hay số còn lại ở Plan).
type Line struct {
	OrderItemID uuid.UUID
	Qty         int32
}

// Plan là kết quả: Delivered các dòng có số giao > 0, Rest các dòng còn lại > 0 (số mua − số giao), cùng thứ tự dòng
// đơn.
type Plan struct {
	Delivered, Rest []Line
}

// Decide xét yêu cầu req (số giao từng dòng đơn) cho vận đơn sh có các dòng đơn items, tiền thu collected:
//   - vận đơn shipping hay delivery_failed; có partial_reported mở thì thêm awaiting_pickup (hãng báo giao một phần khi
//     CRM chưa nhận tin lấy hàng); trạng thái khác (đã giao, đang hoàn, đang hủy…) → RefuseStatus;
//   - req có đúng mỗi dòng đơn một lần, 0 ≤ số giao ≤ số mua, 0 < Σ giao < Σ mua → không thì RefuseQty;
//   - 0 ≤ collected ≤ COD → không thì RefuseCOD.
func Decide(sh Shipment, items []Item, req []Line, collected int64) (Plan, Refusal) {
	if !allowed(sh) {
		return Plan{}, RefuseStatus
	}
	plan, ok := split(items, req)
	if !ok {
		return Plan{}, RefuseQty
	}
	if collected < 0 || collected > sh.COD {
		return Plan{}, RefuseCOD
	}
	return plan, OK
}

func allowed(sh Shipment) bool {
	switch sh.Status {
	case "shipping", "delivery_failed":
		return true
	case "awaiting_pickup":
		return sh.PartialReported
	}
	return false
}

// split ghép req vào items; ok = false khi yêu cầu sai số lượng (RefuseQty).
func split(items []Item, req []Line) (Plan, bool) {
	if len(req) != len(items) {
		return Plan{}, false
	}
	var p Plan
	var bought, gave int64
	for _, it := range items {
		i := slices.IndexFunc(req, func(l Line) bool { return l.OrderItemID == it.ID })
		if i < 0 {
			return Plan{}, false // thiếu dòng (cùng số dòng nên kéo theo có dòng lạ hay trùng)
		}
		q := req[i].Qty
		if q < 0 || q > it.Qty {
			return Plan{}, false
		}
		if q > 0 {
			p.Delivered = append(p.Delivered, Line{OrderItemID: it.ID, Qty: q})
		}
		if rest := it.Qty - q; rest > 0 {
			p.Rest = append(p.Rest, Line{OrderItemID: it.ID, Qty: rest})
		}
		bought += int64(it.Qty)
		gave += int64(q)
	}
	if gave == 0 || gave == bought {
		return Plan{}, false
	}
	return p, true
}
