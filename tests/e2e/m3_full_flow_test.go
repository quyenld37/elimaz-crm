package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Đường trọn M3 (kế hoạch M3, Task M3-48) trên ba vai trò thật, hãng giả thật (tools/fakecarrier, tiến trình riêng) và
// đủ hai module M3 như cmd/crm (stackOptions.m3). Trong lúc tích hợp hãng thật tạm dừng (CS-21/11-1) chỉ dùng hãng giả.
// Mẫu 1 biến thể giá 250.000, ship 30.000 → COD đơn 1 cái 280.000.

// newM3Run dựng stack đủ M3 với hãng giả (biến môi trường thêm env của hãng giả).
func newM3Run(t *testing.T, env ...string) *orderRun {
	t.Helper()
	return newOrderRunWith(t, stackOptions{m3: true}, env...)
}

// Đơn chính (VC02, VC03, VC05, VC07, VC06, TI02, TI03): báo phí → gửi hãng bằng báo phí đó → hãng lấy hàng → giao thất
// bại → việc gọi lại mở → nhân viên ghi "giao lại" → lệnh giao lại tới hãng → giao thành công (xuất kho một lần, COD
// hãng giữ) → bảng kê elimaz_v1 của mã hãng → khớp → chốt (COD về shop, phí hãng tách) → đối chiếu đêm không phát hiện
// nào. Tổng sổ COD = 0, sổ kho = số dư, không việc dead.
func TestM3FullFlow(t *testing.T) {
	// Không t.Parallel (Task 484): newStack đặt tracer provider của cả tiến trình.
	r := newM3Run(t)
	v := r.productWithStock("M3FULL", 5)
	orderID := str(r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000), http.StatusCreated)["id"])
	quoteID := r.quote(orderID)
	shipmentID := str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.Account, "quoteId": quoteID, "service": "std"}, http.StatusAccepted)["id"])
	tracking := r.awaitingTracking(shipmentID)

	r.advance(tracking, fake.RawPickedUp)
	r.shipmentIs(shipmentID, "shipping")
	r.advance(tracking, fake.RawDeliveryFailed)
	r.shipmentIs(shipmentID, "delivery_failed")
	r.redeliver(shipmentID)
	r.advance(tracking, fake.RawDelivered)
	r.deliveredHolding(orderID, shipmentID, 280000)
	r.stockIs(v, stock{onHand: 4, available: 4})
	if n := r.count(`SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1 AND bucket = 'on_hand'
		AND delta < 0`, v); n != 1 {
		t.Fatalf("xuất kho %d lần, muốn 1", n)
	}

	sh := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
	fee, _ := sh["fee"].(float64)
	csv := fmt.Sprintf("ma_van_don,tien_cod,phi_van_chuyen,phi_khac,thuc_nhan\n%s,280000,%d,0,%d\n",
		tracking, int64(fee), 280000-int64(fee))
	statementID := r.confirmStatement(csv, 1)
	// Chốt chỉ phát cod_statement.confirmed; lô ghi tiền chạy ở consumer sau commit (cash.ConfirmStatement, Task 527).
	var c map[string]int64
	waitFor(t, 3*stepBudget, "lô ghi tiền của bảng kê "+statementID, func() bool {
		c = r.cod(shipmentID)
		return c["carrier_holding"] == 0 && c["shop_received"] == 280000-int64(fee) && c["carrier_fee"] == int64(fee)
	})
	r.codLedgerZero()

	r.nightlyReconcileOK()
	r.ledgerMatchesBalance(v)
	r.noDeadJobs()
}

