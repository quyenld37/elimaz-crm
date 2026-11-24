package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

const looksPath = "/api/v1/content/looks"

// lookEnv: couponEnv (quản lý có content.*, sale không có) cộng hai mẫu đang bán pA, pB của tenant A, mỗi mẫu một ảnh
// sản phẩm đã xử lý (photoA, photoB); tenant B có mẫu pX với ảnh photoX.
type lookEnv struct {
	*couponEnv
	pA, pB, photoA, photoB uuid.UUID
	other, photoX          uuid.UUID
}

func newLookEnv(t *testing.T) *lookEnv {
	t.Helper()
	e := &lookEnv{couponEnv: newCouponEnv(t)}
	e.pA, e.photoA = lookPiece(t, e.couponEnv, e.tenant, "LK1", 1)
	e.pB, e.photoB = lookPiece(t, e.couponEnv, e.tenant, "LK2", 2)
	e.other = e.staff.Tenant(t, "B")
	_, e.photoX = lookPiece(t, e.couponEnv, e.other, "LKX", 3)
	return e
}

func lookPiece(t *testing.T, e *couponEnv, tenant uuid.UUID, code string, n int) (product, photo uuid.UUID) {
	t.Helper()
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		seller := catalogtest.Seller(t, tx, tenant, "ELIMAZ", "own")
		product = catalogtest.Product(t, tx, tenant, seller, code)
		catalogtest.Variant(t, tx, tenant, seller, product, n, 250000)
		photo = filestest.Image(t, tx, tenant, "product_image")
		_, err := tx.Exec(t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
			VALUES ($1, $2, $3, 1)`, tenant, product, photo)
		return err //nolint:wrapcheck // test
	})
	return product, photo
}

// lookJSON: bộ id lên trang chủ khi home, ảnh photo, các món products (chấm 50, 30/60/90), extra chèn thêm trường.
func lookJSON(id uuid.UUID, home bool, photo uuid.UUID, extra string, products ...uuid.UUID) string {
	items := make([]string, len(products))
	for i, p := range products {
		items[i] = fmt.Sprintf(`{"productId":%q,"x":50,"y":%d}`, p, 30*(i+1))
	}
	return fmt.Sprintf(`{"id":%q,"onHome":%t,"name":"Đi làm","photoFileId":%q,"items":[%s]%s}`, id, home, photo,
		strings.Join(items, ","), extra)
}

func looksBody(rev int, looks ...string) string {
	return fmt.Sprintf(`{"revision":%d,"looks":[%s]}`, rev, strings.Join(looks, ","))
}

// [KM04] Bộ phối qua HTTP: sale đọc → 403; mỗi luật sai → 422 đúng mã, không lưu; lưu hợp lệ → revision 1, trạng thái
// (hết hàng vì chưa nhập tồn, lý do, món gây ra, tiêu đề tự động, link), mẫu, ảnh; revision cũ → 409; xem trước không
// ghi, cũng kiểm luật; một audit content.look.save.
func TestLooksAPI_SaveRulesAndRead(t *testing.T) {
	t.Parallel()
	e := newLookEnv(t)
	expect(t, "sale đọc", e.do(http.MethodGet, looksPath, couponSale, "", ""), http.StatusForbidden, "forbidden")
	g := e.ok("đọc rỗng", e.do(http.MethodGet, looksPath, couponManager, "", ""), http.StatusOK)
	if g["revision"] != float64(0) || len(g["looks"].([]any)) != 0 { //nolint:forcetypeassert // test
		t.Fatalf("chưa lưu: %v", g)
	}
	checkLookRules(e)
	id := uuid.New()
	body := looksBody(0, lookJSON(id, true, e.photoB, `,"title":"  Tiêu đề  "`, e.pA, e.pB), lookJSON(uuid.New(), false, e.photoA, "", e.pA))
	saved := decodeLooks(t, "lưu", e.do(http.MethodPut, looksPath, couponManager, "s1", body))
	key := content.LookLinkKey(id)
	switch {
	case saved.Revision != 1, len(saved.Looks) != 2, saved.Looks[0].Title != "Tiêu đề":
		t.Fatalf("sau lưu: %+v", saved)
	case saved.Statuses[0] != lookStatusJSON{
		Shown: false, Reason: "sold-out", ProductID: e.pA.String(), AutoTitle: "Áo thử, áo thử", LinkKey: key,
		Path: "/bo-phoi/di-lam-" + key,
	}, saved.Statuses[1].Reason != "disabled":
		t.Fatalf("trạng thái: %+v", saved.Statuses)
	case len(saved.Products) != 2, len(saved.Photos) != 2, !saved.Photos[0].Ready:
		t.Fatalf("mẫu, ảnh: %+v %+v", saved.Products, saved.Photos)
	}
	expect(t, "revision cũ", e.do(http.MethodPut, looksPath, couponManager, "s2", looksBody(0)), http.StatusConflict,
		"version_conflict")
	preview := decodeLooks(t, "xem trước", e.do(http.MethodPost, looksPath+"/preview", couponManager, "",
		fmt.Sprintf(`{"looks":[%s]}`, lookJSON(id, false, e.photoA, "", e.pA))))
	if len(preview.Statuses) != 1 || preview.Statuses[0].Reason != "disabled" || len(preview.Photos) != 1 {
		t.Fatalf("xem trước: %+v", preview)
	}
	expect(t, "xem trước luật sai", e.do(http.MethodPost, looksPath+"/preview", couponManager, "",
		fmt.Sprintf(`{"looks":[%s]}`, lookJSON(id, true, e.photoA, ""))), http.StatusUnprocessableEntity, "look_items_invalid")
	if again := decodeLooks(t, "đọc lại", e.do(http.MethodGet, looksPath, couponManager, "", "")); again.Revision != 1 {
		t.Fatalf("xem trước đã ghi: revision %d", again.Revision)
	}
	audits := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'content.look.save'
		AND entity_id = $1 AND (before->>'revision')::int = 0 AND (after->>'revision')::int = 1`, e.tenant.String())
	if audits != 1 {
		t.Fatalf("audit content.look.save: %d dòng, mong 1", audits)
	}
}

