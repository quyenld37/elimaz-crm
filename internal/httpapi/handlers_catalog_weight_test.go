package httpapi

import (
	"net/http"
	"testing"
)

// updateVariant nhận weightGrams (Task 190, QĐ30): 0 → 422 invalid_input (không đổi gì); 450 → 450 trong câu trả lời
// và getProduct; null → bỏ cân nặng (trường vắng mặt).
func TestCatalogAPI_UpdateVariantWeight(t *testing.T) {
	t.Parallel()
	e := newCatalogAPIEnv(t)
	p := e.createDam01()
	v := p["variants"].([]any)[0].(map[string]any) //nolint:forcetypeassert // test
	if _, ok := v["weightGrams"]; ok {
		t.Fatalf("biến thể mới có weightGrams: %v", v)
	}
	vpath := "/api/v1/catalog/variants/" + v["id"].(string) //nolint:forcetypeassert // test
	expect(t, "cân nặng 0", e.do(http.MethodPatch, vpath, catalogManager, "w0", `{"version":1,"weightGrams":0}`),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "cân nặng 50001", e.do(http.MethodPatch, vpath, catalogManager, "w1", `{"version":1,"weightGrams":50001}`),
		http.StatusUnprocessableEntity, "invalid_input")
	upd := e.do(http.MethodPatch, vpath, catalogManager, "w2", `{"version":1,"weightGrams":450}`)
	if upd.Code != http.StatusOK || decodeJSON(t, upd)["weightGrams"] != float64(450) {
		t.Fatalf("đặt 450: %d %s", upd.Code, upd.Body.String())
	}
	got := decodeJSON(t, e.do(http.MethodGet, "/api/v1/catalog/products/"+p["id"].(string), catalogManager, "", "")) //nolint:forcetypeassert // test
	if g := got["variants"].([]any)[0].(map[string]any)["weightGrams"]; g != float64(450) {                          //nolint:forcetypeassert // test
		t.Fatalf("getProduct: weightGrams %v", g)
	}
	clr := e.do(http.MethodPatch, vpath, catalogManager, "w3", `{"version":2,"weightGrams":null}`)
	if _, ok := decodeJSON(t, clr)["weightGrams"]; clr.Code != http.StatusOK || ok {
		t.Fatalf("bỏ cân nặng: %d %s", clr.Code, clr.Body.String())
	}
}