// Đơn hủy khi chờ thu gom (TD03): vận đơn awaiting_pickup, khách hủy đơn → đơn cancel_requested, hàng vẫn giữ → lệnh
// hủy tới hãng giả → đơn cancelled, vận đơn cancelled (order_cancel), nhả hàng. Sổ COD không có dòng của vận đơn.
func TestM3FullFlow_CancelAwaitingPickup(t *testing.T) {
	r := newM3Run(t)
	v := r.productWithStock("M3CANCEL", 5)
	o := r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000), http.StatusCreated)
	orderID := str(o["id"])
	shipmentID := str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.Account}, http.StatusAccepted)["id"])
	r.awaitingTracking(shipmentID)
	o = r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)
	r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/cancel",
		map[string]any{"version": o["version"], "reason": "customer_changed_mind"}, http.StatusOK)
	waitForAPI(t, stepBudget, "đơn cancelled, vận đơn cancelled order_cancel", func() bool {
		got := r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID, nil, http.StatusOK)
		sh := r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)
		return got["status"] == "cancelled" && sh["status"] == "cancelled" && sh["cancelKind"] == "order_cancel"
	})
	r.stockIs(v, stock{onHand: 5, available: 5})
	if n := r.count(`SELECT count(*) FROM cash.cod_ledger_entries WHERE shipment_id = $1`, shipmentID); n != 0 {
		t.Errorf("vận đơn đã hủy trước thu gom có %d dòng sổ COD", n)
	}
	r.ledgerMatchesBalance(v)
	r.noDeadJobs()
}

