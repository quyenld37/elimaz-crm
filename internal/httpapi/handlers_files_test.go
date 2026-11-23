package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	filesA  = "a@files.test" // tenant A, có files.upload
	filesB  = "b@files.test" // tenant B, có files.upload
	filesNo = "n@files.test" // tenant A, không có quyền nào
)

type filesAPIEnv struct {
	t     *testing.T
	staff *testkit.Staff
	keys  *cfaccesstest.AccessKeys
	store *files.MemStore
	srv   http.Handler
	a     uuid.UUID
	n     int
}

func newFilesAPIEnv(t *testing.T) *filesAPIEnv {
	t.Helper()
	return newFilesAPIEnvLog(t, slog.New(slog.NewJSONHandler(&lockedBuffer{buf: &bytes.Buffer{}}, nil)))
}

// newFilesAPIEnvLog như newFilesAPIEnv, log của máy chủ ghi vào logger.
func newFilesAPIEnvLog(t *testing.T, logger *slog.Logger) *filesAPIEnv {
	t.Helper()
	e := &filesAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), store: filestest.MemFileStore(t)}
	a, b := e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	e.a = a
	e.staff.Grant(t, a, e.staff.User(t, a, filesA, "active"), []string{"files.upload"}, "all", "")
	e.staff.Grant(t, b, e.staff.User(t, b, filesB, "active"), []string{"files.upload"}, "all", "")
	e.staff.User(t, a, filesNo, "active")
	authE := &authEnv{t: t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}, FilesPublicBaseURL: "https://img.test"},
		DB:     e.staff.App, Files: e.store,
		Logger: logger, Access: authE.verifier(),
	})
	return e
}

