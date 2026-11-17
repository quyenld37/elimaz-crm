// Phép đo đường mua flash sale bằng Go (kế hoạch M0 bản 5, Task 1; ADR-44 "Phép đo đầu tiên của M0").
// Mã đo, module riêng, không được nhập từ cmd/ hay internal/.
//
//	go run . setup  -units 500               # dựng lại schema spike, nạp suất, in mã món
//	go run . serve  -limit 1 [-otel]         # POST /buy trên 127.0.0.1:4190
//	go run . verify -item <id> -limit 1      # kiểm quy luật, thoát 1 khi sai
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

//go:embed schema.sql
var schemaSQL string

const (
	// Chuỗi kết nối dev (docker/compose.dev.yml); đổi cổng bằng CRM_SPIKE_DATABASE_URL hoặc cờ -db.
	defaultAppURL = "postgres://crm_app:crm_app_dev@127.0.0.1:5442/crm?sslmode=disable"
	// Tenant cố định của các lần đo bằng k6.
	defaultTenant = "00000000-0000-4000-8000-000000000001"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "dùng: flashsale serve|setup|verify [cờ]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "setup":
		err = runSetup(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		err = fmt.Errorf("lệnh không biết: %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lỗi:", err)
		os.Exit(1)
	}
}

func dbFlags(fs *flag.FlagSet) (app, owner *string) {
	def := os.Getenv("CRM_SPIKE_DATABASE_URL")
	if def == "" {
		def = defaultAppURL
	}
	app = fs.String("db", def, "chuỗi kết nối của crm_app")
	owner = fs.String("owner-db", "", "chuỗi kết nối của crm_owner (mặc định: như -db, người dùng crm_owner)")
	return app, owner
}

// ownerURLFrom đổi người dùng, mật khẩu của chuỗi kết nối crm_app thành crm_owner dev; giữ máy, cổng, database.
func ownerURLFrom(appURL string) (string, error) {
	u, err := url.Parse(appURL)
	if err != nil {
		return "", fmt.Errorf("đọc chuỗi kết nối: %w", err)
	}
	u.User = url.UserPassword("crm_owner", "crm_owner_dev")
	return u.String(), nil
}

