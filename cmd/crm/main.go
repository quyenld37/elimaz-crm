// Lệnh crm là tệp chạy duy nhất của CRM. CRM_ROLE chọn vai trò: ingest, api, worker, migrate.
// `crm healthcheck` kiểm tiến trình cùng cấu hình đang sống (dùng trong image không có shell).
// `crm seed-dev <email>` (CRM_ROLE=migrate) dựng tenant, chủ shop và định tuyến email cho máy dev.
// `crm seed-web-fixture --file <json> --staff-email <email>` (CRM_ROLE=migrate, không production) dựng danh mục mẫu
// của bộ e2e website.
// `crm seed-staging …` (không production) dựng dữ liệu giả cho bản thử (khung Task 260, Task 275 làm).
// `crm recompute-customer-stats <mã khách>` (mọi vai trò, bằng DATABASE_URL) tính lại chỉ số khách cho gốc hiện tại.
// `crm verify-flash --sale <mã đợt>` (mọi vai trò, bằng DATABASE_URL) kiểm một đợt flash sale, thoát 1 khi sai.
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
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess"
	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/observability/alertjobs"
	"elimaz.com.vn/crm/internal/platform/observability/liveprobe"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/telemetry"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/wiring/flashstate"
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
	errNoPreset       = errors.New("IMAGE_PRESET_VERSION chưa có bộ kích thước")
)

func main() {
	if isNhanhSnapshotCommand(os.Args[1:]) {
		// Lệnh chụp chỉ đọc (ADR-48): không database, không cấu hình vai trò; chỉ cần biến môi trường của lệnh.
		os.Exit(runNhanhSnapshot(context.Background(), os.Args[2:], config.FromOS, os.Stdout, os.Stderr))
	}
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
	case len(args) == 2 && args[0] == recomputeCommand:
		if _, err := uuid.Parse(args[1]); err != nil {
			_, _ = fmt.Fprintf(stderr, "mã khách %q không phải UUID; cách gọi: `crm %s <mã khách>`\n", args[1], recomputeCommand)
			return exitConfig
		}
	case isOneOffCommand(args):
	case len(args) > 0:
		_, _ = fmt.Fprintf(stderr, "lệnh lạ %q; chỉ có `crm`, `crm healthcheck`, `crm seed-dev <email>`, "+
			"`crm %s <mã khách>`, `crm %s create|revoke …`, `crm %s --file … --staff-email …` hoặc "+
			"`crm %s --dir … --tenant … --dry-run|--apply`, `crm %s …`, `crm %s --sale <mã đợt>`, `crm nhanh-snapshot …`, `crm nhanh-import-products prepare|finish …`\n", args,
			recomputeCommand, serviceKeyCommand, seedFixtureCommand, importWebsiteCommand, seedStagingCommand,
			verifyFlashCommand)
		return exitConfig
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer stop()
	logger, closer, err := logging.NewLogger(cfg.LogDir, string(cfg.Role), stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitConfig
	}
	defer func() { _ = closer.Close() }()

	switch {
	case len(args) == 2 && args[0] == "seed-dev":
		if err := seedDev(ctx, cfg, args[1], logger); err != nil {
			_, _ = fmt.Fprintln(stderr, "seed-dev:", err)
			return exitFail
		}
		return exitOK
	case isOneOffCommand(args):
		return runOneOffCommand(ctx, cfg, args, stdout, stderr, logger)
	case len(args) == 2 && args[0] == recomputeCommand:
		if err := runRecompute(ctx, cfg, uuid.MustParse(args[1]), stdout, logger); err != nil {
			_, _ = fmt.Fprintln(stderr, recomputeCommand+":", err)
			return exitFail
		}
		return exitOK
	}
	if err := serve(ctx, cfg, logger); err != nil {
		logger.Error("tiến trình dừng vì lỗi", logging.Err(err))
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
			logger.Warn("đẩy nốt dấu vết", logging.Err(err))
		}
	}()
	d, err := db.Open(ctx, cfg.DatabaseURL, poolOptions(cfg))
	if err != nil {
		return fmt.Errorf("mở database: %w", err)
	}
	defer d.Pool.Close()

	logger.Info("khởi động", slog.Any("config", cfg))
	// Sổ mục đích tệp trong Go phải khớp platform.file_purposes (Task M1-41): lệch là lỗi khởi động, không phục vụ.
	registerFilePurposes()
	if cfg.Role == config.RoleAPI || cfg.Role == config.RoleWorker {
		if err := files.CheckPurposes(ctx, d); err != nil {
			return fmt.Errorf("kiểm mục đích tệp: %w", err)
		}
	}
	reg := metrics.NewRegistry()
	d.OnRetry = func(code string, _ int) { reg.Inc("tx_retry", code, 1) }
	d.OnExhausted = func(code string) { reg.Inc("tx_retry_exhausted", code, 1) }
	flusher := &metrics.Flusher{
		DB: d, Reg: reg, Process: metrics.ProcessName(cfg.Role), Role: cfg.Role, Log: logger, DiskPaths: diskPaths(cfg),
	}
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
		// Tệp tạm của yêu cầu xóa dữ liệu của Meta (Task 179), cùng nhịp.
		if err := startDataDeletionDrainer(fctx, &wg, cfg, d, logger); err != nil {
			return err
		}
	}
	var hub *realtime.PGHub
	if cfg.Role == config.RoleAPI {
		// Kết nối LISTEN riêng của api (ngoài pool, ADR-42 tính thêm 1 kết nối), tự nối lại, dừng cùng vòng ghi chỉ số.
		hub = realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Metrics: reg, Logger: logger})
		wg.Go(func() { hub.Run(fctx) })
	}
	return serveHTTP(ctx, cfg, d, reg, hub, logger)
}

