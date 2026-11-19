package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
)

const (
	orderClerkMain = "len-don-kho-chinh@elimaz.test"
	orderClerkW2   = "len-don-kho-phu@elimaz.test"
	orderClerkPage = "len-don-page@elimaz.test"
)

// orderScopeEnv: shop có kho chính MAIN và kho phụ W2; một biến thể có 5 cái ở mỗi kho. Nhân viên: orderClerk
// (order.create toàn shop), orderClerkMain (theo kho MAIN), orderClerkW2 (theo kho W2), orderClerkPage (theo Page).
type orderScopeEnv struct {
	*orderAPIEnv
	w2 uuid.UUID
	v  uuid.UUID
}

func newOrderScopeEnv(t *testing.T) *orderScopeEnv {
	t.Helper()
	e := &orderScopeEnv{orderAPIEnv: newOrderAPIEnv(t)}
	e.v = e.variant(e.shop.Seller, 100000, 5)
	tenant := e.shop.Tenant
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, tenant, e.shop.Seller).Scan(&e.w2); err != nil {
			return err //nolint:wrapcheck // test
		}
		invtest.SeedStock(t, tx, e.v, e.w2, 5)
		return nil
	})
	perms := []string{"order.create", "order.read"}
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, orderClerkMain, "active"), perms, "warehouse", e.shop.Warehouse.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, orderClerkW2, "active"), perms, "warehouse", e.w2.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, orderClerkPage, "active"), perms, "page", "p1")
	return e
}

// bodyAt là thân createOrder kiểu M1 (1 cái của e.v) giữ ở kho wh (uuid.Nil: bỏ trống warehouseId → kho chính).
func (e *orderScopeEnv) bodyAt(wh uuid.UUID) string {
	b := orderBody(0, line(e.v, 1))
	if wh == uuid.Nil {
		return b
	}
	return strings.Replace(b, `{"customer"`, fmt.Sprintf(`{"warehouseId":"%s","customer"`, wh), 1)
}

func (e *orderScopeEnv) held(wh uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(held), 0) FROM inventory.stock_available_v WHERE variant_id = $1 AND warehouse_id = $2`, e.v, wh)
}

// TestOrderAPI_WarehouseScope (Task 411, D-02): lên đơn chỉ giữ hàng ở kho trong phạm vi order.create của nhân viên
// (grant all hoặc warehouse:<kho>); ngoài phạm vi → 403 forbidden, không đơn, không giữ hàng, kể cả khi bỏ trống
// warehouseId (kho chính) và khi kho không có.
func TestOrderAPI_WarehouseScope(t *testing.T) {
	e := newOrderScopeEnv(t)
	main := e.shop.Warehouse
	cases := []struct {
		name   string
		email  string
		wh     uuid.UUID
		intent string
		want   int
	}{
		{"toàn shop, kho phụ", orderClerk, e.w2, "", http.StatusCreated},
		{"toàn shop, bỏ trống kho", orderClerk, uuid.Nil, "", http.StatusCreated},
		{"theo kho chính, bỏ trống kho", orderClerkMain, uuid.Nil, "", http.StatusCreated},
		{"theo kho chính, gửi đúng kho chính", orderClerkMain, main, "", http.StatusCreated},
		{"theo kho chính, kho phụ", orderClerkMain, e.w2, "", http.StatusForbidden},
		{"theo kho chính, kho phụ, đơn nháp M2", orderClerkMain, e.w2, "draft", http.StatusForbidden},
		{"theo kho chính, kho không có", orderClerkMain, uuid.New(), "", http.StatusForbidden},
		{"theo kho phụ, kho phụ", orderClerkW2, e.w2, "", http.StatusCreated},
		{"theo kho phụ, bỏ trống kho (kho chính)", orderClerkW2, uuid.Nil, "", http.StatusForbidden},
		{"theo Page, bỏ trống kho", orderClerkPage, uuid.Nil, "", http.StatusForbidden},
		{"theo Page, kho phụ", orderClerkPage, e.w2, "", http.StatusForbidden},
	}
	for i, c := range cases {
		beforeOrders, beforeMain, beforeW2 := e.count(`SELECT count(*) FROM ord.orders`), e.held(main), e.held(e.w2)
		body := e.bodyAt(c.wh)
		if c.intent != "" {
			body = strings.Replace(body, `{`, `{"intent":"`+c.intent+`",`, 1)
		}
		rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", c.email, fmt.Sprintf("s%d", i), body)
		if c.want == http.StatusForbidden {
			expect(t, c.name, rec, http.StatusForbidden, "forbidden")
			if n, m, w := e.count(`SELECT count(*) FROM ord.orders`), e.held(main), e.held(e.w2); n != beforeOrders || m != beforeMain || w != beforeW2 {
				t.Errorf("%s: bị chặn mà vẫn ghi: đơn %d→%d, giữ chính %d→%d, giữ phụ %d→%d", c.name, beforeOrders, n, beforeMain, m, beforeW2, w)
			}
			continue
		}
		if rec.Code != c.want {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		wantWh := c.wh
		if wantWh == uuid.Nil {
			wantWh = main
		}
		if got := decodeJSON(t, rec)["warehouseId"]; got != wantWh.String() {
			t.Errorf("%s: đơn giữ ở kho %v, mong %s", c.name, got, wantWh)
		}
	}
}

// TestOrderAPI_WarehouseScopeNotStored: 403 vì ngoài phạm vi không lưu theo Idempotency-Key — được cấp kho rồi gửi lại
// cùng khóa thì tạo được đơn.
func TestOrderAPI_WarehouseScopeNotStored(t *testing.T) {
	e := newOrderScopeEnv(t)
	rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", orderClerkMain, "k1", e.bodyAt(e.w2))
	expect(t, "ngoài phạm vi", rec, http.StatusForbidden, "forbidden")
	var uid uuid.UUID
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id FROM identity.users WHERE email = $1`, orderClerkMain).Scan(&uid)
	})
	e.staff.Grant(t, e.shop.Tenant, uid, []string{"order.create"}, "warehouse", e.w2.String())
	rec = serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/orders", orderClerkMain, "k1", e.bodyAt(e.w2))
	if rec.Code != http.StatusCreated {
		t.Fatalf("gửi lại sau khi được cấp kho: %d %s", rec.Code, rec.Body.String())
	}
}
