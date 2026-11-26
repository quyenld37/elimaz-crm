package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// streamLog là một lần CRM mở luồng comment live /{video}/live_comments: lúc mở, lúc đóng (nil: còn mở). Bộ thử tải M4
// (Task 180) đo "luồng live được nhận lại trong 15 giây" sau khi giết worker từ sổ này.
type streamLog struct {
	Video  string     `json:"video"`
	Opened time.Time  `json:"opened"`
	Closed *time.Time `json:"closed,omitempty"`
}

// streamLogs ghi mọi lần mở luồng; an toàn khi gọi từ nhiều goroutine.
type streamLogs struct {
	mu   sync.Mutex
	logs []streamLog
}

// wrap ghi lúc mở và lúc đóng của mỗi yêu cầu GET .../live_comments rồi chuyển cho next (máy giả tự kiểm token).
func (s *streamLogs) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		video, ok := strings.CutSuffix(strings.Trim(r.URL.Path, "/"), "/live_comments")
		if r.Method != http.MethodGet || !ok {
			next.ServeHTTP(w, r)
			return
		}
		if i := strings.LastIndex(video, "/"); i >= 0 {
			video = video[i+1:]
		}
		s.mu.Lock()
		s.logs = append(s.logs, streamLog{Video: video, Opened: time.Now().UTC()})
		i := len(s.logs) - 1
		s.mu.Unlock()
		defer func() {
			at := time.Now().UTC()
			s.mu.Lock()
			s.logs[i].Closed = &at
			s.mu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}

// serve trả sổ mở luồng (GET /_fake/streams).
func (s *streamLogs) serve(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	out := make([]streamLog, len(s.logs))
	for i, l := range s.logs {
		out[i] = l
		if l.Closed != nil {
			at := *l.Closed
			out[i].Closed = &at
		}
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