// newInbound dựng việc tách gói webhook và handler sự kiện vào của worker. Worker chỉ tách gói (Split), không kiểm
// chữ ký (ingest đã kiểm), nên các nguồn dựng không cần bí mật. selftest.Inbound phát báo hiệu selftest.pong qua bus
// (pg_notify trong transaction của việc); nguồn và handler vào của M3 (gồm hãng giả của M1 khi carrier.FakeOn) đến từ
// wireM3, của M4 (adapter kênh nhắn tin) từ wireM4, cùng danh sách nguồn với ingest.
func newInbound(reg *metrics.Registry, bus selftest.Publisher, m3 m3Parts, m4 m4Parts) ([]queue.Handler, error) {
	sources := append(append([]webhooks.Source{selftest.NewSource("")}, m3.Sources...), m4.Sources...)
	handlers := append(append([]webhooks.InboundHandler{selftest.NewInbound(bus)}, m3.Inbound...), m4.Inbound...)
	split, err := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: reg})
	if err != nil {
		return nil, fmt.Errorf("dựng việc tách gói: %w", err)
	}
	hs, err := webhooks.InboundJobHandlers(sources, handlers)
	if err != nil {
		return nil, fmt.Errorf("dựng handler sự kiện vào: %w", err)
	}
	return append([]queue.Handler{split}, hs...), nil
}

