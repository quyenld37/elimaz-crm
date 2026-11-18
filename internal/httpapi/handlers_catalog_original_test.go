package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
)

// putOriginal ghi ảnh gốc orig/<sha> của tệp fileID lên kho e.mem; trả nội dung.
func (e *catalogAPIEnv) putOriginal(fileID uuid.UUID) string {
	e.t.Helper()
	var sha string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT encode(content_sha256, 'hex') FROM platform.files WHERE id = $1", fileID).Scan(&sha)
	})
	body := "goc-" + sha
	if err := e.mem.Put(e.t.Context(), files.BucketOriginals, files.OriginalKey(sha), strings.NewReader(body),
		int64(len(body)), "image/jpeg"); err != nil {
		e.t.Fatal(err)
	}
	return body
}

func (e *catalogAPIEnv) originalPath(productID, imageID string) string {
	return "/api/v1/catalog/products/" + productID + "/images/" + imageID + "/original"
}

// TP05: getOriginalImageURL — người có catalog.manage lấy đường dẫn ảnh gốc hạn 5 phút, tải về dạng attachment tên
// <mã mẫu>-<vị trí>.<đuôi>; người chỉ có catalog.read → 403; ảnh đã xóa, ảnh của mẫu khác, mẫu của tenant khác → 404.
// Hết hạn trên kho thật: TestOriginalURL_ExpiresOnStore (gói files).
func TestOriginalURL_ExpiresAndNeedsPermission(t *testing.T) {
	e := newCatalogAPIEnv(t)
	e.useImageServer()
	p := e.createDam01()
	id := p["id"].(string) //nolint:forcetypeassert // test
	f1, f2 := e.imageFile(e.tenant), e.imageFile(e.tenant)
	p = e.attach(id, p["version"], f1, "i1", "")
	p = e.attach(id, p["version"], f2, "i2", "")
	imgs := listOf(t, p, "images")
	i1, i2 := imgs[0].(map[string]any)["id"].(string), imgs[1].(map[string]any)["id"].(string) //nolint:forcetypeassert // test
	body := e.putOriginal(f2)

	before := time.Now()
	rec := e.do(http.MethodGet, e.originalPath(id, i2), catalogManager, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("getOriginalImageURL: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeJSON(t, rec)
	exp, err := time.Parse(time.RFC3339, out["expiresAt"].(string)) //nolint:forcetypeassert // test
	if err != nil || exp.Before(before.Add(5*time.Minute-2*time.Second)) || exp.After(time.Now().Add(5*time.Minute+time.Second)) {
		t.Errorf("expiresAt phải là 5 phút sau: %v (%v)", out["expiresAt"], err)
	}
	url := out["url"].(string) //nolint:forcetypeassert // test
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, header, got := testkit.Do(t, req)
	if status != http.StatusOK || string(got) != body {
		t.Fatalf("đọc ảnh gốc: %d %q", status, got)
	}
	if cd := header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "DAM01-2.jpg") {
		t.Errorf("Content-Disposition sai: %q", cd)
	}

	// Người chỉ xem: 403, không có đường dẫn.
	rec = e.do(http.MethodGet, e.originalPath(id, i2), catalogViewer, "", "")
	if rec.Code != http.StatusForbidden || decodeJSON(t, rec)["error"] != "forbidden" || strings.Contains(rec.Body.String(), "http") {
		t.Errorf("catalog.read: mong 403 forbidden, có %d %s", rec.Code, rec.Body.String())
	}

	// Xóa ảnh đầu: ảnh còn lại đứng thứ 1 → tên DAM01-1; ảnh đã xóa → 404.
	if rec = e.do(http.MethodDelete, "/api/v1/catalog/products/"+id+"/images/"+i1, catalogManager, "d1", ""); rec.Code != http.StatusOK {
		t.Fatalf("deleteProductImage: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, e.originalPath(id, i2), catalogManager, "", "")
	if rec.Code != http.StatusOK || !strings.Contains(decodeJSON(t, rec)["url"].(string), "DAM01-1.jpg") { //nolint:forcetypeassert // test
		t.Errorf("sau khi xóa ảnh đầu, tên phải là DAM01-1.jpg: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct{ name, path, email string }{
		{"ảnh đã xóa", e.originalPath(id, i1), catalogManager},
		{"ảnh không thuộc mẫu", e.originalPath(uuid.NewString(), i2), catalogManager},
		{"tenant khác", e.originalPath(id, i2), catalogOtherB},
	} {
		rec = e.do(http.MethodGet, c.path, c.email, "", "")
		if rec.Code != http.StatusNotFound || decodeJSON(t, rec)["error"] != "not_found" {
			t.Errorf("%s: mong 404 not_found, có %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
}
