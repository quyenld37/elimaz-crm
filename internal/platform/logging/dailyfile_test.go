package logging_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/logging"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *clock) Set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

func TestDailyFileRollsAtMidnight(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 11, 18, 23, 59, 0, 0, time.UTC)}
	f := logging.NewDailyFile(dir, "api")
	f.Now = c.Now
	t.Cleanup(func() { _ = f.Close() })

	if _, err := f.Write([]byte("một\n")); err != nil {
		t.Fatal(err)
	}
	c.Set(time.Date(2026, 11, 19, 0, 1, 0, 0, time.UTC))
	if _, err := f.Write([]byte("hai\n")); err != nil {
		t.Fatal(err)
	}
	got := names(t, dir)
	want := []string{"api-2026-11-18.log", "api-2026-11-19.log"}
	if !slices.Equal(got, want) {
		t.Fatalf("tệp = %v, muốn %v", got, want)
	}
	b, _ := os.ReadFile(filepath.Join(dir, want[1]))
	if string(b) != "hai\n" {
		t.Fatalf("tệp ngày 19 = %q", b)
	}
}

func TestDailyFileKeeps30(t *testing.T) {
	dir := t.TempDir()
	// Tệp của vai trò khác không bị đụng tới.
	if err := os.WriteFile(filepath.Join(dir, "worker-2026-01-01.log"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &clock{}
	f := logging.NewDailyFile(dir, "api")
	f.Now = c.Now
	t.Cleanup(func() { _ = f.Close() })
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := range 31 {
		c.Set(start.AddDate(0, 0, i))
		if _, err := f.Write([]byte("x\n")); err != nil {
			t.Fatal(err)
		}
	}
	got := names(t, dir)
	var api []string
	for _, n := range got {
		if strings.HasPrefix(n, "api-") {
			api = append(api, n)
		}
	}
	if len(api) != 30 {
		t.Fatalf("còn %d tệp api, muốn 30: %v", len(api), api)
	}
	if slices.Contains(api, "api-2026-10-01.log") {
		t.Fatal("tệp cũ nhất phải bị xóa")
	}
	if !slices.Contains(got, "worker-2026-01-01.log") {
		t.Fatal("tệp của vai trò khác bị xóa")
	}
}

func TestDailyFileDropsOverMaxBytes(t *testing.T) {
	dir := t.TempDir()
	f := logging.NewDailyFile(dir, "ingest")
	f.MaxBytes = 10
	f.Now = func() time.Time { return time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { _ = f.Close() })
	for _, line := range []string{"12345\n", "67890\n", "abcde\n"} {
		n, err := f.Write([]byte(line))
		if err != nil || n != len(line) {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ingest-2026-11-18.log"))
	if string(b) != "12345\n" {
		t.Fatalf("nội dung = %q", b)
	}
	if f.Dropped() != 2 {
		t.Fatalf("Dropped = %d, muốn 2", f.Dropped())
	}
}

func TestDailyFileConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	f := logging.NewDailyFile(dir, "worker")
	t.Cleanup(func() { _ = f.Close() })
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 20 {
				_, _ = f.Write([]byte("dòng\n"))
			}
		})
	}
	wg.Wait()
	got := names(t, dir)
	if len(got) != 1 {
		t.Fatalf("tệp = %v", got)
	}
	b, _ := os.ReadFile(filepath.Join(dir, got[0]))
	if strings.Count(string(b), "dòng\n") != 1000 {
		t.Fatalf("số dòng = %d", strings.Count(string(b), "dòng\n"))
	}
}

func TestNewLoggerWritesDailyFileOnlyWithLogDir(t *testing.T) {
	t.Run("không LOG_DIR", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		var out strings.Builder
		l, closer, err := logging.NewLogger("", string(config.RoleAPI), &out)
		if err != nil {
			t.Fatal(err)
		}
		l.Info("xin chào")
		_ = closer.Close()
		if got := names(t, dir); len(got) != 0 {
			t.Fatalf("không LOG_DIR mà vẫn tạo tệp: %v", got)
		}
		if !strings.Contains(out.String(), "xin chào") {
			t.Fatal("stdout phải có dòng log")
		}
	})
	t.Run("có LOG_DIR", func(t *testing.T) {
		dir := t.TempDir()
		var out strings.Builder
		l, closer, err := logging.NewLogger(dir, string(config.RoleWorker), &out)
		if err != nil {
			t.Fatal(err)
		}
		l.Info("vào tệp", slog.String("phone", "0911"))
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
		got := names(t, dir)
		if len(got) != 1 || !strings.HasPrefix(got[0], "worker-") {
			t.Fatalf("tệp = %v", got)
		}
		b, _ := os.ReadFile(filepath.Join(dir, got[0]))
		if !strings.Contains(string(b), "vào tệp") || strings.Contains(string(b), "0911") {
			t.Fatalf("nội dung tệp = %s", b)
		}
	})
}
