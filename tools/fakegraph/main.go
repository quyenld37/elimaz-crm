// Lệnh fakegraph là máy Meta Graph API giả cho console dev và bài thử tải (kế hoạch M4, Task 146): cùng máy chủ với
// internal/testkit/fakegraph, nghe HTTP thường ở cổng 4193, có sẵn một Page và một người dùng dev, thêm đường điều
// khiển /_fake/* (lỗi kế tiếp, mức dùng, thu hồi token, comment live, gửi webhook có chữ ký). Trạng thái chỉ trong bộ
// nhớ. Không bao giờ chạy ở production: từ chối khởi động khi CRM_ENV=production.
//
// Biến môi trường: PORT (mặc định 4193), CRM_ENV, FAKEGRAPH_APP_ID, FAKEGRAPH_APP_SECRET (32 ký tự hex; mặc định là
// chuỗi dev công khai, đặt cùng giá trị vào META_APP_SECRET của CRM dev), FAKEGRAPH_PAGE_ID.
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
	"regexp"
	"strconv"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	defaultPort   = 4193
	defaultAppID  = "1626301872631379"
	defaultPageID = "100000000000001"
	// devAppSecret là app secret của máy dev (công khai, không phải bí mật thật).
	devAppSecret  = "fa4e9a9f00000000000000000000de00"
	devPageToken  = "EAAB-dev-page-token" //nolint:gosec // G101: token giả của máy giả
	devUserToken  = "EAAB-dev-user-token" //nolint:gosec // G101: token giả của máy giả
	devLoginCode  = "dev-login-code"
	readTimeout   = 10 * time.Second
	shutdownGrace = 5 * time.Second
)

var (
	errProduction = errors.New("fakegraph không chạy khi CRM_ENV=production")
	errBadSecret  = errors.New("FAKEGRAPH_APP_SECRET phải là 32 ký tự hex")
	errBadPort    = errors.New("PORT không hợp lệ")
	hex32         = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

type config struct {
	port                  int
	appID, secret, pageID string
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{port: defaultPort, appID: defaultAppID, secret: devAppSecret, pageID: defaultPageID}
	if getenv("CRM_ENV") == "production" {
		return c, errProduction
	}
	if v := getenv("PORT"); v != "" {
		p, err := strconv.ParseUint(v, 10, 16)
		if err != nil || p == 0 {
			return c, fmt.Errorf("%w: %q", errBadPort, v)
		}
		c.port = int(p)
	}
	for name, dst := range map[string]*string{"FAKEGRAPH_APP_ID": &c.appID, "FAKEGRAPH_APP_SECRET": &c.secret, "FAKEGRAPH_PAGE_ID": &c.pageID} {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	if !hex32.MatchString(c.secret) {
		return c, errBadSecret
	}
	return c, nil
}

// newServer dựng máy giả có sẵn Page và người dùng dev, cùng đường điều khiển /_fake/*.
func newServer(c config) http.Handler {
	fg := fakegraph.NewHandler(fakegraph.Options{AppID: c.appID, AppSecret: c.secret})
	fg.AddPage(fakegraph.Page{ID: c.pageID, Name: "Shop dev", Token: devPageToken})
	fg.AddUser(fakegraph.User{Code: devLoginCode, Token: devUserToken, PageIDs: []string{c.pageID}})
	mux := http.NewServeMux()
	mountAdmin(mux, fg)
	mux.Handle("/", fg)
	return mux
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	// Công cụ dev riêng, không nhập internal/platform/config (cấu hình của CRM): đọc biến môi trường tại đây.
	err := run(ctx, os.Getenv, logger) //nolint:forbidigo // xem chú thích trên
	stop()
	if err != nil {
		logger.Error("fakegraph dừng vì lỗi", logging.Err(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	c, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(c.port)))
	if err != nil {
		return fmt.Errorf("mở cổng %d: %w", c.port, err)
	}
	srv := &http.Server{Handler: newServer(c), ReadHeaderTimeout: readTimeout}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	logger.Info("fakegraph đang nghe", slog.Int("port", c.port), slog.String("page_id", c.pageID))
	select {
	case serveErr := <-done:
		return fmt.Errorf("máy chủ dừng: %w", serveErr)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	shutErr := srv.Shutdown(sctx)
	if serveErr := <-done; !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("máy chủ dừng: %w", serveErr)
	}
	if shutErr != nil {
		return fmt.Errorf("tắt máy chủ: %w", shutErr)
	}
	return nil
}
