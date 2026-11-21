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

// authProblems trả mỗi route sai luật x-auth (M5-05, luật openapi 6) một dòng nêu operationId: Auth chỉ nhận access,
// service_key, public; public đúng khi Public; route service_key chỉ ở /api/v1/web/*, khai quyền web.*, path
// /api/v1/web/* luôn service_key; Admission chỉ trên route service_key required; BodyLimit 0 hoặc 1 KiB – 8 MiB.
func authProblems(routes []httpapi.Route) []string {
	var out []string
	for _, r := range routes {
		_, path, _ := strings.Cut(r.Pattern, " ")
		web := strings.HasPrefix(path, "/api/v1/web/")
		switch {
		case r.Auth != "access" && r.Auth != "service_key" && r.Auth != "public":
			out = append(out, r.OperationID+": Auth lạ "+strconv.Quote(r.Auth))
		case (r.Auth == "public") != r.Public:
			out = append(out, r.OperationID+": Auth public phải đi cùng Public")
		case web != (r.Auth == "service_key"):
			out = append(out, r.OperationID+": path /api/v1/web/* khi và chỉ khi Auth service_key")
		case r.Auth == "service_key" && !strings.HasPrefix(r.Permission, "web."):
			out = append(out, r.OperationID+": route service_key phải có quyền web.*, có "+strconv.Quote(r.Permission))
		}
		if r.Admission != "" && (r.Auth != "service_key" || r.IdempotencyMode != "required") {
			out = append(out, r.OperationID+": Admission chỉ trên route service_key required")
		}
		if r.BodyLimit != 0 && (r.BodyLimit < 1<<10 || r.BodyLimit > 8<<20) {
			out = append(out, r.OperationID+": BodyLimit ngoài 1 KiB – 8 MiB")
		}
	}
	return out
}

func TestRoutesAuthRules(t *testing.T) {
	if got := authProblems(httpapi.Routes); len(got) != 0 {
		t.Fatalf("bảng route sai luật x-auth:\n%s", strings.Join(got, "\n"))
	}
	fake := []httpapi.Route{
		{Pattern: "GET /api/v1/web/products", OperationID: "okWeb", Permission: "web.catalog.read", Auth: "service_key"},
		{
			Pattern: "POST /api/v1/web/flash", OperationID: "okFlash", Permission: "web.flash.buy", Auth: "service_key",
			Write: true, IdempotencyMode: "required", Admission: "flash", BodyLimit: 4 << 20,
		},
		{Pattern: "GET /health/live", OperationID: "okPublic", Public: true, Auth: "public"},
		{Pattern: "GET /api/v1/x", OperationID: "okStaff", Permission: "catalog.read", Auth: "access"},
		{Pattern: "GET /api/v1/web/a", OperationID: "webStaffPerm", Permission: "catalog.read", Auth: "service_key"},
		{Pattern: "GET /api/v1/web/b", OperationID: "webAccess", Permission: "web.catalog.read", Auth: "access"},
		{Pattern: "GET /api/v1/c", OperationID: "serviceOffWeb", Permission: "web.catalog.read", Auth: "service_key"},
		{Pattern: "GET /api/v1/d", OperationID: "noAuth", Permission: "catalog.read"},
		{Pattern: "GET /health/x", OperationID: "publicNotMarked", Public: true, Auth: "access"},
		{
			Pattern: "POST /api/v1/web/e", OperationID: "admissionExempt", Permission: "web.flash.buy", Auth: "service_key",
			Write: true, IdempotencyMode: "exempt", Admission: "flash",
		},
		{Pattern: "POST /api/v1/f", OperationID: "bodyTooBig", Permission: "catalog.read", Auth: "access", BodyLimit: 16 << 20},
	}
	got := strings.Join(authProblems(fake), "\n")
	for _, w := range []string{"webStaffPerm", "webAccess", "serviceOffWeb", "noAuth", "publicNotMarked", "admissionExempt", "bodyTooBig"} {
		if !strings.Contains(got, w) {
			t.Errorf("bảng giả phải báo %s, có:\n%s", w, got)
		}
	}
	for _, ok := range []string{"okWeb", "okFlash", "okPublic", "okStaff"} {
		if strings.Contains(got, ok) {
			t.Errorf("route đúng luật %s không được bị báo:\n%s", ok, got)
		}
	}
}
