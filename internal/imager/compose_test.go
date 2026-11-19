//go:build imagercompose

// Test trên compose dev (make test-imager-compose): container imager không có biến bí mật, không tới được PostgreSQL.
// Chạy trên máy (cần docker), với dự án compose đã dựng bằng `--profile imager up`.
package imager_test

import (
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

func compose(t *testing.T, args ...string) (string, error) {
	t.Helper()
	project := os.Getenv("COMPOSE_PROJECT")
	if project == "" {
		t.Fatal("cần COMPOSE_PROJECT (make test-imager-compose đặt)")
	}
	base := []string{"compose", "-p", project, "-f", "../../docker/compose.dev.yml", "--profile", "imager"}
	out, err := exec.CommandContext(t.Context(), "docker", append(base, args...)...).CombinedOutput() //nolint:gosec // G702: tham số do test dựng từ biến môi trường của make
	return string(out), err
}

func TestImagerCompose_NoSecretsInEnvironment(t *testing.T) {
	// /proc/1/environ là môi trường của tini (PID 1), imager thừa hưởng nguyên; đọc cả tiến trình imager để chắc.
	out, err := compose(t, "exec", "-T", "imager", "sh", "-c",
		`for p in /proc/[0-9]*; do if [ "$(cat $p/comm 2>/dev/null)" = imager ]; then tr '\0' '\n' < $p/environ; fi; done`)
	if err != nil {
		t.Fatalf("đọc env: %v\n%s", err, out)
	}
	if !strings.Contains(out, "IMAGER_MAX_PARALLEL=") {
		t.Fatalf("không đọc được env của tiến trình imager:\n%s", out)
	}
	bad := regexp.MustCompile(`(?i)secret|password|token|key|database`)
	for line := range strings.SplitSeq(out, "\n") {
		name, _, _ := strings.Cut(line, "=")
		if bad.MatchString(name) || bad.MatchString(line) {
			t.Fatalf("biến môi trường bị cấm trong imager: %s", name)
		}
	}
}

func TestImagerCompose_CannotReachPostgres(t *testing.T) {
	// Theo tên dịch vụ (DNS của mạng default) và theo IP của container postgres: cả hai phải thất bại.
	ipOut, err := exec.CommandContext(t.Context(), "docker", "inspect", "-f", //nolint:gosec // G702: tên container từ COMPOSE_PROJECT của make
		"{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", os.Getenv("COMPOSE_PROJECT")+"-postgres-1").Output()
	if err != nil {
		t.Fatalf("docker inspect postgres: %v", err)
	}
	targets := append([]string{"postgres"}, strings.Fields(string(ipOut))...)
	if len(targets) < 2 {
		t.Fatalf("không lấy được IP của postgres: %q", ipOut)
	}
	for _, host := range targets {
		out, err := compose(t, "exec", "-T", "imager", "timeout", "3", "bash", "-c",
			"exec 3<>/dev/tcp/"+host+"/5432 && echo NOI_DUOC")
		if err == nil || strings.Contains(out, "NOI_DUOC") {
			t.Fatalf("imager nối được %s:5432:\n%s", host, out)
		}
	}
	mustReachS3(t)
}

// mustReachS3 là đối chứng: cùng cách nối tới cổng S3 qua bộ chuyển cổng s3-imaging (cùng mạng imaging) thì được, để
// chắc các phép thử "không nối được" không hỏng vì lý do khác.
func mustReachS3(t *testing.T) {
	t.Helper()
	if out, err := compose(t, "exec", "-T", "imager", "timeout", "3", "bash", "-c",
		"exec 3<>/dev/tcp/s3-imaging/8333 && echo NOI_DUOC"); err != nil || !strings.Contains(out, "NOI_DUOC") {
		t.Fatalf("imager phải nối được s3-imaging:8333: %v\n%s", err, out)
	}
}

// containerIPs: mọi IP của container <dự án>-<dịch vụ>-1 trên mọi mạng của nó.
func containerIPs(t *testing.T, svc string) []string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "docker", "inspect", "-f", //nolint:gosec // G702: tên container từ COMPOSE_PROJECT của make
		"{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", os.Getenv("COMPOSE_PROJECT")+"-"+svc+"-1").Output()
	if err != nil {
		t.Fatalf("docker inspect %s: %v", svc, err)
	}
	ips := strings.Fields(string(out))
	if len(ips) == 0 {
		t.Fatalf("không lấy được IP của %s: %q", svc, out)
	}
	return ips
}

// Review độc lập M1 C-01 (ADR-45, ADR-36): master (9333, 19333), volume (8080, 18080), filer (8888, 18888) và S3 gRPC
// (18333) của SeaweedFS không kiểm danh tính; imager (chỗ giải mã dữ liệu lạ) không được tới cổng nào của container
// seaweedfs, theo tên lẫn theo IP. Imager chỉ tới cổng S3 (có kiểm chữ ký) qua s3-imaging, và s3-imaging không chuyển
// cổng nào khác.
func TestImagerCompose_CannotReachStoreInternals(t *testing.T) {
	ports := []string{"8333", "8888", "18888", "9333", "19333", "8080", "18080", "18333"}
	for _, host := range append([]string{"seaweedfs"}, containerIPs(t, "seaweedfs")...) {
		for _, port := range ports {
			out, err := compose(t, "exec", "-T", "imager", "timeout", "3", "bash", "-c",
				"exec 3<>/dev/tcp/"+host+"/"+port+" && echo NOI_DUOC")
			if err == nil || strings.Contains(out, "NOI_DUOC") {
				t.Errorf("imager nối được %s:%s:\n%s", host, port, out)
			}
		}
	}
	for _, port := range ports[1:] {
		out, err := compose(t, "exec", "-T", "imager", "timeout", "3", "bash", "-c",
			"exec 3<>/dev/tcp/s3-imaging/"+port+" && echo NOI_DUOC")
		if err == nil || strings.Contains(out, "NOI_DUOC") {
			t.Errorf("s3-imaging mở cổng %s cho imager:\n%s", port, out)
		}
	}
	mustReachS3(t)
}

func TestImagerCompose_ReadOnlyAndUnprivileged(t *testing.T) {
	out, err := compose(t, "exec", "-T", "imager", "sh", "-c", "id -u; touch /usr/local/bin/x 2>&1; touch /tmp/x && echo TMP_OK")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "65534\n") || !strings.Contains(out, "Read-only file system") || !strings.Contains(out, "TMP_OK") {
		t.Fatalf("cần chạy bằng 65534, hệ thống tệp chỉ đọc trừ /tmp:\n%s", out)
	}
}

func TestImagerCompose_NoInternet(t *testing.T) {
	for _, target := range []string{"1.1.1.1/443", "8.8.8.8/53", "host.docker.internal/" + envOr("CRM_PG_PORT", "5442")} {
		out, err := compose(t, "exec", "-T", "imager", "timeout", "3", "bash", "-c", "exec 3<>/dev/tcp/"+target+" && echo NOI_DUOC")
		if err == nil || strings.Contains(out, "NOI_DUOC") {
			t.Fatalf("imager ra được %s:\n%s", target, out)
		}
	}
}

// Worker dev chạy trên máy gọi imager qua imager-port (127.0.0.1:CRM_IMAGER_PORT).
func TestImagerCompose_HostReachesImagerThroughForwarder(t *testing.T) {
	url := "http://127.0.0.1:" + envOr("CRM_IMAGER_PORT", "4193") + "/health/live"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", url, resp.StatusCode)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
