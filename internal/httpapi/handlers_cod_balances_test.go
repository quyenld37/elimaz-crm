package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// newCodShop dựng cửa hàng thử (shiptest.Shop) và người đặt đơn trong tenant của e (một lần mỗi test).
func (e *m3Env) newCodShop() (*shiptest.Shop, uuid.UUID) {
	e.t.Helper()
	user := e.staff.User(e.t, e.tenant, "dat-don@elimaz.test", "active")
	var shop *shiptest.Shop
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		shop = shiptest.NewShop(e.t, tx, e.tenant)
		return nil
	})
	return shop, user
}

// codShipment tạo đơn, vận đơn thật COD cod của shop (chủ bảng đặt cod_amount) và cho consumer cash ghi sổ theo các
// bước steps (Seq 3, 4, …; mỗi bước một danh sách tác động).
func (e *m3Env) codShipment(shop *shiptest.Shop, user uuid.UUID, cod int64, steps ...[]string) uuid.UUID {
	e.t.Helper()
	ctx := e.t.Context()
	// Đơn, vận đơn và COD trong một transaction của chủ bảng (đặt cod_amount cần chủ bảng).
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		variant := shop.Variant(e.t, tx, shop.Seller, 250000, 5)
		o, err := shop.Order(ctx, tx, shop.Seller, user, 0, order.ItemInput{VariantID: variant, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sh, err := shop.Shipment(ctx, tx, shop.Seller, o.ID, user)
		if id = sh.ID; err == nil && cod != 0 {
			_, err = tx.Exec(ctx, "UPDATE fulfillment.shipments SET cod_amount = $2 WHERE id = $1", id, cod)
		}
		return err //nolint:wrapcheck // test
	})
	for i, effects := range steps {
		p, err := json.Marshal(fulfillment.StatusChanged{ShipmentID: id, Status: "x", Effects: effects})
		if err != nil {
			e.t.Fatal(err)
		}
		ev := events.StoredEvent{ID: uuid.New(), TenantID: e.tenant, Seq: int64(3 + i), Payload: p}
		if err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
			return cash.Consumer{}.Handle(ctx, ev, tx)
		}); err != nil {
			e.t.Fatalf("ghi sổ bước %d: %v", i, err)
		}
	}
	return id
}

// getShipmentCod qua HTTP (Task M3-29): kế toán đọc số dư năm ngăn và bút toán theo thứ tự ghi (vế, lý do, tác nhân
// hãng, bút toán bị đảo); vận đơn chưa có sổ → mọi ngăn 0, danh sách rỗng; vận đơn không có → 404; Kho (không có
// cash.cod.read) → 403.
func TestShipmentCodAPI(t *testing.T) {
	e := newM3Env(t)
	shop, user := e.newCodShop()
	s := e.codShipment(shop, user, 630000, []string{"picked_up", "delivered"}, []string{"delivery_reversed", "returning"})
	code, b := e.send(http.MethodGet, "/api/v1/shipments/"+s.String()+"/cod", m3Accounter, "")
	if code != http.StatusOK {
		t.Fatalf("GET: %d %v", code, b)
	}
	bal := b["balances"].(map[string]any) //nolint:forcetypeassert // test
	for _, k := range []string{"toCollect", "carrierHolding", "shopReceived", "carrierFee", "salesClearing"} {
		if bal[k] != float64(0) {
			t.Errorf("%s = %v", k, bal[k])
		}
	}
	trs := b["transfers"].([]any) //nolint:forcetypeassert // test
	var kinds []string
	for _, x := range trs {
		kinds = append(kinds, x.(map[string]any)["kind"].(string)) //nolint:forcetypeassert // test
	}
	if len(kinds) != 4 || kinds[0] != "expected" || kinds[1] != "collected" || kinds[2] != "reversal" ||
		kinds[3] != "returned" {
		t.Fatalf("bút toán %v", kinds)
	}
	collected, rev := trs[1].(map[string]any), trs[2].(map[string]any) //nolint:forcetypeassert // test
	actor := rev["actor"].(map[string]any)                             //nolint:forcetypeassert // test
	if rev["reversesTransferId"] != collected["id"] || rev["reason"] == nil || actor["name"] != "Hãng vận chuyển" ||
		actor["id"] != uuid.Nil.String() || len(rev["entries"].([]any)) != 2 { //nolint:forcetypeassert // test
		t.Errorf("bút toán đảo %v", rev)
	}
	entry := collected["entries"].([]any)[0].(map[string]any) //nolint:forcetypeassert // test
	if entry["account"] != "to_collect" || entry["amount"] != float64(-630000) {
		t.Errorf("vế đầu của collected %v", entry)
	}

	fresh := e.codShipment(shop, user, 500000)
	if code, b := e.send(http.MethodGet, "/api/v1/shipments/"+fresh.String()+"/cod", m3Owner, ""); code != http.StatusOK ||
		len(b["transfers"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Errorf("vận đơn chưa có sổ: %d %v", code, b)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/shipments/"+uuid.NewString()+"/cod", m3Accounter, ""); code != http.StatusNotFound ||
		b["error"] != "not_found" {
		t.Errorf("vận đơn không có: %d %v", code, b)
	}
	if code, _ := e.send(http.MethodGet, "/api/v1/shipments/"+s.String()+"/cod", m3Warehouse, ""); code != http.StatusForbidden {
		t.Errorf("Kho: mong 403, có %d", code)
	}
}
