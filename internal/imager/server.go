package imager

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// maxRequestBody: JSON của một việc (một đường dẫn nguồn, tối đa 32 đường dẫn PUT) nhỏ hơn nhiều.
const maxRequestBody = 1 << 20

// Server phục vụ POST /v1/process và GET /health/live. Không xác thực bên gọi (ADR-45: imager không giữ khóa);
// chỉ nghe trên mạng nội bộ `imaging`.
type Server struct {
	runner Runner
	hc     *http.Client
	lim    Limits
	slots  chan struct{}
	log    *slog.Logger
}

// NewServer dựng Server; maxParallel là IMAGER_MAX_PARALLEL; quá số đó thì yêu cầu mới nhận 503 ngay, không xếp hàng.
func NewServer(r Runner, hc *http.Client, lim Limits, maxParallel int, log *slog.Logger) *Server {
	return &Server{runner: r, hc: hc, lim: lim, slots: make(chan struct{}, max(1, maxParallel)), log: log}
}

// Handler trả bảng định tuyến của imager.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/process", s.process)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

func (s *Server) process(w http.ResponseWriter, r *http.Request) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "busy"})
		return
	}
	req, err := decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	start := time.Now()
	res := Process(r.Context(), s.runner, s.hc, req, s.lim)
	// Một dòng log ở mép. Không ghi đường dẫn (có chữ ký) hay nội dung ảnh.
	s.log.LogAttrs(r.Context(), slog.LevelInfo, "xử lý ảnh",
		slog.String("status", res.Status), slog.String("reason", res.Reason),
		slog.String("detected_type", res.DetectedType), slog.Int("width", res.Width), slog.Int("height", res.Height),
		slog.Int("outputs", len(res.Outputs)), slog.Int64("duration_ms", time.Since(start).Milliseconds()))
	writeJSON(w, http.StatusOK, res)
}

var errTrailing = errors.New("dữ liệu thừa sau JSON")

func decode(w http.ResponseWriter, r *http.Request) (Request, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	var req Request
	if err := dec.Decode(&req); err != nil {
		return Request{}, errors.Join(ErrBadRequest, err)
	}
	if dec.More() {
		return Request{}, errors.Join(ErrBadRequest, errTrailing)
	}
	if err := req.Validate(); err != nil {
		return Request{}, err
	}
	return req, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
