// Package observability phục vụ các điểm kiểm sức khỏe của tiến trình (/health/live, /health/ready). Cảnh báo, kiểm
// sâu và theo dõi PostgreSQL thêm ở Task 19, 20.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Hạn của lần hỏi database trong /health/ready.
const readyTimeout = time.Second

// Pinger là database mà /health/ready hỏi; *pgxpool.Pool thỏa interface này.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health phục vụ /health/live và /health/ready.
type Health struct {
	db Pinger
}

// NewHealth tạo Health hỏi db.
func NewHealth(db Pinger) *Health { return &Health{db: db} }

// Register đăng ký GET /health/live và GET /health/ready vào mux.
func (h *Health) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", h.live)
	mux.HandleFunc("GET /health/ready", h.ready)
}

func (h *Health) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, `{"status":"ok"}`)
}

// Check hỏi database với hạn 1 s; nil nghĩa là sẵn sàng. Dùng chung cho /health/ready của mọi vai trò.
func (h *Health) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		return fmt.Errorf("hỏi database: %w", err)
	}
	return nil
}

// ready trả 200 khi database trả lời trong 1 s, ngược lại 503. Không nêu lỗi gốc ra ngoài.
func (h *Health) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.Check(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, `{"status":"unavailable","checks":{"db":"down"}}`)
		return
	}
	writeJSON(w, http.StatusOK, `{"status":"ok"}`)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
