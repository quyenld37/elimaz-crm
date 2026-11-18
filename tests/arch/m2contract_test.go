package arch

import (
	"slices"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/httpapi"
)

// m2Operations là bảng operation của kế hoạch M2, Task M2-02 (hợp đồng viết một lần cho cả mốc): operationId,
// "METHOD đường", quyền. Hai dòng cuối là operation của M1 được mở rộng (Q-X2).
func m2Operations() [][3]string {
	return [][3]string{
		{"listCustomers", "GET /api/v1/customers", "customer.read"},
		{"createCustomer", "POST /api/v1/customers", "customer.write"},
		{"getCustomer", "GET /api/v1/customers/{id}", "customer.read"},
		{"updateCustomer", "PATCH /api/v1/customers/{id}", "customer.write"},
		{"addCustomerIdentity", "POST /api/v1/customers/{id}/identities", "customer.write"},
		{"removeCustomerIdentity", "DELETE /api/v1/customers/{id}/identities/{identityId}", "customer.write"},
		{"addCustomerAddress", "POST /api/v1/customers/{id}/addresses", "customer.write"},
		{"updateCustomerAddress", "PATCH /api/v1/customers/{id}/addresses/{addressId}", "customer.write"},
		{"deleteCustomerAddress", "DELETE /api/v1/customers/{id}/addresses/{addressId}", "customer.write"},
		{"listAdminUnits", "GET /api/v1/admin-units", "customer.read"},
		{"listCustomerMergeCandidates", "GET /api/v1/customers/{id}/merge-candidates", "customer.read"},
		{"mergeCustomer", "POST /api/v1/customers/{id}/merge", "customer.merge"},
		{"undoCustomerMerge", "POST /api/v1/customer-merges/{mergeId}/undo", "customer.merge"},
		{"getCustomerTimeline", "GET /api/v1/customers/{id}/timeline", "customer.read"},
		{"addCustomerNote", "POST /api/v1/customers/{id}/notes", "customer.write"},
		{"listCustomerTags", "GET /api/v1/customer-tags", "customer.read"},
		{"createCustomerTag", "POST /api/v1/customer-tags", "customer.tags.manage"},
		{"updateCustomerTag", "PATCH /api/v1/customer-tags/{tagId}", "customer.tags.manage"},
		{"addCustomerTag", "PUT /api/v1/customers/{id}/tags/{tagId}", "customer.write"},
		{"removeCustomerTag", "DELETE /api/v1/customers/{id}/tags/{tagId}", "customer.write"},
		{"getCustomerTagRule", "GET /api/v1/customer-tags/{tagId}/rule", "customer.read"},
		{"putCustomerTagRule", "PUT /api/v1/customer-tags/{tagId}/rule", "customer.tags.manage"},
		{"getCustomerImportTemplate", "GET /api/v1/customer-imports/template", "customer.import"},
		{"startCustomerImport", "POST /api/v1/customer-imports", "customer.import"},
		{"getCustomerImport", "GET /api/v1/customer-imports/{importId}", "customer.import"},
		{"listOrders", "GET /api/v1/orders", "order.read"},
		{"updateOrder", "PATCH /api/v1/orders/{id}", "order.update"},
		{"confirmOrder", "POST /api/v1/orders/{id}/confirm", "order.confirm"},
		{"approveOrderDiscount", "POST /api/v1/orders/{id}/approve-discount", "order.discount.approve"},
		{"setOrderPending", "POST /api/v1/orders/{id}/pending", "order.confirm"},
		{"cancelOrder", "POST /api/v1/orders/{id}/cancel", "order.cancel"},
		{"getOrderTimeline", "GET /api/v1/orders/{id}/timeline", "order.read"},
		{"addOrderNote", "POST /api/v1/orders/{id}/notes", "order.update"},
		{"checkOrderDuplicates", "GET /api/v1/order-duplicate-check", "order.create"},
		{"listOrderDuplicates", "GET /api/v1/order-duplicates", "order.read"},
		{"resolveOrderDuplicate", "POST /api/v1/order-duplicates/{orderId}/{otherOrderId}/resolve", "order.duplicates.resolve"},
		{"createReturn", "POST /api/v1/orders/{id}/returns", "order.return"},
		{"listReturns", "GET /api/v1/returns", "order.read"},
		{"getReturn", "GET /api/v1/returns/{returnId}", "order.read"},
		{"receiveReturn", "POST /api/v1/returns/{returnId}/receive", "order.return.receive"},
		{"closeReturn", "POST /api/v1/returns/{returnId}/close", "order.return"},
		{"cancelReturn", "POST /api/v1/returns/{returnId}/cancel", "order.return"},
		{"listTrafficSources", "GET /api/v1/traffic-sources", "order.read"},
		{"createTrafficSource", "POST /api/v1/traffic-sources", "order.settings.manage"},
		{"updateTrafficSource", "PATCH /api/v1/traffic-sources/{sourceId}", "order.settings.manage"},
		{"getOrderSettings", "GET /api/v1/order-settings", "order.read"},
		{"updateOrderSettings", "PATCH /api/v1/order-settings", "order.settings.manage"},
		{"listCoupons", "GET /api/v1/coupons", "promotion.read"},
		{"createCoupon", "POST /api/v1/coupons", "promotion.manage"},
		{"getCoupon", "GET /api/v1/coupons/{couponId}", "promotion.read"},
		{"updateCoupon", "PATCH /api/v1/coupons/{couponId}", "promotion.manage"},
		{"createCouponBatch", "POST /api/v1/coupon-batches", "promotion.manage"},
		{"getCouponBatch", "GET /api/v1/coupon-batches/{batchId}", "promotion.read"},
		{"listCouponRedemptions", "GET /api/v1/coupons/{couponId}/redemptions", "promotion.read"},
		{"checkCoupon", "GET /api/v1/coupon-check", "order.create"},
		// M1, mở rộng (Q-X2): giữ đường, operationId, quyền.
		{"createOrder", "POST /api/v1/orders", "order.create"},
		{"getOrder", "GET /api/v1/orders/{id}", "order.read"},
	}
}

