package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// Mã sự kiện của fb_common.js: k6 dựng, loadkit đọc lại lúc gửi; lượt khác, dạng sai → không nhận.
func TestFBEventID(t *testing.T) {
	t.Parallel()
	sent := time.UnixMilli(1_763_632_800_123)
	for _, k := range []fbEventKind{fbInbox, fbComment} {
		kind, at, ok := parseFBEventID(fbEventID(k, "fb1", sent, 3, 7), "fb1")
		if !ok || kind != k || !at.Equal(sent) {
			t.Errorf("%s: %v %v %v", k, kind, at, ok)
		}
	}
	for _, bad := range []string{fbEventID(fbInbox, "fb0", sent, 1, 1), "m_fb1_x_1_1", "x_fb1_1_1_1", "m_fb1_1_1", "mid.123"} {
		if _, _, ok := parseFBEventID(bad, "fb1"); ok {
			t.Errorf("%q phải không nhận", bad)
		}
	}
	if at, ok := parseSendMarker("lt fb1 1763632800123 4 9", "fb1"); !ok || !at.Equal(sent) {
		t.Errorf("dấu tin gửi: %v %v", at, ok)
	}
	for _, bad := range []string{"lt fb0 1 1 1", "lt fb1 x 1 1", "Shop chào bạn"} {
		if _, ok := parseSendMarker(bad, "fb1"); ok {
			t.Errorf("%q phải không nhận", bad)
		}
	}
}

func frame(conv string) []byte {
	return fmt.Appendf(nil, `{"topic":"conversation.changed","entity":{"type":"conversation","id":%q}}`, conv)
}

// Độ trễ = khung đầu tiên của hội thoại của sự kiện nhận sau lúc gửi, ở từng client; client không có khung nào sau
// lúc gửi là cặp thiếu và đẩy phân vị lên (không bị bỏ qua).
func TestFBSink_Result(t *testing.T) {
	t.Parallel()
	sent := time.UnixMilli(1_000_000)
	s := newFBSink(2)
	s.changed(0, frame("c1"), sent.Add(-time.Second)) // khung cũ của hội thoại, trước lúc gửi: không tính
	s.changed(0, frame("c1"), sent.Add(300*time.Millisecond))
	s.changed(0, frame("c1"), sent.Add(900*time.Millisecond))
	s.changed(1, frame("c1"), sent.Add(500*time.Millisecond))
	s.changed(0, frame("c2"), sent.Add(2500*time.Millisecond))
	s.changed(0, []byte(`{"topic":"live.comments","entity":{"type":"live_session","id":"s"}}`), sent)
	s.reconnected(1)
	s.failed(1)
	r := s.result("fb1", []loadstats.Event{
		{ExternalID: fbEventID(fbInbox, "fb1", sent, 1, 1), Conversation: "c1"},
		{ExternalID: fbEventID(fbComment, "fb1", sent, 1, 2), Conversation: "c2"},
		{ExternalID: fbEventID(fbInbox, "fb0", sent, 1, 1), Conversation: "c1"}, // lượt khác
	})
	if r.Inbox.Events != 1 || r.Inbox.Pairs != 2 || r.Inbox.Missing != 0 || r.Inbox.P50 != 300 || r.Inbox.Max != 500 {
		t.Errorf("tin: %+v", r.Inbox)
	}
	if r.Comment.Events != 1 || r.Comment.Pairs != 2 || r.Comment.Missing != 1 || r.Comment.P50 != 2500 ||
		r.Comment.P95 != missingMs {
		t.Errorf("comment (client 1 không nhận khung c2): %+v", r.Comment)
	}
	if r.Clients[0].Foreign != 1 || r.Clients[1].Reconnects != 1 || r.Clients[1].Errors != 1 {
		t.Errorf("client: %+v", r.Clients)
	}
}

func TestRankWithMissing(t *testing.T) {
	t.Parallel()
	v := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := rankWithMissing(v, 0, 95); got != 10 {
		t.Errorf("p95 không thiếu: %v", got)
	}
	if got := rankWithMissing(v, 1, 95); got != missingMs {
		t.Errorf("p95 có một cặp thiếu trong 11: %v", got)
	}
	if got := rankWithMissing(v, 1, 50); got != 6 {
		t.Errorf("p50: %v", got)
	}
	if rankWithMissing(nil, 0, 95) != 0 {
		t.Error("rỗng")
	}
}

func sendCall(t *testing.T, text, ref string, at time.Time) graphCall {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"recipient": map[string]string{"id": "7"},
		"message":   map[string]string{"text": text, "metadata": ref},
	})
	if err != nil {
		t.Fatal(err)
	}
	return graphCall{Method: "POST", Path: "1/messages", Body: b, At: at}
}

// client_ref (metadata) tới máy giả hai lần thì đếm; độ trễ bấm → Send API chỉ tính tin của lượt.
func TestSendStats(t *testing.T) {
	t.Parallel()
	t0 := time.UnixMilli(5_000_000)
	calls := []graphCall{
		sendCall(t, fmt.Sprintf("lt fb1 %d 1 1", t0.UnixMilli()), "r1", t0.Add(200*time.Millisecond)),
		sendCall(t, fmt.Sprintf("lt fb1 %d 1 2", t0.UnixMilli()), "r2", t0.Add(700*time.Millisecond)),
		sendCall(t, "Anh chị cho shop xin địa chỉ", "r3", t0),
		sendCall(t, "Anh chị cho shop xin địa chỉ", "r3", t0), // gửi lại cùng client_ref
		sendCall(t, "lt fb0 1 1 1", "", t0),
		{Method: "GET", Path: "1/messages"},
	}
	s := sendStats(calls, "fb1")
	if s.Calls != 5 || s.WithRef != 4 || s.DuplicateRef != 1 {
		t.Errorf("%+v", s)
	}
	if s.ClickToAPI.Count != 2 || s.ClickToAPI.P50 != 200 || s.ClickToAPI.Max != 700 {
		t.Errorf("độ trễ: %+v", s.ClickToAPI)
	}
}
