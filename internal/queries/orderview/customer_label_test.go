package orderview_test

import (
	"testing"

	"elimaz.com.vn/crm/internal/queries/orderview"
)

// CustomerLabel (Task 196): bốn nhãn chỉ nội bộ (nháp, chờ duyệt giảm, chờ khách, hết hàng) về new; nhãn khác giữ
// nguyên Label.
func TestCustomerLabel(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a    orderview.Axes
		want orderview.LabelKey
	}{
		{orderview.Axes{Status: "draft"}, orderview.LabelNew},
		{orderview.Axes{Status: "new"}, orderview.LabelNew},
		{orderview.Axes{Status: "new", PendingReason: "discount_approval"}, orderview.LabelNew},
		{orderview.Axes{Status: "new", PendingReason: "awaiting_customer"}, orderview.LabelNew},
		{orderview.Axes{Status: "new", PendingReason: "out_of_stock"}, orderview.LabelNew},
		{orderview.Axes{Status: "confirmed"}, orderview.LabelConfirmed},
		{orderview.Axes{Status: "confirmed", PackingStatus: "picking"}, orderview.LabelPacking},
		{orderview.Axes{Status: "confirmed", ShipmentStatus: "awaiting_pickup"}, orderview.LabelAwaitingPickup},
		{orderview.Axes{Status: "confirmed", ShipmentStatus: "shipping"}, orderview.LabelShipping},
		{orderview.Axes{Status: "cancel_requested"}, orderview.LabelCancelRequested},
		{orderview.Axes{Status: "cancelled"}, orderview.LabelCancelled},
		{orderview.Axes{Status: "delivered"}, orderview.LabelDelivered},
	} {
		if got := orderview.CustomerLabel(c.a); got != c.want {
			t.Errorf("%+v: %s, mong %s", c.a, got, c.want)
		}
	}
}
