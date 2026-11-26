// Package server là máy chủ HTTP của hãng vận chuyển giả (chương trình tools/fakecarrier): API tạo, tra, đọc kiện
// có xác thực (Bearer bí mật webhook), trang /admin đổi trạng thái kiện (gửi webhook có chữ ký) và đổi chế độ lỗi.
// Tách khỏi package main để test của CRM dựng máy chủ trong httptest.
//
// Khi nào không dùng: chỉ cho dev, staging và test; không bao giờ chạy ở production.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// maxBodyBytes giới hạn thân yêu cầu đọc vào.
const maxBodyBytes = 64 << 10

// defaultHang là thời gian giữ yêu cầu ở chế độ timeout_* khi Options.Hang bằng 0.
const defaultHang = 30 * time.Second

// retryAfterSeconds là Retry-After của chế độ rate_limited.
const retryAfterSeconds = "7"

// Options là cấu hình của máy chủ: kho trạng thái (nil → chỉ trong bộ nhớ), bí mật Bearer và chữ ký webhook, URL nhận
// webhook (rỗng: không gửi), thời gian giữ yêu cầu ở chế độ timeout_after_create, client gửi webhook, logger, phí báo
// cố định (0: theo công thức cân nặng, tiền; > 0: một dịch vụ std đúng số này, cho e2e website, Task 511).
type Options struct {
	Store      *Store
	Secret     string
	WebhookURL string
	Hang       time.Duration
	HTTPClient *http.Client
	Logger     *slog.Logger
	QuoteFee   int64
}

// Server là bộ xử lý HTTP của hãng giả.
type Server struct {
	http.Handler

	st     *Store
	cfg    Options
	hc     *http.Client
	logger *slog.Logger
	now    func() time.Time

	mu    sync.Mutex
	calls map[string]int
}

// New dựng các route: /v1/* (Bearer), /admin/* (không xác thực: công cụ dev chỉ nghe 127.0.0.1), /health.
func New(o Options) *Server {
	st := o.Store
	if st == nil {
		st, _ = OpenStore("") // đường rỗng không đọc tệp, không lỗi
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if o.Hang <= 0 {
		o.Hang = defaultHang
	}
	s := &Server{st: st, cfg: o, hc: hc, logger: logger, now: time.Now, calls: map[string]int{}}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/parcels", s.auth(opCreate, s.createParcel))
	mux.Handle("GET /v1/parcels", s.auth("lookup", s.parcelByCode))
	mux.Handle("GET /v1/parcels/{tracking}", s.auth("status", s.parcelByTracking))
	s.routesM3(mux)
	mux.HandleFunc("POST /admin/parcels/{tracking}/advance", s.advance)
	mux.HandleFunc("POST /admin/mode", s.setMode)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int{"parcels": st.count()})
	})
	s.Handler = mux
	return s
}

// auth kiểm Bearer rồi đếm một lần gọi thao tác op (Calls).
func (s *Server) auth(op string, next http.HandlerFunc) http.Handler {
	want := []byte("Bearer " + s.cfg.Secret)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		s.mu.Lock()
		s.calls[op]++
		s.mu.Unlock()
		next(w, r)
	})
}

// failMode trả lời theo chế độ lỗi chung (reject 422, auth_revoked 401, rate_limited 429 Retry-After: 7); true khi đã
// trả lời.
func failMode(w http.ResponseWriter, mode string) bool {
	switch mode {
	case modeReject:
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "rejected"})
	case modeAuthRevoked:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token_revoked"})
	case modeRateLimited:
		w.Header().Set("Retry-After", retryAfterSeconds)
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
	default:
		return false
	}
	return true
}

type createReq struct {
	Code       string `json:"code"`
	CODAmount  int64  `json:"codAmount"`
	AccountKey string `json:"accountKey"`
	RouteKey   string `json:"routeKey"`
}

// createParcel: fail_before_create → 503 (chưa tạo); reject, auth_revoked, rate_limited → 422, 401, 429 (chưa tạo);
// timeout_after_create → tạo rồi giữ yêu cầu tới khi bên gọi bỏ; ok, forge → 200 {tracking, fee}; cùng code lần hai
// trả kiện cũ.
func (s *Server) createParcel(w http.ResponseWriter, r *http.Request) {
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
	if failMode(w, mode) {
		return
	}
	p, err := s.st.create(in.Code, in.AccountKey, in.RouteKey, in.CODAmount, s.now())
	if err != nil {
		s.logger.Error("lưu kiện", logging.Err(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	if mode == modeTimeoutAfterCreate {
		select {
		case <-r.Context().Done():
		case <-time.After(s.cfg.Hang):
		}
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "timeout"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) parcelByCode(w http.ResponseWriter, r *http.Request) {
	p, ok := s.st.byCode(r.URL.Query().Get("code"), r.URL.Query().Get("accountKey"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) parcelByTracking(w http.ResponseWriter, r *http.Request) {
	if p, ok := s.pathParcel(w, r); ok {
		writeJSON(w, http.StatusOK, p)
	}
}

// pathParcel đọc kiện theo {tracking} của đường; không có → trả 404, ok = false.
func (s *Server) pathParcel(w http.ResponseWriter, r *http.Request) (parcel, bool) {
	p, ok := s.st.byTracking(r.PathValue("tracking"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	}
	return p, ok
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