// do gửi yêu cầu; yêu cầu ghi mang khóa chống trùng mới mỗi lần.
func (e *filesAPIEnv) do(method, path, email, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	key := ""
	if method != http.MethodGet {
		e.n++
		key = fmt.Sprintf("k%d", e.n)
	}
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

type uploadResp struct {
	FileID    uuid.UUID         `json:"fileId"`
	PutURL    string            `json:"putUrl"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

func (e *filesAPIEnv) createUpload(email string, size int64) (int, uploadResp, string) {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/files/uploads", email,
		fmt.Sprintf(`{"purpose":"product_image","fileName":"ao.jpg","contentType":"image/jpeg","sizeBytes":%d}`, size))
	var out uploadResp
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			e.t.Fatal(err)
		}
	}
	return rec.Code, out, rec.Body.String()
}

func (e *filesAPIEnv) put(u uploadResp, n int) int {
	e.t.Helper()
	req, err := http.NewRequestWithContext(e.t.Context(), http.MethodPut, u.PutURL, bytes.NewReader(make([]byte, n)))
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range u.Headers {
		req.Header.Set(k, v)
	}
	code, _, _ := testkit.Do(e.t, req)
	return code
}

// filesRows đếm dòng platform.files của tenant A.
func (e *filesAPIEnv) filesRows() int {
	e.t.Helper()
	var n int
	e.staff.Exec(e.t, e.a, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT count(*) FROM platform.files").Scan(&n)
	})
	return n
}

// TP03 phần kích thước, loại, mục đích: 422 trước khi có dòng nào.
func TestFilesAPI_CreateUploadLimits(t *testing.T) {
	t.Parallel()
	e := newFilesAPIEnv(t)
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"ảnh 25 MiB", `{"purpose":"product_image","fileName":"a.jpg","contentType":"image/jpeg","sizeBytes":26214400}`, 422, "file_too_large"},
		{"svg", `{"purpose":"product_image","fileName":"a.svg","contentType":"image/svg+xml","sizeBytes":100}`, 422, "file_type_not_allowed"},
		{"mục đích lạ", `{"purpose":"khong_co","fileName":"a.jpg","contentType":"image/jpeg","sizeBytes":100}`, 422, "file_purpose_not_allowed"},
		{"thiếu trường", `{"purpose":"product_image"}`, 400, "bad_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.do(http.MethodPost, "/api/v1/files/uploads", filesA, c.body)
			if rec.Code != c.status || errCode(t, rec) != c.code {
				t.Fatalf("muốn %d %s, nhận %d %s", c.status, c.code, rec.Code, rec.Body.String())
			}
		})
	}
	if rec := e.do(http.MethodPost, "/api/v1/files/uploads", filesNo,
		`{"purpose":"product_image","fileName":"a.jpg","contentType":"image/jpeg","sizeBytes":100}`); rec.Code != http.StatusForbidden {
		t.Fatalf("không có files.upload phải 403, nhận %d", rec.Code)
	}
}

// Tạo → PUT bằng putUrl → complete → get; complete trước khi tải 409; tenant B 404 (PQ08).
func TestFilesAPI_UploadFlow(t *testing.T) {
	t.Parallel()
	e := newFilesAPIEnv(t)
	code, up, body := e.createUpload(filesA, 1000)
	if code != http.StatusCreated || up.FileID == uuid.Nil || up.PutURL == "" || up.Headers["Content-Type"] != "image/jpeg" {
		t.Fatalf("createUpload: %d %s", code, body)
	}
	complete := "/api/v1/files/" + up.FileID.String() + "/complete"
	getPath := "/api/v1/files/" + up.FileID.String()
	if rec := e.do(http.MethodPost, complete, filesA, ""); rec.Code != http.StatusConflict || errCode(t, rec) != "upload_missing" {
		t.Fatalf("complete trước khi tải: muốn 409 upload_missing, nhận %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodGet, getPath, filesA, ""); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"awaiting_upload"`)) {
		t.Fatalf("getFile awaiting_upload: %d %s", rec.Code, rec.Body.String())
	}
	if c := e.put(up, 1000); c != http.StatusOK {
		t.Fatalf("PUT: %d", c)
	}
	for _, path := range []string{complete, getPath} {
		method := http.MethodPost
		if path == getPath {
			method = http.MethodGet
		}
		if rec := e.do(method, path, filesB, ""); rec.Code != http.StatusNotFound || errCode(t, rec) != "not_found" {
			t.Fatalf("tenant B %s %s: muốn 404, nhận %d %s", method, path, rec.Code, rec.Body.String())
		}
	}
	rec := e.do(http.MethodPost, complete, filesA, "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"uploaded"`)) {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body.String())
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 'file.process' AND dedupe_key = $1", up.FileID.String()); n != 1 {
		t.Fatalf("phải đúng 1 lệnh file.process, có %d", n)
	}
	rec = e.do(http.MethodGet, getPath, filesA, "")
	var got struct {
		Status   string `json:"status"`
		Variants []any  `json:"variants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Status != "uploaded" || got.Variants == nil {
		t.Fatalf("getFile: %d %s", rec.Code, rec.Body.String())
	}
}

// Gửi lại createUpload cùng khóa chống trùng: cùng tệp, cùng đường dẫn, không thêm dòng.
func TestFilesAPI_CreateUploadReplay(t *testing.T) {
	t.Parallel()
	e := newFilesAPIEnv(t)
	before := e.filesRows()
	body1 := `{"purpose":"product_image","fileName":"b.jpg","contentType":"image/png","sizeBytes":10}`
	r1 := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/files/uploads", filesA, "same-key", body1)
	r2 := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/files/uploads", filesA, "same-key", body1)
	var u1, u2 uploadResp
	_ = json.Unmarshal(r1.Body.Bytes(), &u1)
	_ = json.Unmarshal(r2.Body.Bytes(), &u2)
	if r1.Code != http.StatusCreated || r2.Code != http.StatusCreated || u1.FileID != u2.FileID || u1.PutURL != u2.PutURL ||
		!u1.ExpiresAt.Equal(u2.ExpiresAt) || r2.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại cùng khóa: %d %s / %d %s replayed=%q", r1.Code, r1.Body.String(), r2.Code, r2.Body.String(),
			r2.Header().Get("Idempotent-Replayed"))
	}
	if after := e.filesRows(); after != before+1 {
		t.Fatalf("gửi lại cùng khóa phải thêm đúng 1 tệp, thêm %d", after-before)
	}
}

// Không có files.upload thì cả ba route 403; mã lạ 404.
func TestFilesAPI_PermissionsAndUnknown(t *testing.T) {
	t.Parallel()
	e := newFilesAPIEnv(t)
	id := uuid.NewString()
	for _, r := range [][2]string{{http.MethodPost, "/api/v1/files/" + id + "/complete"}, {http.MethodGet, "/api/v1/files/" + id}} {
		if rec := e.do(r[0], r[1], filesNo, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s không quyền: muốn 403, nhận %d", r[0], r[1], rec.Code)
		}
		if rec := e.do(r[0], r[1], filesA, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s mã lạ: muốn 404, nhận %d", r[0], r[1], rec.Code)
		}
	}
}

// L3 của threat model M1 (Task 414): đường dẫn ký sẵn mà createUpload trả cho trình duyệt không lọt vào log hay dấu
// vết của máy chủ — qua tạo, gửi lại cùng khóa, complete trước và sau khi tải, getFile.
func TestFilesAPI_SignedURLNotLogged(t *testing.T) {
	// Không t.Parallel (Task 474): CaptureSpans đổi TracerProvider toàn cục.
	spans := testkit.CaptureSpans(t)
	logger, logs := testkit.CaptureLog(t)
	e := newFilesAPIEnvLog(t, logger)
	code, up, body := e.createUpload(filesA, 1000)
	if code != http.StatusCreated {
		t.Fatalf("createUpload: %d %s", code, body)
	}
	u, err := url.Parse(up.PutURL)
	if err != nil {
		t.Fatal(err)
	}
	sig := u.Query().Get("sig")
	if sig == "" {
		t.Fatalf("đường dẫn ký sẵn không có chữ ký: %s", up.PutURL)
	}
	complete := "/api/v1/files/" + up.FileID.String() + "/complete"
	e.do(http.MethodPost, complete, filesA, "") // 409 upload_missing: đường lỗi
	if c := e.put(up, 1000); c != http.StatusOK {
		t.Fatalf("PUT: %d", c)
	}
	e.do(http.MethodPost, complete, filesA, "")
	e.do(http.MethodGet, "/api/v1/files/"+up.FileID.String(), filesA, "")
	text := logs.String() + testkit.SpanText(spans.GetSpans().Snapshots())
	if !strings.Contains(text, "/api/v1/files") {
		t.Fatalf("log và span phải có các yêu cầu (kiểm chỗ quét có dữ liệu): %s", text)
	}
	for _, bad := range []string{sig, "X-Amz-Signature", u.RawQuery} {
		if strings.Contains(text, bad) {
			t.Errorf("log hoặc dấu vết chứa %q", bad)
		}
	}
}
