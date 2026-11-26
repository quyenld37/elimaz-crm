package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/selftest"
)

// eventID dựng eventId của một ping: lt.<run>.<ms gửi>.<VU>.<lượt>. k6 (common.js) dựng đúng dạng này; client SSE
// đọc lại lúc gửi để tính độ trễ webhook → báo hiệu (cùng một máy nên cùng đồng hồ).
func eventID(run string, sent time.Time, vu, iter int) string {
	return fmt.Sprintf("lt.%s.%d.%d.%d", run, sent.UnixMilli(), vu, iter)
}

// commandKey là dedupe_key của lệnh command.selftest thứ n mà chaos.sh thêm trong lượt run.
func commandKey(run string, n int) string { return fmt.Sprintf("lt.%s.cmd.%d", run, n) }

// parseEventID tách run và lúc gửi của eventId; ok false khi không đúng dạng.
func parseEventID(id string) (string, time.Time, bool) {
	parts := strings.Split(id, ".")
	if len(parts) != 5 || parts[0] != "lt" || parts[1] == "" {
		return "", time.Time{}, false
	}
	ms, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	for _, p := range parts[3:] {
		if _, err := strconv.Atoi(p); err != nil {
			return "", time.Time{}, false
		}
	}
	return parts[1], time.UnixMilli(ms), true
}

// clientResult là số đo của một client SSE.
type clientResult struct {
	Unique     int64 `json:"unique"`     // eventId của lượt chạy, nhận lần đầu
	Duplicates int64 `json:"duplicates"` // nhận lại eventId đã nhận
	Foreign    int64 `json:"foreign"`    // pong không thuộc lượt chạy
	Reconnects int64 `json:"reconnects"` // nối lại sau khi đã nối được
	Errors     int64 `json:"errors"`     // lần nối hỏng (mạng, mã khác 200)
}

// latencyResult: độ trễ webhook → báo hiệu (mili giây), tính trên lần nhận đầu ở mỗi client.
type latencyResult struct {
	Count int64   `json:"count"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
}

type sseResult struct {
	Run     string         `json:"run"`
	Clients []clientResult `json:"clients"`
	Latency latencyResult  `json:"latency"`
}

// collector gom số đo của mọi client; an toàn khi gọi từ nhiều goroutine.
type collector struct {
	run string

	mu      sync.Mutex
	clients []clientResult
	seen    []map[string]struct{}
	lat     []float64
}

func newCollector(n int, run string) *collector {
	c := &collector{run: run, clients: make([]clientResult, n), seen: make([]map[string]struct{}, n)}
	for i := range c.seen {
		c.seen[i] = map[string]struct{}{}
	}
	return c
}

// record ghi một pong eventID mà client i nhận lúc at.
func (c *collector) record(i int, id string, at time.Time) {
	run, sent, ok := parseEventID(id)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !ok || run != c.run {
		c.clients[i].Foreign++
		return
	}
	if _, dup := c.seen[i][id]; dup {
		c.clients[i].Duplicates++
		return
	}
	c.seen[i][id] = struct{}{}
	c.clients[i].Unique++
	c.lat = append(c.lat, float64(at.Sub(sent).Milliseconds()))
}

// changed nhận data của một khung changed mà client i đọc lúc at: chỉ đếm khung selftest.pong.
func (c *collector) changed(i int, data []byte, at time.Time) {
	if id, ok := pongID(data); ok {
		c.record(i, id, at)
	}
}

func (c *collector) reconnected(i int) {
	c.mu.Lock()
	c.clients[i].Reconnects++
	c.mu.Unlock()
}

func (c *collector) failed(i int) {
	c.mu.Lock()
	c.clients[i].Errors++
	c.mu.Unlock()
}

func (c *collector) result() sseResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	lat := slices.Clone(c.lat)
	r := sseResult{Run: c.run, Clients: slices.Clone(c.clients), Latency: latencyResult{Count: int64(len(lat))}}
	if len(lat) > 0 {
		slices.Sort(lat)
		r.Latency.P50, r.Latency.P95, r.Latency.P99 = percentile(lat, 50), percentile(lat, 95), percentile(lat, 99)
		r.Latency.Max = lat[len(lat)-1]
	}
	return r
}

// percentile trả phân vị pct theo hạng gần nhất (giá trị thứ ⌈pct·n/100⌉ sau khi sắp); v rỗng thì 0.
func percentile(v []float64, pct int) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	rank := int(math.Ceil(float64(pct) * float64(len(s)) / 100))
	return s[max(rank, 1)-1]
}

type sseClientOptions struct {
	Base       string // gốc api, ví dụ http://127.0.0.1:4190
	Token      string // thẻ Access của nhân viên
	Index      int
	MinBackoff time.Duration // lần nối lại đầu; gấp đôi mỗi lần hỏng liền, tối đa 5 s
	HTTP       *http.Client
}

const maxBackoff = 5 * time.Second

// frameSink nhận khung changed và biến cố nối của các client SSE; an toàn khi gọi từ nhiều goroutine.
type frameSink interface {
	changed(i int, data []byte, at time.Time) // data của khung changed client i đọc lúc at
	reconnected(i int)                        // client i nối lại được sau lần nối được đầu tiên
	failed(i int)                             // client i nối hỏng (mạng, mã khác 200)
}

// runClient giữ một luồng SSE tới khi ctx hủy: đưa mọi khung changed cho col, mất luồng thì nối lại có lùi
// dần (mỗi lần nối được sau lần nối được đầu tiên tính là Reconnects, nối hỏng tính là Errors).
func runClient(ctx context.Context, o sseClientOptions, col frameSink) {
	if o.HTTP == nil {
		o.HTTP = &http.Client{} // không đặt Timeout: hạn đó cắt cả luồng dài; hủy bằng ctx
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = 200 * time.Millisecond
	}
	backoff := o.MinBackoff
	connected := false
	onOpen := func() {
		if connected {
			col.reconnected(o.Index)
		}
		connected = true
	}
	for ctx.Err() == nil {
		ok := streamOnce(ctx, o, col, onOpen)
		if ctx.Err() != nil {
			return
		}
		if ok {
			backoff = o.MinBackoff
		} else {
			col.failed(o.Index)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if !ok {
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// streamOnce mở một luồng và đọc tới khi luồng đóng; trả true khi đã nhận 200 text/event-stream (lúc đó gọi onOpen).
func streamOnce(ctx context.Context, o sseClientOptions, col frameSink, onOpen func()) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Base+"/api/v1/stream", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", o.Token)
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return false
	}
	onOpen()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var event string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == realtime.EventChanged:
			col.changed(o.Index, []byte(strings.TrimPrefix(line, "data: ")), time.Now())
		}
	}
	return true
}

// pongID trả eventId của khung changed selftest.pong; ok false với khung khác.
func pongID(data []byte) (string, bool) {
	var p struct {
		Topic  string          `json:"topic"`
		Entity realtime.Entity `json:"entity"`
	}
	if err := json.Unmarshal(data, &p); err != nil || p.Topic != selftest.PongTopic || p.Entity.Type != selftest.Key {
		return "", false
	}
	return p.Entity.ID, true
}
