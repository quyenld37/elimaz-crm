// Package e2e chứa test chạy tệp chạy thật của CRM với PostgreSQL thật (ADR-44 chốt 2).
package e2e

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

const secret32 = "0123456789abcdef0123456789abcdef"

// buildBinary dựng cmd/crm vào thư mục tạm của test.
func buildBinary(t *testing.T) string {
	t.Helper()
	return buildPackage(t, "./cmd/crm")
}

// builds giữ tệp chạy đã dựng của tiến trình test (Task 484): mỗi gói main dựng một lần (trước đó mỗi test liên kết lại
// cmd/crm, 10–29 s một lần), test dùng chung tệp chạy chỉ đọc. dir do TestMain tạo và xóa.
var builds = struct { //nolint:gochecknoglobals // bộ nhớ đệm tệp chạy của tiến trình test, có khóa
	mu  sync.Mutex
	dir string
	pkg map[string]*builtPackage
}{pkg: map[string]*builtPackage{}}

type builtPackage struct {
	once sync.Once
	bin  string
	err  error
}

// buildPackage dựng gói main pkg (đường dẫn so với gốc module) một lần cho cả tiến trình test; trả đường dẫn tệp chạy.
// Lần dựng lỗi thì mọi test cần gói đó đều dừng với cùng lỗi.
func buildPackage(t *testing.T, pkg string) string {
	t.Helper()
	builds.mu.Lock()
	b, ok := builds.pkg[pkg]
	if !ok {
		b = &builtPackage{}
		builds.pkg[pkg] = b
	}
	dir := builds.dir
	builds.mu.Unlock()
	b.once.Do(func() { b.bin, b.err = goBuild(pkg, dir) })
	if b.err != nil {
		t.Fatal(b.err)
	}
	return b.bin
}

// goBuild chạy go build cho pkg ở gốc module, ghi tệp chạy vào thư mục con riêng của dir (tên tệp là tên gói).
func goBuild(pkg, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	root := filepath.Dir(strings.TrimSpace(string(out)))
	sub, err := os.MkdirTemp(dir, "bin-")
	if err != nil {
		return "", fmt.Errorf("thư mục tệp chạy: %w", err)
	}
	bin := filepath.Join(sub, filepath.Base(pkg))
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", bin, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build %s: %w\n%s", pkg, err, b)
	}
	return bin, nil
}

func databaseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("CRM_TEST_DATABASE_URL")
	if u == "" {
		t.Fatal("thiếu CRM_TEST_DATABASE_URL: chạy `make db-up` (hoặc đặt CRM_PG_PORT) rồi chạy test qua make hoặc đặt biến này")
	}
	return u
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// lineBuffer gom stdout của tiến trình con, an toàn khi đọc song song.
type lineBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lineBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, _ := b.buf.Write(p) // strings.Builder.Write không bao giờ lỗi
	return n, nil
}

