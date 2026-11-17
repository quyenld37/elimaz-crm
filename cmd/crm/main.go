// Lệnh crm là tệp chạy duy nhất của CRM. CRM_ROLE chọn vai trò: ingest, api, worker, migrate.
// `crm healthcheck` kiểm tiến trình cùng cấu hình đang sống (dùng trong image không có shell).
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
	"sync"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/telemetry"
)

// Thời hạn tắt êm: tổng dưới 25 s (kế hoạch M0, Task 3).
const (
	httpShutdownTimeout  = 20 * time.Second
	traceShutdownTimeout = 3 * time.Second
	healthcheckTimeout   = 3 * time.Second
	readHeaderTimeout    = 10 * time.Second
)

// Mã thoát.
const (
	exitOK     = 0
	exitFail   = 1
	exitConfig = 2
)

var (
	errNoHealthcheck  = errors.New("vai trò migrate chạy một lần rồi thoát, không có healthcheck")
	errStaleHeartbeat = errors.New("worker không còn ghi nhịp sống")
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], config.FromOS, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	role, err := config.ParseRole(getenv("CRM_ROLE"))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitConfig
	}
	cfg, err := config.Load(role, getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitConfig
	}
	switch {
	case len(args) == 1 && args[0] == "healthcheck":
		if err := healthcheck(ctx, cfg); err != nil {
			_, _ = fmt.Fprintln(stderr, "healthcheck:", err)
			return exitFail
		}
		return exitOK
	case len(args) > 0:
		_, _ = fmt.Fprintf(stderr, "lệnh lạ %q; chỉ có `crm` hoặc `crm healthcheck`\n", args)
		return exitConfig
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer stop()
	logger, closer, err := logging.NewLogger(cfg, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitConfig
	}
	defer func() { _ = closer.Close() }()

	if err := serve(ctx, cfg, logger); err != nil {
		logger.Error("tiến trình dừng vì lỗi", slog.String("error", err.Error()))
		return exitFail
	}
	logger.Info("đã tắt êm")
	return exitOK
}

// serve dựng dấu vết và pool, chạy vai trò tới khi ctx bị hủy, rồi dọn dẹp. Vai trò migrate áp dụng migration
// bằng chuỗi chủ bảng rồi thoát.
func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if cfg.Role == config.RoleMigrate {
		return runMigrate(ctx, cfg, logger)
	}
	shutdownTraces, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return fmt.Errorf("dựng dấu vết: %w", err)
	}
	defer func() {
		tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), traceShutdownTimeout)
		defer cancel()
		if err := shutdownTraces(tctx); err != nil {
			logger.Warn("đẩy nốt dấu vết", slog.String("error", err.Error()))
		}
	}()
	d, err := db.Open(ctx, cfg.DatabaseURL, poolOptions(cfg))
	if err != nil {
		return fmt.Errorf("mở database: %w", err)
	}
	defer d.Pool.Close()

	logger.Info("khởi động", slog.Any("config", cfg))
	reg := metrics.NewRegistry()
	d.OnRetry = func(code string, _ int) { reg.Inc("tx_retry", code, 1) }
	d.OnExhausted = func(code string) { reg.Inc("tx_retry_exhausted", code, 1) }
	flusher := &metrics.Flusher{DB: d, Reg: reg, Process: metrics.ProcessName(cfg.Role), Role: cfg.Role, Log: logger}
	if cfg.Role == config.RoleWorker {
		// Chỉ worker gửi nhịp sống ra ngoài (ADR-38): api còn sống không được che việc worker đã chết.
		flusher.Pinger = &metrics.Pinger{URL: cfg.HeartbeatURL}
		flusher.Run(ctx) // Task 11 thêm hàng đợi chạy song song
		return nil
	}
	var wg sync.WaitGroup
	defer wg.Wait() // lần ghi chỉ số cuối (tối đa 5 s, song song với lúc tắt HTTP) xong rồi mới đóng pool
	// fctx dừng vòng ghi cả khi serveHTTP trả lỗi trước tín hiệu tắt (defer chạy ngược: cancel trước Wait).
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg.Go(func() { flusher.Run(fctx) })
	return serveHTTP(ctx, cfg, d, reg, logger)
}