// Đơn hoàn (TD02): hãng lấy hàng → đang hoàn → hãng báo đã hoàn (Q-9: chỉ ghi mốc) → phiếu hoàn carrier_return chờ kho
// → kho bấm nhận → đơn, vận đơn returned, tồn tăng lại đúng một lần; sổ COD mọi ngăn về 0.
func TestM3FullFlow_CarrierReturn(t *testing.T) {
	r := newM3Run(t)
	v := r.productWithStock("M3RETURN", 5)
	orderID := str(r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 1, 30000), http.StatusCreated)["id"])
	shipmentID, tracking := r.pickedUp(orderID)
	r.advance(tracking, fake.RawReturning)
	r.shipmentIs(shipmentID, "returning")
	r.advance(tracking, fake.RawReturned)
	// Q-9: hãng báo "đã hoàn" chỉ ghi mốc carrier_reported_returned_at, vận đơn vẫn returning tới khi kho nhận (TD02).
	waitFor(t, stepBudget, "mốc hãng báo đã hoàn", func() bool {
		return r.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND status = 'returning'
			AND carrier_reported_returned_at IS NOT NULL`, shipmentID) == 1
	})
	// Hàng về chưa vào tồn bán được tới khi kho bấm nhận (TD02).
	if got := r.stock(v); got.onHand != 5 || got.available != 4 {
		t.Fatalf("trước khi kho nhận: tồn %+v, muốn on_hand 5 (in_transit 1), available 4", got)
	}
	r.receiveReturn(orderID, shipmentID)
	r.shipmentIs(shipmentID, "returned")
	r.stockIs(v, stock{onHand: 5, available: 5})
	for k, b := range r.cod(shipmentID) {
		if b != 0 {
			t.Errorf("sổ COD ngăn %s = %d sau hoàn, muốn 0", k, b)
		}
	}
	r.codLedgerZero()
	r.ledgerMatchesBalance(v)
	r.noDeadJobs()
}

// Khách trả 1 trong 2 món (TD05): đơn 2 cái giao xong → phiếu trả 1 cái → kho nhận (bán được) → nghĩa vụ hoàn tiền mở
// cho phiếu đó → kế toán chi đủ → nghĩa vụ paid; tồn tăng 1.
func TestM3FullFlow_CustomerReturnRefund(t *testing.T) {
	r := newM3Run(t)
	v := r.productWithStock("M3REFUND", 5)
	o := r.mustCall(http.MethodPost, "/api/v1/orders", orderReq(v, 2, 30000), http.StatusCreated)
	orderID := str(o["id"])
	items, _ := o["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("đơn = %v, muốn 1 dòng", o)
	}
	itemID := str(items[0].(map[string]any)["id"]) //nolint:forcetypeassert // test
	shipmentID, tracking := r.pickedUp(orderID)
	r.advance(tracking, fake.RawDelivered)
	r.deliveredHolding(orderID, shipmentID, 530000)
	r.stockIs(v, stock{onHand: 3, available: 3})

	ret := r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/returns", map[string]any{
		"kind": "customer_return", "reason": "Không vừa", "lines": []map[string]any{{"orderItemId": itemID, "qty": 1}},
	}, http.StatusCreated)
	returnID := str(ret["id"])
	r.mustCall(http.MethodPost, "/api/v1/returns/"+returnID+"/receive", map[string]any{
		"version": ret["version"], "lines": []map[string]any{{"orderItemId": itemID, "condition": "sellable", "sellable": 1}},
	}, http.StatusOK)
	r.stockIs(v, stock{onHand: 4, available: 4})

	var obligation map[string]any
	waitForAPI(t, stepBudget, "nghĩa vụ hoàn tiền mở của phiếu trả", func() bool {
		page := r.mustCall(http.MethodGet, "/api/v1/refund-obligations?status=open", nil, http.StatusOK)
		list, _ := page["items"].([]any)
		for _, it := range list {
			if m, _ := it.(map[string]any); m["returnId"] == returnID {
				obligation = m
				return true
			}
		}
		return false
	})
	amount, _ := obligation["amount"].(float64)
	if obligation["source"] != "customer_return" || amount <= 0 || amount > 250000 {
		t.Fatalf("nghĩa vụ hoàn = %v, muốn customer_return, 0 < số tiền ≤ 250000", obligation)
	}
	paid := r.mustCall(http.MethodPost, "/api/v1/refund-obligations/"+str(obligation["id"])+"/payments",
		map[string]any{"amount": int64(amount), "reference": "CK thử"}, http.StatusOK)
	if paid["status"] != "paid" || paid["paidAmount"] != amount {
		t.Fatalf("sau khi chi = %v", paid)
	}
	r.ledgerMatchesBalance(v)
	r.noDeadJobs()
}

// quote xin báo phí của đơn ở tài khoản hãng giả của người bán mặc định và chờ báo phí ok; trả quoteId.
func (r *orderRun) quote(orderID string) string {
	r.t.Helper()
	acc := r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipping-quotes",
		map[string]any{"carrierAccountIds": []any{r.s.shop.Account}}, http.StatusAccepted)
	reqID := str(acc["requestId"])
	var id string
	waitForAPI(r.t, stepBudget, "báo phí ok", func() bool {
		page := r.mustCall(http.MethodGet, "/api/v1/orders/"+orderID+"/shipping-quotes?requestId="+reqID, nil, http.StatusOK)
		list, _ := page["items"].([]any)
		for _, it := range list {
			m, _ := it.(map[string]any)
			if m["status"] == "ok" && m["service"] == "std" {
				id = str(m["quoteId"])
				return true
			}
			if m["status"] == "error" || m["status"] == "unavailable" {
				r.t.Fatalf("báo phí lỗi: %v", m)
			}
		}
		return false
	})
	return id
}

// pickedUp gửi đơn orderID cho hãng giả bằng tài khoản của người bán mặc định, chờ chờ thu gom, hãng báo lấy hàng và
// chờ vận đơn shipping; trả mã vận đơn, mã hãng.
func (r *orderRun) pickedUp(orderID string) (shipmentID, tracking string) {
	r.t.Helper()
	shipmentID = str(r.mustCall(http.MethodPost, "/api/v1/orders/"+orderID+"/shipments",
		map[string]any{"carrierAccountId": r.s.shop.Account}, http.StatusAccepted)["id"])
	tracking = r.awaitingTracking(shipmentID)
	r.advance(tracking, fake.RawPickedUp)
	r.shipmentIs(shipmentID, "shipping")
	return shipmentID, tracking
}

// shipmentIs chờ vận đơn sang trạng thái status.
func (r *orderRun) shipmentIs(shipmentID, status string) {
	r.t.Helper()
	waitForAPI(r.t, stepBudget, "vận đơn "+status, func() bool {
		return r.mustCall(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil, http.StatusOK)["status"] == status
	})
}

// redeliver: việc gọi lại mở của vận đơn (VC07) → nhân viên ghi "giao lại" → lệnh command.shipment.redeliver tới hãng
// giả xong; việc gọi lại done.
func (r *orderRun) redeliver(shipmentID string) {
	r.t.Helper()
	var task map[string]any
	waitForAPI(r.t, stepBudget, "việc gọi lại mở", func() bool {
		page := r.mustCall(http.MethodGet, "/api/v1/callback-tasks?status=open", nil, http.StatusOK)
		list, _ := page["items"].([]any)
		for _, it := range list {
			if m, _ := it.(map[string]any); m["shipmentId"] == shipmentID {
				task = m
				return true
			}
		}
		return false
	})
	got := r.mustCall(http.MethodPost, "/api/v1/callback-tasks/"+str(task["id"])+"/outcomes",
		map[string]any{"outcome": "redeliver", "note": "Khách hẹn chiều nay", "version": task["version"]}, http.StatusOK)
	if got["status"] != "done" {
		r.t.Fatalf("việc gọi lại sau giao lại = %v, muốn done", got)
	}
	waitFor(r.t, stepBudget, "lệnh giao lại xong", func() bool {
		return r.s.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'`,
			fulfillment.RedeliverKind().Name) == 1
	})
}

