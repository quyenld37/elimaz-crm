package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Kiểm kho mở sổ qua API (kế hoạch M7, Task 262 = M7-17; MO02).

const (
	openAll = "mo-so@elimaz.test"    // đọc, đếm, mở sổ ở mọi kho
	openW1  = "mo-so-w1@elimaz.test" // đọc, đếm, mở sổ chỉ ở kho w1
)

func (e *invAPIEnv) grantOpening() {
	e.t.Helper()
	perms := []string{"inventory.read", "inventory.stocktake", "inventory.stocktake.opening"}
	e.staff.Grant(e.t, e.tenant, e.staff.User(e.t, e.tenant, openAll, "active"), perms, "all", "")
	e.staff.Grant(e.t, e.tenant, e.staff.User(e.t, e.tenant, openW1, "active"), perms, "warehouse", e.w1.String())
}

func (e *invAPIEnv) createOpening(email string, wh fmt.Stringer, key string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/api/v1/inventory/opening-stocktakes", email, key, fmt.Sprintf(`{"warehouseId":%q}`, wh))
}

func (e *invAPIEnv) postOpening(email, id, key string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/api/v1/inventory/opening-stocktakes/"+id+"/post", email, key, "")
}

// Luồng đủ: mở (3 dòng phải đếm), mở trùng 409 opening_exists, kho khác 403, thiếu dòng 422 uncounted_lines kèm count,
// đăng: 50 yêu cầu cùng khóa + 2 khóa khác song song → đúng một lần đăng, sổ không nhân đôi; đọc, danh sách theo kho
// được giao, kiểm lại ok; đếm sau đăng 409 stocktake_not_open; tenant khác 404.
func TestOpeningAPI_Flow(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.grantOpening()
	vs := []uuid.UUID{e.variant(e.w1, 0), e.variant(e.w1, 0), e.variant(e.w1, 0)}
	o := mustOK(t, "createOpeningStocktake", e.createOpening(openW1, e.w1, "o1"), http.StatusCreated)
	if o["linesExpected"] != float64(3) || o["linesCounted"] != float64(0) || o["status"] != "open" ||
		o["warehouseId"] != e.w1.String() {
		t.Fatalf("phiên mở sổ: %v", o)
	}
	id := o["stocktakeId"].(string) //nolint:forcetypeassert // test
	expect(t, "mở lần hai", e.createOpening(openW1, e.w1, "o2"), http.StatusConflict, "opening_exists")
	expect(t, "mở ở w2 bằng quyền w1", e.createOpening(openW1, e.w2, "o3"), http.StatusForbidden, "forbidden")
	w2 := mustOK(t, "mở ở w2", e.createOpening(openAll, e.w2, "o4"), http.StatusCreated)

	for i, v := range vs[:2] {
		mustOK(t, "đếm", e.countStk(openW1, id, v, fmt.Sprintf(`{"counted":%d}`, i+4), fmt.Sprintf("c%d", i)), http.StatusOK)
	}
	rec := e.postOpening(openW1, id, "p0")
	if b := mustOK(t, "đăng thiếu dòng", rec, http.StatusUnprocessableEntity); b["error"] != "uncounted_lines" || b["count"] != float64(1) {
		t.Fatalf("đăng thiếu dòng: %v", b)
	}
	mustOK(t, "đếm dòng cuối", e.countStk(openW1, id, vs[2], `{"counted":0}`, "c2"), http.StatusOK)
	expect(t, "đăng ở w2 bằng quyền w1", e.postOpening(openW1, w2["stocktakeId"].(string), "p1"), //nolint:forcetypeassert // test
		http.StatusForbidden, "forbidden")

	winner := e.postOpeningConcurrently(id)
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1", id); n != 2 {
		t.Fatalf("sổ: %d dòng, mong 2", n)
	}
	if e.onHand(vs[0], e.w1) != 4 || e.onHand(vs[1], e.w1) != 5 || e.onHand(vs[2], e.w1) != 0 {
		t.Fatalf("on_hand sau đăng: %d %d %d", e.onHand(vs[0], e.w1), e.onHand(vs[1], e.w1), e.onHand(vs[2], e.w1))
	}
	r := mustOK(t, "đăng lại cùng khóa thắng", e.postOpening(openW1, id, winner), http.StatusOK)
	if r["linesExpected"] != float64(3) || r["countedLines"] != float64(3) || r["units"] != float64(9) {
		t.Fatalf("kết quả đăng: %v", r)
	}

	w2ID := w2["stocktakeId"].(string) //nolint:forcetypeassert // test
	e.assertOpeningReads(id, w2ID)
	expect(t, "kiểm phiên chưa đăng", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes/"+w2ID+"/check", openAll,
		"", ""), http.StatusConflict, "stocktake_open")
	expect(t, "đếm sau đăng", e.countStk(openW1, id, vs[0], `{"counted":1,"lineVersion":1}`, "c9"), http.StatusConflict,
		"stocktake_not_open")
	expect(t, "phiên thường qua đường mở sổ", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes/"+uuid.NewString(),
		openAll, "", ""), http.StatusNotFound, "not_found")
	expect(t, "tenant B đọc phiên của A", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes/"+id, invOtherB, "", ""),
		http.StatusNotFound, "not_found")
	for _, a := range []string{"inventory.opening.start", "inventory.opening_applied"} {
		if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = $1", a); n < 1 {
			t.Fatalf("thiếu audit %s", a)
		}
	}
}