type lookStatusJSON struct {
	Shown                                       bool
	Reason, ProductID, AutoTitle, LinkKey, Path string
}

type lookListJSON struct {
	Revision int
	Looks    []struct {
		Title  string
		OnHome bool
	}
	Statuses []lookStatusJSON
	Products []struct{ ID string }
	Photos   []struct{ Ready bool }
}

// decodeLooks đòi 200 và đọc thân (LookList hay LookPreview).
func decodeLooks(t *testing.T, name string, rec *httptest.ResponseRecorder) lookListJSON {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
	}
	var out lookListJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

// checkLookRules: mỗi luật sai → 422 đúng mã (không lưu).
func checkLookRules(e *lookEnv) {
	e.t.Helper()
	many := func(n, home int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = lookJSON(uuid.New(), i < home, e.photoA, "", e.pA)
		}
		return out
	}
	for i, c := range []struct {
		name, body, code string
	}{
		{"7 bộ trang chủ", looksBody(0, many(7, 7)...), "too_many_home_looks"},
		{"501 bộ", looksBody(0, many(501, 0)...), "too_many_looks"},
		{"0 món", looksBody(0, lookJSON(uuid.New(), true, e.photoA, "")), "look_items_invalid"},
		{"4 món", looksBody(0, lookJSON(uuid.New(), true, e.photoA, "", e.pA, e.pB, uuid.New(), uuid.New())), "look_items_invalid"},
		{"món trùng", looksBody(0, lookJSON(uuid.New(), true, e.photoA, "", e.pA, e.pA)), "look_items_invalid"},
		{"chấm 120 %", looksBody(0, fmt.Sprintf(`{"id":%q,"onHome":true,"name":"Họp","photoFileId":%q,"items":[{"productId":%q,"x":120,"y":5}]}`,
			uuid.New(), e.photoA, e.pA)), "look_dot_invalid"},
		{"ảnh không thuộc món", looksBody(0, lookJSON(uuid.New(), true, e.photoB, "", e.pA)), "look_photo_invalid"},
		{"ảnh tenant khác", looksBody(0, lookJSON(uuid.New(), true, e.photoX, "", e.pA)), "look_photo_invalid"},
		{"khung giờ ngược", looksBody(0, lookJSON(uuid.New(), true, e.photoA,
			`,"startsAt":"2026-12-02T00:00:00Z","endsAt":"2026-12-01T00:00:00Z"`, e.pA)), "look_window_invalid"},
		{"tên 29", looksBody(0, lookJSON(uuid.New(), true, e.photoA, `,"name":"`+strings.Repeat("a", 29)+`"`, e.pA)), "invalid_look"},
		{"cao 210", looksBody(0, lookJSON(uuid.New(), true, e.photoA, `,"modelHeightCm":210`, e.pA)), "invalid_look"},
	} {
		expect(e.t, c.name, e.do(http.MethodPut, looksPath, couponManager, fmt.Sprintf("bad-%d", i), c.body),
			http.StatusUnprocessableEntity, c.code)
	}
}

