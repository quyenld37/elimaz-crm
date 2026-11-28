package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Đảo "Thành công" (kế hoạch M3, Task M3-27; TD10, TI08) trên stack thật (api, ingest, worker, hãng giả, consumer
// cash). Một đơn 1 cái (250.000 + ship 30.000 = COD 280.000), tồn 5.

// deliveredRun tạo mẫu code tồn 5, đơn 1 cái, vận đơn; hãng giả báo picked_up rồi delivered; chờ đơn delivered và
// sổ COD hãng giữ 280.000. Trả biến thể, đơn, vận đơn, mã hãng.
func (r *orderRun) deliveredRun(code string) (v uuid.UUID, orderID, shipmentID, tracking string) {
	r.t.Helper()
	v = r.productWithStock(code, 5)
	orderID = str(r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000), http.StatusCreated)["id"])
	shipmentID = str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.Account}, http.StatusAccepted)["id"])
	tracking = r.awaitingTracking(shipmentID)
	r.advance(tracking, "picked_up")
	waitFor(r.t, stepBudget, "vận đơn shipping", func() bool {
		return r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)["status"] == "shipping"
	})
	r.advance(tracking, "delivered")
	r.deliveredHolding(orderID, shipmentID, 280000)
	r.stockIs(v, stock{onHand: 4, available: 4})
	return v, orderID, shipmentID, tracking
}

// deliveredHolding chờ đơn orderID delivered và sổ COD của vận đơn shipmentID có hãng đang giữ cod.
func (r *orderRun) deliveredHolding(orderID, shipmentID string, cod int64) {
	r.t.Helper()
	waitFor(r.t, stepBudget, "đơn delivered, COD hãng giữ", func() bool {
		o := r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)
		return o["status"] == "delivered" && r.cod(shipmentID)["carrier_holding"] == cod
	})
}

// awaitingTracking chờ vận đơn shipmentID sang awaiting_pickup có mã hãng; trả mã hãng.
func (r *orderRun) awaitingTracking(shipmentID string) string {
	r.t.Helper()
	var tracking string
	waitFor(r.t, stepBudget, "vận đơn awaiting_pickup", func() bool {
		got := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
		tracking = str(got["trackingCode"])
		return got["status"] == "awaiting_pickup" && tracking != ""
	})
	return tracking
}

// reversedIs chờ: vận đơn returning, đơn confirmed, hàng về đang chuyển (on_hand 5, in_transit 1), sổ COD mọi ngăn 0,
// một phiếu carrier_return requested; sổ tồn = số dư, tổng sổ COD = 0.
func (r *orderRun) reversedIs(v uuid.UUID, orderID, shipmentID string) {
	r.t.Helper()
	zero := map[string]int64{"to_collect": 0, "carrier_holding": 0, "shop_received": 0, "carrier_fee": 0, "sales_clearing": 0}
	waitFor(r.t, stepBudget, "vận đơn returning, đơn confirmed, sổ COD về 0", func() bool {
		sh := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
		o := r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)
		c := r.cod(shipmentID)
		for k, want := range zero {
			if c[k] != want {
				return false
			}
		}
		return sh["status"] == "returning" && o["status"] == "confirmed" &&
			r.stock(v) == stock{onHand: 5, inTransit: 1, available: 4}
	})
	if n := r.count(`SELECT count(*) FROM ord.returns WHERE shipment_id = $1 AND kind = 'carrier_return'
		AND status = 'requested'`, shipmentID); n != 1 {
		r.t.Fatalf("phiếu hoàn requested = %d, muốn 1", n)
	}
	if n := r.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'order.delivery_reversed'
		AND aggregate_id = $1`, orderID); n != 1 {
		r.t.Fatalf("order.delivery_reversed = %d, muốn 1", n)
	}
	r.ledgerMatchesBalance(v)
	if n := r.count(`SELECT coalesce(sum(amount), 0)::int FROM cash.cod_ledger_entries`); n != 0 {
		r.t.Fatalf("tổng sổ COD = %d, muốn 0", n)
	}
}

// receiveReturn: kho nhận phiếu hoàn của vận đơn (hàng còn bán được) → đơn returned.
func (r *orderRun) receiveReturn(orderID, shipmentID string) {
	r.t.Helper()
	var (
		id, item string
		version  int64
	)
	r.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(r.t.Context(), `SELECT r.id, r.version, i.id FROM ord.returns r
			JOIN ord.order_items i ON i.order_id = r.order_id WHERE r.shipment_id = $1 AND r.kind = 'carrier_return'`,
			shipmentID).Scan(&id, &version, &item)
	})
	r.mustCall(http.MethodPost, "/api/v1/returns/"+id+"/receive", map[string]any{
		"version": version, "lines": []map[string]any{{"orderItemId": item, "condition": "sellable"}},
	}, http.StatusOK)
	waitFor(r.t, stepBudget, "đơn returned", func() bool {
		return r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)["status"] == "returned"
	})
}

// TD10: hãng sửa "đã giao" thành "đang hoàn"; tin webhook chỉ là tín hiệu, lệnh tra bù (API có xác thực) mới đổi →
// vận đơn returning, đơn confirmed, đảo phiếu xuất, sổ COD về 0 (cash đảo collected, ghi returned), phiếu hoàn; cảnh
// báo carrier-corrected; kho nhận hàng hoàn → đơn returned.
func TestM3ReverseDelivered_CarrierCorrected(t *testing.T) {
	// Không t.Parallel (Task 484): newStack đặt tracer provider của cả tiến trình.
	r := newOrderRun(t)
	v, orderID, shipmentID, tracking := r.deliveredRun("E2E118A")
	r.advance(tracking, "returning")
	r.reversedIs(v, orderID, shipmentID)
	if n := r.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.carrier_corrected_alert'
		AND payload->>'shipmentId' = $1`, shipmentID); n != 1 {
		t.Fatalf("lệnh cảnh báo carrier-corrected = %d, muốn 1", n)
	}
	r.receiveReturn(orderID, shipmentID)
}

// TI08: nhân viên xem trước (ba câu tác động, không chặn), áp với token → như hãng sửa lại; áp lại token cũ → 409.
func TestM3ReverseDelivered_StaffCorrection(t *testing.T) {
	r := newOrderRun(t)
	v, orderID, shipmentID, _ := r.deliveredRun("E2E118B")
	p := r.mustCall(http.MethodPost, "/api/v1/shipments/"+shipmentID+"/corrections/preview",
		map[string]any{"to": "returning", "reason": "Bấm nhầm Thành công"}, http.StatusOK)
	effects, _ := p["effects"].([]any)
	blocking, _ := p["blocking"].([]any)
	if len(effects) != 3 || len(blocking) != 0 || p["from"] != "delivered" {
		t.Fatalf("xem trước = %v", p)
	}
	body := map[string]any{"to": "returning", "reason": "Bấm nhầm Thành công", "previewToken": p["token"]}
	r.mustCall(http.MethodPost, "/api/v1/shipments/"+shipmentID+"/corrections", body, http.StatusOK)
	r.reversedIs(v, orderID, shipmentID)
	if st, b, err := r.call(http.MethodPost, "/api/v1/shipments/"+shipmentID+"/corrections", body); err != nil ||
		st != http.StatusConflict {
		t.Fatalf("áp lại token cũ = %d %v (%v), muốn 409", st, b, err)
	}
	if n := r.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.correction'
		AND entity_id = $1`, shipmentID); n != 1 {
		t.Fatalf("audit đảo Thành công = %d, muốn 1", n)
	}
	r.receiveReturn(orderID, shipmentID)
}
