package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

// shipOrderM3 gắn cho đơn id một vận đơn hãng fake_b đang chuyển (chủ bảng: vận đơn giả của testkit đổi sang
// shipping, mã hãng), ghi bút toán phải thu, một sự kiện hãng (carrier_api) và một sự kiện nhân viên staff (người làm
// staff) sau mốc at; trả mã vận đơn.
func (e *orderAPIEnv) shipOrderM3(id uuid.UUID, staff uuid.UUID, at time.Time) uuid.UUID {
	e.t.Helper()
	ship := uuid.New()
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		testkit.StubShipment(e.t, tx, id, ship)
		for _, q := range []string{
			`UPDATE fulfillment.shipments SET status = 'shipping', carrier_key = 'fake_b', cancel_kind = NULL,
				tracking_code = 'TRM328', cod_amount = 500000 WHERE id = @ship`,
			`UPDATE ord.orders SET current_shipment_id = @ship, packing_status = 'packed' WHERE id = @order`,
			`WITH t AS (INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, actor_type)
				SELECT tenant_id, id, 'expected', 'm328:' || id, 'system' FROM fulfillment.shipments WHERE id = @ship
				RETURNING tenant_id, id)
			INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, account, amount, shipment_id)
			SELECT t.tenant_id, t.id, x.a, x.n, @ship FROM t, (VALUES ('to_collect', 500000), ('sales_clearing', -500000)) AS x (a, n)`,
			`INSERT INTO fulfillment.shipment_events (tenant_id, shipment_id, event_key, raw_status, status, occurred_at, raw,
				canonical, source, applied)
			SELECT tenant_id, id, 'k1', 'picked', 'shipping', @at::timestamptz, '{}', 'picked_up', 'carrier_api', true
			FROM fulfillment.shipments WHERE id = @ship`,
			`INSERT INTO fulfillment.shipment_events (tenant_id, shipment_id, event_key, raw_status, status, occurred_at, raw,
				canonical, source, applied, actor_id)
			SELECT tenant_id, id, 'k2', 'fail', 'delivery_failed', @at::timestamptz + interval '1 minute', '{}',
			       'delivery_failed', 'staff', true, @staff::uuid
			FROM fulfillment.shipments WHERE id = @ship`,
		} {
			if _, err := tx.Exec(e.t.Context(), q, pgx.NamedArgs{"ship": ship, "order": id, "at": at, "staff": staff}); err != nil {
				return fmt.Errorf("vận đơn mẫu: %w", err)
			}
		}
		return nil
	})
	return ship
}

// checkPackFields: dòng có vận đơn mang trường của màn Đóng gói (Task 136): packingStatus, id vận đơn, tài khoản hãng,
// mode; chưa in nhãn thì không có labelPrintedAt.
func checkPackFields(t *testing.T, row, sh map[string]any, ship uuid.UUID) {
	t.Helper()
	if row["packingStatus"] != "packed" || sh["id"] != ship.String() || sh["carrierAccountId"] == nil ||
		sh["mode"] == nil || sh["labelPrintedAt"] != nil {
		t.Errorf("trường đóng gói: %v", row)
	}
}

