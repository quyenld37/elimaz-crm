package orderview_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Trục vận đơn của M3 (kế hoạch M3, Task M3-28): đơn confirmed + packed không vận đơn → "Đã đóng gói"; có vận đơn
// creating → "Đang gửi hãng" (sending, dù đang lấy hay đã gói); awaiting_pickup → "Chờ thu gom"; delivery_failed →
// "Thất bại". Bảng của M2 (TestLabel_Table) vẫn đúng.
func TestLabel_ShipmentAxisM3(t *testing.T) {
	t.Parallel()
	cases := []labelCase{
		{"đã gói, chưa vận đơn", orderview.Axes{Status: "confirmed", PackingStatus: "packed"}, orderview.LabelPacked},
		{"vận đơn creating, đã gói", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "creating"}, orderview.LabelSending},
		{"vận đơn creating, đang lấy", orderview.Axes{Status: "confirmed", PackingStatus: "picking", ShipmentStatus: "creating"}, orderview.LabelSending},
		{"vận đơn creating, chưa gói", orderview.Axes{Status: "confirmed", PackingStatus: "none", ShipmentStatus: "creating"}, orderview.LabelSending},
		{"chờ thu gom", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "awaiting_pickup"}, orderview.LabelAwaitingPickup},
		{"thất bại", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "delivery_failed"}, orderview.LabelDeliveryFailed},
		{"yêu cầu hủy giữ theo đơn", orderview.Axes{Status: "cancel_requested", ShipmentStatus: "creating"}, orderview.LabelCancelRequested},
		{"vận đơn đã hủy, đã gói", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "cancelled"}, orderview.LabelConfirmed},
	}
	checkLabelCases(t, cases)
	if orderview.LabelSending != "sending" {
		t.Fatalf("LabelSending = %q", orderview.LabelSending)
	}
}
