// Package reconcile là đối chiếu sổ COD hằng đêm (kế hoạch M3, Task M3-33; spec 05 mục 2; TI03): bảng đích số dư theo
// trạng thái vận đơn (Target) và một lần chạy theo tenant (Run): tổng sổ bằng 0, không bút toán lệch, số dư từng vận
// đơn khớp đích. Chỉ báo, không sửa sổ.
package reconcile

import "elimaz.com.vn/crm/internal/modules/cash/internal/postings"

// Trạng thái vận đơn (khớp CHECK shipments_status_check của fulfillment, 0300).
const (
	statusCreating        = "creating"
	statusAwaitingPickup  = "awaiting_pickup"
	statusCancelRequested = "cancel_requested"
	statusCancelled       = "cancelled"
	statusShipping        = "shipping"
	statusDeliveryFailed  = "delivery_failed"
	statusDelivered       = "delivered"
	statusReturning       = "returning"
	statusReturned        = "returned"
)

// Expectation là số dư đích của một vận đơn: ToCollect, SalesClearing đúng từng ngăn; Settled là tổng carrier_holding +
// shop_received + carrier_fee (phần phí-only bù trừ nhau trong tổng); EachZero: ba ngăn đó phải bằng 0 từng ngăn (trước
// khi giao). Known = false: trạng thái lạ, không có đích (bên gọi báo lệch).
type Expectation struct {
	Known                             bool
	ToCollect, Settled, SalesClearing int64
	EachZero                          bool
}

// Target là bảng đích số dư theo trạng thái (dùng chung với test luật ghi sổ):
//
//   - creating, awaiting_pickup, cancel_requested, cancelled: mọi ngăn 0;
//   - shipping, delivery_failed: to_collect = cod, sales_clearing = −cod, ngăn khác 0;
//   - delivered: to_collect = 0, carrier_holding + shop_received + carrier_fee = codCollected,
//     sales_clearing = −codCollected;
//   - returning, returned: to_collect = 0, sales_clearing = 0, tổng ba ngăn = 0; giao một phần (partial) thì phần đã
//     thu ở lại như delivered (luật ghi sổ chỉ trả phần còn ở to_collect).
//
// codCollected là số hãng báo đã thu (hãng không báo: bên gọi truyền cod, như luật ghi sổ). Vận đơn không COD (cod = 0)
// không có dòng sổ nào dù hãng báo đã thu (luật ghi sổ bỏ qua): mọi ngăn 0.
func Target(status string, partial bool, cod, codCollected int64) Expectation {
	switch status {
	case statusCreating, statusAwaitingPickup, statusCancelRequested, statusCancelled:
		return Expectation{Known: true, EachZero: true}
	case statusShipping, statusDeliveryFailed:
		return Expectation{Known: true, ToCollect: cod, SalesClearing: -cod, EachZero: true}
	case statusDelivered:
		if cod == 0 {
			return Expectation{Known: true, EachZero: true}
		}
		return Expectation{Known: true, Settled: codCollected, SalesClearing: -codCollected}
	case statusReturning, statusReturned:
		if partial && cod > 0 {
			return Expectation{Known: true, Settled: codCollected, SalesClearing: -codCollected}
		}
		return Expectation{Known: true}
	}
	return Expectation{}
}

// Match báo số dư b khớp đích x (đích lạ: không khớp).
func (x Expectation) Match(b postings.Balances) bool {
	if !x.Known || b.ToCollect != x.ToCollect || b.SalesClearing != x.SalesClearing {
		return false
	}
	if x.EachZero {
		return b.CarrierHolding == 0 && b.ShopReceived == 0 && b.CarrierFee == 0
	}
	return b.CarrierHolding+b.ShopReceived+b.CarrierFee == x.Settled
}
