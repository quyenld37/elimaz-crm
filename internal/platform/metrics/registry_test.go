package metrics_test

import (
	"math"
	"sync"
	"testing"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

func findRow(t *testing.T, rows []metrics.Row, name, label string) metrics.Row {
	t.Helper()
	for _, r := range rows {
		if r.Name == name && r.Label == label {
			return r
		}
	}
	t.Fatalf("không có dòng %s/%q trong %+v", name, label, rows)
	return metrics.Row{}
}

// 1.000 goroutine cùng Observe dưới -race: không tranh chấp, đủ 1.000 lần.
func TestRegistryConcurrentObserve(t *testing.T) {
	reg := metrics.NewRegistry()
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Go(func() { reg.Observe("job_ms", float64(i%7), "k") })
	}
	wg.Wait()
	r := findRow(t, reg.Drain(), "job_ms", "k")
	if r.Count != 1000 {
		t.Fatalf("count = %d, muốn 1000", r.Count)
	}
	total := 0
	for _, n := range r.Hist {
		total += n
	}
	if total != 1000 {
		t.Fatalf("tổng các ngăn = %d, muốn 1000", total)
	}
}

func TestRegistryKinds(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.Observe("http_ms", 40, "GET /x")
	reg.Observe("http_ms", 6000, "GET /x")
	reg.Inc("tx_retry", "40P01", 2)
	reg.Inc("tx_retry", "40P01", 3)
	reg.Gauge("sse_conns", 5, "")
	reg.Gauge("sse_conns", 3, "")
	rows := reg.Drain()

	h := findRow(t, rows, "http_ms", "GET /x")
	if h.Kind != metrics.KindHistogram || h.Count != 2 || h.Sum != 6040 || h.Max != 6000 {
		t.Fatalf("histogram sai: %+v", h)
	}
	if len(h.Hist) != len(metrics.HistBoundsMs)+1 || h.Hist[0] != 1 || h.Hist[len(h.Hist)-1] != 1 {
		t.Fatalf("ngăn sai (40 → ngăn đầu, 6000 → ngăn cuối): %v", h.Hist)
	}
	c := findRow(t, rows, "tx_retry", "40P01")
	if c.Kind != metrics.KindCounter || c.Count != 5 || c.Hist != nil {
		t.Fatalf("counter sai: %+v", c)
	}
	g := findRow(t, rows, "sse_conns", "")
	if g.Kind != metrics.KindGauge || g.Sum != 3 || g.Max != 5 || g.Count != 2 {
		t.Fatalf("gauge phải giữ giá trị cuối 3, max 5: %+v", g)
	}
	if again := reg.Drain(); len(again) != 0 {
		t.Fatalf("Drain phải làm rỗng registry, còn %+v", again)
	}
}

func TestP95FromHist(t *testing.T) {
	hist := make([]int, len(metrics.HistBoundsMs)+1)
	hist[0], hist[6] = 95, 5 // 95 giá trị ≤ 50, 5 giá trị ≤ 1000
	if got := metrics.P95FromHist(hist); got != 50 {
		t.Fatalf("P95 = %v, muốn 50", got)
	}
	hist[0], hist[6] = 94, 6
	if got := metrics.P95FromHist(hist); got != 1000 {
		t.Fatalf("P95 = %v, muốn 1000", got)
	}
	over := make([]int, len(metrics.HistBoundsMs)+1)
	over[len(over)-1] = 10
	if got := metrics.P95FromHist(over); !math.IsInf(got, 1) {
		t.Fatalf("mọi giá trị > 5000 → +Inf, nhận %v", got)
	}
	if got := metrics.P95FromHist(make([]int, len(metrics.HistBoundsMs)+1)); got != 0 {
		t.Fatalf("không có giá trị → 0, nhận %v", got)
	}
}
