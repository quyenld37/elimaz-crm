package arch

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// Route công khai chỉ là ba đường health; route "chỉ cần đăng nhập" chỉ là GET /api/v1/me (Task 315); mọi route
// khác khai một quyền có trong identity.Permissions.
func TestRoutesPublicSetAndPermissions(t *testing.T) {
	var public, authOnly []string
	for _, r := range httpapi.Routes {
		switch {
		case r.Public && r.Permission != "":
			t.Errorf("%s vừa công khai vừa có quyền", r.OperationID)
		case r.Authenticated && (r.Public || r.Permission != "" || r.Write):
			t.Errorf("%s chỉ cần đăng nhập mà vẫn công khai, có quyền hoặc là route ghi", r.OperationID)
		case r.Authenticated:
			authOnly = append(authOnly, r.Pattern)
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
	if want := []string{"GET /api/v1/me"}; !slices.Equal(authOnly, want) {
		t.Fatalf("tập route chỉ cần đăng nhập phải là %v, có %v", want, authOnly)
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

// idempotencyProblems trả mỗi route sai luật chống trùng một dòng nêu operationId (ADR-41, ADR-47): route exempt hoặc
// no-replay phải là POST và có lý do; route ghi khác phải required; route đọc không có chế độ.
func idempotencyProblems(routes []httpapi.Route) []string {
	var out []string
	for _, r := range routes {
		switch {
		case !r.Write && r.IdempotencyMode != "":
			out = append(out, r.OperationID+": route đọc không được có IdempotencyMode")
		case !r.Write:
		case r.IdempotencyMode == "exempt" || r.IdempotencyMode == "no-replay":
			if strings.TrimSpace(r.IdempotencyReason) == "" {
				out = append(out, r.OperationID+": "+r.IdempotencyMode+" thiếu IdempotencyReason")
			}
			if r.Method != http.MethodPost {
				out = append(out, r.OperationID+": "+r.IdempotencyMode+" chỉ cho POST")
			}
		case r.IdempotencyMode != "required":
			out = append(out, r.OperationID+": route ghi phải IdempotencyMode required, có "+strconv.Quote(r.IdempotencyMode))
		}
	}
	return out
}

func TestRoutesIdempotencyModes(t *testing.T) {
	if got := idempotencyProblems(httpapi.Routes); len(got) != 0 {
		t.Fatalf("bảng route sai luật chống trùng:\n%s", strings.Join(got, "\n"))
	}
	fake := []httpapi.Route{
		{Method: "POST", OperationID: "okExempt", Write: true, IdempotencyMode: "exempt", IdempotencyReason: "chỉ đọc"},
		{Method: "POST", OperationID: "okRequired", Write: true, IdempotencyMode: "required"},
		{Method: "GET", OperationID: "okRead"},
		{Method: "POST", OperationID: "exemptNoReason", Write: true, IdempotencyMode: "exempt", IdempotencyReason: "  "},
		{Method: "PUT", OperationID: "exemptPut", Write: true, IdempotencyMode: "exempt", IdempotencyReason: "x"},
		{Method: "POST", OperationID: "noReplayNoReason", Write: true, IdempotencyMode: "no-replay"},
		{Method: "DELETE", OperationID: "writeNoMode", Write: true},
		{Method: "GET", OperationID: "readWithMode", IdempotencyMode: "required"},
	}
	got := strings.Join(idempotencyProblems(fake), "\n")
	for _, w := range []string{"exemptNoReason", "exemptPut", "noReplayNoReason", "writeNoMode", "readWithMode"} {
		if !strings.Contains(got, w) {
			t.Errorf("bảng giả phải báo %s, có:\n%s", w, got)
		}
	}
	for _, ok := range []string{"okExempt", "okRequired", "okRead"} {
		if strings.Contains(got, ok) {
			t.Errorf("route đúng luật %s không được bị báo:\n%s", ok, got)
		}
	}
}
