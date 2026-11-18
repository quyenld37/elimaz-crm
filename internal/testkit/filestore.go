package testkit

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	oexec "os/exec"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/files"
)

// Danh tính dev của SeaweedFS (docker/seaweedfs/s3.json): khóa cố định, công khai, chỉ cho máy dev.
const (
	S3App   = "crm-app"
	S3Admin = "crm-admin"

	devS3AppKey      = "crm-app-dev"
	devS3AppSecret   = "crm-app-dev-only-not-a-secret" //nolint:gosec // khóa dev công khai trong docker/seaweedfs/s3.json
	devS3AdminKey    = "crm-admin-dev"
	devS3AdminSecret = "crm-admin-dev-only-not-a-secret" //nolint:gosec // như trên
)

// MemFileStore dựng files.MemStore có máy chủ HTTP phục vụ đường dẫn ký sẵn; đóng khi test xong. Dùng cho test của
// mọi mốc cần kho tệp mà không cần SeaweedFS.
func MemFileStore(t testing.TB) *files.MemStore {
	t.Helper()
	m := files.NewMemStore()
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	m.SetBaseURL(srv.URL)
	return m
}

// S3FileStore dựng files.S3Store bằng danh tính crm-app trên SeaweedFS của compose dev (S3Config).
func S3FileStore(t testing.TB) *files.S3Store {
	t.Helper()
	s, err := files.NewS3Store(S3Config(t, S3App))
	if err != nil {
		t.Fatalf("testkit: dựng S3Store: %v", err)
	}
	return s
}

// S3Config trả cấu hình kho dev cho danh tính who (S3App hoặc S3Admin). Địa chỉ: CRM_TEST_S3_URL nếu có; không thì
// 127.0.0.1:CRM_S3_PORT; không có CRM_S3_PORT thì hỏi docker cổng của dịch vụ seaweedfs thuộc dự án compose
// COMPOSE_PROJECT_NAME (phiên song song có cổng riêng); cuối cùng 8333. Kho không trả lời đúng (không phải SeaweedFS
// dev, hay chưa `make db-up`) thì test đỏ, không bỏ qua.
func S3Config(t testing.TB, who string) files.S3Config {
	t.Helper()
	ak, sk := devS3AppKey, devS3AppSecret
	if who == S3Admin {
		ak, sk = devS3AdminKey, devS3AdminSecret
	}
	cfg := files.S3Config{Endpoint: s3Endpoint(t), Region: "us-east-1", AccessKeyID: ak, SecretAccessKey: sk, BucketPrefix: "crm-"}
	return cfg
}

func s3Endpoint(t testing.TB) string {
	t.Helper()
	ep := os.Getenv("CRM_TEST_S3_URL") //nolint:forbidigo // testkit chỉ chạy trong test, đọc biến của môi trường test
	if ep == "" {
		port := os.Getenv("CRM_S3_PORT") //nolint:forbidigo // như trên
		if port == "" {
			port = composeS3Port(t)
		}
		ep = "http://127.0.0.1:" + port
	}
	checkS3(t, ep)
	return ep
}

// composeS3Port hỏi docker cổng máy của seaweedfs:8333 thuộc dự án COMPOSE_PROJECT_NAME; không hỏi được → "8333".
func composeS3Port(t testing.TB) string {
	t.Helper()
	project := os.Getenv("COMPOSE_PROJECT_NAME") //nolint:forbidigo // như trên
	if project == "" {
		return "8333"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := oexec.CommandContext(ctx, "docker", "ps", "-q", //nolint:gosec // G204: lệnh cố định, tên dự án chỉ là giá trị nhãn lọc

		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service=seaweedfs").Output()
	if err != nil || strings.TrimSpace(string(id)) == "" {
		return "8333"
	}
	out, err := oexec.CommandContext(ctx, "docker", "port", strings.Fields(string(id))[0], "8333/tcp").Output() //nolint:gosec // mã container do docker trả
	if err != nil {
		return "8333"
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(strings.Split(string(out), "\n")[0]))
	if err != nil {
		return "8333"
	}
	return port
}

// checkS3 đòi ep là SeaweedFS dev: HEAD bucket crm-tmp (chưa ký) phải trả 403 hay 200 của S3, không phải lỗi mạng
// hay dịch vụ khác (cổng 8333 có thể là của dự án khác trên máy).
func checkS3(t testing.TB, ep string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, ep+"/crm-tmp", nil) //nolint:gosec // G704: địa chỉ kho dev do môi trường test đặt
	if err != nil {
		t.Fatalf("testkit: địa chỉ kho %q sai: %v", ep, err)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req) //nolint:gosec // G704: như trên
	if err != nil {
		t.Fatalf("testkit: SeaweedFS dev không trả lời ở %s (`make db-up`, đặt CRM_S3_PORT): %v", ep, err)
	}
	_ = resp.Body.Close()
	if resp.Header.Get("Server") != "SeaweedFS" && !strings.HasPrefix(resp.Header.Get("Server"), "SeaweedFS") {
		t.Fatalf("testkit: %s không phải SeaweedFS (Server=%q): đặt CRM_S3_PORT đúng cổng của compose dev", ep, resp.Header.Get("Server"))
	}
}