// newCommands là các loại lệnh ra ngoài của tiến trình: kiểm tệp nhập Excel catalog.import.validate (Task M1-47; chỉ
// đọc kho tệp), nhập khách customer.import (Task M2-13; đọc kho tệp, ghi theo lô), xử lý ảnh file.process, file.process_private (Task M1-43),
// tạo lại kích thước file.regenerate (Task M1-44d;
// chỉ khi có IMAGER_URL, thiếu thì lệnh chờ trong hàng đợi), lệnh thử command.selftest (bên ngoài tại chỗ, không
// gửi gì thật, chỉ khi CRM_ENV khác production) và lệnh của M3 (m3.Commands, gồm hai lệnh vận chuyển của M1
// fulfillment.create_shipment, fulfillment.refresh_status với hãng giả khi carrier.FakeOn; Task M1-31, M3-04).
func newCommands(cfg config.Config, d *db.DB, reg *metrics.Registry, bus realtime.Bus, evs *events.Registry,
	m3 m3Parts, m4 m4Parts, m6 m6Parts, logger *slog.Logger,
) ([]commands.Handler, error) {
	fs, err := files.NewS3Store(s3Config(cfg))
	if err != nil {
		return nil, fmt.Errorf("dựng kho tệp: %w", err)
	}
	hs := []commands.Handler{
		catalog.NewImportValidator(catalog.ImportValidatorOptions{DB: d, Store: fs}),
		// Nhập khách từ Excel (Task M2-13): đọc kho tệp ngoài transaction, ghi theo lô 500 dòng.
		customer.NewImporter(customer.ImporterOptions{
			DB: d, Store: fs, Writer: customer.Writer{Events: evs, Queue: queue.NewQueue(), Realtime: bus},
		}),
		// Đối chiếu kho (Task M1-46): chỉ hỏi kho, không cần imager.
		files.NewVerifyHandler(files.ProcessorOptions{DB: d, Store: fs, Bus: bus}),
	}
	if cfg.ImagerURL != "" {
		if _, ok := files.Preset(cfg.ImagePresetVersion); !ok {
			return nil, fmt.Errorf("%w: %d", errNoPreset, cfg.ImagePresetVersion)
		}
		signCfg := s3Config(cfg)
		signCfg.Endpoint = cfg.S3ImagerEndpoint // imager chỉ tới được kho qua mạng imaging (SigV4 ký cả host)
		signer, err := files.NewS3Store(signCfg)
		if err != nil {
			return nil, fmt.Errorf("dựng kho tệp ký cho imager: %w", err)
		}
		o := files.ProcessorOptions{
			DB: d, Store: fs, Signer: signer, Imager: imager.NewClient(cfg.ImagerURL, &http.Client{}, cfg.ImagerMaxParallel),
			PresetVersion: cfg.ImagePresetVersion, Bus: bus, Metrics: reg, RetryMax: cfg.ImageRetryMax,
		}
		hs = append(hs, files.NewProcessHandler(o), files.NewProcessPrivateHandler(o), files.NewRegenerateHandler(o))
	} else {
		logger.Warn("thiếu IMAGER_URL: không xử lý ảnh, lệnh file.process chờ trong hàng đợi")
	}
	if cfg.Env != "production" {
		hs = append(hs, selftest.NewCommand(selftest.Loopback{}))
	}
	return slices.Concat(hs, m3.Commands, m4.Commands, m6.Commands), nil
}

