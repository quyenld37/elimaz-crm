// Lệnh fakecarrier là hãng vận chuyển giả cho dev và staging (kế hoạch M1, Task M1-31; Q2): API tạo, tra, đọc kiện
// có xác thực (Bearer FAKECARRIER_WEBHOOK_SECRET), trang /admin để đổi trạng thái kiện (gửi webhook có chữ ký tới
// FAKECARRIER_WEBHOOK_URL) và đổi chế độ lỗi. Trạng thái trong bộ nhớ, ghi tệp JSON (FAKECARRIER_DATA) để sống qua
// khởi động lại. Không bao giờ chạy ở production: CRM chỉ nối khi CRM_ENV khác production.
//
// Biến môi trường: PORT (mặc định 4194), FAKECARRIER_WEBHOOK_SECRET (≥ 32 ký tự; mặc định là chuỗi dev công khai),
// FAKECARRIER_WEBHOOK_URL (rỗng: không gửi webhook), FAKECARRIER_DATA (mặc định /tmp/fakecarrier.json; "-" chỉ trong
// bộ nhớ), FAKECARRIER_QUOTE_FEE (số nguyên đồng > 0: báo đúng một phí cố định cho mọi kiện, dùng cho e2e website;
// rỗng: theo công thức).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/tools/fakecarrier/server"
)

const (
	defaultPort   = 4194
	defaultData   = "/tmp/fakecarrier.json"
	minSecretLen  = 32
	readTimeout   = 10 * time.Second
	shutdownGrace = 5 * time.Second
	// defaultHang: chế độ timeout_after_create giữ yêu cầu tới khi bên gọi bỏ (hạn của lệnh CRM là 30 s).
	defaultHang = 60 * time.Second
	// devSecret là bí mật mặc định của máy dev (công khai, không phải bí mật thật; staging đặt biến riêng).
	devSecret = "fakecarrier-dev-only-not-a-secret-000" //nolint:gosec // G101: chuỗi dev công khai, không phải bí mật
)

var errShortSecret = errors.New("FAKECARRIER_WEBHOOK_SECRET phải dài ít nhất 32 ký tự")

// config là cấu hình của chương trình.
type config struct {
	port       int
	secret     string
	webhookURL string
	dataPath   string
	hang       time.Duration
	quoteFee   int64
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{port: defaultPort, secret: devSecret, webhookURL: getenv("FAKECARRIER_WEBHOOK_URL"), dataPath: defaultData, hang: defaultHang}
	if v := getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return c, fmt.Errorf("PORT %q không hợp lệ", v) //nolint:err113 // lỗi cấu hình một lần
		}
		c.port = p
	}
	if v := getenv("FAKECARRIER_WEBHOOK_SECRET"); v != "" {
		c.secret = v
	}
	if len(c.secret) < minSecretLen {
		return c, errShortSecret
	}
	if v := getenv("FAKECARRIER_QUOTE_FEE"); v != "" {
		fee, err := strconv.ParseInt(v, 10, 64)
		if err != nil || fee <= 0 {
			return c, fmt.Errorf("FAKECARRIER_QUOTE_FEE %q phải là số nguyên đồng > 0", v) //nolint:err113 // lỗi cấu hình một lần
		}
		c.quoteFee = fee
	}
	switch v := getenv("FAKECARRIER_DATA"); v {
	case "":
	case "-":
		c.dataPath = ""
	default:
		c.dataPath = v
	}
	return c, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Công cụ dev riêng, không nhập internal/platform/config (cấu hình của CRM): đọc biến môi trường tại đây.
	if err := serveSignals(os.Getenv, logger); err != nil { //nolint:forbidigo // xem chú thích trên
		logger.Error("fakecarrier dừng vì lỗi", logging.Err(err))
		os.Exit(1)
	}
}

// serveSignals chạy tới khi nhận SIGTERM hay Ctrl-C.
func serveSignals(getenv func(string) string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return run(ctx, getenv, logger)
}

func run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	st, err := server.OpenStore(cfg.dataPath)
	if err != nil {
		return fmt.Errorf("mở kho trạng thái: %w", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.port)))
	if err != nil {
		return fmt.Errorf("mở cổng %d: %w", cfg.port, err)
	}
	h := server.New(server.Options{
		Store: st, Secret: cfg.secret, WebhookURL: cfg.webhookURL, Hang: cfg.hang, HTTPClient: &http.Client{}, Logger: logger,
		QuoteFee: cfg.quoteFee,
	})
	srv := &http.Server{Handler: h, ReadHeaderTimeout: readTimeout}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	logger.Info("fakecarrier đang nghe", slog.Int("port", cfg.port), slog.Bool("webhook", cfg.webhookURL != ""))
	select {
	case err := <-errCh:
		return fmt.Errorf("máy chủ dừng: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("tắt máy chủ: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("máy chủ dừng: %w", err)
	}
	return nil
}
