package arch

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// Route công khai chỉ là ba đường health; mọi route khác khai một quyền có trong identity.Permissions.
func TestRoutesPublicSetAndPermissions(t *testing.T) {
	var public []string
	for _, r := range httpapi.Routes {
		switch {
		case r.Public && r.Permission != "":
			t.Errorf("%s vừa công khai vừa có quyền", r.OperationID)
		case r.Public:
			public = append(public, r.Pattern)
		case !identity.IsKnown(identity.Permission(r.Permission)):
			t.Errorf("%s khai quyền %q không có trong danh sách quyền", r.OperationID, r.Permission)
		}
	}
	slices.Sort(public)
	want := []string{"GET /health/deep", "GET /health/live", "GET /health/ready"}
	if !slices.Equal(public, want) {
		t.Fatalf("tập route công khai phải là %v, có %v", want, public)
	}
}

// Mọi route trong bảng đều được NewServer phục vụ (không ra 404 của ServeMux); đường ngoài bảng ra 404.
func TestRoutesAllServed(t *testing.T) {
	var logs bytes.Buffer
	srv := httpapi.NewServer(httpapi.Deps{
		Config: config.Config{ConsoleOrigins: []string{"https://crm.elimaz.test"}},
		DB:     testkit.OpenAppDB(t, db.PoolOptions{MaxConns: 2}),
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	param := regexp.MustCompile(`\{[^}]+\}`)
	for _, r := range httpapi.Routes {
		_, path, _ := strings.Cut(r.Pattern, " ")
		path = param.ReplaceAllString(path, "0195f3a0-0000-7000-8000-000000000001")
		req := httptest.NewRequestWithContext(t.Context(), r.Method, path, strings.NewReader("{}"))
		req.Header.Set("Origin", "https://crm.elimaz.test")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s (%s) không được phục vụ: %d %s", r.OperationID, r.Pattern, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/khong-co-trong-bang", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("đường ngoài Routes phải 404, có %d", rec.Code)
	}
}