// poolOptions: ngân sách kết nối theo vai trò (cfg.DBMaxConns, ADR-42), giới hạn theo transaction của vai trò
// (ADR-46), dấu vết SQL khi OTEL bật. Ba giới hạn của kết nối giữ mặc định ADR-26.
func poolOptions(cfg config.Config) db.PoolOptions {
	o := db.PoolOptions{MaxConns: cfg.DBMaxConns, Tracing: cfg.OTelEnabled}
	switch cfg.Role {
	case config.RoleAPI:
		o.DefaultTx = db.TxAPI
	case config.RoleIngest:
		o.DefaultTx = db.TxIngest
	case config.RoleWorker:
		o.DefaultTx = db.TxWorker
	case config.RoleMigrate:
	}
	return o
}

// runMigrate áp dụng migration nhúng trong tệp chạy (db.Migrate) và log tên các tệp vừa áp dụng.
func runMigrate(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	applied, err := db.Migrate(ctx, cfg.Migrate.OwnerURL)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Info("migrate xong", slog.Any("applied", applied), slog.Int("count", len(applied)))
	return nil
}

// serveHTTP phục vụ /health/* (đo http_ms theo route) tới khi ctx bị hủy, rồi tắt êm trong httpShutdownTimeout.
func serveHTTP(ctx context.Context, cfg config.Config, d *db.DB, reg *metrics.Registry, logger *slog.Logger) error {
	mux := http.NewServeMux()
	observability.NewHealth(d.Pool).Register(mux)
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("mở cổng %d: %w", cfg.Port, err)
	}
	srv := &http.Server{Handler: metrics.Middleware(reg, mux), ReadHeaderTimeout: readHeaderTimeout}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	logger.Info("đang nghe", slog.Int("port", cfg.Port))

	select {
	case err := <-errCh:
		return fmt.Errorf("máy chủ HTTP dừng: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), httpShutdownTimeout)
	defer cancel()
	shutdownErr := srv.Shutdown(sctx)
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("máy chủ HTTP dừng: %w", err)
	}
	if shutdownErr != nil {
		return fmt.Errorf("tắt máy chủ HTTP: %w", shutdownErr)
	}
	return nil
}

// healthcheck: ingest và api gọi /health/live của chính nó; worker đòi nhịp sống của chính nó
// (platform.process_heartbeats, cùng tên máy) mới hơn metrics.HeartbeatMaxAge (90 s).
func healthcheck(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	switch cfg.Role {
	case config.RoleIngest, config.RoleAPI:
		return checkLive(ctx, cfg.Port)
	case config.RoleWorker:
		d, err := db.Open(ctx, cfg.DatabaseURL, db.PoolOptions{MaxConns: 1})
		if err != nil {
			return fmt.Errorf("mở database: %w", err)
		}
		defer d.Pool.Close()
		age, err := metrics.HeartbeatAge(ctx, d, metrics.ProcessName(cfg.Role))
		if err != nil {
			return fmt.Errorf("hỏi nhịp sống: %w", err)
		}
		if age > metrics.HeartbeatMaxAge {
			return fmt.Errorf("%w: nhịp sống cũ %v, quá %v", errStaleHeartbeat, age.Round(time.Second), metrics.HeartbeatMaxAge)
		}
		return nil
	case config.RoleMigrate:
	}
	return errNoHealthcheck
}

func checkLive(ctx context.Context, port int) error {
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/health/live"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("tạo yêu cầu: %w", err)
	}
	resp, err := (&http.Client{Timeout: healthcheckTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("gọi %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s trả %d", url, resp.StatusCode)
	}
	return nil
}
