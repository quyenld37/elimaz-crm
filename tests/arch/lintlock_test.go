package arch

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// holdLintLock giữ khóa golangci-lint.lock của thư mục dir (cùng kiểu khóa flock mà golangci-lint dùng) tới khi gọi
// hàm trả về. Khóa đang bị lần lint khác giữ (cùng worktree) thì chờ nó xong.
func holdLintLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "golangci-lint.lock")
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			_ = f.Close()
			t.Fatalf("giữ khóa %s: %v", path, err)
		}
		// golangci-lint xóa tệp khóa sau khi nhả: tệp vừa khóa được có thể đã bị xóa và lần lint sau tạo tệp mới, khi
		// đó khóa của tệp cũ không chặn ai, mở lại.
		held, errHeld := f.Stat()
		cur, errCur := os.Stat(path)
		if errHeld == nil && errCur == nil && os.SameFile(held, cur) {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}
		}
		_ = f.Close()
	}
}

// Tiến trình golangci-lint khác trên máy (hook Stop, ci-task, phiên khác) giữ khóa trong $TMPDIR chung: lint của test
// vẫn chạy, không đỏ "parallel golangci-lint is running". $TMPDIR chung được giả bằng thư mục riêng của test để không
// giữ khóa thật của các phiên khác.
func TestLintNotBlockedByOtherRunners(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("TMPDIR", shared)
	release := holdLintLock(t, shared)
	defer release()
	dir := strayTree(t)
	if issues, stderr := golangciLint(t, dir, "./clean/"); len(issues) != 0 {
		t.Fatalf("gói sạch có phát hiện: %+v\n%s", issues, stderr)
	}
}

// Hai lần lint của test trong cùng worktree (hook Stop và ci-task chạy tests/arch cùng lúc) dùng chung khóa riêng của
// worktree: lần đến sau chờ lần trước xong (quá 5 giây chờ mặc định của golangci-lint), không bỏ.
func TestLintWaitsForSameWorktreeRunner(t *testing.T) {
	dir := strayTree(t)
	release := holdLintLock(t, lintTempDir(t))
	timer := time.AfterFunc(6*time.Second, release)
	defer func() {
		if timer.Stop() {
			release()
		}
	}()
	start := time.Now()
	if issues, stderr := golangciLint(t, dir, "./clean/"); len(issues) != 0 {
		t.Fatalf("gói sạch có phát hiện: %+v\n%s", issues, stderr)
	}
	if waited := time.Since(start); waited < 6*time.Second {
		t.Fatalf("lint xong sau %v khi khóa còn bị giữ: không dùng khóa riêng của worktree", waited)
	}
}
