package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

const (
	stkApply   = "kk-apply@elimaz.test" // đọc, đếm, áp dụng ở mọi kho
	stkCountW1 = "kk-w1@elimaz.test"    // đọc, đếm chỉ ở kho w1; không áp dụng
)

func (e *invAPIEnv) grantStocktake() {
	e.t.Helper()
	e.staff.Grant(e.t, e.tenant, e.staff.User(e.t, e.tenant, stkApply, "active"),
		[]string{"inventory.read", "inventory.stocktake", "inventory.stocktake.apply"}, "all", "")
	e.staff.Grant(e.t, e.tenant, e.staff.User(e.t, e.tenant, stkCountW1, "active"),
		[]string{"inventory.read", "inventory.stocktake"}, "warehouse", e.w1.String())
}

func (e *invAPIEnv) startStk(email string, wh fmt.Stringer, key string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/api/v1/inventory/stocktakes", email, key, fmt.Sprintf(`{"warehouseId":%q}`, wh))
}

func (e *invAPIEnv) countStk(email, id string, v fmt.Stringer, body, key string) *httptest.ResponseRecorder {
	return e.do(http.MethodPut, "/api/v1/inventory/stocktakes/"+id+"/lines/"+v.String(), email, key, body)
}

// openW1 cấp quyền kiểm kho, tạo biến thể on_hand 5 ở w1 và mở phiên ở w1 (stkApply); trả biến thể, phiên, mã phiên.
func (e *invAPIEnv) openW1() (uuid.UUID, map[string]any, string) {
	e.t.Helper()
	e.grantStocktake()
	v := e.variant(e.w1, 5)
	s := mustOK(e.t, "startStocktake", e.startStk(stkApply, e.w1, "s1"), http.StatusCreated)
	return v, s, s["id"].(string) //nolint:forcetypeassert // test
}

func version(t *testing.T, m map[string]any) int {
	t.Helper()
	f, ok := m["version"].(float64)
	if !ok {
		t.Fatalf("thiếu version: %v", m)
	}
	return int(f)
}

// Luồng đủ qua API: mở, đếm, xem trước, áp dụng (thiếu so với phần giữ → shortages), audit; mở trùng kho → 409.
func TestStocktakeAPI_Flow(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	v, s, id := e.openW1()
	if s["status"] != "open" || s["warehouseId"] != e.w1.String() {
		t.Fatalf("phiên tạo sai: %v", s)
	}
	expect(t, "mở phiên thứ hai cùng kho", e.startStk(stkApply, e.w1, "s2"), http.StatusConflict, "stocktake_open")

	l := mustOK(t, "recordStocktakeCount", e.countStk(stkCountW1, id, v, `{"counted":7}`, "c1"), http.StatusOK)
	if l["expectedOnShelf"] != float64(5) || l["delta"] != float64(2) || l["version"] != float64(1) {
		t.Fatalf("dòng đếm: %v", l)
	}
	expect(t, "ghi lại không lineVersion", e.countStk(stkCountW1, id, v, `{"counted":4}`, "c2"), http.StatusConflict,
		"version_conflict")
	mustOK(t, "ghi lại", e.countStk(stkCountW1, id, v, `{"counted":4,"lineVersion":1}`, "c3"), http.StatusOK)
	expect(t, "số âm", e.countStk(stkCountW1, id, v, `{"counted":-1,"lineVersion":2}`, "c4"), http.StatusBadRequest, "bad_request")

	g := mustOK(t, "getStocktake", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+id, stkCountW1, "", ""), http.StatusOK)
	lines := g["lines"].([]any)     //nolint:forcetypeassert // test
	gl := lines[0].(map[string]any) //nolint:forcetypeassert // test
	if len(lines) != 1 || gl["delta"] != float64(-1) || gl["availableAfter"] != float64(4) || gl["onHand"] != float64(5) {
		t.Fatalf("xem trước: %v", g)
	}
	expect(t, "người chỉ có inventory.stocktake áp dụng", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/apply",
		stkCountW1, "a1", fmt.Sprintf(`{"version":%d}`, version(t, g))), http.StatusForbidden, "forbidden")
	r := mustOK(t, "applyStocktake", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/apply", stkApply, "a2",
		fmt.Sprintf(`{"version":%d}`, version(t, g))), http.StatusOK)
	if r["adjusted"] != float64(1) || len(r["shortages"].([]any)) != 0 || e.onHand(v, e.w1) != 4 { //nolint:forcetypeassert // test
		t.Fatalf("áp dụng: %v, on_hand %d", r, e.onHand(v, e.w1))
	}
	if st := r["stocktake"].(map[string]any); st["status"] != "applied" { //nolint:forcetypeassert // test
		t.Fatalf("trạng thái sau áp dụng: %v", r)
	}
	expect(t, "hủy phiên đã áp dụng", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/cancel", stkApply, "x1",
		fmt.Sprintf(`{"version":%d}`, version(t, g)+1)), http.StatusConflict, "stocktake_not_open")
	expect(t, "đếm trên phiên đã áp dụng", e.countStk(stkApply, id, v, `{"counted":1,"lineVersion":2}`, "c5"),
		http.StatusConflict, "stocktake_not_open")
	for _, a := range []string{"inventory.stocktake.start", "inventory.stocktake.count", "inventory.stocktake.apply"} {
		if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = $1", a); n < 1 {
			t.Fatalf("thiếu audit %s", a)
		}
	}
	expect(t, "tenant B đọc phiên của A", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+id, invOtherB, "", ""),
		http.StatusNotFound, "not_found")
}

