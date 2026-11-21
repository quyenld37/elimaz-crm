package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	exchangeReturnOnly = "doi-chi-tra@elimaz.test"
	exchangeMain       = "doi-kho-chinh@elimaz.test"
	exchangeW2         = "doi-kho-phu@elimaz.test"
)

// TestReturnAPI_ExchangeWarehouseScope (Task 436, B-06): phiếu đổi tạo đơn thay thế giữ hàng ở kho của đơn gốc, nên
// ngoài order.return của route còn cần order.create ở đúng kho đó (luật Task 411 của createOrder). Chỉ order.return,
// hay order.create theo kho khác → 403 forbidden, không phiếu, không giữ hàng; phiếu khách trả thường không cần
// order.create. order.create theo đúng kho chính → 201. Từ chối không lưu theo khóa: được cấp quyền rồi gửi lại cùng
// khóa → 201.
func TestReturnAPI_ExchangeWarehouseScope(t *testing.T) {
	e := newReturnAPIEnv(t)
	tenant := e.shop.Tenant
	w2 := uuid.New() // kho phụ: nhân viên có order.create theo kho này không đổi được hàng của đơn ở kho chính
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO inventory.warehouses (id, tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, $3, 'DOI2', 'Kho phụ đổi hàng')`, w2, tenant, e.shop.Seller)
		return err //nolint:wrapcheck // test
	})
	ret := []string{"order.read", "order.return"}
	only := e.staff.User(t, tenant, exchangeReturnOnly, "active")
	e.staff.Grant(t, tenant, only, ret, "all", "")
	for email, wh := range map[string]uuid.UUID{exchangeMain: e.shop.Warehouse, exchangeW2: w2} {
		u := e.staff.User(t, tenant, email, "active")
		e.staff.Grant(t, tenant, u, ret, "all", "")
		e.staff.Grant(t, tenant, u, []string{"order.create"}, "warehouse", wh.String())
	}
	v, l := e.variant(e.shop.Seller, 250000, 5), e.variant(e.shop.Seller, 250000, 5)
	o := e.deliveredAPIOrder("d1", v)
	path := fmt.Sprintf("/api/v1/orders/%s/returns", o["id"])
	exchange := fmt.Sprintf(`{"kind":"exchange","lines":[{"orderItemId":"%s","qty":1}],"replacement":{"items":[%s]}}`,
		firstItem(o), m2Line(l, 1, 0))
	held := func() int {
		return e.count(`SELECT coalesce(sum(held), 0) FROM inventory.stock_available_v WHERE variant_id = $1`, l)
	}
	returns := func() int {
		return e.count(`SELECT count(*) FROM ord.returns WHERE order_id = $1`, uuid.MustParse(fmt.Sprint(o["id"])))
	}

	for i, email := range []string{exchangeReturnOnly, exchangeW2} {
		expect(t, "đổi hàng của "+email, e.returnPost(email, fmt.Sprintf("x%d", i), path, exchange), http.StatusForbidden,
			"forbidden")
	}
	if h, n := held(), returns(); h != 0 || n != 0 {
		t.Fatalf("sau 403: giữ %d, %d phiếu; muốn 0, 0", h, n)
	}
	customer := fmt.Sprintf(`{"kind":"customer_return","lines":[{"orderItemId":"%s","qty":1}]}`, firstItem(o))
	rec := e.returnPost(exchangeReturnOnly, "c1", path, customer)
	r := decodeJSON(t, rec)
	if rec.Code != http.StatusCreated {
		t.Fatalf("khách trả chỉ với order.return: %d %s", rec.Code, rec.Body.String())
	}
	if rec = e.returnPost(exchangeReturnOnly, "c2", fmt.Sprintf("/api/v1/returns/%s/cancel", r["id"]),
		fmt.Sprintf(`{"version":%v}`, r["version"])); rec.Code != http.StatusOK {
		t.Fatalf("hủy phiếu khách trả: %d %s", rec.Code, rec.Body.String())
	}

	rec = e.returnPost(exchangeMain, "m1", path, exchange)
	r = decodeJSON(t, rec)
	if rec.Code != http.StatusCreated || r["replacementOrderId"] == nil || held() != 1 {
		t.Fatalf("đổi hàng theo kho chính: %d %s, giữ %d", rec.Code, rec.Body.String(), held())
	}
	if rec = e.returnPost(exchangeMain, "m2", fmt.Sprintf("/api/v1/returns/%s/cancel", r["id"]),
		fmt.Sprintf(`{"version":%v}`, r["version"])); rec.Code != http.StatusOK || held() != 0 {
		t.Fatalf("hủy phiếu đổi: %d %s, giữ %d", rec.Code, rec.Body.String(), held())
	}

	e.staff.Grant(t, tenant, only, []string{"order.create"}, "warehouse", e.shop.Warehouse.String())
	if rec = e.returnPost(exchangeReturnOnly, "x0", path, exchange); rec.Code != http.StatusCreated {
		t.Fatalf("được cấp kho rồi gửi lại cùng khóa: %d %s", rec.Code, rec.Body.String())
	}
}