// 6.1 qua HTTP: đang có 5 bộ trang chủ, hai lần lưu song song cùng revision (khóa chống trùng khác nhau), mỗi lần bật
// thêm một bộ → một 200, một 409 version_conflict; danh sách cuối 6 bộ trang chủ. Cùng khóa 20 lần → một lần đổi, một
// audit, một sự kiện.
func TestLooksAPI_Concurrent(t *testing.T) {
	t.Parallel()
	e := newLookEnv(t)
	ids := make([]uuid.UUID, 7)
	build := func(home ...int) string {
		looks := make([]string, len(ids))
		for i, id := range ids {
			on := i < 5
			for _, h := range home {
				on = on || i == h
			}
			looks[i] = lookJSON(id, on, e.photoA, "", e.pA)
		}
		return looksBody(1, looks...)
	}
	for i := range ids {
		ids[i] = uuid.New()
	}
	first := strings.Replace(build(), `"revision":1`, `"revision":0`, 1)
	e.ok("5 bộ trang chủ", e.do(http.MethodPut, looksPath, couponManager, "base", first), http.StatusOK)
	raceTwoSaves(t, e, build(5), build(6))
	g := decodeLooks(t, "đọc", e.do(http.MethodGet, looksPath, couponManager, "", ""))
	home := 0
	for _, l := range g.Looks {
		if l.OnHome {
			home++
		}
	}
	if g.Revision != 2 || home != 6 {
		t.Fatalf("sau hai lần lưu: revision %d, %d bộ trang chủ", g.Revision, home)
	}
	body := strings.Replace(build(), `"revision":1`, `"revision":2`, 1)
	if n := len(sameKeyBodies(t, 20, func() *httptest.ResponseRecorder {
		return e.do(http.MethodPut, looksPath, couponManager, "same-key", body)
	})); n != 1 {
		t.Fatalf("câu trả lời 200 khác nhau hoặc không có: %d loại", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'content.look.save'`); n != 3 {
		t.Fatalf("audit: %d dòng, mong 3 (gốc, lần thắng, 20 lần cùng khóa)", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'content.looks.changed'`); n != 3 {
		t.Fatalf("sự kiện: %d, mong 3", n)
	}
}

// raceTwoSaves gửi song song hai lần lưu a, b (khóa chống trùng khác nhau): đúng một 200, một 409 version_conflict.
func raceTwoSaves(t *testing.T, e *lookEnv, a, b string) {
	t.Helper()
	recs := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for k, body := range []string{a, b} {
		wg.Go(func() { recs[k] = e.do(http.MethodPut, looksPath, couponManager, fmt.Sprintf("race-%d", k), body) })
	}
	wg.Wait()
	codes := map[int]int{}
	for _, rec := range recs {
		codes[rec.Code]++
		if rec.Code == http.StatusConflict {
			expect(t, "lần thua", rec, http.StatusConflict, "version_conflict")
		}
	}
	if codes[http.StatusOK] != 1 || codes[http.StatusConflict] != 1 {
		t.Fatalf("hai lần lưu: %d %s / %d %s", recs[0].Code, recs[0].Body, recs[1].Code, recs[1].Body)
	}
}

// Tenant khác: danh sách riêng từng shop; quản lý shop B thấy rỗng, không dùng được ảnh của A, lưu của B không đổi A.
func TestLooksAPI_OtherTenant(t *testing.T) {
	t.Parallel()
	e := newLookEnv(t)
	e.ok("A lưu", e.do(http.MethodPut, looksPath, couponManager, "a1", looksBody(0, lookJSON(uuid.New(), true, e.photoA, "", e.pA))),
		http.StatusOK)
	const managerB = "quan-ly-b@elimaz.test"
	e.staff.Grant(t, e.other, e.staff.User(t, e.other, managerB, "active"), rolePerms(t, "manager"), "all", "")
	if b := decodeLooks(t, "B đọc", e.do(http.MethodGet, looksPath, managerB, "", "")); b.Revision != 0 || len(b.Looks) > 0 {
		t.Fatalf("B thấy: %+v", b)
	}
	expect(t, "B dùng ảnh của A", e.do(http.MethodPut, looksPath, managerB, "b1",
		looksBody(0, lookJSON(uuid.New(), true, e.photoA, "", e.pA))), http.StatusUnprocessableEntity, "look_photo_invalid")
	decodeLooks(t, "B lưu", e.do(http.MethodPut, looksPath, managerB, "b2", looksBody(0)))
	if a := decodeLooks(t, "A đọc", e.do(http.MethodGet, looksPath, couponManager, "", "")); a.Revision != 1 || len(a.Looks) != 1 {
		t.Fatalf("A bị đổi: %+v", a)
	}
}