func (b *lineBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func roleEnv(role, dbURL string, port int) []string {
	env := []string{
		"CRM_ROLE=" + role, "CRM_ENV=test", "DATABASE_URL=" + dbURL, "OTEL_ENABLED=false",
		"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
	}
	switch role {
	case "ingest":
		env = append(env, "PORT="+strconv.Itoa(port), "SPOOL_DIR="+os.TempDir(), "SELFTEST_WEBHOOK_SECRET="+secret32)
	case "api":
		env = append(env, "PORT="+strconv.Itoa(port), "ACCESS_TEAM_DOMAIN=test.cloudflareaccess.com", "ACCESS_AUD=aud",
			"ACCESS_MFA_CLAIM=amr", "ACCESS_MFA_VALUES=mfa", "CONSOLE_ORIGINS=http://127.0.0.1:4192", "HEALTH_TOKEN="+secret32)
	}
	if role == "api" || role == "worker" {
		// Kho tệp bắt buộc từ Task M1-41; dựng S3Store không gọi mạng nên địa chỉ không cần sống.
		env = append(env, "S3_ENDPOINT=http://127.0.0.1:1", "S3_ACCESS_KEY_ID=crm-app-dev",
			"S3_SECRET_ACCESS_KEY=crm-app-dev-only-not-a-secret", "FILES_PUBLIC_BASE_URL=http://127.0.0.1:1")
	}
	return env
}

func waitReady(ctx context.Context, url string) error {
	client := &http.Client{Timeout: time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("tạo yêu cầu: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("chờ %s: %w (lỗi cuối: %w)", url, ctx.Err(), err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// waitHealthcheck chạy `crm healthcheck` với env tới khi thoát 0.
func waitHealthcheck(ctx context.Context, bin string, env []string) error {
	for {
		cmd := exec.CommandContext(ctx, bin, "healthcheck")
		cmd.Env = env
		var stderr strings.Builder
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("chờ healthcheck: %w (lần cuối: %w %s)", ctx.Err(), err, stderr.String())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func TestBootEachRoleThenGracefulStop(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	_, _, dbURL := testkit.StartPostgres(t) // đã migrate: cả ba vai trò ghi chỉ số và nhịp sống
	for _, role := range []string{"ingest", "api", "worker"} {
		t.Run(role, func(t *testing.T) { bootAndStop(t, bin, dbURL, role) })
	}
}

// bootAndStop chạy một vai trò, chờ sẵn sàng trong 10 s (worker: `crm healthcheck` thấy nhịp sống), gửi SIGTERM,
// đòi thoát mã 0 trong 25 s.
func bootAndStop(t *testing.T, bin, dbURL, role string) {
	t.Helper()
	port := freePort(t)
	// Thư mục tệp tạm riêng (Task 484): mỗi tiến trình ingest một thư mục (webhooks.Spool), test chạy song song.
	env := append(roleEnv(role, dbURL, port), "SPOOL_DIR="+t.TempDir()) // biến sau cùng thắng
	cmd, done, stdout, stderr := startRole(t, bin, env)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var err error
	if role == "worker" {
		err = waitHealthcheck(ctx, bin, roleEnv(role, dbURL, port))
	} else {
		err = waitReady(ctx, fmt.Sprintf("http://127.0.0.1:%d/health/ready", port))
	}
	if err != nil {
		t.Fatalf("%s chưa sẵn sàng trong 10 s: %v\nstdout:\n%s\nstderr:\n%s", role, err, stdout.String(), stderr.String())
	}

	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s thoát lỗi sau SIGTERM: %v\nstdout:\n%s\nstderr:\n%s", role, err, stdout.String(), stderr.String())
		}
		t.Logf("%s tắt êm sau %v", role, time.Since(start))
	case <-time.After(25 * time.Second):
		t.Fatalf("%s không thoát trong 25 s sau SIGTERM", role)
	}
	assertJSONLines(t, stdout.String())
}

// startRole chạy tệp chạy với env; done nhận kết quả Wait; tiến trình bị giết khi test xong.
func startRole(t *testing.T, bin string, env []string) (cmd *exec.Cmd, done <-chan error, stdout, stderr *lineBuffer) {
	t.Helper()
	stdout, stderr = &lineBuffer{}, &lineBuffer{}
	cmd = exec.CommandContext(t.Context(), bin)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, ch, stdout, stderr
}

func assertJSONLines(t *testing.T, out string) {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(out))
	n := 0
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, `"msg"`) {
			t.Errorf("stdout có dòng không phải log JSON: %q", line)
		}
		n++
	}
	if n == 0 {
		t.Error("stdout không có dòng log nào")
	}
}

func TestBootMissingVariableFails(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	env := roleEnv("api", databaseURL(t), freePort(t))
	var kept []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "ACCESS_AUD=") {
			kept = append(kept, kv)
		}
	}
	stderr, err := runForStderr(t, bin, kept)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("thiếu ACCESS_AUD phải thoát mã khác 0, nhận %v", err)
	}
	if !strings.Contains(stderr, "ACCESS_AUD") {
		t.Fatalf("stderr phải nêu ACCESS_AUD: %s", stderr)
	}
}

