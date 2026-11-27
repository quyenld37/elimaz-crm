package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func variantIDsOf(t *testing.T, p map[string]any) []string {
	t.Helper()
	var out []string
	for _, v := range listOf(t, p, "variants") {
		out = append(out, v.(map[string]any)["id"].(string)) //nolint:forcetypeassert // test
	}
	return out
}

func idsJSON(ids []string) string { return `["` + strings.Join(ids, `","`) + `"]` }

// TestCatalogAPI_ReorderVariants (Task 513): reorderProductVariants trả mẫu theo thứ tự mới, version +1; getProduct đọc
// lại cùng thứ tự; gửi lại cùng khóa trả cùng kết quả, không sắp thêm lần nữa; version cũ 409; danh sách sai tập (thiếu,
// trùng, mã lạ) 422 invalid_input; người chỉ xem 403; tenant khác 404.
func TestCatalogAPI_ReorderVariants(t *testing.T) {
	t.Parallel()
	e := newCatalogAPIEnv(t)
	p := e.createDam01()
	id := p["id"].(string) //nolint:forcetypeassert // test
	ids := variantIDsOf(t, p)
	path := "/api/v1/catalog/products/" + id + "/variants/order"
	want := []string{ids[2], ids[0], ids[1]}
	body := fmt.Sprintf(`{"productVersion":1,"variantIds":%s}`, idsJSON(want))
	rec := e.do(http.MethodPut, path, catalogManager, "vo1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorderProductVariants: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeJSON(t, rec)
	if !slices.Equal(variantIDsOf(t, got), want) || got["version"] != float64(2) {
		t.Fatalf("mẫu sau khi sắp: %v", got)
	}
	if _, ok := got["images"]; !ok {
		t.Errorf("câu trả lời phải là Product đủ trường (images): %v", got)
	}
	rec = e.do(http.MethodPut, path, catalogManager, "vo1", body)
	if rec.Code != http.StatusOK || !slices.Equal(variantIDsOf(t, decodeJSON(t, rec)), want) {
		t.Errorf("gửi lại cùng khóa: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/v1/catalog/products/"+id, catalogViewer, "", "")
	if p := decodeJSON(t, rec); !slices.Equal(variantIDsOf(t, p), want) || p["version"] != float64(2) {
		t.Errorf("getProduct sau khi sắp (gửi lại không sắp thêm): %v", p)
	}
	expect(t, "version cũ", e.do(http.MethodPut, path, catalogManager, "vo2", body), http.StatusConflict, "version_conflict")
	n := 0
	for name, list := range map[string][]string{
		"thiếu": ids[:2],
		"trùng": {ids[0], ids[0], ids[1]},
		"mã lạ": {ids[0], ids[1], "0190a000-0000-7000-8000-000000000000"},
		"thừa":  {ids[0], ids[1], ids[2], "0190a000-0000-7000-8000-000000000000"},
	} {
		n++
		expect(t, name, e.do(http.MethodPut, path, catalogManager, fmt.Sprintf("bad-%d", n),
			fmt.Sprintf(`{"productVersion":2,"variantIds":%s}`, idsJSON(list))), http.StatusUnprocessableEntity, "invalid_input")
	}
	expect(t, "người chỉ xem", e.do(http.MethodPut, path, catalogViewer, "vo3", body), http.StatusForbidden, "forbidden")
	expect(t, "tenant khác", e.do(http.MethodPut, path, catalogOtherB, "vo4", body), http.StatusNotFound, "not_found")
}
