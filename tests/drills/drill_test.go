//go:build drill

// Package drills là các bài diễn tập hạ tầng của CRM (kế hoạch M1, Task M1-46): chạy trên bộ compose dev của một dự án
// riêng, có bước làm hỏng thật (docker kill, xóa volume). Không chạy trong `go test ./...` (nhãn build drill); chạy
// qua scripts/files/crash-drill.sh, scripts/files/restore-drill.sh.
//
// An toàn: chỉ đụng container, volume của dự án compose CRM_DRILL_PROJECT (bắt buộc, không nhận "crm" hay
// "crm-shared": bộ dùng chung của các phiên khác).
package drills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	drillAUD    = "aud-drill"
	drillOrigin = "https://crm.drill.test"
	drillStaff  = "kho@drill.test" // files.upload, platform.files.manage
)

// drill là một lần diễn tập: database thử, máy chủ API thật (httpapi) trên kho SeaweedFS của dự án compose, worker
// chạy file.process (imager giả, xem testkit.StubImager) và file.verify.
type drill struct {
	t       *testing.T
	project string
	staff   *testkit.Staff
	tenant  uuid.UUID
	keys    *testkit.AccessKeys
	fs      *files.S3Store
	api     *httptest.Server
	w       *queue.Worker
	hc      *http.Client
	n       atomic.Int64
}

func newDrill(t *testing.T) *drill {
	t.Helper()
	project := os.Getenv("CRM_DRILL_PROJECT")
	switch project {
	case "", "crm", "crm-shared":
		t.Fatalf("đặt CRM_DRILL_PROJECT là dự án compose riêng của bài diễn tập (không phải %q)", project)
	}
	if os.Getenv("COMPOSE_PROJECT_NAME") != project {
		t.Fatalf("COMPOSE_PROJECT_NAME (%q) phải bằng CRM_DRILL_PROJECT (%q): kho của test và kho bị làm hỏng phải là một",
			os.Getenv("COMPOSE_PROJECT_NAME"), project)
	}
	s := testkit.OpenStaff(t)
	d := &drill{
		t: t, project: project, staff: s, tenant: s.Tenant(t, "A"), keys: testkit.AccessKeyServer(t),
		fs: testkit.S3FileStore(t), hc: &http.Client{Timeout: 60 * time.Second},
	}
	s.Grant(t, d.tenant, s.User(t, d.tenant, drillStaff, "active"), []string{"files.upload", "platform.files.manage"}, "all", "")
	d.api = httptest.NewServer(httpapi.NewServer(httpapi.Deps{
		Config: config.Config{ConsoleOrigins: []string{drillOrigin}, FilesPublicBaseURL: "https://img.drill.test", ImagePresetVersion: 1},
		DB:     s.App, Files: d.fs, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Access: d.keys.Verifier(t, drillAUD),
		// Một nhân viên gửi vài trăm yêu cầu trong vài giây: bài diễn tập đo kho tệp, không đo giới hạn tần suất.
		RateLimit: httpapi.RateLimit{Max: 1 << 20, Window: time.Minute},
	}))
	t.Cleanup(d.api.Close)
	o := files.ProcessorOptions{DB: s.App, Store: d.fs, Imager: testkit.StubImager{}, PresetVersion: 1}
	d.w = queue.NewWorker(s.App, nil, queue.WorkerOptions{
		WorkerID: "drill-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(s.App, []commands.Handler{files.NewProcessHandler(o), files.NewVerifyHandler(o)}),
	})
	return d
}