// Kho đã có hàng → 409 warehouse_not_empty; đường áp dụng của kiểm kho thường không đăng được phiên mở sổ (400).
func TestOpeningAPI_Refused(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.grantOpening()
	e.grantStocktake()
	e.variant(e.w2, 2)
	v := e.variant(e.w1, 0)
	expect(t, "kho có hàng", e.createOpening(openAll, e.w2, "o1"), http.StatusConflict, "warehouse_not_empty")
	o := mustOK(t, "mở w1", e.createOpening(openAll, e.w1, "o2"), http.StatusCreated)
	id := o["stocktakeId"].(string) //nolint:forcetypeassert // test
	mustOK(t, "đếm", e.countStk(openAll, id, v, `{"counted":1}`, "c1"), http.StatusOK)
	g := mustOK(t, "getStocktake", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+id, stkApply, "", ""), http.StatusOK)
	expect(t, "applyStocktake trên phiên mở sổ", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/apply", stkApply,
		"a1", fmt.Sprintf(`{"version":%d}`, version(t, g))), http.StatusBadRequest, "invalid_input")
	if e.onHand(v, e.w1) != 0 {
		t.Fatal("áp dụng bị chặn mà tồn đổi")
	}
}

func keysOf(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}

// postOpeningConcurrently gửi 50 yêu cầu đăng cùng một khóa và 2 yêu cầu khóa khác cùng lúc; đòi đúng một khóa được 200,
// các yêu cầu khác 409 stocktake_not_open (hay request_in_progress của cùng khóa); trả khóa thắng.
func (e *invAPIEnv) postOpeningConcurrently(id string) string {
	e.t.Helper()
	recs := make([]*httptest.ResponseRecorder, 52)
	keys := make([]string, len(recs))
	var wg sync.WaitGroup
	for i := range recs {
		keys[i] = "p-same"
		if i >= 50 {
			keys[i] = fmt.Sprintf("p-other-%d", i)
		}
		wg.Go(func() { recs[i] = e.postOpening(openW1, id, keys[i]) })
	}
	wg.Wait()
	winners := map[string]bool{}
	for i, r := range recs {
		switch {
		case r.Code == http.StatusOK:
			winners[keys[i]] = true
		case r.Code == http.StatusConflict && (errCode(e.t, r) == "stocktake_not_open" || errCode(e.t, r) == "request_in_progress"):
		default:
			e.t.Fatalf("đăng song song: mã lạ %d %s", r.Code, r.Body.String())
		}
	}
	if len(winners) != 1 {
		e.t.Fatalf("đăng song song: %d khóa được 200, mong 1 (%v)", len(winners), winners)
	}
	return keysOf(winners)
}

// assertOpeningReads: đọc phiên đã đăng, danh sách theo kho được giao và theo bộ lọc kho, đọc phiên kho khác 403,
// kiểm lại khớp.
func (e *invAPIEnv) assertOpeningReads(id, w2ID string) {
	e.t.Helper()
	g := mustOK(e.t, "getOpeningStocktake", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes/"+id, openW1, "", ""),
		http.StatusOK)
	if g["status"] != "applied" || g["unitsCounted"] != float64(9) || g["linesCounted"] != float64(3) || g["appliedAt"] == nil {
		e.t.Fatalf("đọc phiên: %v", g)
	}
	l := mustOK(e.t, "listOpeningStocktakes", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes", openW1, "", ""),
		http.StatusOK)
	if items := l["items"].([]any); len(items) != 1 || items[0].(map[string]any)["stocktakeId"] != id { //nolint:forcetypeassert // test
		e.t.Fatalf("danh sách theo kho được giao: %v", l)
	}
	all := mustOK(e.t, "listOpeningStocktakes all", e.do(http.MethodGet,
		"/api/v1/inventory/opening-stocktakes?warehouseId="+e.w2.String(), openAll, "", ""), http.StatusOK)
	if items := all["items"].([]any); len(items) != 1 || items[0].(map[string]any)["warehouseId"] != e.w2.String() { //nolint:forcetypeassert // test
		e.t.Fatalf("danh sách lọc kho w2: %v", all)
	}
	expect(e.t, "đọc phiên w2 bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes/"+w2ID, openW1,
		"", ""), http.StatusForbidden, "forbidden")
	c := mustOK(e.t, "checkOpeningStocktake", e.do(http.MethodGet, "/api/v1/inventory/opening-stocktakes/"+id+"/check",
		openW1, "", ""), http.StatusOK)
	if c["ok"] != true || c["changedSince"] != false || len(c["mismatches"].([]any)) != 0 { //nolint:forcetypeassert // test
		e.t.Fatalf("kiểm lại: %v", c)
	}
}
