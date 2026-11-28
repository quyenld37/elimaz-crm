package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Hai người bán (ADR-29, Task M3-48): API chạy với người bán mặc định A (ELIMAZ); người bán B (PARTNER1) có tài khoản
// hãng giả riêng (fake-2) và tuyến webhook riêng. Đường của B (đơn, vận đơn bằng public API của module như đường ghi
// của B sẽ làm; hãng giả thật, webhook, worker): tạo kiện → lấy hàng → giao thất bại → giao thành công → COD hãng giữ.
// Trong lúc đó: gửi đơn của A bằng tài khoản hãng của B → 409 carrier_account_unavailable, không vận đơn; API của A
// không đọc được vận đơn, việc gọi lại, danh sách vận đơn của đơn B (404 / không có); đường trọn của A vẫn chạy đúng
// cùng lúc; tồn, sổ COD của hai người bán tách nhau.
func TestM3TwoSellers(t *testing.T) {
	r := newM3Run(t)
	r.s.partnerRoute()
	va := r.productWithStock("M3SELLA", 5)
	pv := r.partnerWithStock(5)

	// Đơn của A gửi bằng tài khoản hãng của B → chặn.
	orderA := str(r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(va, 1, 30000), http.StatusCreated)["id"])
	st, body, err := r.call(http.MethodPost, "/api/v1/orders/"+orderA+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.PartnerAccount})
	if err != nil || st != http.StatusConflict || body["error"] != "carrier_account_unavailable" {
		t.Fatalf("đơn A bằng tài khoản B = %d %v (%v), muốn 409 carrier_account_unavailable", st, body, err)
	}
	if n := r.count(`SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1`, orderA); n != 0 {
		t.Fatalf("đơn A có %d vận đơn sau khi bị chặn", n)
	}

	// Đường của B.
	orderB, shipB := r.partnerShipment(pv)
	trackingB := r.awaitingTrackingOwner(shipB)
	r.advance(trackingB, fake.RawPickedUp)
	r.advance(trackingB, fake.RawDeliveryFailed)
	waitFor(t, stepBudget, "việc gọi lại của B mở", func() bool {
		return r.count(`SELECT count(*) FROM fulfillment.callback_tasks WHERE shipment_id = $1 AND status = 'open'`, shipB) == 1
	})

	// Đường của A chạy cùng lúc.
	shipA := str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderA+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.Account}, http.StatusAccepted)["id"])
	trackingA := r.awaitingTracking(shipA)
	r.advance(trackingA, fake.RawPickedUp)
	r.advance(trackingA, fake.RawDelivered)
	r.advance(trackingB, fake.RawDelivered)
	waitFor(t, stepBudget, "hai vận đơn delivered, COD hãng giữ", func() bool {
		return r.shipmentStatusOwner(shipA) == "delivered" && r.shipmentStatusOwner(shipB) == "delivered" &&
			r.cod(shipA)["carrier_holding"] == 280000 && r.cod(shipB.String())["carrier_holding"] == 280000
	})

	// API của A không thấy gì của B.
	for _, path := range []string{
		"/api/v1/shipments/" + shipB.String(), "/api/v1/shipments/" + shipB.String() + "/events",
		"/api/v1/orders/" + orderB.String(),
	} {
		if st, b, err := r.call(http.MethodGet, path, nil); err != nil || st != http.StatusNotFound {
			t.Errorf("A đọc %s = %d %v (%v), muốn 404", path, st, b, err)
		}
	}
	page := r.mustCall(http.MethodGet, "/api/v1/callback-tasks", nil, http.StatusOK)
	list, _ := page["items"].([]any)
	for _, it := range list {
		if m, _ := it.(map[string]any); m["shipmentId"] == shipB.String() {
			t.Errorf("danh sách gọi lại của A có việc của B: %v", m)
		}
	}
	orders := r.mustCall(http.MethodGet, "/api/v1/orders?limit=50", nil, http.StatusOK)
	olist, _ := orders["items"].([]any)
	for _, it := range olist {
		if m, _ := it.(map[string]any); m["id"] == orderB.String() {
			t.Errorf("danh sách đơn của A có đơn của B")
		}
	}

	r.stockIs(va, stock{onHand: 4, available: 4})
	r.stockIs(pv, stock{onHand: 4, available: 4})
	r.ledgerMatchesBalance(va)
	r.ledgerMatchesBalance(pv)
	r.codLedgerZero()
	r.noDeadJobs()
}

// partnerShipment tạo đơn tay 1 cái (ship 30.000, địa chỉ đã khớp mã hãng giả) của người bán PARTNER1 với biến thể pv
// và vận đơn bằng tài khoản hãng giả của PARTNER1 (crm_app trong tenant, public API order, fulfillment như đường ghi
// của người bán đó); lệnh tạo vận đơn chạy ở worker của stack. Trả mã đơn, mã vận đơn.
func (r *orderRun) partnerShipment(pv uuid.UUID) (orderID, shipmentID uuid.UUID) {
	r.t.Helper()
	ctx := r.t.Context()
	var owner uuid.UUID
	r.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM identity.users WHERE email = $1`, stackOwner).Scan(&owner)
	})
	r.inApp(func(tx pgx.Tx) error {
		o, err := r.s.shop.OrderTo(ctx, tx, r.s.shop.Partner, owner, 30000, order.ShipAddress{
			Name: "Chị Mai", Phone: logging.Phone("0901234568"), Line: "5 Nguyễn Huệ", ProvinceCode: fakeProvince,
			WardCode: fakeWard,
		}, order.ItemInput{VariantID: pv, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sh, err := r.s.shop.Shipment(ctx, tx, r.s.shop.Partner, o.ID, owner)
		orderID, shipmentID = o.ID, sh.ID
		return err //nolint:wrapcheck // test
	})
	return orderID, shipmentID
}

// awaitingTrackingOwner chờ (đọc bằng chủ bảng: vận đơn của người bán khác người bán của API) vận đơn sang
// awaiting_pickup có mã hãng; trả mã hãng.
func (r *orderRun) awaitingTrackingOwner(shipmentID uuid.UUID) string {
	r.t.Helper()
	var tracking string
	waitFor(r.t, stepBudget, "vận đơn của người bán B awaiting_pickup", func() bool {
		var status string
		r.inTenant(func(tx pgx.Tx) error {
			return tx.QueryRow(r.t.Context(), `SELECT status, coalesce(tracking_code, '') FROM fulfillment.shipments
				WHERE id = $1`, shipmentID).Scan(&status, &tracking)
		})
		return status == "awaiting_pickup" && tracking != ""
	})
	return tracking
}

// shipmentStatusOwner đọc trạng thái vận đơn bằng chủ bảng.
func (r *orderRun) shipmentStatusOwner(id any) string {
	r.t.Helper()
	var status string
	r.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(r.t.Context(), `SELECT status FROM fulfillment.shipments WHERE id = $1`, id).Scan(&status)
	})
	return status
}

// inApp chạy fn bằng crm_app trong ngữ cảnh tenant của stack; lỗi làm test đỏ.
func (r *orderRun) inApp(fn func(tx pgx.Tx) error) {
	r.t.Helper()
	if err := tenancy.WithTenant(r.t.Context(), r.s.app, r.s.tenant, fn); err != nil {
		r.t.Fatal(err)
	}
}
