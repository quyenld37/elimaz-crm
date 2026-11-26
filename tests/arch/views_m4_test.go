package arch

import (
	"slices"
	"testing"
)

// View của mốc trước M4 đọc (kế hoạch M4, Task 143; R3): ord.orders_v có bốn cột M4 cần, 11 cột đầu đúng thứ tự của
// M1; customer.customers_v có 4 cột đầu của M1; customer.customer_identities_v có mặt. Đọc docs/views.md (TestViewsDoc*
// giữ tệp khớp database thật). Đỏ thì là lỗi ở M1/M2: dừng, báo điều phối viên, không tự thêm migration.
func TestViews_M4Needs(t *testing.T) {
	t.Parallel()
	doc := readViewsDoc(t, repoRoot(t))
	orders := doc.views["ord.orders_v"]
	for _, c := range []string{"conversation_id", "channel_account_id", "draft_expires_at", "version"} {
		if !slices.Contains(orders, c) {
			t.Errorf("ord.orders_v thiếu cột %s", c)
		}
	}
	m1Orders := []string{
		"id", "tenant_id", "code", "seller_id", "customer_id", "status", "channel", "grand_total",
		"cod_amount", "current_shipment_id", "created_at",
	}
	if len(orders) < len(m1Orders) || !slices.Equal(orders[:len(m1Orders)], m1Orders) {
		t.Errorf("ord.orders_v: 11 cột đầu phải là %v, có %v", m1Orders, orders)
	}
	customers := doc.views["customer.customers_v"]
	if m1 := []string{"id", "root_id", "full_name", "created_at"}; len(customers) < 4 || !slices.Equal(customers[:4], m1) {
		t.Errorf("customer.customers_v: 4 cột đầu phải là %v, có %v", m1, customers)
	}
	if len(doc.views["customer.customer_identities_v"]) == 0 {
		t.Error("thiếu customer.customer_identities_v")
	}
}
