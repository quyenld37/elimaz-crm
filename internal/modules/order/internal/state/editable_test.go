package state_test

import (
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

func TestEditable(t *testing.T) {
	t.Parallel()
	all := state.EditScope{
		Lines: true, Discounts: true, Coupon: true, Address: true, ShippingFee: true,
		CarrierNote: true, CareBy: true, TrafficSource: true,
	}
	afterShipment := state.EditScope{CareBy: true, TrafficSource: true}
	careOnly := state.EditScope{CareBy: true}
	cases := []struct {
		name string
		a    orderview.Axes
		want state.EditScope
	}{
		{"draft", orderview.Axes{Status: "draft", PackingStatus: "none"}, all},
		{"new chờ duyệt", orderview.Axes{Status: "new", PendingReason: "discount_approval", PackingStatus: "none"}, all},
		{"confirmed chưa vận đơn", orderview.Axes{Status: "confirmed", PackingStatus: "none"}, all},
		{"confirmed đã đóng gói, chưa vận đơn", orderview.Axes{Status: "confirmed", PackingStatus: "packed"}, all},
		{"confirmed chờ thu gom", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "awaiting_pickup"}, afterShipment},
		{"confirmed đang chuyển", orderview.Axes{Status: "confirmed", ShipmentStatus: "shipping"}, afterShipment},
		{"confirmed vận đơn creating", orderview.Axes{Status: "confirmed", ShipmentStatus: "creating"}, afterShipment},
		{"new có vận đơn (không thể có)", orderview.Axes{Status: "new", ShipmentStatus: "shipping"}, afterShipment},
		{"cancel_requested", orderview.Axes{Status: "cancel_requested", ShipmentStatus: "awaiting_pickup"}, careOnly},
		{"cancelled", orderview.Axes{Status: "cancelled"}, careOnly},
		{"delivered", orderview.Axes{Status: "delivered", ShipmentStatus: "delivered"}, careOnly},
		{"returned", orderview.Axes{Status: "returned"}, careOnly},
		{"trạng thái lạ", orderview.Axes{Status: "shipping"}, state.EditScope{}},
		{"trạng thái rỗng", orderview.Axes{}, state.EditScope{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := state.Editable(c.a); got != c.want {
				t.Fatalf("Editable(%+v) = %+v, muốn %+v", c.a, got, c.want)
			}
		})
	}
}

// Có vận đơn hiện hành hoặc đơn đã kết thúc: không bao giờ mở trường mà hãng giữ (Q25); người phụ trách luôn sửa được
// với trạng thái hợp lệ.
func TestEditable_ShipmentLocks(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		a := orderview.Axes{
			Status:         rapid.SampledFrom([]string{"draft", "new", "confirmed", "cancel_requested", "cancelled", "delivered", "returned"}).Draw(t, "status"),
			PendingReason:  rapid.SampledFrom([]string{"", "awaiting_customer", "out_of_stock", "discount_approval"}).Draw(t, "pending"),
			PackingStatus:  rapid.SampledFrom([]string{"none", "picking", "packed"}).Draw(t, "packing"),
			ShipmentStatus: rapid.SampledFrom([]string{"", "creating", "awaiting_pickup", "shipping", "delivery_failed", "returning", "returned", "delivered"}).Draw(t, "shipment"),
		}
		got := state.Editable(a)
		if !got.CareBy {
			t.Fatalf("Editable(%+v): người phụ trách không sửa được", a)
		}
		open := got.Lines || got.Discounts || got.Coupon || got.Address || got.ShippingFee || got.CarrierNote
		if open && (a.ShipmentStatus != "" || (a.Status != "draft" && a.Status != "new" && a.Status != "confirmed")) {
			t.Fatalf("Editable(%+v) = %+v: mở trường hãng giữ", a, got)
		}
	})
}
