package httpapi

import (
	"maps"
	"net/http"
	"strings"
	"testing"
)

// measureOf trả measureCm của mẫu trong câu trả lời (nil khi vắng mặt).
func measureOf(t *testing.T, p map[string]any) map[string]any {
	t.Helper()
	m, ok := p["measureCm"]
	if !ok {
		return nil
	}
	return m.(map[string]any) //nolint:forcetypeassert // test
}

// Số đo của mẫu qua API nhân viên (Task 516): tạo không số đo → measureCm vắng mặt; tạo kèm Dài 88, Rộng 20 → trả
// đúng (Cao vắng mặt), getProduct đọc lại; sửa tên giữ số đo; sửa {height:5} → chỉ còn Cao; null bỏ cả ba; 0, 1001 ở
// tạo hay sửa → 422 invalid_input (không đổi gì).
func TestCatalogAPI_ProductMeasure(t *testing.T) {
	t.Parallel()
	e := newCatalogAPIEnv(t)
	if p := e.createDam01(); measureOf(t, p) != nil {
		t.Fatalf("mẫu không số đo có measureCm: %v", p["measureCm"])
	}
	body := strings.Replace(dam01Body, `"code":"DAM01"`, `"code":"QUAN01","measureCm":{"length":88,"width":20}`, 1)
	body = strings.ReplaceAll(body, "DAM01-", "QUAN01-")
	if body == dam01Body {
		t.Fatal("không dựng được thân tạo mẫu có số đo")
	}
	rec := e.do(http.MethodPost, "/api/v1/catalog/products", catalogManager, "m1", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("tạo có số đo: %d %s", rec.Code, rec.Body.String())
	}
	p := decodeJSON(t, rec)
	want := map[string]any{"length": float64(88), "width": float64(20)}
	if got := measureOf(t, p); !maps.Equal(got, want) {
		t.Fatalf("tạo: measureCm %v, mong %v", got, want)
	}
	path := "/api/v1/catalog/products/" + p["id"].(string) //nolint:forcetypeassert // test
	if got := measureOf(t, decodeJSON(t, e.do(http.MethodGet, path, catalogManager, "", ""))); !maps.Equal(got, want) {
		t.Fatalf("getProduct: measureCm %v", got)
	}
	bad := strings.Replace(body, `"length":88`, `"length":0`, 1)
	expect(t, "tạo Dài 0", e.do(http.MethodPost, "/api/v1/catalog/products", catalogManager, "m2", strings.ReplaceAll(bad, "QUAN01", "QUAN02")),
		http.StatusUnprocessableEntity, "invalid_input")
	steps := []struct {
		key, body string
		want      map[string]any
	}{
		{"m3", `{"version":1,"name":"Quần tây"}`, want},
		{"m4", `{"version":2,"measureCm":{"height":5}}`, map[string]any{"height": float64(5)}},
		{"m5", `{"version":3,"measureCm":null}`, nil},
		{
			"m6", `{"version":4,"measureCm":{"length":1,"width":1000,"height":3}}`,
			map[string]any{"length": float64(1), "width": float64(1000), "height": float64(3)},
		},
	}
	for _, s := range steps {
		rec := e.do(http.MethodPatch, path, catalogManager, s.key, s.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", s.body, rec.Code, rec.Body.String())
		}
		if got := measureOf(t, decodeJSON(t, rec)); !maps.Equal(got, s.want) {
			t.Fatalf("%s: measureCm %v, mong %v", s.body, got, s.want)
		}
	}
	expect(t, "sửa Rộng 1001", e.do(http.MethodPatch, path, catalogManager, "m7", `{"version":5,"measureCm":{"width":1001}}`),
		http.StatusUnprocessableEntity, "invalid_input")
	got := decodeJSON(t, e.do(http.MethodGet, path, catalogManager, "", ""))
	if got["version"] != float64(5) || measureOf(t, got)["width"] != float64(1000) {
		t.Fatalf("số đo sai không được đổi gì: %v %v", got["version"], got["measureCm"])
	}
}
