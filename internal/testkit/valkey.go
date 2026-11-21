package testkit

import (
	"context"
	"fmt"
	"net"
	oexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Image và mật khẩu dev của Valkey: cùng giá trị với docker/compose.dev.yml (kế hoạch M5 Task 184; chỉ máy dev).
const (
	valkeyImage = "valkey/valkey:9.1@sha256:3ab0eca823ebbf0fe352476ebac330ac9e5f14b592b9a954eab296d341e81b0e"
	// Mật khẩu dev công khai trong compose, không dùng ở máy chủ.
	valkeyCachePass = "crm-cache-dev-0123456789abcdef" //nolint:gosec // mật khẩu dev công khai
	valkeyGatePass  = "crm-gate-dev-0123456789abcdef"  //nolint:gosec // mật khẩu dev công khai
	valkeyAdminPass = "crm-admin-dev-0123456789abcdef" //nolint:gosec // mật khẩu dev công khai
	valkeyReadyWait = 30 * time.Second
)

// Valkey là một tiến trình Valkey riêng của một test (container docker riêng, cổng ngẫu nhiên trên 127.0.0.1), dựng
// bằng docker/valkey/start.sh với gate.conf và danh sách quyền từ users.acl.tmpl, như compose dev. Dừng, tạm dừng
// được mà không ảnh hưởng test khác. Khi nào không dùng: cần Valkey của compose dev (thử bằng tay, đo tải).
type Valkey struct {
	id   string
	addr string
}

// StartValkey dựng container Valkey 9.1 (gate.conf) và chờ sẵn sàng; container bị xóa khi test xong. Không có docker
// hay container không lên thì test đỏ (không bỏ qua).
func StartValkey(t testing.TB) *Valkey {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(self), "..", "..", "docker", "valkey")
	ctx, cancel := context.WithTimeout(context.Background(), valkeyReadyWait)
	defer cancel()
	out, err := oexec.CommandContext(ctx, "docker", "run", "-d", "--label", "crm-testkit=valkey", //nolint:gosec // G204: lệnh cố định, đường dẫn của repo
		"-e", "VALKEY_CACHE_PASSWORD="+valkeyCachePass, "-e", "VALKEY_GATE_PASSWORD="+valkeyGatePass,
		"-e", "VALKEY_ADMIN_PASSWORD="+valkeyAdminPass, "--tmpfs", "/etc/valkey",
		"-v", src+":/etc/valkey-src:ro", "-p", "127.0.0.1::6379",
		"--entrypoint", "sh", valkeyImage, "/etc/valkey-src/start.sh", "gate.conf").CombinedOutput()
	if err != nil {
		t.Fatalf("testkit: docker run valkey: %v: %s", err, out)
	}
	v := &Valkey{id: strings.TrimSpace(string(out))}
	t.Cleanup(func() {
		_ = oexec.Command("docker", "rm", "-f", "-v", v.id).Run() //nolint:gosec,noctx // mã container do docker trả; dọn sau khi test đã xong
	})
	port, err := oexec.CommandContext(ctx, "docker", "port", v.id, "6379/tcp").Output() //nolint:gosec // mã container do docker trả
	if err != nil {
		t.Fatalf("testkit: docker port valkey: %v", err)
	}
	_, p, err := net.SplitHostPort(strings.TrimSpace(strings.Split(string(port), "\n")[0]))
	if err != nil {
		t.Fatalf("testkit: cổng valkey %q: %v", port, err)
	}
	v.addr = net.JoinHostPort("127.0.0.1", p)
	// Sẵn sàng khi máy chủ trả lời qua đúng cổng mở ra máy chạy test (colima chuyển cổng trễ hơn tiến trình trong
	// container): PING chưa xác thực được trả -NOAUTH.
	for {
		if v.answers(ctx) {
			return v
		}
		select {
		case <-ctx.Done():
			logs, _ := oexec.Command("docker", "logs", v.id).CombinedOutput() //nolint:gosec,noctx // mã container do docker trả
			t.Fatalf("testkit: valkey không sẵn sàng sau %s: %s", valkeyReadyWait, logs)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Addr là host:port của Valkey trên máy chạy test.
func (v *Valkey) Addr() string { return v.addr }

// URL là chuỗi kết nối valkey://user:pass@host:port/0 của một người dùng ACL: crm_cache, crm_gate hoặc crm_admin.
func (v *Valkey) URL(user string) string {
	pass := map[string]string{"crm_cache": valkeyCachePass, "crm_gate": valkeyGatePass, "crm_admin": valkeyAdminPass}[user]
	return fmt.Sprintf("valkey://%s:%s@%s/0", user, pass, v.addr)
}

// Stop dừng container (Valkey ngừng, cổng đóng); container vẫn bị xóa khi test xong.
func (v *Valkey) Stop(t testing.TB) {
	t.Helper()
	if out, err := oexec.CommandContext(t.Context(), "docker", "stop", "-t", "1", v.id).CombinedOutput(); err != nil { //nolint:gosec // mã container do docker trả
		t.Fatalf("testkit: docker stop valkey: %v: %s", err, out)
	}
}

// answers: gửi PING (chưa xác thực) tới v.addr, máy chủ trả một dòng lỗi RESP (-NOAUTH) là đã nghe.
func (v *Valkey) answers(ctx context.Context) bool {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", v.addr)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		return false
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	return err == nil && n > 0 && (buf[0] == '-' || buf[0] == '+')
}
