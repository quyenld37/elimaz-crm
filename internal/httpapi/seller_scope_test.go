package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Người bán (ADR-29, Task 527): API chạy với người bán mặc định (ELIMAZ); vận đơn của người bán PARTNER1 và mọi việc
// con của nó không đọc, không ghi được qua API — cùng 404 như mã lạ — và không có trong danh sách, số đếm.

const sellerOwner = "chu-shop@elimaz.test"

type sellerEnv struct {
	*orderAPIEnv
	ship  *shiptest.Shop
	owner uuid.UUID
	// files: kho tệp của server, module tiền (nhập bảng kê, Task 531); nil khi env không cần.
	files *files.MemStore
}

// sellerShip là một vận đơn của một người bán cùng đơn và việc con của nó.
type sellerShip struct {
	order, shipment uuid.UUID
	kids            shiptest.Children
}

func newSellerEnv(t *testing.T) *sellerEnv {
	t.Helper()
	e := &sellerEnv{orderAPIEnv: newOrderAPIEnv(t)}
	tenant := e.shop.Tenant
	e.ship = &shiptest.Shop{Shop: e.shop}
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		for _, a := range []struct {
			dst    *uuid.UUID
			seller uuid.UUID
			ref    string
		}{{&e.ship.Account, e.shop.Seller, shiptest.AccountRef}, {&e.ship.PartnerAccount, e.shop.Partner, shiptest.PartnerAccountRef}} {
			id, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
				SellerID: a.seller, WarehouseID: e.shop.Warehouse, CarrierKey: shiptest.CarrierKey, Name: "Hãng giả",
				ExternalAccountRef: a.ref,
			})
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			*a.dst = id
		}
		return nil
	})
	e.owner = e.staff.User(t, tenant, sellerOwner, "active")
	e.staff.Grant(t, tenant, e.owner, rolePerms(t, "owner"), "all", "")
	d := shipDeps(t, e.orderAPIEnv)
	d.Cash = cash.New(cash.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry()}).Service
	e.srv = NewServer(d)
	return e
}

// shipmentOf tạo đơn 1 cái và vận đơn (creating) của người bán seller bằng tài khoản hãng giả của người bán đó, đánh
// dấu kẹt, rồi các việc con (shiptest.ChildrenOf).
func (e *sellerEnv) shipmentOf(seller uuid.UUID) sellerShip {
	e.t.Helper()
	v := e.variant(seller, 250000, 5)
	var s sellerShip
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.shop.Tenant, func(tx pgx.Tx) error {
		o, err := e.ship.Order(e.t.Context(), tx, seller, e.owner, 30000, order.ItemInput{VariantID: v, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sh, err := e.ship.Shipment(e.t.Context(), tx, seller, o.ID, e.owner)
		s.order, s.shipment = o.ID, sh.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), `UPDATE fulfillment.shipments SET stuck_since = now() - interval '1 day'
			WHERE id = $1`, s.shipment); err != nil {
			return err //nolint:wrapcheck // test
		}
		s.kids = shiptest.ChildrenOf(e.t, tx, e.shop.Tenant, s.shipment, s.order, e.owner)
		return nil
	})
	return s
}

func (e *sellerEnv) send(n int, method, path, body string) (int, string) {
	e.t.Helper()
	key := ""
	if method != http.MethodGet {
		key = fmt.Sprintf("seller-%d", n)
	}
	rec := serveAs(e.srv, e.keys, method, path, sellerOwner, key, body)
	return rec.Code, rec.Body.String()
}

