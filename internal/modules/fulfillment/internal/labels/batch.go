// Package labels là phần thuần (không database, không mạng) của vùng in nhãn (kế hoạch M3, Task M3-16): chia vận đơn
// được chọn thành lô mỗi tài khoản hãng, kiểm tệp nhãn hãng trả về, mã lỗi của lô.
package labels

import (
	"cmp"
	"slices"

	"github.com/google/uuid"
)

// Lý do vận đơn không vào lô nào (LabelBatchSkipped.reason).
const (
	// ReasonNoCarrierLabel: vận đơn nhập tay, giao tay (self), hay hãng không in nhãn qua API.
	ReasonNoCarrierLabel = "no_carrier_label"
	// ReasonNoTrackingCode: hãng chưa cấp mã (vận đơn đang tạo).
	ReasonNoTrackingCode = "no_tracking_code"
	// ReasonCancelled: vận đơn đã hủy (không in nhãn của kiện không giao).
	ReasonCancelled = "cancelled"
)

// Trạng thái, chế độ của vận đơn mà Plan cần (nhắc lại để gói không nhập fulfillment).
const (
	modeAPI         = "api"
	statusCancelled = "cancelled"
)

// Shipment là vận đơn được chọn in: Labels là adapter của hãng in được nhãn qua API, Batch là in được nhiều vận đơn
// trong một lần gọi (Capabilities.LabelsBatch).
type Shipment struct {
	ID, AccountID uuid.UUID
	Mode, Status  string
	HasTracking   bool
	Labels, Batch bool
}

// Group là một lô: các vận đơn của một tài khoản hãng, id tăng dần.
type Group struct {
	AccountID   uuid.UUID
	ShipmentIDs []uuid.UUID
}

// Skip là vận đơn không vào lô kèm lý do.
type Skip struct {
	ShipmentID uuid.UUID
	Reason     string
}

// Plan chia ss thành lô mỗi tài khoản hãng (theo mã tài khoản tăng dần, vận đơn theo id tăng dần) và danh sách bỏ qua
// (theo id tăng dần). Đã hủy → cancelled; nhập tay hay hãng không in nhãn → no_carrier_label; chưa có mã hãng →
// no_tracking_code. Hãng không in nhiều vận đơn một lần (Batch = false) → mỗi vận đơn một lô (Q-18). Mã trùng tính một
// lần.
func Plan(ss []Shipment) ([]Group, []Skip) {
	ss = slices.Clone(ss)
	slices.SortFunc(ss, func(a, b Shipment) int { return cmpID(a.ID, b.ID) })
	ss = slices.CompactFunc(ss, func(a, b Shipment) bool { return a.ID == b.ID })
	var skips []Skip
	byAccount := map[uuid.UUID]int{}
	var groups []Group
	for _, s := range ss {
		switch {
		case s.Status == statusCancelled:
			skips = append(skips, Skip{ShipmentID: s.ID, Reason: ReasonCancelled})
			continue
		case s.Mode != modeAPI || !s.Labels:
			skips = append(skips, Skip{ShipmentID: s.ID, Reason: ReasonNoCarrierLabel})
			continue
		case !s.HasTracking:
			skips = append(skips, Skip{ShipmentID: s.ID, Reason: ReasonNoTrackingCode})
			continue
		}
		if i, ok := byAccount[s.AccountID]; ok && s.Batch {
			groups[i].ShipmentIDs = append(groups[i].ShipmentIDs, s.ID)
			continue
		}
		if s.Batch {
			byAccount[s.AccountID] = len(groups)
		}
		groups = append(groups, Group{AccountID: s.AccountID, ShipmentIDs: []uuid.UUID{s.ID}})
	}
	slices.SortStableFunc(groups, func(a, b Group) int {
		return cmp.Or(cmpID(a.AccountID, b.AccountID), cmpID(a.ShipmentIDs[0], b.ShipmentIDs[0]))
	})
	return groups, skips
}

func cmpID(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }
