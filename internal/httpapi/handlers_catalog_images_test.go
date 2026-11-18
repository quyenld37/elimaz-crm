package httpapi

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

const imagesBaseURL = "https://anh.elimaz.test"

// imageFile tạo tệp ảnh sản phẩm ready (12 biến thể) của tenant.
func (e *catalogAPIEnv) imageFile(tenant uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		id = filestest.Image(e.t, tx, tenant, "product_image")
		return nil
	})
	return id
}

func listOf(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("thiếu %s: %v", key, m)
	}
	return v
}

func (e *catalogAPIEnv) attach(id string, version any, file uuid.UUID, key, extra string) map[string]any {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/catalog/products/"+id+"/images", catalogManager, key,
		fmt.Sprintf(`{"productVersion":%v,"fileId":"%s"%s}`, version, file, extra))
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("attachProductImage: %d %s", rec.Code, rec.Body.String())
	}
	return decodeJSON(e.t, rec)
}

// Bốn operation ảnh và ảnh trong getProduct, ảnh bìa trong listProducts (KH02, TP06).
func TestCatalogAPI_Images(t *testing.T) {
	e := newCatalogAPIEnv(t)
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}, FilesPublicBaseURL: imagesBaseURL, ImagePresetVersion: 1},
		DB:     e.staff.App, Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: &bytes.Buffer{}}, nil)),
		Access: (&authEnv{t: t, keys: e.keys}).verifier(),
	})
	p := e.createDam01()
	id := p["id"].(string) //nolint:forcetypeassert // test
	if imgs := listOf(t, p, "images"); len(imgs) != 0 {
		t.Fatalf("mẫu mới phải có images rỗng: %v", p["images"])
	}
	f1, f2 := e.imageFile(e.tenant), e.imageFile(e.tenant)
	p = e.attach(id, p["version"], f1, "i1", "")
	p = e.attach(id, p["version"], f2, "i2", `,"color":"Đỏ"`)
	imgs := listOf(t, p, "images")
	if len(imgs) != 2 || p["version"] != float64(3) {
		t.Fatalf("sau khi gắn 2 ảnh: %v", p)
	}
	first, second := imgs[0].(map[string]any), imgs[1].(map[string]any) //nolint:forcetypeassert // test
	if second["color"] != "Đỏ" || first["status"] != "ready" || len(listOf(t, first, "variants")) != 12 {
		t.Fatalf("ảnh trả về sai: %v", imgs)
	}
	url, _ := listOf(t, first, "variants")[0].(map[string]any)["url"].(string) //nolint:forcetypeassert // test
	if !strings.HasPrefix(url, imagesBaseURL+"/img/") {
		t.Errorf("đường dẫn biến thể sai: %s", url)
	}

	e.checkAttachRefusals(id)

	// Sắp lại.
	i1, i2 := first["id"].(string), second["id"].(string) //nolint:forcetypeassert // test
	rec := e.do(http.MethodPut, "/api/v1/catalog/products/"+id+"/images/order", catalogManager, "o1",
		fmt.Sprintf(`{"productVersion":3,"imageIds":["%s","%s"]}`, i2, i1))
	if rec.Code != http.StatusOK {
		t.Fatalf("reorderProductImages: %d %s", rec.Code, rec.Body.String())
	}
	if got := listOf(t, decodeJSON(t, rec), "images"); got[0].(map[string]any)["id"] != i2 { //nolint:forcetypeassert // test
		t.Errorf("thứ tự sau khi sắp lại sai: %v", got)
	}
	rec = e.do(http.MethodPut, "/api/v1/catalog/products/"+id+"/images/order", catalogManager, "o2",
		fmt.Sprintf(`{"productVersion":4,"imageIds":["%s"]}`, i2))
	if rec.Code != http.StatusBadRequest || decodeJSON(t, rec)["error"] != "invalid_input" {
		t.Errorf("sắp lại thiếu ảnh: mong 400 invalid_input, có %d %s", rec.Code, rec.Body.String())
	}

	// Ảnh bìa của danh sách là ảnh đứng đầu.
	rec = e.do(http.MethodGet, "/api/v1/catalog/products", catalogViewer, "", "")
	items := listOf(t, decodeJSON(t, rec), "items")
	cover, _ := items[0].(map[string]any)["cover"].(map[string]any) //nolint:forcetypeassert // test
	if rec.Code != http.StatusOK || cover["imageId"] != i2 || cover["status"] != "ready" || len(listOf(t, cover, "variants")) != 12 {
		t.Fatalf("ảnh bìa trong listProducts sai: %d %s", rec.Code, rec.Body.String())
	}

	e.checkDeleteRestore(id, i1, i2)
	e.checkLost(id, i2, f2)
}

// checkAttachRefusals: tệp đang xử lý 409 file_not_ready; tệp của tenant khác 404; người chỉ xem 403; version cũ 409.
func (e *catalogAPIEnv) checkAttachRefusals(id string) {
	e.t.Helper()
	var processing uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		processing = filestest.Uploaded(e.t, tx, e.tenant, "product_image")
		return nil
	})
	b := e.staff.Tenant(e.t, "C")
	for i, c := range []struct {
		name, email, body string
		code              int
		errCode           string
	}{
		{"đang xử lý", catalogManager, fmt.Sprintf(`{"productVersion":3,"fileId":"%s"}`, processing), http.StatusConflict, "file_not_ready"},
		{"tenant khác", catalogManager, fmt.Sprintf(`{"productVersion":3,"fileId":"%s"}`, e.imageFile(b)), http.StatusNotFound, "not_found"},
		{"chỉ xem", catalogViewer, fmt.Sprintf(`{"productVersion":3,"fileId":"%s"}`, e.imageFile(e.tenant)), http.StatusForbidden, "forbidden"},
		{"version cũ", catalogManager, fmt.Sprintf(`{"productVersion":1,"fileId":"%s"}`, e.imageFile(e.tenant)), http.StatusConflict, "version_conflict"},
	} {
		rec := e.do(http.MethodPost, "/api/v1/catalog/products/"+id+"/images", c.email, "e"+strconv.Itoa(i), c.body)
		if rec.Code != c.code || decodeJSON(e.t, rec)["error"] != c.errCode {
			e.t.Errorf("%s: mong %d %s, có %d %s", c.name, c.code, c.errCode, rec.Code, rec.Body.String())
		}
	}
}

// checkDeleteRestore (TP06): xóa → còn 1; khôi phục → 2; quá 30 ngày → 410 restore_expired; tenant khác → 404.
func (e *catalogAPIEnv) checkDeleteRestore(id, i1, i2 string) {
	e.t.Helper()
	rec := e.do(http.MethodDelete, "/api/v1/catalog/products/"+id+"/images/"+i2, catalogManager, "d1", "")
	if rec.Code != http.StatusOK || len(listOf(e.t, decodeJSON(e.t, rec), "images")) != 1 {
		e.t.Fatalf("deleteProductImage: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeJSON(e.t, e.do(http.MethodGet, "/api/v1/catalog/products/"+id, catalogViewer, "", ""))
	if imgs := listOf(e.t, got, "images"); len(imgs) != 1 {
		e.t.Errorf("getProduct sau khi xóa: %v", imgs)
	}
	rec = e.do(http.MethodPost, "/api/v1/catalog/products/"+id+"/images/"+i2+"/restore", catalogManager, "r1", "")
	if rec.Code != http.StatusOK || len(listOf(e.t, decodeJSON(e.t, rec), "images")) != 2 {
		e.t.Fatalf("restoreProductImage: %d %s", rec.Code, rec.Body.String())
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "UPDATE catalog.product_images SET deleted_at = now() - interval '31 days' WHERE id = $1", i1)
		return err //nolint:wrapcheck // test
	})
	rec = e.do(http.MethodPost, "/api/v1/catalog/products/"+id+"/images/"+i1+"/restore", catalogManager, "r2", "")
	if rec.Code != http.StatusGone || decodeJSON(e.t, rec)["error"] != "restore_expired" {
		e.t.Errorf("khôi phục quá 30 ngày: mong 410 restore_expired, có %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodDelete, "/api/v1/catalog/products/"+id+"/images/"+i2, catalogOtherB, "d-b", "")
	if rec.Code != http.StatusNotFound {
		e.t.Errorf("tenant B xóa ảnh của A: mong 404, có %d", rec.Code)
	}
}

// checkLost (TP12): getProduct 200, ảnh mất có status lost, không variants.
func (e *catalogAPIEnv) checkLost(id, i2 string, f2 uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		filestest.MarkLost(e.t, tx, f2)
		return nil
	})
	rec := e.do(http.MethodGet, "/api/v1/catalog/products/"+id, catalogViewer, "", "")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("getProduct có ảnh mất: %d", rec.Code)
	}
	for _, it := range listOf(e.t, decodeJSON(e.t, rec), "images") {
		img := it.(map[string]any) //nolint:forcetypeassert // test
		if img["id"] == i2 && (img["status"] != "lost" || img["variants"] != nil) {
			e.t.Errorf("ảnh mất phải status lost, không variants: %v", img)
		}
	}
}