// openPool mở pool theo Global Constraints: chế độ cache statement (cấm simple protocol) và giới hạn ADR-26.
func openPool(ctx context.Context, connURL string, maxConns int32, tracer ...pgx.QueryTracer) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connURL)
	if err != nil {
		return nil, fmt.Errorf("đọc chuỗi kết nối: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30s"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5s"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60s"
	cfg.ConnConfig.RuntimeParams["application_name"] = "crm-spike-flashsale"
	if len(tracer) > 0 {
		cfg.ConnConfig.Tracer = tracer[0]
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("mở pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("nối database: %w", err)
	}
	return pool, nil
}

// applySchema xóa và tạo lại schema spike. Chạy bằng crm_owner.
func applySchema(ctx context.Context, owner *pgxpool.Pool) error {
	// Tệp nhiều câu lệnh, không tham số: chạy qua giao thức truy vấn đơn của pgconn (như công cụ migration).
	conn, err := owner.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lấy kết nối: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Conn().PgConn().Exec(ctx, schemaSQL).ReadAll(); err != nil {
		return fmt.Errorf("chạy schema.sql: %w", err)
	}
	return nil
}

// seed nạp units suất `available` cho món item của tenant. Chạy bằng crm_owner.
func seed(ctx context.Context, owner *pgxpool.Pool, tenant, item string, units int) error {
	return inTenantTx(ctx, owner, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT spike.seed($1, $2, $3)", tenant, item, units)
		return err
	})
}

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	appURL, ownerURL := dbFlags(fs)
	units := fs.Int("units", 500, "số suất")
	tenant := fs.String("tenant", defaultTenant, "tenant")
	item := fs.String("item", "", "mã món (mặc định: sinh mới)")
	_ = fs.Parse(args)
	ctx := context.Background()
	if *ownerURL == "" {
		var err error
		if *ownerURL, err = ownerURLFrom(*appURL); err != nil {
			return err
		}
	}
	owner, err := openPool(ctx, *ownerURL, 2)
	if err != nil {
		return err
	}
	defer owner.Close()
	if err := applySchema(ctx, owner); err != nil {
		return err
	}
	if *item == "" {
		if err := owner.QueryRow(ctx, "SELECT uuidv7()::text").Scan(item); err != nil {
			return fmt.Errorf("sinh mã món: %w", err)
		}
	}
	if err := seed(ctx, owner, *tenant, *item, *units); err != nil {
		return fmt.Errorf("nạp suất: %w", err)
	}
	fmt.Println(*item)
	return nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	appURL, _ := dbFlags(fs)
	tenant := fs.String("tenant", defaultTenant, "tenant")
	item := fs.String("item", "", "mã món")
	limit := fs.Int("limit", 1, "giới hạn mỗi khách đã dùng khi đo")
	_ = fs.Parse(args)
	ctx := context.Background()
	pool, err := openPool(ctx, *appURL, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	rep, err := verify(ctx, pool, *tenant, *item, *limit)
	if err != nil {
		return err
	}
	fmt.Println(rep)
	if !rep.OK() {
		return errors.New("verify: SAI")
	}
	fmt.Println("verify: ĐẠT")
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	appURL, _ := dbFlags(fs)
	addr := fs.String("addr", "127.0.0.1:4190", "địa chỉ nghe")
	limit := fs.Int("limit", 1, "số suất tối đa mỗi khách")
	maxInflight := fs.Int("max-inflight", 64, "số lượt mua xử lý cùng lúc; quá thì 503 retry")
	poolSize := fs.Int("pool", 20, "số kết nối tối đa (ngân sách của api, ADR-42)")
	withOtel := fs.Bool("otel", false, "bật OpenTelemetry (otelhttp + otelpgx), lấy mẫu 100%")
	otlp := fs.String("otlp", "127.0.0.1:4318", "OTLP HTTP endpoint (Jaeger)")
	every := fs.Duration("report", 10*time.Second, "chu kỳ in phân vị độ trễ")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var tracers []pgx.QueryTracer
	shutdownOtel := func(context.Context) error { return nil }
	if *withOtel {
		var err error
		if shutdownOtel, err = setupOtel(ctx, *otlp); err != nil {
			return err
		}
		tracers = append(tracers, otelpgx.NewTracer())
	}
	pool, err := openPool(ctx, *appURL, int32(*poolSize), tracers...)
	if err != nil {
		return err
	}
	defer pool.Close()

	buy := newBuyServer(pool, *limit, *maxInflight)
	var handler http.Handler = buy
	if *withOtel {
		handler = otelhttp.NewHandler(handler, "POST /buy")
	}
	lat := &latencyRecorder{extra: func() string { return fmt.Sprintf("tx_retry=%v", buy.retries.snapshot()) }}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           lat.wrap(handler),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	go lat.reportEvery(ctx, *every)
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("đang nghe", "addr", *addr, "limit", *limit, "max_inflight", *maxInflight, "pool", *poolSize, "otel", *withOtel)

	select {
	case err := <-errc:
		return fmt.Errorf("máy chủ: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serr := srv.Shutdown(sctx)
	fmt.Println("TỔNG", lat.summary(true))
	return errors.Join(serr, shutdownOtel(sctx))
}

func setupOtel(ctx context.Context, endpoint string) (func(context.Context) error, error) {
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("mở OTLP exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		// Hàng đợi lớn để ở 1.000 lượt/giây không bỏ span (bỏ span làm chi phí đo trông nhỏ hơn thật).
		sdktrace.WithBatcher(exp, sdktrace.WithMaxQueueSize(1<<16), sdktrace.WithMaxExportBatchSize(4096)),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "crm-spike-flashsale"))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}

// latencyRecorder đo độ trễ phía máy chủ: từ lúc nhận yêu cầu tới lúc handler ghi xong câu trả lời.
type latencyRecorder struct {
	mu      sync.Mutex
	start   time.Time
	window  []time.Duration
	all     []time.Duration
	byCode  map[int]int
	winFrom time.Time
	last    time.Time
	extra   func() string // in thêm cuối mỗi dòng (bộ đếm chạy lại transaction)
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (l *latencyRecorder) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t0 := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		h.ServeHTTP(sw, r)
		d := time.Since(t0)
		l.mu.Lock()
		if l.start.IsZero() {
			l.start, l.winFrom, l.byCode = t0, t0, map[int]int{}
		}
		l.window = append(l.window, d)
		l.all = append(l.all, d)
		l.byCode[sw.code]++
		l.last = t0.Add(d)
		l.mu.Unlock()
	})
}

func (l *latencyRecorder) reportEvery(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s := l.summary(false); s != "" {
				fmt.Println(time.Now().Format("15:04:05"), s)
			}
		}
	}
}

// summary in n, số lượt/giây, p50/p95/p99/max của cửa sổ vừa qua (all=false, rồi xóa cửa sổ) hoặc của cả lần chạy.
func (l *latencyRecorder) summary(all bool) string {
	l.mu.Lock()
	var ds []time.Duration
	var from, to time.Time
	if all {
		ds, from, to = slices.Clone(l.all), l.start, l.last
	} else {
		ds, from, to = l.window, l.winFrom, time.Now()
		l.window, l.winFrom = nil, to
	}
	codes := fmt.Sprint(l.byCode)
	l.mu.Unlock()
	extra := ""
	if l.extra != nil {
		extra = " " + l.extra()
	}
	if len(ds) == 0 {
		return ""
	}
	slices.Sort(ds)
	pct := func(p float64) time.Duration { return ds[min(len(ds)-1, int(p*float64(len(ds))))] }
	secs := max(to.Sub(from).Seconds(), 0.001)
	return fmt.Sprintf("n=%d rps=%.0f p50=%s p95=%s p99=%s max=%s codes=%s%s",
		len(ds), float64(len(ds))/secs, pct(0.50).Round(10*time.Microsecond), pct(0.95).Round(10*time.Microsecond),
		pct(0.99).Round(10*time.Microsecond), ds[len(ds)-1].Round(10*time.Microsecond), codes, extra)
}
