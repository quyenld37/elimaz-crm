package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// m3Operations là bảng operation mới của kế hoạch M3, Task M3-04 (hợp đồng viết một lần cho cả mốc): operationId,
// "METHOD đường", quyền.
func m3Operations() [][3]string {
	return [][3]string{
		{"listCarrierAccounts", "GET /api/v1/carrier-accounts", "fulfillment.carrier_accounts.read"},
		{"createCarrierAccount", "POST /api/v1/carrier-accounts", "fulfillment.carrier_accounts.manage"},
		{"updateCarrierAccount", "PATCH /api/v1/carrier-accounts/{id}", "fulfillment.carrier_accounts.manage"},
		{"putCarrierCredentials", "PUT /api/v1/carrier-accounts/{id}/credentials", "fulfillment.carrier_accounts.manage"},
		{"checkCarrierAccount", "POST /api/v1/carrier-accounts/{id}/check", "fulfillment.carrier_accounts.manage"},
		{"setCarrierAccountStatus", "POST /api/v1/carrier-accounts/{id}/status", "fulfillment.carrier_accounts.manage"},
		{"setWarehousePickup", "PUT /api/v1/inventory/warehouses/{id}/pickup", "fulfillment.settings.manage"},
		{"syncCarrierLocations", "POST /api/v1/carrier-accounts/{id}/locations/sync", "fulfillment.carrier_accounts.manage"},
		{"listUnmappedLocations", "GET /api/v1/carrier-locations/unmapped", "fulfillment.carrier_accounts.read"},
		{"requestShippingQuotes", "POST /api/v1/orders/{id}/shipping-quotes", "fulfillment.quote"},
		{"listShippingQuotes", "GET /api/v1/orders/{id}/shipping-quotes", "fulfillment.quote"},
		{"listOrderShipments", "GET /api/v1/orders/{id}/shipments", "fulfillment.read"},
		{"listShipmentEvents", "GET /api/v1/shipments/{id}/events", "fulfillment.read"},
		{"resendShipment", "POST /api/v1/shipments/{id}/resend", "fulfillment.ship"},
		{"resolveShipmentCreate", "POST /api/v1/shipments/{id}/resolve-create", "fulfillment.ship"},
		{"cancelShipment", "POST /api/v1/shipments/{id}/cancel", "fulfillment.cancel"},
		{"syncShipment", "POST /api/v1/shipments/{id}/sync", "fulfillment.read"},
		{"setShipmentManualStatus", "POST /api/v1/shipments/{id}/manual-status", "fulfillment.manual_status"},
		{"requestAddressChange", "POST /api/v1/shipments/{id}/address-changes", "fulfillment.ship"},
		{"resolveAddressChange", "POST /api/v1/address-changes/{id}/resolve", "fulfillment.ship"},
		{"createLabelBatch", "POST /api/v1/label-batches", "fulfillment.print"},
		{"getLabelBatch", "GET /api/v1/label-batches/{id}", "fulfillment.print"},
		{"getPickingList", "GET /api/v1/picking-list", "fulfillment.print"},
		{"setPackingStatus", "POST /api/v1/orders/packing", "fulfillment.pack"},
		{"listCallbackTasks", "GET /api/v1/callback-tasks", "fulfillment.callbacks.work"},
		{"recordCallbackOutcome", "POST /api/v1/callback-tasks/{id}/outcomes", "fulfillment.callbacks.work"},
		{"assignCallbackTask", "POST /api/v1/callback-tasks/{id}/assign", "fulfillment.callbacks.work"},
		{"listStuckShipments", "GET /api/v1/shipments/stuck", "fulfillment.read"},
		{"listShipmentsForReview", "GET /api/v1/shipments/review", "fulfillment.read"},
		{"resolveShipmentAnomaly", "POST /api/v1/shipment-anomalies/{id}/resolve", "fulfillment.correct"},
		{"listAgedReservations", "GET /api/v1/inventory/aged-reservations", "inventory.aged.read"},
		{"getCustomerDeliveryStats", "GET /api/v1/customers/{id}/delivery-stats", "customer.read"},
		{"recordPartialDelivery", "POST /api/v1/shipments/{id}/partial-delivery", "fulfillment.correct"},
		{"previewShipmentCorrection", "POST /api/v1/shipments/{id}/corrections/preview", "fulfillment.correct"},
		{"applyShipmentCorrection", "POST /api/v1/shipments/{id}/corrections", "fulfillment.correct"},
		{"getShipmentCod", "GET /api/v1/shipments/{id}/cod", "cash.cod.read"},
		{"recordPayment", "POST /api/v1/orders/{id}/payments", "cash.payments.record"},
		{"listOrderPayments", "GET /api/v1/orders/{id}/payments", "cash.payments.record"},
		{"listRefundObligations", "GET /api/v1/refund-obligations", "cash.cod.read"},
		{"payRefundObligation", "POST /api/v1/refund-obligations/{id}/payments", "cash.refunds.record"},
		{"waiveRefundObligation", "POST /api/v1/refund-obligations/{id}/waive", "cash.refunds.waive"},
		{"importCodStatement", "POST /api/v1/cod-statements", "cash.statements.import"},
		{"listCodStatements", "GET /api/v1/cod-statements", "cash.cod.read"},
		{"getCodStatement", "GET /api/v1/cod-statements/{id}", "cash.cod.read"},
		{"confirmCodStatement", "POST /api/v1/cod-statements/{id}/confirm", "cash.statements.confirm"},
		{"listCodDiscrepancies", "GET /api/v1/cod/discrepancies", "cash.cod.read"},
		{"listCodOverdue", "GET /api/v1/cod/overdue", "cash.cod.read"},
		{"previewCodAdjustment", "POST /api/v1/cod/adjustments/preview", "cash.adjust"},
		{"createCodAdjustment", "POST /api/v1/cod/adjustments", "cash.adjust"},
		{"listReconcileFindings", "GET /api/v1/cod/reconcile-findings", "cash.cod.read"},
		{"getFulfillmentSettings", "GET /api/v1/fulfillment-settings", "fulfillment.read"},
		{"updateFulfillmentSettings", "PUT /api/v1/fulfillment-settings", "fulfillment.settings.manage"},
	}
}

