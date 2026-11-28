package web_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// serviceKeyWith tạo khóa dịch vụ tên "Website <quyền>+<quyền>…" có các quyền perms (website thử: web.catalog.read +
// web.catalog.preview).
func (e *env) serviceKeyWith(perms ...identity.Permission) string {
	e.t.Helper()
	names := make([]string, len(perms))
	for i, p := range perms {
		names[i] = string(p)
	}
	var full string
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		_, full, err = identity.CreateServiceKey(e.t.Context(), tx, identity.CreateServiceKeyInput{
			Name: "Website " + strings.Join(names, "+"), Permissions: perms, Actor: e.owner,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo khóa %v: %v", perms, err)
	}
	return full
}

// unpublish đưa mẫu id về nháp qua catalog.Writer.Unpublish (như unpublishProducts).
func (e *env) unpublish(id uuid.UUID) {
	e.t.Helper()
	e.app(func(tx pgx.Tx) error {
		got, err := e.writer().Unpublish(e.t.Context(), tx, []uuid.UUID{id}, e.owner, time.Now())
		if err == nil && !slices.Equal(got, []uuid.UUID{id}) {
			e.t.Fatalf("Unpublish: %v", got)
		}
		return err //nolint:wrapcheck // test
	})
}

// previewOf trả (có mẫu id trong trang, giá trị preview của nó).
func previewOf(t *testing.T, items []map[string]any, id uuid.UUID) (found, preview bool) {
	t.Helper()
	for _, it := range items {
		if it["id"] == id.String() {
			p, _ := it["preview"].(bool)
			return true, p
		}
	}
	return false, false
}

// [MO01] Website thử (khóa có web.catalog.preview) thấy mẫu nháp kèm preview: true ở trang và theo mã; khóa chỉ có
// web.catalog.read gọi ngay sau đó (bản đệm 60 s của khóa xem trước còn) vẫn không thấy mẫu nháp, và ngược lại khóa
// xem trước không nhận bản đệm của khóa thường. Mẫu đang bán không có preview.
func TestWebCatalog_Preview(t *testing.T) {
	_, conn := valkeyCache(t)
	e := newEnv(t, conn)
	preview := e.serviceKeyWith("web.catalog.read", "web.catalog.preview")
	a, live := e.product("PV1", "[]"), e.product("PV2", "[]")
	e.unpublish(a.ID)
	list, byID := "/api/v1/web/catalog/products", "/api/v1/web/catalog/products?ids="+a.ID.String()+","+live.ID.String()

	check := func(name, key string, wantDraft bool) {
		t.Helper()
		for _, path := range []string{list, byID} {
			items := decode(t, e.get(key, path)).Items
			found, pv := previewOf(t, items, a.ID)
			if found != wantDraft || pv != wantDraft {
				t.Errorf("%s %s: có mẫu nháp %v, preview %v; mong %v", name, path, found, pv, wantDraft)
			}
			if f, lp := previewOf(t, items, live.ID); !f || lp {
				t.Errorf("%s %s: mẫu đang bán có %v, preview %v", name, path, f, lp)
			}
		}
	}
	check("xem trước (lần đầu)", preview, true)
	check("khóa thường sau xem trước", e.key, false)
	e.rc.take()
	check("xem trước sau khóa thường", preview, true)
	check("khóa thường lần hai", e.key, false)
	if n, _ := e.rc.take(); n != 0 {
		t.Errorf("lần hai của hai khóa tải PostgreSQL %d lần, mong 0 (mọi khóa đệm đều còn, mỗi loại khóa nhận bản của mình)", n)
	}
	if rec := e.get(e.otherKey, list); rec.Code != http.StatusForbidden {
		t.Errorf("khóa web.orders.read đọc danh mục: %d", rec.Code)
	}
}

// Đơn web với biến thể của mẫu nháp, bằng khóa có cả web.orders.create và web.catalog.preview → 422 item_unavailable
// (biến thể không sellable; kế hoạch ghi variant_not_for_sale, đường web của M5 trả item_unavailable), không có đơn.
// Đơn đặt khi mẫu còn bán, sau khi ẩn lại mẫu vẫn giữ giá dòng đơn lúc đặt.
func TestWebCreateOrder_DraftProduct(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	both := o.serviceKeyWith("web.orders.create", "web.catalog.preview")
	p := o.product("PD1", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	old := o.order(phone(31), "", quoteLine(v, 1, 350000))
	rec := o.place(old.ref, old)
	c := created(t, rec.Code, rec.Body.Bytes())

	o.unpublish(p.ID)
	w := o.order(phone(32), "", quoteLine(v, 1, 350000))
	r := refused(t, o.placeRaw(both, w.ref, w.body()), http.StatusUnprocessableEntity, "item_unavailable")
	if !slices.Equal(r.VariantIDs, []string{v.String()}) || o.ordersOf(w.ref) != 0 {
		t.Fatalf("đơn với mẫu nháp: %+v, %d đơn", r, o.ordersOf(w.ref))
	}
	var price int64
	o.staff.Exec(t, o.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT unit_price FROM ord.order_items WHERE order_id = $1`, c.OrderID).Scan(&price)
	})
	if price != 350000 {
		t.Errorf("giá dòng đơn cũ sau khi ẩn mẫu: %d", price)
	}
}
