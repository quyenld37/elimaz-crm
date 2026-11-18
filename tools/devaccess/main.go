// Lệnh devaccess là máy chủ thẻ Cloudflare Access giả, chỉ cho compose dev (kế hoạch M1, Task 395): phục vụ bộ khóa
// /cdn-cgi/access/certs qua HTTPS (chứng chỉ tự ký) cho bộ kiểm thẻ thật của api, và phát thẻ RS256 có MFA qua
// GET /token?email=… trên cổng HTTP riêng (compose chỉ mở cổng này ở 127.0.0.1). Không bao giờ chạy ở production.
//
// Khóa ký và chứng chỉ sinh ở lần chạy đầu, giữ trong DEVACCESS_DATA (quyền 600) để khởi động lại không đổi khóa;
// chứng chỉ công khai chép vào DEVACCESS_CA_DIR (api gắn chỉ đọc, đặt SSL_CERT_DIR tới đó).
//
// Biến môi trường: CRM_ENV (production → từ chối chạy), PORT (cổng phát thẻ, mặc định 4195), TLS_PORT (cổng khóa,
// mặc định 8443), DEVACCESS_HOST (tên máy trong chứng chỉ và iss, mặc định devaccess; api đặt
// ACCESS_TEAM_DOMAIN=<host>:<TLS_PORT>), ACCESS_AUD (mặc định dev-aud), ACCESS_MFA_CLAIM (mặc định amr),
// DEVACCESS_DATA (mặc định /data), DEVACCESS_CA_DIR (mặc định /ca).
//
// `devaccess healthcheck`: hỏi /health của chính nó (image distroless không có shell).
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
)

const (
	defaultPort    = 4195
	defaultTLSPort = 8443
	readTimeout    = 10 * time.Second
	shutdownGrace  = 5 * time.Second
	tokenTTL       = time.Hour
)

var errProduction = errors.New("devaccess không được chạy khi CRM_ENV=production")

// config là cấu hình của chương trình.
type config struct {
	port, tlsPort  int
	host           string
	aud, mfaClaim  string
	dataDir, caDir string
}

func (c config) issuer() string {
	return "https://" + net.JoinHostPort(c.host, strconv.Itoa(c.tlsPort))
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		port: defaultPort, tlsPort: defaultTLSPort, host: "devaccess", aud: "dev-aud", mfaClaim: "amr",
		dataDir: "/data", caDir: "/ca",
	}
	if getenv("CRM_ENV") == "production" {
		return c, errProduction
	}
	var problems []string
	for _, p := range []struct {
		key string
		dst *int
	}{{"PORT", &c.port}, {"TLS_PORT", &c.tlsPort}} {
		if v := getenv(p.key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 65535 {
				problems = append(problems, fmt.Sprintf("%s %q không hợp lệ", p.key, v))
				continue
			}
			*p.dst = n
		}
	}
	for _, s := range []struct {
		key string
		dst *string
	}{{"DEVACCESS_HOST", &c.host}, {"ACCESS_AUD", &c.aud}, {"ACCESS_MFA_CLAIM", &c.mfaClaim}, {"DEVACCESS_DATA", &c.dataDir}, {"DEVACCESS_CA_DIR", &c.caDir}} {
		if v := getenv(s.key); v != "" {
			*s.dst = v
		}
	}
	if strings.ContainsAny(c.host, "/?#@: ") {
		problems = append(problems, "DEVACCESS_HOST phải là tên máy, không có cổng hay đường dẫn")
	}
	if len(problems) > 0 {
		return c, fmt.Errorf("cấu hình devaccess: %s", strings.Join(problems, "; ")) //nolint:err113 // lỗi cấu hình một lần
	}
	return c, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Công cụ dev riêng, không nhập internal/platform/config (cấu hình của CRM): đọc biến môi trường tại đây.
	getenv := os.Getenv //nolint:forbidigo // xem chú thích trên
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(getenv))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	err := run(ctx, getenv, logger)
	stop()
	if err != nil {
		logger.Error("devaccess dừng vì lỗi", logging.Err(err))
		os.Exit(1)
	}
}

// healthcheck trả 0 khi /health của tiến trình đang chạy trả 200.
func healthcheck(getenv func(string) string) int {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(cfg.port)+"/health", nil)
	if err != nil {
		return 1
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// run sinh hoặc nạp khóa, chạy hai máy chủ (HTTPS bộ khóa, HTTP phát thẻ) tới khi ctx bị hủy.
func run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	k, err := loadKeys(cfg.dataDir, cfg.caDir, cfg.host, time.Now())
	if err != nil {
		return err
	}
	lc := &net.ListenConfig{}
	tlsLn, err := lc.Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.tlsPort)))
	if err != nil {
		return fmt.Errorf("mở cổng %d: %w", cfg.tlsPort, err)
	}
	tokLn, err := lc.Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.port)))
	if err != nil {
		_ = tlsLn.Close()
		return fmt.Errorf("mở cổng %d: %w", cfg.port, err)
	}
	certsSrv := &http.Server{
		Handler: certsHandler(k), ReadHeaderTimeout: readTimeout,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{k.tlsCert}, MinVersion: tls.VersionTLS12},
	}
	tokenSrv := &http.Server{Handler: tokenHandler(k, cfg, time.Now), ReadHeaderTimeout: readTimeout}
	errCh := make(chan error, 2)
	go func() { errCh <- certsSrv.ServeTLS(tlsLn, "", "") }()
	go func() { errCh <- tokenSrv.Serve(tokLn) }()
	logger.Info("devaccess đang nghe", slog.Int("port", cfg.port), slog.Int("tls_port", cfg.tlsPort),
		slog.String("issuer", cfg.issuer()), slog.String("kid", k.kid))
	var errs []error
	pending := 2 // số goroutine Serve chưa trả
	select {
	case err := <-errCh:
		pending--
		errs = append(errs, fmt.Errorf("máy chủ dừng: %w", err))
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	errs = append(errs, certsSrv.Shutdown(sctx), tokenSrv.Shutdown(sctx))
	for ; pending > 0; pending-- {
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("máy chủ dừng: %w", err))
		}
	}
	return errors.Join(errs...)
}

func certsHandler(k *keys) http.Handler {
	body := k.jwks()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cdn-cgi/access/certs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	return mux
}

// tokenHandler: GET /token?email=<địa chỉ> → thẻ (text/plain), hạn 1 giờ, MFA; GET /health.
func tokenHandler(k *keys, cfg config, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
			http.Error(w, "cần email hợp lệ: /token?email=<địa chỉ>", http.StatusBadRequest)
			return
		}
		tok, err := k.sign(cfg, email, now())
		if err != nil {
			http.Error(w, "không ký được thẻ", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(tok + "\n"))
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}
