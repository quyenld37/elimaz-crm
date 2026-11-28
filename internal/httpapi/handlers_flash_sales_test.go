package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
)

// flashEnv: couponEnv (quản lý có mọi quyền mẫu của manager, sale không có quyền flash sale) cộng nhân viên chỉ có
// promotion.flash.read, kho MAIN, một biến thể giá thường 350.000.
type flashEnv struct {
	*couponEnv
	variant uuid.UUID
}

const flashReader = "xem-flash@elimaz.test"

func newFlashEnv(t *testing.T) *flashEnv {
	t.Helper()
	e := &flashEnv{couponEnv: newCouponEnv(t)}
	r := e.staff.User(t, e.tenant, flashReader, "active")
	e.staff.Grant(t, e.tenant, r, []string{"promotion.flash.read"}, "all", "")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		seller, _ := catalogtest.Sellers(t, tx, e.tenant)
		if _, err := inventory.EnsureDefaultWarehouse(t.Context(), tx, e.tenant, seller); err != nil {
			return err //nolint:wrapcheck // test
		}
		p := catalogtest.Product(t, tx, e.tenant, seller, "FS01")
		e.variant = catalogtest.Variant(t, tx, e.tenant, seller, p, 1, 350000)
		return nil
	})
	return e
}

const flashSaleBody = `{"name":"Sale 12.12","startsAt":"2026-12-12T05:00:00Z","endsAt":"2026-12-12T07:00:00Z"}`

func (e *flashEnv) item(price int64, qty int) string {
	return fmt.Sprintf(`{"variantId":%q,"salePrice":%d,"quantity":%d}`, e.variant, price, qty)
}

