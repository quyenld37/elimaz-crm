package imager_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

func TestExecRunner_ReturnsStdout(t *testing.T) {
	out, err := execRunner().Run(t.Context(), "/bin/echo", []string{"xin chào"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "xin chào\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestExecRunner_NonZeroExitIsError(t *testing.T) {
	if _, err := execRunner().Run(t.Context(), "/bin/sh", []string{"-c", "echo lỗi >&2; exit 3"}, 0); err == nil {
		t.Fatal("thoát mã 3 phải là lỗi")
	}
}

func TestExecRunner_MinimalEnvironment(t *testing.T) {
	t.Setenv("IMAGER_TEST_SECRET", "khong-duoc-lot")
	out, err := imager.NewExecRunner([]string{"EXTRA=1"}).Run(t.Context(), "/usr/bin/env", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	env := string(out)
	if strings.Contains(env, "IMAGER_TEST_SECRET") {
		t.Fatalf("tiến trình con thừa hưởng biến của imager:\n%s", env)
	}
	for _, want := range []string{"PATH=", "EXTRA=1", "VIPS_BLOCK_UNTRUSTED=1", "MALLOC_ARENA_MAX="} {
		if !strings.Contains(env, want) {
			t.Fatalf("thiếu %s trong môi trường tiến trình con:\n%s", want, env)
		}
	}
}

func TestExecRunner_TimeoutKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	r := imager.NewExecRunner([]string{"FAKEVIPS_PIDFILE=" + pidfile})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, fakeVips(t, "hang.sh"), nil, 0)
	if err == nil {
		t.Fatal("lệnh treo phải trả lỗi")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lỗi %v, cần bọc context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("Run trả về sau %v", d)
	}
	assertGroupGone(t, pidfile)
}

func TestExecRunner_MemoryLimit(t *testing.T) {
	requireTools(t, "prlimit")
	if runtime.GOOS != "linux" {
		t.Skip("prlimit chỉ có trên Linux")
	}
	_, err := execRunner().Run(t.Context(), fakeVips(t, "oom.sh"), nil, 256*mib)
	if err == nil {
		t.Fatal("xin 2 GiB dưới giới hạn 256 MiB phải lỗi")
	}
	if !strings.Contains(err.Error(), "memory exhausted") {
		t.Fatalf("lỗi %v, cần do hết bộ nhớ", err)
	}
	// Cùng giới hạn, lệnh nhỏ vẫn chạy: giới hạn không chặn mọi thứ.
	if _, err := execRunner().Run(t.Context(), "/bin/echo", []string{"ok"}, 256*mib); err != nil {
		t.Fatal(err)
	}
}

func TestExecRunner_StdoutBounded(t *testing.T) {
	_, err := execRunner().Run(t.Context(), "/bin/sh", []string{"-c", "head -c 1048576 /dev/zero"}, 0)
	if err == nil {
		t.Fatal("stdout quá 64 KiB phải lỗi")
	}
}