// runWorker chạy hàng đợi (queue.Worker) với handler của sự kiện nội bộ và dọn dữ liệu, lệnh ra ngoài (Runner, kèm
// việc tra lại lệnh in_doubt), Ticker của việc định kỳ, và vòng ghi chỉ số, nhịp sống tới khi ctx bị hủy (SIGTERM);
// rồi tắt êm hàng đợi (Stop, tối đa workerStopGrace) trước, ghi chỉ số lần cuối sau. Ticker dừng ngay khi ctx bị hủy.
func runWorker(ctx context.Context, d *db.DB, reg *metrics.Registry, flusher *metrics.Flusher, cfg config.Config,
	logger *slog.Logger,
) error {
	cleanup := schedules.NewCleanup(schedules.CleanupOptions{JobRetention: time.Duration(cfg.JobsDoneRetentionDays) * 24 * time.Hour})
	cleanup.RegisterCleanup(webhooks.CleanupDeliveries) // bảng nền tảng không RLS: lịch platform.cleanup
	plan := schedules.NewRegistry()
	if err := cleanup.Define(plan); err != nil {
		return fmt.Errorf("định nghĩa lịch dọn: %w", err)
	}
	// Worker chỉ phát báo hiệu (Publish chạy pg_notify trong tx của việc), không LISTEN: không gọi hub.Run.
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Metrics: reg, Logger: logger})
	fs, err := files.NewS3Store(s3Config(cfg))
	if err != nil {
		return fmt.Errorf("dựng kho tệp: %w", err)
	}
	mods, err := newModules(cfg, d, hub, reg, fs)
	if err != nil {
		return err
	}
	for _, fn := range mods.M3.Cleanups {
		cleanup.RegisterTenantCleanup(fn)
	}
	// Lịch platform.data_deletion (Task 179): xóa dữ liệu đăng nhập Facebook theo yêu cầu của Meta, không tenant, 60 s.
	for _, s := range slices.Concat(mods.Schedules, mods.M3.Schedules, mods.M4.Schedules, mods.M6.Schedules,
		[]schedules.Schedule{dataDeletionProcessor().Schedule()}) {
		if err := plan.Define(s); err != nil {
			return fmt.Errorf("định nghĩa lịch %s: %w", s.Name, err)
		}
	}
	cmds, err := newCommands(cfg, d, reg, hub, mods.Events, mods.M3, mods.M4, mods.M6, logger)
	if err != nil {
		return err
	}
	resolver := commands.NewInDoubtResolver(d, cmds)
	if err := plan.Define(schedules.Schedule{
		Name: commands.ResolveInDoubtSchedule, Every: commands.ResolveInDoubtEvery, Kind: resolver.Kind(),
	}); err != nil {
		return fmt.Errorf("định nghĩa lịch tra lại lệnh: %w", err)
	}
	ops, err := alertSchedules(plan, d, cfg, logger)
	if err != nil {
		return err
	}
	// Đối chiếu tồn hằng đêm (Task M1-36): dòng lịch chèn sẵn ở migration 0122 (kỳ đầu 02:30 giờ Việt Nam).
	if err := plan.Define(schedules.Schedule{
		Name: inventory.ReconcileSchedule, Every: inventory.ReconcileEvery, PerTenant: true, Kind: inventory.ReconcileKind(),
	}); err != nil {
		return fmt.Errorf("định nghĩa lịch đối chiếu tồn: %w", err)
	}
	inbound, err := newInbound(reg, hub, mods.M3, mods.M4)
	if err != nil {
		return err
	}
	handlers := append(append(mods.Events.JobHandlers(), cleanup.Handlers()...), inbound...)
	handlers = append(append(handlers, inventory.NewReconcileHandler(queue.NewQueue()),
		// Áp bản nhập Excel (Task M1-47b): ghi mẫu, biến thể bằng Writer của catalog (sự kiện qua cùng sổ, báo hiệu).
		catalog.NewImportApplier(catalog.Writer{Events: mods.Events, Queue: queue.NewQueue(), Realtime: hub})),
		mods.Handlers...)
	handlers = append(append(append(append(handlers, mods.M4.Jobs...), mods.M6.Jobs...), mods.M7.Jobs...),
		dataDeletionProcessor())
	w := queue.NewWorker(d, handlers, queue.WorkerOptions{
		WorkerID: metrics.ProcessName(cfg.Role) + "/" + uuid.NewString(), // mỗi lần chạy một mã giữ chỗ riêng
		Metrics:  reg, Logger: logger, Role: string(cfg.Role),
		Commands: commands.NewRunner(d, append(append(cmds, resolver), ops...)),
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
	startBackground(ctx, &wg, mods.M4.Background, logger)
	<-ctx.Done()
	if err := w.Stop(workerStopGrace); err != nil {
		logger.Warn("tắt hàng đợi", logging.Err(err))
	}
	cancel()
	wg.Wait()
	return nil
}

// startBackground chạy các luồng nền của module (M4: đọc comment live, Task 170) trong wg tới khi ctx bị hủy; lỗi (trừ
// hủy) ghi log một lần ở đây.
func startBackground(ctx context.Context, wg *sync.WaitGroup, runs []func(context.Context) error, logger *slog.Logger) {
	for _, run := range runs {
		wg.Go(func() {
			if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("luồng nền dừng", logging.Err(err))
			}
		})
	}
}

