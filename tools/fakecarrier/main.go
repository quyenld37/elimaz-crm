// Lệnh fakecarrier là hãng vận chuyển giả cho dev và staging (kế hoạch M1, Task M1-31; Q2): API tạo, tra, đọc kiện
// có xác thực (Bearer FAKECARRIER_WEBHOOK_SECRET), trang /admin để đổi trạng thái kiện (gửi webhook có chữ ký tới
// FAKECARRIER_WEBHOOK_URL) và đổi chế độ lỗi. Trạng thái trong bộ nhớ, ghi tệp JSON (FAKECARRIER_DATA) để sống qua
// khởi động lại. Không bao giờ chạy ở production: CRM chỉ nối khi CRM_ENV khác production.
//
// Biến môi trường: PORT (mặc định 4194), FAKECARRIER_WEBHOOK_SECRET (≥ 32 ký tự; mặc định là chuỗi dev công khai),
// FAKECARRIER_WEBHOOK_URL (rỗng: không gửi webhook), FAKECARRIER_DATA (mặc định /tmp/fakecarrier.json; "-" chỉ trong
// bộ nhớ).
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
)

const (
	defaultPort   = 4194
	defaultData   = "/tmp/fakecarrier.json"
	minSecretLen  = 32
	maxBodyBytes  = 64 << 10
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
	st, err := openStore(cfg.dataPath)
	if err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("", strconv.Itoa(cfg.port)))
	if err != nil {
		return fmt.Errorf("mở cổng %d: %w", cfg.port, err)
	}
	srv := &http.Server{Handler: newServer(st, cfg, &http.Client{}, logger), ReadHeaderTimeout: readTimeout}
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

// server là bộ xử lý HTTP của hãng giả.
type server struct {
	st     *store
	cfg    config
	hc     *http.Client
	logger *slog.Logger
	now    func() time.Time
}

// newServer dựng các route: /v1/* (Bearer), /admin/* (không xác thực: công cụ dev chỉ nghe 127.0.0.1), /health.
func newServer(st *store, cfg config, hc *http.Client, logger *slog.Logger) http.Handler {
	s := &server{st: st, cfg: cfg, hc: hc, logger: logger, now: time.Now}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/parcels", s.auth(s.createParcel))
	mux.Handle("GET /v1/parcels", s.auth(s.parcelByCode))
	mux.Handle("GET /v1/parcels/{tracking}", s.auth(s.parcelByTracking))
	mux.HandleFunc("POST /admin/parcels/{tracking}/advance", s.advance)
	mux.HandleFunc("POST /admin/mode", s.setMode)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int{"parcels": st.count()})
	})
	return mux
}

func (s *server) auth(next http.HandlerFunc) http.Handler {
	want := []byte("Bearer " + s.cfg.secret)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		next(w, r)
	})
}

type createReq struct {
	Code       string `json:"code"`
	CODAmount  int64  `json:"codAmount"`
	AccountKey string `json:"accountKey"`
}

// createParcel: fail_before_create → 503 (chưa tạo); timeout_after_create → tạo rồi giữ yêu cầu tới khi bên gọi bỏ;
// ok, forge → 200 {tracking, fee}; cùng code lần hai trả kiện cũ.
func (s *server) createParcel(w http.ResponseWriter, r *http.Request) {
	var in createReq
	if err := decode(r, &in); err != nil || in.Code == "" || in.AccountKey == "" || in.CODAmount < 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_parcel"})
		return
	}
	mode := s.st.mode()
	if mode == modeFailBeforeCreate {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not_created"})
		return
	}
	p, err := s.st.create(in.Code, in.AccountKey, in.CODAmount, s.now())
	if err != nil {
		s.logger.Error("lưu kiện", logging.Err(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if mode == modeTimeoutAfterCreate {
		select {
		case <-r.Context().Done():
		case <-time.After(s.cfg.hang):
		}
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "timeout"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) parcelByCode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.st.byCode(r.URL.Query().Get("code"), r.URL.Query().Get("accountKey"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) parcelByTracking(w http.ResponseWriter, r *http.Request) {
	p, ok := s.st.byTracking(r.PathValue("tracking"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(v); err != nil {
		return fmt.Errorf("đọc thân: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
