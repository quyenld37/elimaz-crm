// Lệnh imager là tiến trình giải mã ảnh của CRM (ADR-45), chạy trong image riêng có libvips.
// Không có biến bí mật, không nối database; chỉ nghe trên mạng nội bộ `imaging`.
// `imager healthcheck` gọi /health/live của tiến trình đang chạy (HEALTHCHECK của image, không có curl).
//
// Biến môi trường: PORT (4193), IMAGER_MAX_PARALLEL (2), IMAGER_STEP_TIMEOUT (10s), IMAGER_STEP_MEMORY
// (1GiB; nhận số byte hoặc hậu tố KiB/MiB/GiB), VIPS_BIN (vips), VIPSHEADER_BIN (vipsheader), TMPDIR.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/platform/observability"
)

const (
	readHeaderTimeout   = 10 * time.Second
	shutdownTimeout     = 20 * time.Second
	healthcheckTimeout  = 3 * time.Second
	exitOK, exitFail    = 0, 1
	exitConfig          = 2
	defaultStepMemory   = 1 << 30
	defaultStepTimeout  = 10 * time.Second
	defaultMaxParallel  = 2
	maxParallelUpper    = 64
	minStepMemory       = 64 << 20
	maxStepTimeoutUpper = 5 * time.Minute
)

type config struct {
	port        int
	maxParallel int
	lim         imager.Limits
}

var errBadEnv = errors.New("biến môi trường sai")

func main() {
	// imager không được nhập internal/platform/config (luật g của tests/arch: không cấu hình bí mật, không database),
	// nên đọc biến môi trường tại đây.
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stderr)) //nolint:forbidigo // xem chú thích trên
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	cfg, err := load(getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitConfig
	}
	if len(args) == 1 && args[0] == "healthcheck" {
		if err := healthcheck(ctx, cfg.port); err != nil {
			_, _ = fmt.Fprintln(stderr, "healthcheck:", err)
			return exitFail
		}
		return exitOK
	}
	if len(args) != 0 {
		_, _ = fmt.Fprintln(stderr, "dùng: imager [healthcheck]")
		return exitConfig
	}
	log := slog.New(slog.NewJSONHandler(stderr, nil))
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := serve(ctx, cfg, log); err != nil {
		log.LogAttrs(ctx, slog.LevelError, "imager dừng vì lỗi", slog.String("error", err.Error()))
		return exitFail
	}
	return exitOK
}

func serve(ctx context.Context, cfg config, log *slog.Logger) error {
	runner := imager.NewExecRunner([]string{"TMPDIR=" + cfg.lim.TmpDir, "VIPS_CONCURRENCY=" + strconv.Itoa(cfg.maxParallel)})
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.port),
		Handler:           imager.NewServer(runner, imager.NewHTTPClient(cfg.lim.StepTimeout), cfg.lim, cfg.maxParallel, log).Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.LogAttrs(ctx, slog.LevelInfo, "imager nghe", slog.Int("port", cfg.port),
		slog.Int("max_parallel", cfg.maxParallel), slog.Duration("step_timeout", cfg.lim.StepTimeout),
		slog.Int64("step_memory", cfg.lim.StepMemory))
	select {
	case err := <-errc:
		return fmt.Errorf("nghe HTTP: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("tắt HTTP: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("nghe HTTP: %w", err)
	}
	return nil
}

func load(getenv func(string) string) (config, error) {
	var errs []error
	intVar := func(key string, def, lo, hi int) int {
		s := strings.TrimSpace(getenv(key))
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < lo || n > hi {
			errs = append(errs, fmt.Errorf("%w: %s phải là số nguyên trong [%d, %d]", errBadEnv, key, lo, hi))
		}
		return n
	}
	cfg := config{
		port:        intVar("PORT", 4193, 1, 65535),
		maxParallel: intVar("IMAGER_MAX_PARALLEL", defaultMaxParallel, 1, maxParallelUpper),
		lim: imager.Limits{
			StepTimeout:   defaultStepTimeout,
			StepMemory:    defaultStepMemory,
			TmpDir:        os.TempDir(),
			VipsBin:       orDefault(getenv("VIPS_BIN"), "vips"),
			VipsHeaderBin: orDefault(getenv("VIPSHEADER_BIN"), "vipsheader"),
		},
	}
	if s := strings.TrimSpace(getenv("IMAGER_STEP_TIMEOUT")); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 || d > maxStepTimeoutUpper {
			errs = append(errs, fmt.Errorf("%w: IMAGER_STEP_TIMEOUT phải là thời lượng trong (0, %v]", errBadEnv, maxStepTimeoutUpper))
		}
		cfg.lim.StepTimeout = d
	}
	if s := strings.TrimSpace(getenv("IMAGER_STEP_MEMORY")); s != "" {
		n, err := parseBytes(s)
		if err != nil || n < minStepMemory {
			errs = append(errs, fmt.Errorf("%w: IMAGER_STEP_MEMORY phải ≥ 64MiB (số byte hoặc KiB/MiB/GiB)", errBadEnv))
		}
		cfg.lim.StepMemory = n
	}
	if t := strings.TrimSpace(getenv("TMPDIR")); t != "" {
		cfg.lim.TmpDir = t
	}
	return cfg, errors.Join(errs...)
}

func orDefault(s, def string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

// parseBytes: "1073741824", "256MiB", "1GiB", "512KiB".
func parseBytes(s string) (int64, error) {
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}} {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			s, mult = num, u.mult
			break
		}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("đọc số byte: %w", err)
	}
	if n <= 0 || n > (1<<62)/mult {
		return 0, fmt.Errorf("%w: số byte ngoài khoảng", errBadEnv)
	}
	return n * mult, nil
}

func healthcheck(ctx context.Context, port int) error {
	if err := observability.CheckLive(ctx, port, healthcheckTimeout); err != nil {
		return fmt.Errorf("kiểm /health/live: %w", err)
	}
	return nil
}