// alertSchedules định nghĩa các lịch của cảnh báo: platform.alerts mỗi 60 s (CheckRules, Task 19), platform.digest
// (tin tổng kết 08:00 giờ Việt Nam) và channel.data_use_checkup (nhắc Data Use Checkup, Task 179); trả hai lệnh đó cùng hai lệnh cảnh báo của kho (cùng Alerter) để đăng
// ký vào Runner. Gửi Telegram nên là lệnh, không phải handler transactional.
func alertSchedules(plan *schedules.Registry, d *db.DB, cfg config.Config, logger *slog.Logger) ([]commands.Handler, error) {
	alerter := observability.NewTelegramAlerter(d, cfg, nil, logger)
	alerts := alertjobs.NewAlertsCommand(d, alerter, observability.RuleOptions{
		BackupExpected: cfg.BackupExpected, FileSyncExpected: cfg.FileSyncExpected, DBDiskBudgetBytes: cfg.DBDiskBudgetBytes, ConnBudget: cfg.DBConnBudget,
	}, logger)
	digest := alertjobs.NewDigestCommand(d, alerter, logger)
	// Nhắc Data Use Checkup hằng năm của app Meta (Task 179): lịch hằng ngày, gửi khi còn ≤ 30 ngày, 7 ngày một lần.
	checkup := channel.NewDataUseCheckupCommand(d, alerter, cfg.Meta.DataUseCheckupDue, nil, logger)
	for _, s := range []schedules.Schedule{
		{Name: alertjobs.AlertsSchedule, Every: alertjobs.AlertsEvery, Kind: alerts.Kind()},
		{Name: alertjobs.DigestSchedule, Every: alertjobs.DigestEvery, Kind: digest.Kind()},
		{Name: channel.DataUseCheckupSchedule, Every: channel.DataUseCheckupEvery, Kind: checkup.Kind()},
	} {
		if err := plan.Define(s); err != nil {
			return nil, fmt.Errorf("định nghĩa lịch %s: %w", s.Name, err)
		}
	}
	// Cảnh báo thiếu hàng sau kiểm kho (Task M1-35) và lệch sổ sau đối chiếu đêm (Task M1-36): lệnh do inventory ghi,
	// không phải lịch. Cảnh báo hãng từ chối khóa API (Task M3-09): lệnh do fulfillment.MarkAuthFailed ghi. Cảnh báo
	// đồng bộ vận đơn (tồn thiếu khi áp, Task M3-11): lệnh do lệnh tra fulfillment.refresh_status ghi. Cảnh báo tiền COD
	// đã về rồi hãng sửa thành hoàn (Task M3-29): lệnh do consumer cash ghi. Cảnh báo lệch sổ COD sau đối chiếu đêm
	// (Task M3-33): lệnh do lệnh cash.reconcile ghi.
	return append([]commands.Handler{
		alerts, digest, inventory.NewShortageAlertCommand(alerter, logger), inventory.NewStockMismatchCommand(alerter, logger),
		inventory.NewFlashMismatchCommand(alerter, logger),
		fulfillment.NewAuthAlertCommand(alerter, logger), fulfillment.NewTrackAlertCommand(d, alerter, logger),
		// Cảnh báo hãng sửa Thành công sang Đang chuyển hoàn (Task M3-27): lệnh do tác động delivery_reversed ghi.
		fulfillment.NewCorrectedAlertCommand(alerter, logger),
		cash.NewRemittedReturnedAlertCommand(alerter),
		cash.NewReconcileAlertCommand(alerter),
		// Cảnh báo đơn kẹt mỗi ngày (Task M3-20): lệnh do lịch fulfillment.stuck ghi.
		fulfillment.NewStuckAlertCommand(d, alerter, logger),
		// Cảnh báo Page bắt đầu gửi thật (kế hoạch M4, Task 145): lệnh do channel.Writer.SetSendMode ghi.
		channel.NewSendModeAlertCommand(alerter, logger),
		channel.NewSubscribeAlertCommand(alerter, logger),
		// Cảnh báo phiên live (Task 171): luồng comment hỏng (do nguồn comment ghi) và đối chiếu cuối phiên thiếu comment
		// (do lệnh command.fb.live_reconcile ghi).
		conversation.NewLiveAlertCommand(alerter, logger),
		checkup,
		// Cảnh báo sức khỏe kênh (Task 150), gồm channel.auth_alert do channel.Writer.ReportTokenInvalid (Task 156, gửi tin
		// nhận lỗi token) và kiểm sức khỏe mất quyền cùng ghi.
	}, channel.HealthAlertCommands(alerter, logger)...), nil
}