// TestShipmentRoutes_OtherSellerNotFound: chủ shop (mọi quyền, mọi kho) gọi từng route theo mã của vận đơn PARTNER1
// (hay việc con, đơn của nó) → 404 với đúng mã lỗi của vận đơn lạ; vận đơn, việc con không đổi. Đối chứng: cùng route
// đọc trên vận đơn của người bán mặc định → 200.
func TestShipmentRoutes_OtherSellerNotFound(t *testing.T) {
	t.Parallel()
	e := newSellerEnv(t)
	own, other := e.shipmentOf(e.shop.Seller), e.shipmentOf(e.shop.Partner)
	sh, o, k := other.shipment.String(), other.order.String(), other.kids
	reason := `"reason":"Khách nhờ kiểm lại"`
	cases := []sellerCall{
		{http.MethodGet, "/api/v1/shipments/" + sh, ""},
		{http.MethodGet, "/api/v1/shipments/" + sh + "/events", ""},
		{http.MethodGet, "/api/v1/shipments/" + sh + "/address-changes", ""},
		{http.MethodGet, "/api/v1/shipments/" + sh + "/cod", ""},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/sync", ""},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/cancel", `{` + reason + `}`},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/resend", ""},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/resolve-create", `{"result":"not_created",` + reason + `}`},
		{
			http.MethodPost, "/api/v1/shipments/" + sh + "/manual-status",
			`{"to":"shipping","at":"2026-11-28T08:00:00Z",` + reason + `}`,
		},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/address-changes", `{"address":{"name":"Chị Mai",` +
			`"phone":"0901234568","line":"5 Nguyễn Huệ","provinceCode":"79","wardCode":"26734"}}`},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/partial-delivery", `{"items":[{"orderItemId":"` +
			uuid.NewString() + `","deliveredQty":1}],"codCollected":0,` + reason + `}`},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/corrections/preview", `{"to":"returning",` + reason + `}`},
		{http.MethodPost, "/api/v1/shipments/" + sh + "/corrections", `{"to":"returning","previewToken":"x",` + reason + `}`},
		{http.MethodPost, "/api/v1/address-changes/" + k.AddressChange.String() + "/resolve", `{"result":"rejected"}`},
		{http.MethodPost, "/api/v1/callback-tasks/" + k.Callback.String() + "/outcomes", `{"outcome":"no_answer","version":1}`},
		{http.MethodPost, "/api/v1/callback-tasks/" + k.Callback.String() + "/assign", `{"assigneeId":"` + e.owner.String() + `"}`},
		{http.MethodPost, "/api/v1/shipment-anomalies/" + k.Anomaly.String() + "/resolve", `{"resolution":"Đã kiểm"}`},
		{http.MethodGet, "/api/v1/label-batches/" + k.LabelBatch.String(), ""},
		{http.MethodPost, "/api/v1/label-batches", `{"shipmentIds":["` + sh + `"]}`},
		{http.MethodGet, "/api/v1/orders/" + o + "/shipments", ""},
		{http.MethodGet, "/api/v1/picking-list?shipmentIds=" + sh, ""},
		{http.MethodGet, "/api/v1/picking-list?shipmentIds=" + own.shipment.String() + "&shipmentIds=" + sh, ""},
		{http.MethodPost, "/api/v1/orders/" + o + "/shipments", `{"carrierAccountId":"` + e.ship.PartnerAccount.String() + `"}`},
		{http.MethodPost, "/api/v1/cod/adjustments/preview", `{"shipmentId":"` + sh + `","mode":"move","from":"carrier_holding",` +
			`"to":"shop_received","amount":1000,` + reason + `}`},
		{http.MethodPost, "/api/v1/cod/adjustments", `{"shipmentId":"` + sh + `","mode":"move","from":"carrier_holding",` +
			`"to":"shop_received","amount":1000,"token":"x",` + reason + `}`},
	}
	e.expectNotFound(0, cases, func() string { return e.snapshot(other) })
	for _, p := range []string{
		"/api/v1/shipments/" + own.shipment.String(), "/api/v1/shipments/" + own.shipment.String() + "/events",
		"/api/v1/orders/" + own.order.String() + "/shipments", "/api/v1/label-batches/" + own.kids.LabelBatch.String(),
		"/api/v1/picking-list?shipmentIds=" + own.shipment.String(),
	} {
		if code, body := e.send(0, http.MethodGet, p, ""); code != http.StatusOK {
			t.Errorf("đối chứng GET %s của người bán mặc định = %d %s, muốn 200", p, code, body)
		}
	}
}

// sellerCall là một lời gọi API trên dữ liệu của người bán khác.
type sellerCall struct{ method, path, body string }

// expectNotFound gọi từng ca (khóa chống trùng đánh số từ key) → muốn 404 not_found; state (chụp dữ liệu của người bán
// khác) không đổi sau mọi ca.
func (e *sellerEnv) expectNotFound(key int, cases []sellerCall, state func() string) {
	e.t.Helper()
	before := state()
	for i, c := range cases {
		code, body := e.send(key+i, c.method, c.path, c.body)
		if code != http.StatusNotFound || !strings.Contains(body, `"error":"not_found"`) {
			e.t.Errorf("%s %s của người bán khác = %d %s, muốn 404 not_found", c.method, c.path, code, body)
		}
	}
	if after := state(); after != before {
		e.t.Errorf("dữ liệu của người bán khác đổi: trước %s, sau %s", before, after)
	}
}

