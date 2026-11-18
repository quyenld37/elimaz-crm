package orderview_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Bảng nhãn của kế hoạch M2, Task M2-08: 16 hàng, đúng thứ tự thanh nhãn.
func TestLabel_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a    orderview.Axes
		want orderview.LabelKey
	}{
		{"draft", orderview.Axes{Status: "draft", PackingStatus: "none"}, "draft"},
		{"new không lý do", orderview.Axes{Status: "new", PackingStatus: "none"}, "new"},
		{"new chờ duyệt giảm", orderview.Axes{Status: "new", PendingReason: "discount_approval", PackingStatus: "none"}, "awaiting_discount_approval"},
		{"new chờ khách", orderview.Axes{Status: "new", PendingReason: "awaiting_customer", PackingStatus: "none"}, "awaiting_customer"},
		{"new hết hàng", orderview.Axes{Status: "new", PendingReason: "out_of_stock", PackingStatus: "none"}, "out_of_stock"},
		{"confirmed chưa vận đơn", orderview.Axes{Status: "confirmed", PackingStatus: "none"}, "confirmed"},
		{"confirmed đang lấy hàng", orderview.Axes{Status: "confirmed", PackingStatus: "picking"}, "packing"},
		{"confirmed đã đóng gói", orderview.Axes{Status: "confirmed", PackingStatus: "packed"}, "packed"},
		{"chờ thu gom", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "awaiting_pickup"}, "awaiting_pickup"},
		{"đang chuyển", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "shipping"}, "shipping"},
		{"giao hụt", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "delivery_failed"}, "delivery_failed"},
		{"đang hoàn", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "returning"}, "returning"},
		{"vận đơn đã hoàn, đơn chưa", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "returned"}, "returning"},
		{"yêu cầu hủy", orderview.Axes{Status: "cancel_requested", PackingStatus: "packed", ShipmentStatus: "awaiting_pickup"}, "cancel_requested"},
		{"đã hủy", orderview.Axes{Status: "cancelled", PackingStatus: "none"}, "cancelled"},
		{"thành công", orderview.Axes{Status: "delivered", PackingStatus: "packed", ShipmentStatus: "delivered"}, "delivered"},
		{"đã hoàn", orderview.Axes{Status: "returned", PackingStatus: "packed", ShipmentStatus: "returned"}, "returned"},
		// Tổ hợp ngoài bảng (Q-T65-5).
		{"delivered + picking theo Status", orderview.Axes{Status: "delivered", PackingStatus: "picking"}, "delivered"},
		{"cancelled + vận đơn theo Status", orderview.Axes{Status: "cancelled", ShipmentStatus: "shipping"}, "cancelled"},
		{"new lý do lạ", orderview.Axes{Status: "new", PendingReason: "khac"}, "new"},
		{"draft có lý do", orderview.Axes{Status: "draft", PendingReason: "awaiting_customer"}, "draft"},
		{"vận đơn creating, đang lấy", orderview.Axes{Status: "confirmed", PackingStatus: "picking", ShipmentStatus: "creating"}, "packing"},
		{"vận đơn creating, đã gói", orderview.Axes{Status: "confirmed", PackingStatus: "packed", ShipmentStatus: "creating"}, "confirmed"},
		{"vận đơn chờ thu gom, đang lấy", orderview.Axes{Status: "confirmed", PackingStatus: "picking", ShipmentStatus: "awaiting_pickup"}, "awaiting_pickup"},
		{"packing rỗng", orderview.Axes{Status: "confirmed"}, "confirmed"},
		{"trạng thái lạ", orderview.Axes{Status: "shipping"}, ""},
		{"trạng thái rỗng", orderview.Axes{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := orderview.Label(c.a); got != c.want {
				t.Fatalf("Label(%+v) = %q, muốn %q", c.a, got, c.want)
			}
		})
	}
}

func TestLabels_OrderAndConstants(t *testing.T) {
	t.Parallel()
	want := []orderview.LabelKey{
		orderview.LabelDraft, orderview.LabelNew, orderview.LabelAwaitingDiscountApproval,
		orderview.LabelAwaitingCustomer, orderview.LabelOutOfStock, orderview.LabelConfirmed, orderview.LabelPacking,
		orderview.LabelPacked, orderview.LabelAwaitingPickup, orderview.LabelShipping, orderview.LabelDeliveryFailed,
		orderview.LabelReturning, orderview.LabelCancelRequested, orderview.LabelCancelled, orderview.LabelDelivered,
		orderview.LabelReturned,
	}
	got := orderview.Labels()
	if !slices.Equal(got, want) {
		t.Fatalf("Labels() = %v, muốn %v", got, want)
	}
	got[0] = "x"
	if orderview.Labels()[0] != orderview.LabelDraft {
		t.Fatal("Labels() trả slice dùng chung: bên gọi sửa được danh sách gốc")
	}
	wantStr := []string{
		"draft", "new", "awaiting_discount_approval", "awaiting_customer", "out_of_stock", "confirmed",
		"packing", "packed", "awaiting_pickup", "shipping", "delivery_failed", "returning", "cancel_requested",
		"cancelled", "delivered", "returned",
	}
	for i, k := range want {
		if string(k) != wantStr[i] {
			t.Fatalf("hằng thứ %d = %q, muốn %q", i, k, wantStr[i])
		}
	}
}

// Mọi nhãn trong Labels() đều đạt được, và với trạng thái hợp lệ thì trục bất kỳ cũng ra một nhãn trong Labels().
func TestLabel_AlwaysKnownForValidStatus(t *testing.T) {
	t.Parallel()
	statuses := []string{"draft", "new", "confirmed", "cancel_requested", "cancelled", "delivered", "returned"}
	known := orderview.Labels()
	rapid.Check(t, func(t *rapid.T) {
		a := orderview.Axes{
			Status:         rapid.SampledFrom(statuses).Draw(t, "status"),
			PendingReason:  rapid.SampledFrom([]string{"", "awaiting_customer", "out_of_stock", "discount_approval", "x"}).Draw(t, "pending"),
			PackingStatus:  rapid.SampledFrom([]string{"", "none", "picking", "packed", "x"}).Draw(t, "packing"),
			ShipmentStatus: rapid.SampledFrom([]string{"", "creating", "awaiting_pickup", "shipping", "delivery_failed", "returning", "returned", "delivered", "cancel_requested", "cancelled", "x"}).Draw(t, "shipment"),
		}
		if got := orderview.Label(a); !slices.Contains(known, got) {
			t.Fatalf("Label(%+v) = %q, không thuộc Labels()", a, got)
		}
	})
}
