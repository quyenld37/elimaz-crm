package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// Số đo phía máy Meta giả của bài thử tải M4 (Task 180): lời gọi Send API ({page}/messages) CRM đã gửi, client_ref
// (message.metadata) có lần nào tới hai lần không, độ trễ từ lúc nhân viên bấm gửi tới lúc máy giả nhận.

// graphCall là phần của một lời gọi máy giả (GET /_fake/calls) bài đo dùng.
type graphCall struct {
	Method string    `json:"Method"`
	Path   string    `json:"Path"`
	Body   []byte    `json:"Body"`
	At     time.Time `json:"At"`
}

// fbGraphStats là số đo của Send API ở máy giả.
type fbGraphStats struct {
	Calls        int64         `json:"calls"`         // lời gọi POST {page}/messages
	WithRef      int64         `json:"with_ref"`      // có client_ref (metadata)
	DuplicateRef int64         `json:"duplicate_ref"` // lời gọi mang client_ref đã thấy
	ClickToAPI   latencyResult `json:"click_to_api"`  // tin nhân viên của lượt: lúc bấm (nội dung) → lúc máy giả nhận
}

// sendStats tính số đo của các lời gọi calls (mọi Page) cho lượt run.
func sendStats(calls []graphCall, run string) fbGraphStats {
	var s fbGraphStats
	seen := map[string]bool{}
	var lat []float64
	for _, c := range calls {
		if c.Method != http.MethodPost {
			continue
		}
		s.Calls++
		var b struct {
			Message struct {
				Text     string `json:"text"`
				Metadata string `json:"metadata"`
			} `json:"message"`
		}
		_ = json.Unmarshal(c.Body, &b) // thân hỏng: không có client_ref, không có dấu lượt
		if ref := b.Message.Metadata; ref != "" {
			s.WithRef++
			if seen[ref] {
				s.DuplicateRef++
			}
			seen[ref] = true
		}
		if at, ok := parseSendMarker(b.Message.Text, run); ok {
			lat = append(lat, float64(c.At.Sub(at).Milliseconds()))
		}
	}
	s.ClickToAPI = summarize(lat)
	return s
}

// summarize trả số lần và phân vị p50, p95, p99, lớn nhất của v (mili giây); rỗng thì chỉ Count = 0.
func summarize(v []float64) latencyResult {
	r := latencyResult{Count: int64(len(v))}
	if len(v) == 0 {
		return r
	}
	slices.Sort(v)
	r.P50, r.P95, r.P99, r.Max = percentile(v, 50), percentile(v, 95), percentile(v, 99), v[len(v)-1]
	return r
}

// getFake đọc GET <graph><path> của máy giả vào out.
func getFake(ctx context.Context, hc *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil) //nolint:gosec // G704: địa chỉ máy giả do người chạy đưa vào cờ -graph
	if err != nil {
		return fmt.Errorf("dựng yêu cầu máy giả: %w", err)
	}
	resp, err := hc.Do(req) //nolint:gosec // G704: địa chỉ máy giả do người chạy (fb_run.sh) đưa vào cờ -graph
	if err != nil {
		return fmt.Errorf("gọi máy giả %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: máy giả %s → %d", errAPI, u, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("đọc kết quả máy giả %s: %w", u, err)
	}
	return nil
}

// readGraphStats đọc lời gọi Send API của mọi Page trong f ở máy giả graph và tính số đo của lượt run.
func readGraphStats(ctx context.Context, hc *http.Client, graph string, f fbFixture, run string) (fbGraphStats, error) {
	var all []graphCall
	for _, p := range f.Pages {
		var cs []graphCall
		if err := getFake(ctx, hc, graph+"/_fake/calls?path="+url.QueryEscape(p.PageID+"/messages"), &cs); err != nil {
			return fbGraphStats{}, err
		}
		all = append(all, cs...)
	}
	return sendStats(all, run), nil
}