func (e *flashEnv) auditCount(action string) int {
	return e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = $1`, action)
}

// draftRejections: giá bằng giá thường → not_a_discount; thêm biến thể đã có → already_exists; biến thể không có →
// variant_not_for_sale; kết thúc trước bắt đầu, dài 8 ngày → invalid_flash_window.
func (e *flashEnv) draftRejections(t *testing.T, base string) {
	t.Helper()
	expect(t, "giá bằng giá thường", e.do(http.MethodPost, base+"/items", couponManager, "f3", e.item(350000, 5)),
		http.StatusUnprocessableEntity, "not_a_discount")
	expect(t, "thêm hai lần", e.do(http.MethodPost, base+"/items", couponManager, "f4", e.item(150000, 5)),
		http.StatusUnprocessableEntity, "already_exists")
	expect(t, "biến thể không có", e.do(http.MethodPost, base+"/items", couponManager, "f4b",
		fmt.Sprintf(`{"variantId":%q,"salePrice":1000,"quantity":1}`, uuid.New())),
		http.StatusUnprocessableEntity, "variant_not_for_sale")
	expect(t, "kết thúc trước bắt đầu", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "f5",
		`{"name":"x","startsAt":"2026-12-12T05:00:00Z","endsAt":"2026-12-12T04:00:00Z"}`),
		http.StatusUnprocessableEntity, "invalid_flash_window")
	expect(t, "dài 8 ngày", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "f6",
		`{"name":"x","startsAt":"2026-12-12T05:00:00Z","endsAt":"2026-12-20T05:00:00Z"}`),
		http.StatusUnprocessableEntity, "invalid_flash_window")
}

// [FS01 phần soạn] Qua HTTP: tạo đợt (201 nháp), thêm món 199.000 / 500 suất (giá thường 350.000), đọc lại; giá bằng giá
// thường → 422 not_a_discount; thêm hai lần → 422 already_exists; kết thúc trước bắt đầu, dài 8 ngày → 422
// invalid_flash_window; sửa đợt, sửa món chỉ gửi trường đổi; version cũ → 409; bỏ món; đợt đã scheduled → 422
// not_editable; mỗi lần ghi thành công một dòng audit.
func TestFlashSaleAPI_Draft(t *testing.T) {
	t.Parallel()
	e := newFlashEnv(t)
	post := func(path, key, body string) map[string]any {
		return e.ok("POST "+path, e.do(http.MethodPost, path, couponManager, key, body), http.StatusCreated)
	}
	s := post("/api/v1/flash-sales", "f1", flashSaleBody)
	sale, _ := s["sale"].(map[string]any)
	id, _ := sale["id"].(string)
	if sale["status"] != "draft" || sale["version"] != float64(1) || sale["itemCount"] != float64(0) {
		t.Fatalf("đợt mới: %v", s)
	}
	base := "/api/v1/flash-sales/" + id
	it := post(base+"/items", "f2", e.item(199000, 500))
	itemID, _ := it["id"].(string)
	if it["salePrice"] != float64(199000) || it["listPrice"] != float64(350000) || it["quantity"] != float64(500) ||
		it["perCustomerLimit"] != float64(1) || it["holdMinutes"] != float64(10) || it["version"] != float64(1) {
		t.Fatalf("món mới: %v", it)
	}
	got := e.ok("đọc", e.do(http.MethodGet, base, flashReader, "", ""), http.StatusOK)
	items, _ := got["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != itemID { //nolint:forcetypeassert // test
		t.Fatalf("đọc lại: %v", got)
	}
	e.draftRejections(t, base)

	up := e.ok("đổi tên", e.do(http.MethodPatch, base, couponManager, "f7", `{"version":1,"name":"Sale 12.12 mới"}`),
		http.StatusOK)
	upSale, _ := up["sale"].(map[string]any)
	if upSale["name"] != "Sale 12.12 mới" || upSale["version"] != float64(2) || !sameInstant(upSale["endsAt"], "2026-12-12T07:00:00Z") {
		t.Fatalf("sau khi đổi tên: %v", up)
	}
	expect(t, "version cũ", e.do(http.MethodPatch, base, couponManager, "f8", `{"version":1,"name":"y"}`),
		http.StatusConflict, "version_conflict")
	ui := e.ok("sửa món", e.do(http.MethodPatch, base+"/items/"+itemID, couponManager, "f9", `{"version":1,"quantity":300}`),
		http.StatusOK)
	if ui["quantity"] != float64(300) || ui["salePrice"] != float64(199000) || ui["version"] != float64(2) {
		t.Fatalf("sau khi sửa món: %v", ui)
	}
	expect(t, "sửa món version cũ", e.do(http.MethodPatch, base+"/items/"+itemID, couponManager, "f10",
		`{"version":1,"quantity":200}`), http.StatusConflict, "version_conflict")

	s2 := post("/api/v1/flash-sales", "f11", flashSaleBody)
	s2Sale, _ := s2["sale"].(map[string]any)
	id2, _ := s2Sale["id"].(string)
	it2 := post("/api/v1/flash-sales/"+id2+"/items", "f12", e.item(100000, 2))
	it2ID, _ := it2["id"].(string)
	rm := e.ok("bỏ món", e.do(http.MethodDelete, "/api/v1/flash-sales/"+id2+"/items/"+it2ID, couponManager, "f13", ""),
		http.StatusOK)
	if rmItems, _ := rm["items"].([]any); len(rmItems) != 0 {
		t.Fatalf("sau khi bỏ: %v", rm)
	}
	expect(t, "bỏ món đã bỏ", e.do(http.MethodDelete, "/api/v1/flash-sales/"+id2+"/items/"+it2ID, couponManager, "f14", ""),
		http.StatusNotFound, "not_found")

	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.flash_sales SET status = 'scheduled' WHERE id = $1`, id)
		return err //nolint:wrapcheck // test
	})
	expect(t, "sửa đợt scheduled", e.do(http.MethodPatch, base, couponManager, "f15", `{"version":2,"name":"z"}`),
		http.StatusUnprocessableEntity, "not_editable")
	expect(t, "sửa món của đợt scheduled", e.do(http.MethodPatch, base+"/items/"+itemID, couponManager, "f16",
		`{"version":2,"quantity":10}`), http.StatusUnprocessableEntity, "not_editable")

	list := e.ok("danh sách nháp", e.do(http.MethodGet, "/api/v1/flash-sales?status=draft", flashReader, "", ""), http.StatusOK)
	if li, _ := list["items"].([]any); len(li) != 1 || li[0].(map[string]any)["id"] != id2 { //nolint:forcetypeassert // test
		t.Fatalf("lọc nháp: %v", list)
	}
	for action, n := range map[string]int{
		"promotion.flash_sale.create": 2, "promotion.flash_sale.update": 1, "promotion.flash_item.add": 2,
		"promotion.flash_item.update": 1, "promotion.flash_item.remove": 1,
	} {
		if got := e.auditCount(action); got != n {
			t.Errorf("audit %s: %d, mong %d", action, got, n)
		}
	}
	if got := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action =
		'promotion.flash_sale.update' AND entity_id = $1 AND before->>'name' = 'Sale 12.12'
		AND after->>'name' = 'Sale 12.12 mới'`, id); got != 1 {
		t.Errorf("audit sửa đợt có trước, sau: %d", got)
	}
}

// Quyền: chỉ promotion.flash.read → xem được, ghi 403; sale (không quyền flash) → đọc 403; tenant khác → 404.
func TestFlashSaleAPI_Permissions(t *testing.T) {
	t.Parallel()
	e := newFlashEnv(t)
	s := e.ok("tạo", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "p1", flashSaleBody), http.StatusCreated)
	sale, _ := s["sale"].(map[string]any)
	id, _ := sale["id"].(string)
	base := "/api/v1/flash-sales/" + id
	e.ok("chỉ đọc xem", e.do(http.MethodGet, base, flashReader, "", ""), http.StatusOK)
	e.ok("chỉ đọc xem danh sách", e.do(http.MethodGet, "/api/v1/flash-sales", flashReader, "", ""), http.StatusOK)
	for name, rec := range map[string]int{
		"chỉ đọc tạo":      e.do(http.MethodPost, "/api/v1/flash-sales", flashReader, "p2", flashSaleBody).Code,
		"chỉ đọc sửa":      e.do(http.MethodPatch, base, flashReader, "p3", `{"version":1,"name":"x"}`).Code,
		"chỉ đọc thêm món": e.do(http.MethodPost, base+"/items", flashReader, "p4", e.item(100000, 1)).Code,
		"chỉ đọc bỏ món":   e.do(http.MethodDelete, base+"/items/"+uuid.NewString(), flashReader, "p5", "").Code,
		"sale đọc":         e.do(http.MethodGet, base, couponSale, "", "").Code,
	} {
		if rec != http.StatusForbidden {
			t.Errorf("%s: %d, mong 403", name, rec)
		}
	}
	tb := e.staff.Tenant(t, "B")
	mb := e.staff.User(t, tb, "quan-ly-flash-b@elimaz.test", "active")
	e.staff.Grant(t, tb, mb, rolePerms(t, "manager"), "all", "")
	expect(t, "tenant khác đọc", e.do(http.MethodGet, base, "quan-ly-flash-b@elimaz.test", "", ""),
		http.StatusNotFound, "not_found")
	if n := e.auditCount("promotion.flash_sale.create"); n != 1 {
		t.Errorf("audit tạo: %d, mong 1 (lần bị 403 không ghi)", n)
	}
}

// Đợt ở xa trong tương lai: handler lên lịch theo giờ máy (h.now), không theo đồng hồ giả.
const futureFlashBody = `{"name":"Sale xa","startsAt":"2099-12-12T05:00:00Z","endsAt":"2099-12-12T07:00:00Z"}`

// [FS01] [FS08] [FS09] Qua HTTP: tồn 10; món 12 suất → lên lịch 409 insufficient_stock kèm shortages {variantId, sku,
// need 12, sellable 10}, đợt vẫn nháp; sửa còn 6 suất → lên lịch 200 scheduled; lên lịch lại → 422 not_editable; live:
// 6 trống; thêm 3 suất → quantity 9; thêm 5 → 409 insufficient_stock {need 5, sellable 1}; đóng → closed, live 9 đã trả;
// đóng lại, thêm suất sau đóng → 409 flash_not_active; đợt không món → 422 flash_sale_empty; mỗi lần ghi một dòng audit.
func TestFlashSaleAPI_Lifecycle(t *testing.T) {
	t.Parallel()
	e := newFlashEnv(t)
	e.seedStock(t, 10)
	s := e.ok("tạo", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "l1", futureFlashBody), http.StatusCreated)
	sale, _ := s["sale"].(map[string]any)
	id, _ := sale["id"].(string)
	base := "/api/v1/flash-sales/" + id
	it := e.ok("thêm món", e.do(http.MethodPost, base+"/items", couponManager, "l2", e.item(199000, 12)), http.StatusCreated)
	itemID, _ := it["id"].(string)
	rec := e.do(http.MethodPost, base+"/schedule", couponManager, "l3", `{"version":1}`)
	body := e.ok("thiếu hàng", rec, http.StatusConflict)
	if body["error"] != "insufficient_stock" {
		t.Fatalf("thiếu hàng: %v", body)
	}
	sh, _ := body["shortages"].([]any)
	if len(sh) != 1 {
		t.Fatalf("shortages: %v", body)
	}
	if m, _ := sh[0].(map[string]any); m["variantId"] != e.variant.String() || m["sku"] != it["sku"] ||
		m["need"] != float64(12) || m["sellable"] != float64(10) {
		t.Fatalf("món thiếu: %v", m)
	}
	e.ok("sửa còn 6", e.do(http.MethodPatch, base+"/items/"+itemID, couponManager, "l4", `{"version":1,"quantity":6}`),
		http.StatusOK)
	got := e.ok("lên lịch", e.do(http.MethodPost, base+"/schedule", couponManager, "l5", `{"version":1}`), http.StatusOK)
	if gs, _ := got["sale"].(map[string]any); gs["status"] != "scheduled" || gs["version"] != float64(2) {
		t.Fatalf("sau lên lịch: %v", got)
	}
	expect(t, "lên lịch lại", e.do(http.MethodPost, base+"/schedule", couponManager, "l6", `{"version":2}`),
		http.StatusUnprocessableEntity, "not_editable")
	e.checkLive(t, base, map[string]float64{"quantity": 6, "available": 6, "released": 0})
	add := e.ok("thêm suất", e.do(http.MethodPost, base+"/items/"+itemID+"/add-slots", couponManager, "l7", `{"add":3}`),
		http.StatusOK)
	if add["quantity"] != float64(9) || add["version"] != float64(3) {
		t.Fatalf("sau thêm suất: %v", add)
	}
	body = e.ok("thêm vượt", e.do(http.MethodPost, base+"/items/"+itemID+"/add-slots", couponManager, "l8",
		`{"add":5}`), http.StatusConflict)
	if sh, _ := body["shortages"].([]any); body["error"] != "insufficient_stock" || len(sh) != 1 || sh[0].(map[string]any)["sellable"] != float64(1) { //nolint:forcetypeassert // test
		t.Fatalf("thêm vượt: %v", body)
	}
	closed := e.ok("đóng", e.do(http.MethodPost, base+"/close", couponManager, "l9", ""), http.StatusOK)
	if cs, _ := closed["sale"].(map[string]any); cs["status"] != "closed" || cs["closedReason"] != "manual" {
		t.Fatalf("sau đóng: %v", closed)
	}
	e.checkLive(t, base, map[string]float64{"quantity": 9, "available": 0, "released": 9})
	expect(t, "đóng lại", e.do(http.MethodPost, base+"/close", couponManager, "l10", ""), http.StatusConflict,
		"flash_not_active")
	expect(t, "thêm suất sau đóng", e.do(http.MethodPost, base+"/items/"+itemID+"/add-slots", couponManager, "l11",
		`{"add":1}`), http.StatusConflict, "flash_not_active")
	empty := e.ok("đợt rỗng", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "l12", futureFlashBody),
		http.StatusCreated)
	es, _ := empty["sale"].(map[string]any)
	eid, _ := es["id"].(string)
	expect(t, "lên lịch đợt rỗng", e.do(http.MethodPost, "/api/v1/flash-sales/"+eid+"/schedule", couponManager, "l13",
		`{"version":1}`), http.StatusUnprocessableEntity, "flash_sale_empty")
	for action, n := range map[string]int{
		"promotion.flash_sale.schedule": 1, "promotion.flash_item.add_slots": 1, "promotion.flash_sale.close": 1,
	} {
		if got := e.auditCount(action); got != n {
			t.Errorf("audit %s: %d, mong %d", action, got, n)
		}
	}
}

// seedStock nạp n hàng có sẵn của biến thể của env vào kho MAIN.
func (e *flashEnv) seedStock(t *testing.T, n int32) {
	t.Helper()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		wh, err := inventory.DefaultWarehouse(t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		invtest.SeedStock(t, tx, e.variant, wh, n)
		return nil
	})
}

// checkLive đọc số liệu đợt (người chỉ có promotion.flash.read) và so các trường của món đầu.
func (e *flashEnv) checkLive(t *testing.T, base string, want map[string]float64) {
	t.Helper()
	l := e.ok("live", e.do(http.MethodGet, base+"/live", flashReader, "", ""), http.StatusOK)
	items, _ := l["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("live: %v", l)
	}
	m, _ := items[0].(map[string]any)
	for k, v := range want {
		if m[k] != v {
			t.Fatalf("live %s = %v, mong %v (%v)", k, m[k], v, m)
		}
	}
}

// Quyền của vòng đời: chỉ promotion.flash.read → lên lịch, thêm suất, đóng 403, xem live được; sale → live 403.
func TestFlashSaleAPI_LifecyclePermissions(t *testing.T) {
	t.Parallel()
	e := newFlashEnv(t)
	s := e.ok("tạo", e.do(http.MethodPost, "/api/v1/flash-sales", couponManager, "q1", futureFlashBody), http.StatusCreated)
	sale, _ := s["sale"].(map[string]any)
	id, _ := sale["id"].(string)
	base := "/api/v1/flash-sales/" + id
	e.ok("chỉ đọc xem live", e.do(http.MethodGet, base+"/live", flashReader, "", ""), http.StatusOK)
	for name, code := range map[string]int{
		"chỉ đọc lên lịch":  e.do(http.MethodPost, base+"/schedule", flashReader, "q2", `{"version":1}`).Code,
		"chỉ đọc đóng":      e.do(http.MethodPost, base+"/close", flashReader, "q3", "").Code,
		"chỉ đọc thêm suất": e.do(http.MethodPost, base+"/items/"+uuid.NewString()+"/add-slots", flashReader, "q4", `{"add":1}`).Code,
		"sale xem live":     e.do(http.MethodGet, base+"/live", couponSale, "", "").Code,
	} {
		if code != http.StatusForbidden {
			t.Errorf("%s: %d, mong 403", name, code)
		}
	}
	expect(t, "live đợt không có", e.do(http.MethodGet, "/api/v1/flash-sales/"+uuid.NewString()+"/live", flashReader,
		"", ""), http.StatusNotFound, "not_found")
}