// listOrders, getOrderTimeline qua HTTP với vận đơn (kế hoạch M3, Task M3-28): lọc shipmentStatus, carrierKey, codState,
// packingStatus, nhãn vận đơn; dòng có shipment (mã, hãng, mã hãng, trạng thái, nhãn hiển thị) và cod; đơn không vận
// đơn không có hai khối đó; dòng thời gian có mục shipment với actorType carrier (không người làm) và staff (tên nhân
// viên), các mục cũ có actorType staff. Màn Đóng gói (Task 136): dòng có packingStatus, vận đơn có id, carrierAccountId,
// mode (chưa in nhãn: không labelPrintedAt); lọc labelPrinted, warehouseId.
func TestOrderRead_ListTimelineM3(t *testing.T) {
	t.Parallel()
	e := newOrderReadEnv(t)
	v := e.variant(e.shop.Seller, 500000, 10)
	plain := e.mustCreate("m1", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	shipped := e.mustCreate("m2", m2Body("confirmed", fullAddr, 0, m2Line(v, 1, 0)))
	var staff uuid.UUID
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT id FROM identity.users WHERE email = $1", readSale).Scan(&staff)
	})
	sid := uuid.MustParse(fmt.Sprint(shipped["id"]))
	ship := e.shipOrderM3(sid, staff, time.Now().UTC().Add(time.Hour))
	warehouse := fmt.Sprint(shipped["warehouseId"])

	for _, c := range []struct{ name, query, want string }{
		{"đang chuyển", "shipmentStatus=shipping", fmt.Sprint(shipped["code"])},
		{"hãng fake_b", "carrierKey=fake_b", fmt.Sprint(shipped["code"])},
		{"hãng khác", "carrierKey=fake_a", ""},
		{"còn phải thu", "codState=collecting", fmt.Sprint(shipped["code"])},
		{"không COD", "codState=none", fmt.Sprint(plain["code"])},
		{"đã gói", "packingStatus=packed", fmt.Sprint(shipped["code"])},
		{"nhãn đang chuyển", "label=shipping", fmt.Sprint(shipped["code"])},
		{"nhãn đã xác nhận", "label=confirmed", fmt.Sprint(plain["code"])},
		{"chưa in nhãn", "labelPrinted=false", fmt.Sprint(shipped["code"])},
		{"đã in nhãn", "labelPrinted=true", ""},
		{"kho của đơn", "warehouseId=" + warehouse, fmt.Sprint(shipped["code"]) + "," + fmt.Sprint(plain["code"])},
		{"kho khác", "warehouseId=" + uuid.NewString(), ""},
	} {
		rec := e.get(orderViewer, "/api/v1/orders?"+c.query)
		if got := strings.Join(listCodes(t, decodeJSON(t, rec)), ","); rec.Code != http.StatusOK || got != c.want {
			t.Errorf("%s: %d %q, muốn %q", c.name, rec.Code, got, c.want)
		}
	}
	expect(t, "codState lạ", e.get(orderViewer, "/api/v1/orders?codState=khac"), http.StatusBadRequest, "")

	rec := e.get(orderViewer, "/api/v1/orders")
	body := decodeJSON(t, rec)
	items, _ := body["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 2 {
		t.Fatalf("danh sách: %d %s", rec.Code, rec.Body.String())
	}
	got := items[0].(map[string]any)
	sh, _ := got["shipment"].(map[string]any)
	cod, _ := got["cod"].(map[string]any)
	if got["code"] != shipped["code"] || got["label"] != "shipping" || sh == nil || sh["carrierKey"] != "fake_b" ||
		sh["trackingCode"] != "TRM328" || sh["status"] != "shipping" || sh["label"] != "Đang chuyển" || sh["code"] == "" ||
		cod == nil || cod["toCollect"] != float64(500000) || cod["carrierHolding"] != float64(0) ||
		cod["shopReceived"] != float64(0) {
		t.Errorf("dòng có vận đơn: %v", got)
	}
	checkPackFields(t, got, sh, ship)
	if first := items[1].(map[string]any); first["shipment"] != nil || first["cod"] != nil || first["packingStatus"] != "none" {
		t.Errorf("dòng không vận đơn: %v", first)
	}

	rec = e.get(orderViewer, "/api/v1/orders/"+fmt.Sprint(shipped["id"])+"/timeline")
	tl, _ := decodeJSON(t, rec)["items"].([]any)
	if rec.Code != http.StatusOK || len(tl) != 3 {
		t.Fatalf("timeline: %d %s", rec.Code, rec.Body.String())
	}
	want := []struct{ kind, actorType, actor string }{
		{"shipment", "staff", readSale}, {"shipment", "carrier", ""}, {"status", "staff", orderClerk},
	}
	for i, w := range want {
		it := tl[i].(map[string]any)
		actor, _ := it["actor"].(map[string]any)
		if it["kind"] != w.kind || it["actorType"] != w.actorType || (w.actor == "") != (actor == nil) ||
			(actor != nil && actor["name"] != w.actor) {
			t.Errorf("mục %d: %v, muốn %+v", i, it, w)
		}
	}
	if s, _ := tl[0].(map[string]any)["summary"].(map[string]any); s["status"] != "delivery_failed" || s["shipmentCode"] == nil {
		t.Errorf("summary mục nhân viên: %v", s)
	}
}
