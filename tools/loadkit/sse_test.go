package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventIDRoundTrip(t *testing.T) {
	sent := time.UnixMilli(1_763_456_789_123)
	id := eventID("r1", sent, 3, 7)
	if id != "lt.r1.1763456789123.3.7" {
		t.Fatalf("eventID = %q", id)
	}
	run, at, ok := parseEventID(id)
	if !ok || run != "r1" || !at.Equal(sent) {
		t.Fatalf("parseEventID = %q %v %v", run, at, ok)
	}
	for _, bad := range []string{"", "lt", "x.r1.1.1.1", "lt.r1.abc.1.1", "lt.r1.1.1", "lt..1.1.1"} {
		if _, _, ok := parseEventID(bad); ok {
			t.Fatalf("parseEventID(%q) phải lỗi", bad)
		}
	}
}

// Bộ gom: đếm duy nhất và trùng theo từng client, bỏ eventId của lượt chạy khác, độ trễ = lúc nhận − lúc gửi.
func TestCollector(t *testing.T) {
	c := newCollector(2, "r1")
	sent := time.UnixMilli(1_000_000)
	a, b := eventID("r1", sent, 1, 1), eventID("r1", sent, 1, 2)
	c.record(0, a, sent.Add(100*time.Millisecond))
	c.record(0, a, sent.Add(150*time.Millisecond)) // trùng
	c.record(0, b, sent.Add(200*time.Millisecond))
	c.record(1, a, sent.Add(300*time.Millisecond))
	c.record(1, eventID("r0", sent, 1, 1), sent.Add(time.Millisecond)) // lượt khác
	c.record(1, "khong-phai-id", sent)
	c.reconnected(1)
	r := c.result()
	if r.Run != "r1" || len(r.Clients) != 2 {
		t.Fatalf("result = %+v", r)
	}
	if got := r.Clients[0]; got.Unique != 2 || got.Duplicates != 1 || got.Reconnects != 0 {
		t.Fatalf("client 0 = %+v", got)
	}
	if got := r.Clients[1]; got.Unique != 1 || got.Duplicates != 0 || got.Reconnects != 1 || got.Foreign != 2 {
		t.Fatalf("client 1 = %+v", got)
	}
	// Độ trễ chỉ tính lần nhận đầu ở mỗi client: 100, 200, 300.
	if r.Latency.Count != 3 || r.Latency.Max != 300 || r.Latency.P50 != 200 || r.Latency.P99 != 300 {
		t.Fatalf("latency = %+v", r.Latency)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	v := make([]float64, 100)
	for i := range v {
		v[i] = float64(100 - i) // 100 … 1, chưa sắp
	}
	if p := percentile(v, 95); p != 95 {
		t.Fatalf("p95 = %v", p)
	}
	if p := percentile(v, 99); p != 99 {
		t.Fatalf("p99 = %v", p)
	}
	if p := percentile(nil, 95); p != 0 {
		t.Fatalf("rỗng = %v", p)
	}
}

func changedFrame(id string) string {
	return fmt.Sprintf("event: changed\ndata: {\"topic\":\"selftest.pong\",\"entity\":{\"type\":\"selftest\",\"id\":%q}}\n\n", id)
}

// Client SSE gửi thẻ đúng header, đọc khung changed selftest.pong (bỏ khung khác), máy chủ đóng thì tự nối lại.
func TestSSEClientReadsAndReconnects(t *testing.T) {
	sent := time.Now()
	a, b := eventID("r1", sent, 1, 1), eventID("r1", sent, 1, 2)
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stream" || r.Header.Get("Cf-Access-Jwt-Assertion") != "tok" {
			http.Error(w, "sai", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: resync\ndata: {}\n\n: ping\n\n")
		switch conns.Add(1) {
		case 1:
			_, _ = fmt.Fprint(w, changedFrame(a))
			_, _ = fmt.Fprint(w, "event: changed\ndata: {\"topic\":\"khac\",\"entity\":{\"type\":\"x\",\"id\":\"1\"}}\n\n")
			fl.Flush()
		default:
			_, _ = fmt.Fprint(w, changedFrame(b))
			fl.Flush()
			<-r.Context().Done()
		}
	}))
	defer srv.Close()
	col := newCollector(1, "r1")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runClient(ctx, sseClientOptions{Base: srv.URL, Token: "tok", Index: 0, MinBackoff: 10 * time.Millisecond}, col)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for col.result().Clients[0].Unique < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("hết hạn: %+v", col.result())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	got := col.result().Clients[0]
	if got.Unique != 2 || got.Duplicates != 0 || got.Reconnects != 1 {
		t.Fatalf("client = %+v", got)
	}
}

// Mã khác 200 (thẻ sai): client không đếm gì, thử lại có lùi dần, dừng khi ctx hủy.
func TestSSEClientRetriesOnError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "sai", http.StatusUnauthorized)
	}))
	defer srv.Close()
	col := newCollector(1, "r1")
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	runClient(ctx, sseClientOptions{Base: srv.URL, Token: "x", Index: 0, MinBackoff: 20 * time.Millisecond}, col)
	if n := hits.Load(); n < 2 || n > 10 {
		t.Fatalf("%d lần thử trong 300 ms, muốn lùi dần (2..10)", n)
	}
	if r := col.result(); r.Clients[0].Unique != 0 || r.Clients[0].Errors < 2 {
		t.Fatalf("client = %+v", r.Clients[0])
	}
}