// m3Extended là operation của M1, M2 mà M3 mở rộng (Q-X2): chỉ thêm trường tùy chọn, giữ đường, operationId, quyền.
func m3Extended() [][3]string {
	return [][3]string{
		{"createShipment", "POST /api/v1/orders/{id}/shipments", "fulfillment.ship"},
		{"getShipment", "GET /api/v1/shipments/{id}", "fulfillment.read"},
		{"listOrders", "GET /api/v1/orders", "order.read"},
		{"getOrderTimeline", "GET /api/v1/orders/{id}/timeline", "order.read"},
		{"receiveReturn", "POST /api/v1/returns/{returnId}/receive", "order.return.receive"},
	}
}

// Mọi operation của bảng M3 có trong Routes đúng đường, quyền, cờ ghi; route ghi bắt buộc Idempotency-Key. Operation
// mở rộng giữ nguyên đường và quyền.
func TestM3Contract_Operations(t *testing.T) {
	if n := len(m3Operations()); n != 52 {
		t.Fatalf("bảng kế hoạch M3 có 52 operation mới, bảng test có %d", n)
	}
	checkContractOps(t, "M3", append(m3Operations(), m3Extended()...))
}

// Danh sách tạm trả 501 (testdata/m3_pending.txt) chỉ chứa operation mới của M3, không trùng dòng; hai operation cài
// đặt vận chuyển làm ngay ở Task M3-04 không có trong đó. Đối chiếu 501 với danh sách: TestM3Routes_PendingList
// (internal/httpapi).
func TestM3Contract_PendingList(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "m3_pending.txt"))
	if err != nil {
		t.Fatal(err)
	}
	m3 := map[string]bool{}
	for _, w := range m3Operations() {
		m3[w[0]] = true
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(b), "\n") {
		op := strings.TrimSpace(line)
		if op == "" || strings.HasPrefix(op, "#") {
			continue
		}
		switch {
		case !m3[op]:
			t.Errorf("m3_pending.txt có %q không phải operation mới của M3", op)
		case seen[op]:
			t.Errorf("m3_pending.txt có %q hai lần", op)
		case op == "getFulfillmentSettings" || op == "updateFulfillmentSettings":
			t.Errorf("%s đã làm ở Task M3-04, không được chờ", op)
		}
		seen[op] = true
	}
}

// Đóng mốc M3 (Task M3-48): testdata/m3_pending.txt không còn operation nào chờ (chỉ còn chú thích, dòng trống); mọi
// operation của bảng M3 đã hiện thực. TestM3Routes_PendingList (internal/httpapi) đòi khi đó không operation M3 nào
// trả 501.
func TestM3Contract_PendingEmptyAtClose(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "m3_pending.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if op := strings.TrimSpace(line); op != "" && !strings.HasPrefix(op, "#") {
			t.Errorf("m3_pending.txt còn %q: M3 đã đóng mốc, operation phải hiện thực và xóa dòng", op)
		}
	}
}