// call gửi yêu cầu tới API như màn hình (thẻ Access có bước hai; yêu cầu ghi có Origin, khóa chống trùng mới).
func (d *drill) call(ctx context.Context, method, path, body string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.api.URL+path, strings.NewReader(body))
	if err != nil {
		return 0, nil, err //nolint:wrapcheck // test
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", d.keys.Sign(testkit.AccessClaims{Email: drillStaff, AUD: drillAUD, MFA: []string{"mfa"}}))
	if method != http.MethodGet {
		req.Header.Set("Origin", drillOrigin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", fmt.Sprintf("drill-%d", d.n.Add(1)))
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return 0, nil, err //nolint:wrapcheck // test
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err //nolint:wrapcheck // test
}

// upload tải body lên qua đường thật: createUpload → PUT thẳng lên kho bằng đường dẫn ký sẵn → complete. Trả mã tệp
// và lỗi của bước hỏng (complete khác 200 cũng là lỗi).
func (d *drill) upload(ctx context.Context, body []byte) (uuid.UUID, error) {
	code, raw, err := d.call(ctx, http.MethodPost, "/api/v1/files/uploads",
		fmt.Sprintf(`{"purpose":"product_image","fileName":"anh.jpg","contentType":"image/jpeg","sizeBytes":%d}`, len(body)))
	if err != nil || code != http.StatusCreated {
		return uuid.Nil, fmt.Errorf("createUpload: %d %v", code, err)
	}
	var u struct {
		FileID  uuid.UUID         `json:"fileId"`
		PutURL  string            `json:"putUrl"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return uuid.Nil, fmt.Errorf("createUpload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.PutURL, bytes.NewReader(body))
	if err != nil {
		return u.FileID, err //nolint:wrapcheck // test
	}
	for k, v := range u.Headers {
		req.Header.Set(k, v)
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return u.FileID, fmt.Errorf("PUT: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return u.FileID, fmt.Errorf("PUT: %d", resp.StatusCode)
	}
	code, raw, err = d.call(ctx, http.MethodPost, "/api/v1/files/"+u.FileID.String()+"/complete", "")
	if err != nil || code != http.StatusOK {
		return u.FileID, fmt.Errorf("complete: %d %v %s", code, err, raw)
	}
	return u.FileID, nil
}

// drain chạy worker tới khi không còn việc đến hạn.
func (d *drill) drain() {
	d.t.Helper()
	for range 500 {
		n, err := d.w.RunOnce(d.t.Context(), 10)
		if err != nil {
			d.t.Fatalf("RunOnce: %v", err)
		}
		if n == 0 {
			return
		}
	}
	d.t.Fatal("worker còn việc sau 500 lượt")
}

// sha đọc lại đối tượng và trả sha256 hex của nó. Kho vừa khởi động lại có một lúc trả lỗi 500 ("volume not
// found": volume chưa đăng ký lại với master dù healthcheck đã qua): đọc lại tới 2 phút với lỗi khác "không có".
func (d *drill) sha(b files.Bucket, key string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		sum, err := d.readSHA(b, key)
		if err == nil || errors.Is(err, files.ErrObjectNotFound) || time.Now().After(deadline) {
			return sum, err
		}
		time.Sleep(time.Second)
	}
}

func (d *drill) readSHA(b files.Bucket, key string) (string, error) {
	rc, err := d.fs.Open(context.WithoutCancel(d.t.Context()), b, key, 1<<30)
	if err != nil {
		return "", err //nolint:wrapcheck // test
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err //nolint:wrapcheck // test
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func repoDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// docker chạy lệnh docker (trả stdout; lỗi kèm stderr). Lệnh compose luôn có -p của dự án diễn tập.
func (d *drill) docker(timeout time.Duration, args ...string) string {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(d.t.Context()), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // G204: lệnh cố định của bài diễn tập
	cmd.Dir = repoDir()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		d.t.Fatalf("docker %v: %v\n%s%s", args, err, out, stderr.String())
	}
	return string(out)
}

func (d *drill) compose(timeout time.Duration, args ...string) string {
	d.t.Helper()
	return d.docker(timeout, append([]string{"compose", "-p", d.project, "-f", "docker/compose.dev.yml"}, args...)...)
}

// waitStore chờ kho trả lời lại (Head một khóa không có ra "không có", không phải lỗi mạng).
func (d *drill) waitStore() {
	d.t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, err := d.fs.Head(d.t.Context(), files.BucketTmp, "drill/khong-co")
		if err == nil || errors.Is(err, files.ErrObjectNotFound) {
			return
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("kho không trả lời sau 2 phút: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
