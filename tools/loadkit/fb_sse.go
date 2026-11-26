package main

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// Đo "tới màn hình" của bài thử tải M4 (Task 180): báo hiệu chỉ mang mã hội thoại (conversation.changed), không mang
// mã tin. Client SSE ghi lúc nhận mọi khung của từng hội thoại; khi bài xong, loadkit đọc từ database hội thoại của
// từng sự kiện của lượt (mã sự kiện mang lúc gửi) và lấy độ trễ = khung đầu tiên của hội thoại đó nhận sau lúc gửi.
// Cách này có thể ghép nhầm với khung của một thay đổi khác cùng hội thoại trong khoảng trễ đó (sớm hơn thật): mỗi
// PSID nhận một sự kiện mỗi ~60–130 s nên tỉ lệ nhỏ; ghi trong docs/do-luong/thu-tai-m4.md.

const (
	topicConversationChanged = "conversation.changed"
	entityConversation       = "conversation"
)

// fbSink ghi lúc nhận (ms Unix, tăng dần theo lúc đọc) các khung conversation.changed của mỗi client.
type fbSink struct {
	mu      sync.Mutex
	frames  []map[string][]int64 // client → mã hội thoại → lúc nhận
	clients []clientResult       // chỉ dùng Reconnects, Errors, Foreign (khung khác)
}

func newFBSink(n int) *fbSink {
	s := &fbSink{frames: make([]map[string][]int64, n), clients: make([]clientResult, n)}
	for i := range s.frames {
		s.frames[i] = map[string][]int64{}
	}
	return s
}

func (s *fbSink) changed(i int, data []byte, at time.Time) {
	var p struct {
		Topic  string          `json:"topic"`
		Entity realtime.Entity `json:"entity"`
	}
	ok := json.Unmarshal(data, &p) == nil && p.Topic == topicConversationChanged && p.Entity.Type == entityConversation
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok {
		s.clients[i].Foreign++
		return
	}
	s.frames[i][p.Entity.ID] = append(s.frames[i][p.Entity.ID], at.UnixMilli())
	s.clients[i].Unique++
}

func (s *fbSink) reconnected(i int) {
	s.mu.Lock()
	s.clients[i].Reconnects++
	s.mu.Unlock()
}

func (s *fbSink) failed(i int) {
	s.mu.Lock()
	s.clients[i].Errors++
	s.mu.Unlock()
}

// fbLatency là độ trễ tới báo hiệu của một loại sự kiện, trên mọi cặp (sự kiện, client). Missing: cặp không có khung
// nào sau lúc gửi (tính như vô hạn trong phân vị).
type fbLatency struct {
	Events  int64   `json:"events"`
	Pairs   int64   `json:"pairs"`
	Missing int64   `json:"missing"`
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Max     float64 `json:"max"`
}

type fbSSEResult struct {
	Run     string         `json:"run"`
	Clients []clientResult `json:"clients"`
	Inbox   fbLatency      `json:"inbox"`
	Comment fbLatency      `json:"comment"`
}

// result ghép khung đã nhận với events (sự kiện của lượt run có trong database).
func (s *fbSink) result(run string, events []loadstats.Event) fbSSEResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := fbSSEResult{Run: run, Clients: slices.Clone(s.clients)}
	lat := map[fbEventKind][]float64{}
	acc := map[fbEventKind]*fbLatency{fbInbox: &r.Inbox, fbComment: &r.Comment}
	for _, e := range events {
		kind, sent, ok := parseFBEventID(e.ExternalID, run)
		if !ok {
			continue
		}
		a := acc[kind]
		a.Events++
		for c := range s.frames {
			a.Pairs++
			got := s.frames[c][e.Conversation]
			j, _ := slices.BinarySearch(got, sent.UnixMilli())
			if j == len(got) {
				a.Missing++
				continue
			}
			lat[kind] = append(lat[kind], float64(got[j]-sent.UnixMilli()))
		}
	}
	for kind, a := range acc {
		v := lat[kind]
		slices.Sort(v)
		if len(v) > 0 {
			a.Max = v[len(v)-1]
		}
		a.P50, a.P95, a.P99 = rankWithMissing(v, a.Missing, 50), rankWithMissing(v, a.Missing, 95), rankWithMissing(v, a.Missing, 99)
	}
	return r
}

// missingMs là giá trị của cặp thiếu khung trong phân vị (lớn hơn mọi ngưỡng).
const missingMs = 1e9

// rankWithMissing là phân vị pct của sorted (đã sắp) cộng missing giá trị vô hạn ở cuối; rỗng thì 0.
func rankWithMissing(sorted []float64, missing int64, pct int) float64 {
	n := int64(len(sorted)) + missing
	if n == 0 {
		return 0
	}
	rank := max((int64(pct)*n+99)/100, 1)
	if rank > int64(len(sorted)) {
		return missingMs
	}
	return sorted[rank-1]
}