// diskPaths: đĩa mỗi tiến trình tự đo cho luật disk:<process> (gauge disk_used_pct): "/" của container, thư mục tệp
// tạm webhook (ingest) và thư mục log khi có.
func diskPaths(cfg config.Config) map[string]string {
	p := map[string]string{"root": "/"}
	if cfg.SpoolDir != "" {
		p["spool"] = cfg.SpoolDir
	}
	if cfg.LogDir != "" {
		p["log"] = cfg.LogDir
	}
	return p
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

// s3Config là cấu hình kho tệp (danh tính crm-app) từ biến môi trường đã kiểm.
func s3Config(cfg config.Config) files.S3Config {
	return files.S3Config{
		Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, AccessKeyID: cfg.S3AccessKeyID,
		SecretAccessKey: cfg.S3SecretAccessKey, BucketPrefix: cfg.S3BucketPrefix,
	}
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
		store, err := files.NewS3Store(s3Config(cfg))
		if err != nil {
			return fmt.Errorf("dựng kho tệp: %w", err)
		}
		mods, err := newModules(cfg, d, hub, reg, store)
		if err != nil {
			return err
		}
		web, err := newWebKeys(ctx, cfg, d, hub, reg, logger)
		if err != nil {
			return err
		}
		defer web.stop() // sau khi máy chủ HTTP tắt: ghi nốt last_used_at, đóng Valkey
		// Vòng dựng lại cờ hết suất và số còn của món flash sale trên cổng (Task 204): dừng trước khi đóng Valkey.
		flash := flashstate.New(d, web.flashGate, hub, flashstate.DefaultEvery)
		flash.Start(ctx)
		defer flash.Stop()
		wc, err := newWebCache(ctx, cfg, d, hub, reg, logger, 0)
		if err != nil {
			return err
		}
		defer wc.stop() // sau khi máy chủ HTTP tắt: dừng bộ xóa khóa, đóng Valkey bộ đệm
		handler = httpapi.NewServer(httpapi.Deps{
			Config: cfg, DB: d, Logger: logger, Access: access, Files: store,
			Realtime: hub, SSE: realtime.SSEOptions{Stop: streamStop}, Events: mods.Events, Metrics: reg,
			Cash:        mods.M3.Cash.Service,
			Fulfillment: mods.M3.Fulfillment.Service,
			ServiceKeys: web.keys, RateLimiter: web.limiter, ReadCache: wc.rc, WebOrderLimiter: web.orderLimiter,
			CouponGuard: web.couponGuard,
			Admission:   map[string]func(http.Handler) http.Handler{"flash": web.flashAdmission}, FlashGate: web.flashGate,
			Channels: mods.M4.Channels, ChannelKeys: mods.M4.Keys,
			Reports: reportInteractive(cfg, d, reg),
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
	sources, err := ingestSources(cfg)
	if err != nil {
		return nil, err
	}
	in, err := webhooks.NewIngest(webhooks.IngestOptions{
		Role:    string(cfg.Role),
		DB:      d,
		Sources: sources,
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
	// Callback xóa dữ liệu của Meta (Task 179): đường cụ thể hơn POST /hooks/{source}/{routeKey} nên thắng.
	if err := registerDataDeletion(mux, cfg, d, logger); err != nil {
		return nil, err
	}
	return mux, nil
}

// ingestSources là các nguồn webhook của ingest: selftest, nguồn của M4 (adapter kênh nhắn tin, wireM4) và nguồn của M3 từ wireM3 (/hooks/<hãng>/<route_key> của hãng
// có webhook, kèm bí mật; hãng giả chỉ khi carrier.FakeOn: production không bao giờ có /hooks/fake).
func ingestSources(cfg config.Config) ([]webhooks.Source, error) {
	m3, err := wireM3(config.RoleIngest, cfg, m3Deps{})
	if err != nil {
		return nil, err
	}
	m4, err := wireM4(cfg, m4Deps{})
	if err != nil {
		return nil, err
	}
	return slices.Concat([]webhooks.Source{selftest.NewSource(cfg.SelftestWebhookSecret)}, m3.Sources, m4.Sources), nil
}

// healthcheck: ingest và api gọi /health/live của chính nó; worker đòi nhịp sống của chính nó
// (platform.process_heartbeats, cùng tên máy) mới hơn metrics.HeartbeatMaxAge (90 s).
func healthcheck(ctx context.Context, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	switch cfg.Role {
	case config.RoleIngest, config.RoleAPI:
		if err := liveprobe.CheckLive(ctx, cfg.Port, healthcheckTimeout); err != nil {
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

// registerFilePurposes đăng ký mục đích tệp của các mốc sau vào sổ của tiến trình, trước files.CheckPurposes lúc khởi
// động (test TestRegisterFilePurposes_MatchTable đòi sổ khớp bảng): customer_import (M2, Task M2-13), carrier_label và
// cod_statement (M3, Task M3-04).
func registerFilePurposes() {
	customer.RegisterFilePurposes()
	fulfillment.RegisterFilePurposes()     // carrier_label (M3, Task M3-04, 0306)
	cash.RegisterFilePurposes()            // cod_statement (M3, Task M3-04, 0306)
	order.RegisterFilePurposes()           // order_request_photo (M5, Task 195, 0504)
	content.RegisterFilePurposes()         // web_banner (M5, Task 453, 0521)
	reporting.RegisterFilePurposes()       // report_export, customer_export (M6, Task 226, 0604)
	conversation.RegisterFilePurposes()    // chat_outgoing (M4, Task 156, 0413); chat_image, chat_attachment, chat_avatar (Task 155, 0404)
	inventory.RegisterCountImportPurpose() // inventory_count_import (M7, Task 263, 0705; khung Task 260)
	order.RegisterFallbackPurpose()        // order_fallback_import (M7, Task 268, 0710; khung Task 260)
}