// Quyền theo kho: người kho w1 không mở, đếm, hủy, đọc ở kho w2.
func TestStocktakeAPI_WarehouseScope(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.grantStocktake()
	v := e.variant(e.w2, 3)
	expect(t, "mở ở w2 bằng quyền w1", e.startStk(stkCountW1, e.w2, "s1"), http.StatusForbidden, "forbidden")
	s := mustOK(t, "startStocktake w2", e.startStk(stkApply, e.w2, "s2"), http.StatusCreated)
	id := s["id"].(string) //nolint:forcetypeassert // test
	expect(t, "đếm ở w2 bằng quyền w1", e.countStk(stkCountW1, id, v, `{"counted":1}`, "c1"), http.StatusForbidden, "forbidden")
	expect(t, "đọc ở w2 bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+id, stkCountW1, "", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "hủy ở w2 bằng quyền w1", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/cancel", stkCountW1, "x1",
		fmt.Sprintf(`{"version":%d}`, version(t, s))), http.StatusForbidden, "forbidden")
	c := mustOK(t, "cancelStocktake", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/cancel", stkApply, "x2",
		fmt.Sprintf(`{"version":%d}`, version(t, s))), http.StatusOK)
	if c["status"] != "cancelled" {
		t.Fatalf("hủy: %v", c)
	}
	mustOK(t, "mở lại sau hủy", e.startStk(stkApply, e.w2, "s3"), http.StatusCreated)
	expect(t, "phiên không có", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+e.w1.String(), stkApply, "", ""),
		http.StatusNotFound, "not_found")
}

// Áp dụng hai lần song song với hai khóa khác nhau → một 200, một 409; sổ chỉ có dòng của một lần.
func TestStocktakeAPI_ApplyTwice(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	v, _, id := e.openW1()
	mustOK(t, "đếm", e.countStk(stkApply, id, v, `{"counted":3}`, "c1"), http.StatusOK)
	g := mustOK(t, "getStocktake", e.do(http.MethodGet, "/api/v1/inventory/stocktakes/"+id, stkApply, "", ""), http.StatusOK)
	oneWinsOneConflict(t, "stocktake_not_open", func(i int) *httptest.ResponseRecorder {
		return e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/apply", stkApply, fmt.Sprintf("a-%d", i),
			fmt.Sprintf(`{"version":%d}`, version(t, g)))
	})
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1", id); n != 1 {
		t.Fatalf("sổ: mong 1 dòng, có %d", n)
	}
	if e.onHand(v, e.w1) != 3 {
		t.Fatalf("on_hand %d, mong 3", e.onHand(v, e.w1))
	}
}

// oneWinsOneConflict gửi hai yêu cầu song song (do(0), do(1), hai khóa chống trùng khác nhau) → đúng một 200 và một 409
// với mã conflictCode.
func oneWinsOneConflict(t *testing.T, conflictCode string, do func(i int) *httptest.ResponseRecorder) {
	t.Helper()
	pair := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range pair {
		wg.Go(func() { pair[i] = do(i) })
	}
	wg.Wait()
	codes := map[int]int{pair[0].Code: 1}
	codes[pair[1].Code]++
	if codes[http.StatusOK] != 1 || codes[http.StatusConflict] != 1 {
		t.Fatalf("hai khóa: %d %s / %d %s", pair[0].Code, pair[0].Body.String(), pair[1].Code, pair[1].Body.String())
	}
	for _, rec := range pair {
		if rec.Code == http.StatusConflict && errCode(t, rec) != conflictCode {
			t.Fatalf("409 phải %s: %s", conflictCode, rec.Body.String())
		}
	}
}
