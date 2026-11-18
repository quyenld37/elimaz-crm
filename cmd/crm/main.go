// Lệnh crm là tệp chạy duy nhất của CRM. CRM_ROLE chọn vai trò: ingest, api, worker, migrate.
// `crm healthcheck` kiểm tiến trình cùng cấu hình đang sống (dùng trong image không có shell).
// `crm seed-dev <email>` (CRM_ROLE=migrate) dựng tenant, chủ shop và định tuyến email cho máy dev.
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

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/telemetry"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Thời hạn tắt êm: tổng dưới 25 s (kế hoạch M0, Task 3).
const (
	httpShutdownTimeout  = 20 * time.Second
	traceShutdownTimeout = 3 * time.Second
	healthcheckTimeout   = 3 * time.Second
	readHeaderTimeout    = 10 * time.Second
	// workerStopGrace: worker chờ việc đang chạy tối đa 25 s rồi trả việc dở về hàng chờ (spec 04 mục 5.1).
	workerStopGrace = 25 * time.Second
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
	case len(args) == 2 && args[0] == "seed-dev":
	case len(args) > 0:
		_, _ = fmt.Fprintf(stderr, "lệnh lạ %q; chỉ có `crm`, `crm healthcheck` hoặc `crm seed-dev <email>`\n", args)
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

	if len(args) == 2 { // seed-dev
		if err := seedDev(ctx, cfg, args[1], logger); err != nil {
			_, _ = fmt.Fprintln(stderr, "seed-dev:", err)
			return exitFail
		}
		return exitOK
	}
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
		return runWorker(ctx, d, reg, flusher, cfg, logger)
	}
	var wg sync.WaitGroup
	defer wg.Wait() // lần ghi chỉ số cuối (tối đa 5 s, song song với lúc tắt HTTP) xong rồi mới đóng pool
	// fctx dừng vòng ghi cả khi serveHTTP trả lỗi trước tín hiệu tắt (defer chạy ngược: cancel trước Wait).
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wg.Go(func() { flusher.Run(fctx) })
	if cfg.Role == config.RoleIngest {
		// Nạp lại tệp tạm webhook (database trở lại sau khi lỗi), mỗi 5 s, dừng cùng vòng ghi chỉ số.
		spool := &webhooks.Spool{Dir: cfg.SpoolDir}
		drainer := &webhooks.Drainer{Spool: spool, DB: d, Metrics: reg, Logger: logger}
		wg.Go(func() { drainer.Run(fctx) })
	}
	var hub *realtime.PGHub
	if cfg.Role == config.RoleAPI {
		// Kết nối LISTEN riêng của api (ngoài pool, ADR-42 tính thêm 1 kết nối), tự nối lại, dừng cùng vòng ghi chỉ số.
		hub = realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Metrics: reg, Logger: logger})
		wg.Go(func() { hub.Run(fctx) })
	}
	return serveHTTP(ctx, cfg, d, reg, hub, logger)
}

// newEvents dựng sổ consumer của sự kiện nội bộ, dùng chung cho api (Emit) và worker (handler); M0 chưa có consumer,
// module của mốc sau đăng ký ở đây.
func newEvents() *events.Registry { return events.NewRegistry() }

// newInbound dựng việc tách gói webhook và handler sự kiện vào của worker. Worker chỉ tách gói (Split), không kiểm
// chữ ký (ingest đã kiểm), nên nguồn selftest dựng không cần bí mật. M0 chưa có handler sự kiện vào (Task 18 thêm
// selftest.Inbound); module của mốc sau thêm ở đây, cùng danh sách nguồn với ingest.
func newInbound(reg *metrics.Registry) ([]queue.Handler, error) {
	sources := []webhooks.Source{selftest.NewSource("")}
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: reg})
	if err != nil {
		return nil, fmt.Errorf("dựng việc tách gói: %w", err)
	}
	hs, err := webhooks.InboundJobHandlers(sources, nil)
	if err != nil {
		return nil, fmt.Errorf("dựng handler sự kiện vào: %w", err)
	}
	return append([]queue.Handler{split}, hs...), nil
}

// newCommands là các loại lệnh ra ngoài của tiến trình; M0 chưa có lệnh thật, module của mốc sau thêm ở đây.
func newCommands() []commands.Handler { return nil }

