package realtime

import (
	"bytes"
	"net/http"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// Mặc định của ServeSSE (spec 04 mục 6).
const (
	defaultBatch      = 300 * time.Millisecond
	defaultHeartbeat  = 20 * time.Second
	defaultMaxPending = 1000
	writeTimeout      = 10 * time.Second
)

// SSEOptions cấu hình luồng SSE; giá trị 0 lấy mặc định.
type SSEOptions struct {
	// Batch: khung đầu tiên chờ gửi mở một lô; sau Batch (mặc định 300 ms) gửi cả lô, khung giống hệt nhau một lần.
	Batch time.Duration
	// Heartbeat: gửi ": ping" mỗi Heartbeat (mặc định 20 s), chỉ khi Bus đang nghe được.
	Heartbeat time.Duration
	// MaxPending: lô có quá số khung khác nhau này thì thay cả lô bằng một resync (mặc định 1.000).
	MaxPending int
	// Stop đóng thì mọi luồng kết thúc (cmd/crm đóng nó khi tắt máy chủ HTTP: Shutdown không tự cắt luồng dài).
	Stop <-chan struct{}
}

func (o SSEOptions) withDefaults() SSEOptions {
	if o.Batch <= 0 {
		o.Batch = defaultBatch
	}
	if o.Heartbeat <= 0 {
		o.Heartbeat = defaultHeartbeat
	}
	if o.MaxPending <= 0 {
		o.MaxPending = defaultMaxPending
	}
	return o
}

// ServeSSE phục vụ GET /api/v1/stream cho nhân viên u (lớp HTTP đã xác thực, kiểm realtime.subscribe): đăng ký vào
// bus, gửi khung đầu "event: resync", rồi các lô khung changed; ": ping" mỗi Heartbeat khi bus đang nghe. Trả khi
// client đóng, ghi lỗi, hoặc o.Stop đóng; hủy đăng ký trước khi trả.
func ServeSSE(w http.ResponseWriter, r *http.Request, bus Bus, u identity.AuthUser, o SSEOptions) {
	o = o.withDefaults()
	q := newPending(o.MaxPending)
	cancel := bus.Subscribe(u, q.add)
	defer cancel()

	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // cổng vào không giữ lại luồng
	w.WriteHeader(http.StatusOK)
	if !write(w, rc, []Frame{resyncFrame()}) {
		return
	}
	heartbeat := time.NewTicker(o.Heartbeat)
	defer heartbeat.Stop()
	batch := time.NewTimer(o.Batch)
	batch.Stop()
	armed := false
	defer batch.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-o.Stop:
			return
		case <-q.ready:
			if !armed {
				batch.Reset(o.Batch)
				armed = true
			}
		case <-batch.C:
			armed = false
			if !write(w, rc, q.take()) {
				return
			}
		case <-heartbeat.C:
			if listening, _ := bus.Status(); listening && !writePing(w, rc) {
				return
			}
		}
	}
}

// write ghi các khung rồi Flush; false khi không ghi được (client đã đi).
func write(w http.ResponseWriter, rc *http.ResponseController, frames []Frame) bool {
	if len(frames) == 0 {
		return true
	}
	var b bytes.Buffer
	for _, f := range frames {
		b.WriteString("event: ")
		b.WriteString(f.Event)
		b.WriteString("\ndata: ")
		b.Write(f.Data)
		b.WriteString("\n\n")
	}
	return flush(w, rc, b.Bytes())
}

func writePing(w http.ResponseWriter, rc *http.ResponseController) bool {
	return flush(w, rc, []byte(": ping\n\n"))
}

// flush ghi b có hạn writeTimeout (client treo không giữ goroutine mãi) rồi Flush.
func flush(w http.ResponseWriter, rc *http.ResponseController, b []byte) bool {
	_ = rc.SetWriteDeadline(time.Now().Add(writeTimeout)) // writer không hỗ trợ hạn: vẫn ghi
	if _, err := w.Write(b); err != nil {
		return false
	}
	return rc.Flush() == nil
}

// pending là lô khung chờ gửi của một kết nối: add không chặn (gọi từ hub), khung giống hệt nhau chỉ giữ một.
type pending struct {
	max   int
	ready chan struct{} // có khung mới (đệm 1)

	mu       sync.Mutex
	frames   []Frame
	seen     map[string]struct{}
	overflow bool
}

func newPending(maxFrames int) *pending {
	return &pending{max: maxFrames, ready: make(chan struct{}, 1), seen: map[string]struct{}{}}
}

func (p *pending) add(f Frame) {
	key := f.Event + "\x00" + string(f.Data)
	p.mu.Lock()
	switch {
	case p.overflow:
	case len(p.frames) >= p.max:
		// Quá nhiều khung khác nhau: màn hình tải lại hết thay cho từng khung (báo hiệu được phép gộp).
		p.overflow = true
		p.frames = []Frame{resyncFrame()}
	default:
		if _, ok := p.seen[key]; !ok {
			p.seen[key] = struct{}{}
			p.frames = append(p.frames, f)
		}
	}
	p.mu.Unlock()
	select {
	case p.ready <- struct{}{}:
	default:
	}
}

// take lấy cả lô và làm rỗng.
func (p *pending) take() []Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.frames
	p.frames, p.seen, p.overflow = nil, map[string]struct{}{}, false
	return out
}