// runForStderr chạy bin với env và tham số args, tối đa 10 s; trả stderr và lỗi của lần chạy.
func runForStderr(t *testing.T, bin string, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func TestBootUnknownRoleFails(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	stderr, err := runForStderr(t, bin, []string{"CRM_ROLE=web"})
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 || !strings.Contains(stderr, "CRM_ROLE") {
		t.Fatalf("CRM_ROLE lạ phải thoát khác 0 và nêu CRM_ROLE: %v %s", err, stderr)
	}
}

// crm healthcheck (dùng trong image không có shell): 0 khi tiến trình cùng cấu hình đang sống, khác 0 khi không.
func TestHealthcheckCommand(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	// Database đã migrate: từ Task M1-41 api kiểm sổ mục đích tệp với platform.file_purposes lúc khởi động.
	_, _, dbURL := testkit.StartPostgres(t)
	port := freePort(t)
	env := roleEnv("api", dbURL, port)

	check := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "healthcheck")
		cmd.Env = env
		return cmd.Run()
	}
	if err := check(); err == nil {
		t.Fatal("chưa có api chạy mà healthcheck vẫn 0")
	}

	srv := exec.CommandContext(t.Context(), bin)
	srv.Env = env
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Process.Signal(syscall.SIGTERM); _ = srv.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := waitReady(ctx, fmt.Sprintf("http://127.0.0.1:%d/health/live", port)); err != nil {
		t.Fatal(err)
	}
	if err := check(); err != nil {
		t.Fatalf("api đang chạy mà healthcheck lỗi: %v", err)
	}

	// worker: healthcheck kiểm tuổi platform.process_heartbeats của chính tiến trình dưới 90 s.
	_, _, migrated := testkit.StartPostgres(t)
	wenv := roleEnv("worker", migrated, 0)
	wctx, wcancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer wcancel()
	idle := exec.CommandContext(wctx, bin, "healthcheck")
	idle.Env = wenv
	if err := idle.Run(); err == nil {
		t.Fatal("chưa có worker chạy (chưa có nhịp sống) mà healthcheck worker vẫn 0")
	}
	worker := exec.CommandContext(t.Context(), bin)
	worker.Env = wenv
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Process.Signal(syscall.SIGTERM); _ = worker.Wait() })
	if err := waitHealthcheck(wctx, bin, wenv); err != nil {
		t.Fatalf("worker đang chạy mà healthcheck không về 0: %v", err)
	}
	bad := exec.CommandContext(wctx, bin, "healthcheck")
	bad.Env = roleEnv("worker", "postgres://crm_app:x@127.0.0.1:1/crm?sslmode=disable&connect_timeout=1", 0)
	if err := bad.Run(); err == nil {
		t.Fatal("healthcheck worker với database chết phải khác 0")
	}
}

// Máy chủ HTTP lỗi sớm (cổng đã bị chiếm) → tiến trình thoát khác 0 ngay, không treo vì vòng ghi chỉ số còn chạy.
func TestBootPortTakenExitsWithError(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	_, _, dbURL := testkit.StartPostgres(t)
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = roleEnv("api", dbURL, port)
	var stdout lineBuffer
	cmd.Stdout = &stdout
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("api treo khi cổng %d đã bị chiếm (không thoát trong 10 s)\nstdout:\n%s", port, stdout.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("cổng bị chiếm phải thoát khác 0, nhận %v", err)
	}
	if !strings.Contains(stdout.String(), "tiến trình dừng vì lỗi") {
		t.Fatalf("phải log lỗi dừng:\n%s", stdout.String())
	}
}

// Task M1-41: api và worker kiểm sổ mục đích tệp trong Go khớp platform.file_purposes lúc khởi động; lệch (dòng
// thêm bằng migration mà quên RegisterPurpose) thì thoát lỗi nêu khóa, không phục vụ.
func TestBootPurposeMismatchFails(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	_, ownerURL, dbURL := testkit.StartPostgres(t)
	owner, err := pgx.Connect(t.Context(), ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.WithoutCancel(t.Context())) }()
	if _, err := owner.Exec(t.Context(), "INSERT INTO platform.file_purposes (key, bucket) VALUES ('test_quen_dang_ky', 'private')"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "worker"} {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		cmd := exec.CommandContext(ctx, bin)
		cmd.Env = roleEnv(role, dbURL, freePort(t))
		var stdout strings.Builder
		cmd.Stdout = &stdout
		err := cmd.Run()
		cancel()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
			t.Fatalf("%s với mục đích lệch phải thoát mã khác 0, nhận %v", role, err)
		}
		if !strings.Contains(stdout.String(), "test_quen_dang_ky") {
			t.Fatalf("%s: log phải nêu khóa lệch:\n%s", role, stdout.String())
		}
	}
}