// scalar đọc một giá trị text của câu q (chủ bảng, trong tenant).
func (e *sellerEnv) scalar(q string, args ...any) string {
	e.t.Helper()
	var out string
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), q, args...).Scan(&out) //nolint:wrapcheck // test
	})
	return out
}

// snapshot: trạng thái, version của vận đơn và việc con, số vận đơn của đơn, số lô nhãn, số dòng sổ COD, audit.
func (e *sellerEnv) snapshot(s sellerShip) string {
	e.t.Helper()
	return e.scalar(`SELECT concat_ws('|',
			(SELECT status || ':' || version FROM fulfillment.shipments WHERE id = $1),
			(SELECT status || ':' || version || ':' || coalesce(assignee_id::text, '') FROM fulfillment.callback_tasks WHERE id = $2),
			(SELECT status FROM fulfillment.shipment_anomalies WHERE id = $3),
			(SELECT status || ':' || version FROM fulfillment.address_change_requests WHERE id = $4),
			(SELECT count(*) FROM fulfillment.shipments WHERE order_id = $5),
			(SELECT count(*) FROM fulfillment.label_batches),
			(SELECT count(*) FROM cash.cod_ledger_entries WHERE shipment_id = $1),
			(SELECT count(*) FROM identity.audit_logs))`,
		s.shipment, s.kids.Callback, s.kids.Anomaly, s.kids.AddressChange, s.order)
}

// TestShipmentLists_OtherSellerHidden: danh sách đơn kẹt, cần xem (cả số đếm), việc gọi lại (cả số việc mở) chỉ có
// vận đơn của người bán mặc định.
func TestShipmentLists_OtherSellerHidden(t *testing.T) {
	t.Parallel()
	e := newSellerEnv(t)
	own, other := e.shipmentOf(e.shop.Seller), e.shipmentOf(e.shop.Partner)
	for _, c := range []struct {
		path, field string
		want        uuid.UUID
		count       string
	}{
		{"/api/v1/shipments/stuck", "shipmentId", own.shipment, ""},
		{"/api/v1/shipments/review", "anomalyId", own.kids.Anomaly, `"review":1`},
		{"/api/v1/callback-tasks", "id", own.kids.Callback, `"openCount":1`},
	} {
		code, body := e.send(0, http.MethodGet, c.path, "")
		if code != http.StatusOK || !strings.Contains(body, `"`+c.field+`":"`+c.want.String()+`"`) {
			t.Errorf("%s = %d %s, muốn có %s %s của người bán mặc định", c.path, code, body, c.field, c.want)
		}
		for _, id := range []uuid.UUID{other.shipment, other.kids.Anomaly, other.kids.Callback} {
			if strings.Contains(body, id.String()) {
				t.Errorf("%s có mã %s của người bán khác: %s", c.path, id, body)
			}
		}
		if c.count != "" && !strings.Contains(body, c.count) {
			t.Errorf("%s: số đếm muốn %s (chỉ người bán mặc định): %s", c.path, c.count, body)
		}
	}
}

// TestAgedReservations_OtherSellerHidden: giữ hàng quá hạn (Task 531) chỉ có giữ hàng của đơn của người bán mặc định.
func TestAgedReservations_OtherSellerHidden(t *testing.T) {
	t.Parallel()
	e := newSellerEnv(t)
	own, other := e.shipmentOf(e.shop.Seller), e.shipmentOf(e.shop.Partner)
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_reservations SET state_changed_at = now() - interval '400 days'
			WHERE state IN ('held', 'in_transit')`)
		return err //nolint:wrapcheck // test
	})
	code, body := e.send(0, http.MethodGet, "/api/v1/inventory/aged-reservations", "")
	if code != http.StatusOK || !strings.Contains(body, `"orderId":"`+own.order.String()+`"`) {
		t.Fatalf("giữ hàng quá hạn = %d %s, muốn có đơn %s của người bán mặc định", code, body, own.order)
	}
	if strings.Contains(body, other.order.String()) {
		t.Errorf("giữ hàng quá hạn có đơn %s của người bán khác: %s", other.order, body)
	}
}
