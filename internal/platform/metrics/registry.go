// Package metrics gom chỉ số của tiến trình trong bộ nhớ và ghi mỗi phút một lần vào platform.metrics_minute
// (spec 04 mục "Đo lường"; không có /metrics, ADR C6), ghi nhịp sống vào platform.process_heartbeats và gửi nhịp
// sống ra dịch vụ bên ngoài (ADR-38).
package metrics

import (
	"math"
	"slices"
	"sync"
)

// HistBoundsMs là cận trên (mili giây, tính cả cận) của các ngăn histogram; ngăn cuối thêm vào là "lớn hơn 5000".
//
//nolint:gochecknoglobals // Interfaces của kế hoạch M0 Task 10 khai là var; chỉ đọc, không mã nào gán lại.
var HistBoundsMs = []float64{50, 100, 200, 300, 400, 500, 1000, 2000, 5000}

// Kind là loại của một chỉ số. Một tên chỉ số chỉ dùng một loại.
type Kind string

// Ba loại chỉ số. Giá trị cũng là tham số kind của câu UpsertMetricMinute.
const (
	KindHistogram Kind = "h"
	KindCounter   Kind = "c"
	KindGauge     Kind = "g"
)

// Row là một chỉ số đã gom của một phút.
//
//   - Histogram: Count số lần đo, Sum tổng, Max lớn nhất, Hist số lần rơi vào từng ngăn (len(HistBoundsMs)+1).
//   - Counter: Count tổng số đã cộng, Sum bằng Count, Max 0, Hist nil.
//   - Gauge: Count số lần đặt, Sum giá trị cuối, Max giá trị lớn nhất, Hist nil.
type Row struct {
	Name, Label string
	Kind        Kind
	Count       int64
	Sum, Max    float64
	Hist        []int
}

type key struct{ name, label string }

// Registry gom chỉ số trong bộ nhớ, an toàn khi gọi từ nhiều goroutine. Giá trị bằng 0 không dùng được: tạo bằng
// NewRegistry.
type Registry struct {
	mu     sync.Mutex
	hists  map[key]*Row
	counts map[key]*Row
	gauges map[key]*Row
}

// NewRegistry tạo Registry rỗng.
func NewRegistry() *Registry {
	r := &Registry{}
	r.reset()
	return r
}

func (r *Registry) reset() {
	r.hists, r.counts, r.gauges = map[key]*Row{}, map[key]*Row{}, map[key]*Row{}
}

// Observe ghi một lần đo v (mili giây) của histogram name, nhãn label.
func (r *Registry) Observe(name string, v float64, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.hists[key{name, label}]
	if row == nil {
		row = &Row{Name: name, Label: label, Kind: KindHistogram, Max: v, Hist: make([]int, len(HistBoundsMs)+1)}
		r.hists[key{name, label}] = row
	}
	row.Count++
	row.Sum += v
	row.Max = max(row.Max, v)
	row.Hist[bucketOf(v)]++
}

// Inc cộng by vào counter name, nhãn label.
func (r *Registry) Inc(name, label string, by int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.counts[key{name, label}]
	if row == nil {
		row = &Row{Name: name, Label: label, Kind: KindCounter}
		r.counts[key{name, label}] = row
	}
	row.Count += by
	row.Sum = float64(row.Count)
}

// Gauge đặt giá trị hiện tại v của gauge name, nhãn label.
func (r *Registry) Gauge(name string, v float64, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.gauges[key{name, label}]
	if row == nil {
		row = &Row{Name: name, Label: label, Kind: KindGauge, Max: v}
		r.gauges[key{name, label}] = row
	}
	row.Count++
	row.Sum = v
	row.Max = max(row.Max, v)
}

// Drain trả mọi chỉ số đã gom rồi làm rỗng registry.
func (r *Registry) Drain() []Row {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Row, 0, len(r.hists)+len(r.counts)+len(r.gauges))
	for _, m := range []map[key]*Row{r.hists, r.counts, r.gauges} {
		for _, row := range m {
			out = append(out, *row)
		}
	}
	r.reset()
	return out
}

// bucketOf trả chỉ số ngăn của v: ngăn đầu tiên có cận trên ≥ v, hoặc ngăn cuối.
func bucketOf(v float64) int {
	i, _ := slices.BinarySearch(HistBoundsMs, v)
	return i
}

// QuantileFromHist trả cận trên của ngăn chứa phân vị pct (1..100) của hist (theo HistBoundsMs); ngăn cuối là +Inf;
// hist không có giá trị nào thì 0; pct ngoài 1..100 thì NaN.
func QuantileFromHist(hist []int, pct int) float64 {
	if pct < 1 || pct > 100 {
		return math.NaN()
	}
	total := 0
	for _, n := range hist {
		total += n
	}
	if total == 0 {
		return 0
	}
	need := (pct*total + 99) / 100 // ⌈pct·total/100⌉ bằng số nguyên, tránh sai số dấu phẩy động
	seen := 0
	for i, n := range hist {
		seen += n
		if seen >= need {
			if i < len(HistBoundsMs) {
				return HistBoundsMs[i]
			}
			break
		}
	}
	return math.Inf(1)
}