func TestM2Contract_Operations(t *testing.T) {
	want := m2Operations()
	if len(want) != 57 {
		t.Fatalf("bảng kế hoạch có 55 operation mới và 2 của M1, bảng test có %d", len(want))
	}
	byID := map[string]httpapi.Route{}
	for _, r := range httpapi.Routes {
		if _, dup := byID[r.OperationID]; dup {
			t.Errorf("operationId %s trùng", r.OperationID)
		}
		byID[r.OperationID] = r
	}
	for _, w := range want {
		r, ok := byID[w[0]]
		switch {
		case !ok:
			t.Errorf("thiếu operation %s (%s)", w[0], w[1])
		case r.Pattern != w[1] || r.Permission != w[2]:
			t.Errorf("%s: mong %s quyền %s, có %s quyền %s", w[0], w[1], w[2], r.Pattern, r.Permission)
		case r.Write != !strings.HasPrefix(w[1], "GET "):
			t.Errorf("%s: cờ ghi sai", w[0])
		case r.Write && r.IdempotencyMode != "required":
			t.Errorf("%s: route ghi M2 phải bắt buộc Idempotency-Key, có %q", w[0], r.IdempotencyMode)
		}
	}
}

// TestM2Contract_ViewsDoc: dòng docs/views.md của view mới M2 có đúng cột của kế hoạch (Task M2-03, 05, 06); hai dòng
// view M1 sửa cùng migration (R13): customers_v ở Task M2-03 (nối 11 cột), orders_v ở Task M2-05.
func TestM2Contract_ViewsDoc(t *testing.T) {
	doc := readViewsDoc(t, repoRoot(t))
	want := map[string]string{
		"customer.customers_v": "id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, " +
			"delivered_count, returned_count, total_spent, last_order_at, updated_at, version",
		"ord.orders_v":                   "id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at",
		"customer.customer_identities_v": "tenant_id, id, customer_id, kind, scope, value, verified_at, source, created_at",
		"customer.customer_addresses_v":  "tenant_id, id, customer_id, recipient_name, phone, province_code, ward_code, street, is_default",
		"customer.customer_tags_v":       "tenant_id, id, name, color",
		"customer.customer_tag_links_v":  "tenant_id, customer_id, tag_id, source",
		"ord.order_items_v": "id, tenant_id, seller_id, order_id, variant_id, position, sku_snapshot, name_snapshot, quantity, " +
			"unit_price, line_discount, allocated_order_discount, net_amount",
		"ord.order_status_history_v": "id, tenant_id, order_id, from_status, to_status, from_pending, to_pending, actor_type, " +
			"actor_id, reason, occurred_at, recorded_at",
		"ord.returns_v": "id, tenant_id, code, order_id, shipment_id, kind, status, refund_amount, replacement_order_id, " +
			"received_at, created_at",
		"ord.return_items_v":    "id, tenant_id, return_id, order_item_id, quantity, condition, refund_amount",
		"ord.traffic_sources_v": "id, tenant_id, key, name, channel_account_id, active, position",
		"promotion.coupons_v": "id, tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, " +
			"seller_id, max_uses, used_count, per_customer_limit, valid_from, valid_to, status, created_at, updated_at",
	}
	for view, cols := range want {
		got, ok := doc.views[view]
		if !ok {
			t.Errorf("docs/views.md thiếu dòng %s", view)
			continue
		}
		if w := strings.Split(cols, ", "); !slices.Equal(got, w) {
			t.Errorf("%s: mong cột %v, có %v", view, w, got)
		}
	}
	if slices.Contains(doc.views["promotion.coupons_v"], "bound_phone") {
		t.Error("promotion.coupons_v không được có bound_phone (dữ liệu cá nhân)")
	}
	for v := range doc.views {
		if v == "customer.identities_v" {
			t.Error("không có tên thứ hai customer.identities_v (dùng customer.customer_identities_v)")
		}
	}
}