// runWorker chạy hàng đợi (queue.Worker) với handler của sự kiện nội bộ và dọn dữ liệu, lệnh ra ngoài (Runner, kèm
// việc tra lại lệnh in_doubt), Ticker của việc định kỳ, và vòng ghi chỉ số, nhịp sống tới khi ctx bị hủy (SIGTERM);
// rồi tắt êm hàng đợi (Stop, tối đa workerStopGrace) trước, ghi chỉ số lần cuối sau. Ticker dừng ngay khi ctx bị hủy.
func runWorker(ctx context.Context, d *db.DB, reg *metrics.Registry, flusher *metrics.Flusher, cfg config.Config,
	logger *slog.Logger,
) error {
	cleanup := schedules.NewCleanup()
	cleanup.RegisterCleanup(webhooks.CleanupDeliveries) // bảng nền tảng không RLS: lịch platform.cleanup
	plan := schedules.NewRegistry()
	if err := cleanup.Define(plan); err != nil {
		return fmt.Errorf("định nghĩa lịch dọn: %w", err)
	}
	cmds := newCommands()
	resolver := commands.NewInDoubtResolver(d, cmds)
	if err := plan.Define(schedules.Schedule{
		Name: commands.ResolveInDoubtSchedule, Every: commands.ResolveInDoubtEvery, Kind: resolver.Kind(),
	}); err != nil {
		return fmt.Errorf("định nghĩa lịch tra lại lệnh: %w", err)
	}
	inbound, err := newInbound(reg)
	if err != nil {
		return err
	}
	handlers := append(append(newEvents().JobHandlers(), cleanup.Handlers()...), inbound...)
	w := queue.NewWorker(d, handlers, queue.WorkerOptions{
		WorkerID: metrics.ProcessName(cfg.Role) + "/" + uuid.NewString(), // mỗi lần chạy một mã giữ chỗ riêng
		Metrics:  reg, Logger: logger,
		Commands: commands.NewRunner(d, append(cmds, resolver)),
	})
	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("chạy hàng đợi: %w", err)
	}
	// Nhịp sống và chỉ số chạy tiếp trong lúc tắt êm hàng đợi; dừng sau Stop để ghi cả chỉ số của lúc tắt.
	fctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { flusher.Run(fctx) })
	ticker := schedules.NewTicker(d, queue.NewQueue(), plan, schedules.TickerOptions{Logger: logger})
	wg.Go(func() { ticker.Run(ctx) })
	<-ctx.Done()
	if err := w.Stop(workerStopGrace); err != nil {
		logger.Warn("tắt hàng đợi", slog.String("error", err.Error()))
	}
	cancel()
	wg.Wait()
	return nil
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
	return syncDefaultRoles(ctx, cfg, logger)
}

// syncDefaultRoles (sau migrate, Task 315) thêm vai trò mẫu và quyền mới của bảng quyền vào vai trò mẫu chưa bị chủ
// shop sửa ở mọi tenant (identity.SyncDefaultRoles), bằng chuỗi chủ bảng.
func syncDefaultRoles(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	owner, err := openOwner(ctx, cfg)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	n, err := identity.SyncDefaultRoles(ctx, owner)
	if err != nil {
		return err //nolint:wrapcheck // đã bọc trong identity
	}
	logger.Info("đồng bộ vai trò mẫu xong", slog.Int("tenants", n))
	return nil
}

// serveHTTP phục vụ HTTP (đo http_ms theo route) tới khi ctx bị hủy, rồi tắt êm trong httpShutdownTimeout. api phục
// vụ mọi route của hợp đồng (httpapi.NewServer); ingest chỉ có /hooks/*, /health và /health/live, /health/ready.
func serveHTTP(ctx context.Context, cfg config.Config, d *db.DB, reg *metrics.Registry, hub *realtime.PGHub,
	logger *slog.Logger,
) error {
	var handler http.Handler
	streamStop := make(chan struct{}) // đóng khi Shutdown bắt đầu: luồng SSE kết thúc, client tự nối lại máy mới
	if cfg.Role == config.RoleAPI {
		access, err := cfaccess.New(cfaccess.Options{
			TeamDomain: cfg.AccessTeamDomain, AUD: cfg.AccessAUD,
			MFAClaim: cfg.AccessMFAClaim, MFAValues: cfg.AccessMFAValues,
		})
		if err != nil {
			return fmt.Errorf("dựng bộ kiểm thẻ Access: %w", err)
		}
		handler = httpapi.NewServer(httpapi.Deps{
			Config: cfg, DB: d, Logger: logger, Access: access,
			Realtime: hub, SSE: realtime.SSEOptions{Stop: streamStop},
		})
	} else {
		h, err := ingestHandler(cfg, d, reg, logger)
		if err != nil {
			return err
		}
		handler = h
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("mở cổng %d: %w", cfg.Port, err)
	}
	srv := &http.Server{Handler: metrics.Middleware(reg, handler), ReadHeaderTimeout: readHeaderTimeout}
	srv.RegisterOnShutdown(func() { close(streamStop) })
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

// ingestHandler dựng các route của vai trò ingest: /hooks/{source} (GET xác minh, POST nhận), /health (số tệp tạm)
// và /health/live, /health/ready. Không có route nào của api.
func ingestHandler(cfg config.Config, d *db.DB, reg *metrics.Registry, logger *slog.Logger) (http.Handler, error) {
	in, err := webhooks.NewIngest(webhooks.IngestOptions{
		DB:      d,
		Sources: []webhooks.Source{selftest.NewSource(cfg.SelftestWebhookSecret)},
		Spool:   &webhooks.Spool{Dir: cfg.SpoolDir},
		Lockout: webhooks.NewLockout(webhooks.LockoutOptions{}),
		Metrics: reg,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("dựng ingest: %w", err)
	}
	mux := http.NewServeMux()
	observability.NewHealth(d.Pool).Register(mux)
	in.Register(mux)
	return mux, nil
}

// healthcheck: ingest và api gọi /health/live của chính nó; worker đòi nhịp sống của chính nó
// (platform.process_heartbeats, cùng tên máy) mới hơn metrics.HeartbeatMaxAge (90 s).
func healthcheck(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	switch cfg.Role {
	case config.RoleIngest, config.RoleAPI:
		if err := observability.CheckLive(ctx, cfg.Port, healthcheckTimeout); err != nil {
			return fmt.Errorf("kiểm /health/live: %w", err)
		}
		return nil
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