// confirmStatement tải bảng kê csv (elimaz_v1) của tài khoản hãng giả, chờ đọc tệp và khớp (matched, lines dòng), chốt
// với version hiện tại; trả mã bảng kê.
func (r *orderRun) confirmStatement(csv string, lines int) string {
	r.t.Helper()
	id := r.importStatement(csv)
	head := r.waitStatement(id, "matched", 3*stepBudget)
	if head["lineCount"] != float64(lines) {
		r.t.Fatalf("bảng kê %v, muốn %d dòng", head, lines)
	}
	got := r.mustCall(http.MethodPost, "/api/v1/cod-statements/"+id+"/confirm", map[string]any{"version": head["version"]},
		http.StatusOK)
	if s, _ := got["statement"].(map[string]any); s["status"] != "confirmed" {
		r.t.Fatalf("chốt bảng kê = %v", got)
	}
	return id
}

// importStatement tải csv (mục đích bảng kê COD) và nhập bảng kê elimaz_v1 của tài khoản hãng giả; trả mã bảng kê.
func (r *orderRun) importStatement(csv string) string {
	r.t.Helper()
	file := r.s.uploadFile(cash.StatementPurposeKey, "bang-ke-"+uuid.NewString()+".csv", "text/csv", []byte(csv))
	st := r.mustCall(http.MethodPost, "/api/v1/cod-statements",
		map[string]any{"carrierAccountId": r.s.shop.Account, "fileId": file, "format": "elimaz_v1"}, http.StatusAccepted)
	return str(st["id"])
}

// waitStatement chờ bảng kê id tới trạng thái status trong budget; trả phần đầu (statement) của bảng kê.
func (r *orderRun) waitStatement(id, status string, budget time.Duration) map[string]any {
	r.t.Helper()
	var head map[string]any
	waitForAPI(r.t, budget, "bảng kê "+status, func() bool {
		got := r.mustCall(http.MethodGet, "/api/v1/cod-statements/"+id+"?limit=1", nil, http.StatusOK)
		head, _ = got["statement"].(map[string]any)
		return head["status"] == status
	})
	return head
}

// nightlyReconcileOK chạy lịch đối chiếu sổ tiền (cash.reconcile, TI03) cho kỳ hiện tại và chờ lần chạy; danh sách
// phát hiện của lần chạy đó rỗng.
func (r *orderRun) nightlyReconcileOK() {
	r.t.Helper()
	r.s.runSchedule(cash.ReconcileScheduleName, time.Now().UTC().Truncate(time.Hour))
	var page map[string]any
	waitForAPI(r.t, 3*stepBudget, "lần đối chiếu đêm", func() bool {
		page = r.mustCall(http.MethodGet, "/api/v1/cod/reconcile-findings", nil, http.StatusOK)
		return page["runId"] != nil
	})
	if items, _ := page["items"].([]any); len(items) != 0 {
		r.t.Fatalf("đối chiếu đêm có phát hiện: %v", items)
	}
}

// codLedgerZero: tổng mọi dòng sổ COD của tenant bằng 0 (TI10).
func (r *orderRun) codLedgerZero() {
	r.t.Helper()
	if n := r.count(`SELECT coalesce(sum(amount), 0)::bigint FROM cash.cod_ledger_entries`); n != 0 {
		r.t.Fatalf("tổng sổ COD = %d, muốn 0", n)
	}
}

// noDeadJobs: không việc nền nào dead.
func (r *orderRun) noDeadJobs() {
	r.t.Helper()
	if n := r.s.count(`SELECT count(*) FROM platform.jobs WHERE status = 'dead'`); n != 0 {
		var kinds []string
		r.inTenant(func(tx pgx.Tx) error {
			rows, err := tx.Query(r.t.Context(), `SELECT kind FROM platform.jobs WHERE status = 'dead'`)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			kinds, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err //nolint:wrapcheck // test
		})
		r.t.Fatalf("%d việc dead: %v", n, kinds)
	}
}
