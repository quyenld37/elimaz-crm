package main

import (
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// histAt trả histogram có n mẫu ở ngăn i (ngăn 0: ≤ 50 ms, 3: ≤ 300 ms, 6: ≤ 1000 ms).
func histAt(i, n int) []int {
	h := make([]int, len(metrics.HistBoundsMs)+1)
	h[i] = n
	return h
}

func counts(kv map[string]float64) k6Metrics {
	m := k6Metrics{}
	for k, v := range kv {
		m[k] = struct {
			Values map[string]float64 `json:"values"`
		}{Values: map[string]float64{"count": v}}
	}
	return m
}

// goodSpike là một lần flash_spike đạt mọi ngưỡng (kế hoạch M5 Task 219, FS03).
func goodSpike() m5Input {
	return m5Input{
		Mode: m5Spike, Slots: 500, VerifyExit: 0,
		K6: counts(map[string]float64{
			"buy_bought": 500, "buy_sold_out": 9000, "buy_503": 300, "buy_429": 20,
			"buy_err": 0,
		}),
		Before: m5Stats{Deadlocks: 3, FlashBuyHist: histAt(0, 0)},
		After: m5Stats{
			Deadlocks: 3, FlashBuyHist: histAt(0, 9500), FlashBuyCount: 9500, TxRetryExhausted: 10,
			TxRetry: map[string]int64{"40001": 50},
		},
	}
}

func TestM5SpikePasses(t *testing.T) {
	t.Parallel()
	vs := evaluateM5(goodSpike())
	for _, v := range vs {
		if v.Status == statusFail {
			t.Fatalf("tiêu chí %q không đạt: %+v", v.Name, v)
		}
	}
	if !m5Passed(vs) {
		t.Fatal("bài đạt mà m5Passed = false")
	}
}

func TestM5SpikeFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		crit string
		mod  func(*m5Input)
	}{
		{"bán thiếu", critM5Bought, func(in *m5Input) { in.K6["buy_bought"] = counts(map[string]float64{"x": 499})["x"] }},
		{"bán vượt", critM5Bought, func(in *m5Input) { in.K6["buy_bought"] = counts(map[string]float64{"x": 501})["x"] }},
		{"mã lỗi khác", critM5NoOtherErr, func(in *m5Input) { in.K6["buy_err"] = counts(map[string]float64{"x": 1})["x"] }},
		{"p95 chậm", critM5BuyP95, func(in *m5Input) { in.After.FlashBuyHist = histAt(4, 9500) }},
		{"verify sai", critM5Verify, func(in *m5Input) { in.VerifyExit = 1 }},
		{"deadlock tăng", critM5Deadlocks, func(in *m5Input) { in.After.Deadlocks = 4 }},
		{"hết lượt thử ≥ 1%", critM5RetryExhausted, func(in *m5Input) { in.After.TxRetryExhausted = 95 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := goodSpike()
			tc.mod(&in)
			vs := evaluateM5(in)
			if got := statusOf(t, vs, tc.crit); got != statusFail {
				t.Fatalf("%q = %s, muốn %s", tc.crit, got, statusFail)
			}
			if m5Passed(vs) {
				t.Fatal("có tiêu chí không đạt mà m5Passed = true")
			}
		})
	}
}

// Lượt bấm hết 10 lần gửi vẫn 503 chỉ nhận 503 retry_later: không phải mã lỗi khác.
func TestM5SpikeGaveUpIsNotError(t *testing.T) {
	t.Parallel()
	in := goodSpike()
	in.K6["buy_gave_up"] = counts(map[string]float64{"x": 127})["x"]
	if got := statusOf(t, evaluateM5(in), critM5NoOtherErr); got != statusPass {
		t.Fatalf("%q = %s, muốn %s", critM5NoOtherErr, got, statusPass)
	}
	if out := renderM5(in, evaluateM5(in)); !strings.Contains(out, "vẫn 503 127") {
		t.Fatalf("bảng thiếu số lượt hết lần gửi lại:\n%s", out)
	}
}

// Số đo histogram cộng dồn từ trước bài: chỉ phần tăng thêm trong bài được chấm.
func TestM5BuyP95UsesDelta(t *testing.T) {
	t.Parallel()
	in := goodSpike()
	in.Before.FlashBuyHist = histAt(4, 1000) // lượt chậm của bài trước, không thuộc bài này
	in.After.FlashBuyHist = histAt(4, 1000)
	in.After.FlashBuyHist[0] = 9500
	if got := statusOf(t, evaluateM5(in), critM5BuyP95); got != statusPass {
		t.Fatalf("p95 = %s, muốn %s (chỉ lấy phần tăng)", got, statusPass)
	}
}

func TestM5PerCustomer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		bought float64
		want   string
	}{{1, statusPass}, {0, statusFail}, {2, statusFail}} {
		in := m5Input{Mode: m5PerCustomer, Slots: 500, K6: counts(map[string]float64{
			"buy_bought":     tc.bought,
			"buy_over_limit": 99,
		})}
		if got := statusOf(t, evaluateM5(in), critM5OneBought); got != tc.want {
			t.Fatalf("bought %v: %s, muốn %s", tc.bought, got, tc.want)
		}
	}
}

func TestM5Sustained(t *testing.T) {
	t.Parallel()
	good := func() m5Input {
		return m5Input{
			Mode: m5Sustained, Slots: 15000,
			K6:     counts(map[string]float64{"buy_bought": 15000, "complete_ok": 15000, "buy_err": 0, "complete_err": 0}),
			After:  m5Stats{FlashBuyHist: histAt(1, 15000), FlashBuyCount: 15000, PickupEventHist: histAt(3, 15000)},
			Before: m5Stats{},
		}
	}
	vs := evaluateM5(good())
	if !m5Passed(vs) {
		t.Fatalf("bài đạt mà có tiêu chí hỏng: %+v", vs)
	}
	in := good()
	in.After.PickupEventHist = histAt(7, 15000) // ≤ 2 s
	if got := statusOf(t, evaluateM5(in), critM5PickupP95); got != statusFail {
		t.Fatalf("nhận việc 2 s: %s, muốn %s", got, statusFail)
	}
	in = good()
	in.K6["complete_ok"] = counts(map[string]float64{"x": 14999})["x"]
	if got := statusOf(t, evaluateM5(in), critM5Orders); got != statusFail {
		t.Fatalf("thiếu một đơn: %s, muốn %s", got, statusFail)
	}
}

func TestM5Abandon(t *testing.T) {
	t.Parallel()
	good := func() m5Input {
		return m5Input{Mode: m5Abandon, Slots: 500, K6: counts(map[string]float64{
			"buy_bought": 750, "complete_ok": 250, "wave2_bought": 250, "wave2_sold_out_early": 0, // bought gồm cả đợt hai
		})}
	}
	if vs := evaluateM5(good()); !m5Passed(vs) {
		t.Fatalf("bài đạt mà có tiêu chí hỏng: %+v", vs)
	}
	in := good()
	in.K6["wave2_bought"] = counts(map[string]float64{"x": 240})["x"]
	in.K6["buy_bought"] = counts(map[string]float64{"x": 740})["x"] // đợt một vẫn 500, đợt hai thiếu 10
	if got := statusOf(t, evaluateM5(in), critM5Bought); got != statusPass {
		t.Fatalf("đợt một = %s, muốn %s (tổng − đợt hai)", got, statusPass)
	}
	if got := statusOf(t, evaluateM5(in), critM5Reopened); got != statusFail {
		t.Fatalf("thiếu 10 suất mở lại: %s, muốn %s", got, statusFail)
	}
	in = good()
	in.K6["wave2_sold_out_early"] = counts(map[string]float64{"x": 3})["x"]
	if got := statusOf(t, evaluateM5(in), critM5NoFalseSoldOut); got != statusFail {
		t.Fatalf("hết oan: %s, muốn %s", got, statusFail)
	}
}

func TestM5CloseWithHolds(t *testing.T) {
	t.Parallel()
	good := func() m5Input {
		return m5Input{Mode: m5Close, Slots: 200, K6: counts(map[string]float64{
			"buy_bought": 200, "complete_ok": 100, "close_ok": 1,
		})}
	}
	if vs := evaluateM5(good()); !m5Passed(vs) {
		t.Fatalf("bài đạt mà có tiêu chí hỏng: %+v", vs)
	}
	in := good()
	in.K6["complete_ok"] = counts(map[string]float64{"x": 99})["x"]
	if got := statusOf(t, evaluateM5(in), critM5HalfDone); got != statusFail {
		t.Fatalf("thiếu một đơn hoàn tất: %s, muốn %s", got, statusFail)
	}
	in = good()
	delete(in.K6, "close_ok")
	if got := statusOf(t, evaluateM5(in), critM5Closed); got != statusFail {
		t.Fatalf("không đóng được: %s, muốn %s", got, statusFail)
	}
	in = good()
	in.VerifyExit = 1
	if got := statusOf(t, evaluateM5(in), critM5Verify); got != statusFail {
		t.Fatalf("verify sai: %s, muốn %s", got, statusFail)
	}
}

func TestM5WebRead(t *testing.T) {
	t.Parallel()
	good := func() m5Input {
		k := counts(map[string]float64{"read_err": 0})
		k["read_ms"] = struct {
			Values map[string]float64 `json:"values"`
		}{Values: map[string]float64{"p(95)": 20, "p(99)": 60}}
		return m5Input{
			Mode: m5WebRead, K6: k, Before: m5Stats{CacheHit: 100, CacheMiss: 10},
			After: m5Stats{CacheHit: 100 + 297_000, CacheMiss: 10 + 3_000},
		}
	}
	if vs := evaluateM5(good()); !m5Passed(vs) {
		t.Fatalf("bài đạt mà có tiêu chí hỏng: %+v", vs)
	}
	in := good()
	in.After.CacheMiss = 10 + 30_000 // 90,8% trúng
	if got := statusOf(t, evaluateM5(in), critM5CacheHit); got != statusFail {
		t.Fatalf("trúng 90%%: %s, muốn %s", got, statusFail)
	}
	in = good()
	in.After = in.Before // không có lần đọc nào: chưa đo, không phải đạt
	if got := statusOf(t, evaluateM5(in), critM5CacheHit); got != statusNA {
		t.Fatalf("không lần đọc: %s, muốn %s", got, statusNA)
	}
	if m5Passed(evaluateM5(in)) {
		t.Fatal("tiêu chí chưa đo mà m5Passed = true")
	}
}

func TestM5RenderHasEveryVerdict(t *testing.T) {
	t.Parallel()
	in := goodSpike()
	vs := evaluateM5(in)
	out := renderM5(in, vs)
	for _, v := range vs {
		if !strings.Contains(out, v.Name) {
			t.Fatalf("bảng thiếu %q:\n%s", v.Name, out)
		}
	}
	if !strings.Contains(out, "40001") {
		t.Fatalf("bảng thiếu tx_retry theo SQLSTATE:\n%s", out)
	}
}

func goodChaos(mode string) m5Input {
	in := goodSpike()
	in.Mode = mode
	in.K6["buy_ms"] = counts(map[string]float64{"x": 20000})["x"]
	in.K6["buy_503"] = counts(map[string]float64{"x": 400})["x"]
	return in
}

func TestM5ChaosPasses(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{m5ChaosValkey, m5ChaosAPI, m5ChaosPG} {
		if vs := evaluateM5(goodChaos(mode)); !m5Passed(vs) {
			t.Fatalf("%s: bài đạt mà không đạt: %+v", mode, vs)
		}
	}
}

func TestM5ChaosFailures(t *testing.T) {
	t.Parallel()
	one := func(v float64) struct {
		Values map[string]float64 `json:"values"`
	} {
		return counts(map[string]float64{"x": v})["x"]
	}
	cases := []struct {
		name, mode, crit string
		mod              func(*m5Input)
	}{
		{"valkey bán thiếu", m5ChaosValkey, critM5Bought, func(in *m5Input) { in.K6["buy_bought"] = one(499) }},
		{"valkey 500 hàng loạt", m5ChaosValkey, critM5ChaosErrRate, func(in *m5Input) { in.K6["buy_err"] = one(21) }},
		{"valkey verify sai", m5ChaosValkey, critM5Verify, func(in *m5Input) { in.VerifyExit = 1 }},
		{"valkey đơn trùng", m5ChaosValkey, critM5NoDupRef, func(in *m5Input) { in.Dups = 1 }},
		{"api verify sai", m5ChaosAPI, critM5Verify, func(in *m5Input) { in.VerifyExit = 1 }},
		{"api đơn trùng", m5ChaosAPI, critM5NoDupRef, func(in *m5Input) { in.Dups = 2 }},
		{"pg không thấy 503", m5ChaosPG, critM5ChaosSaw503, func(in *m5Input) { in.K6["buy_503"] = one(0) }},
		{"pg lỗi khác 503", m5ChaosPG, critM5ChaosErrRate, func(in *m5Input) { in.K6["buy_err"] = one(15168) }},
		{"pg bán thiếu", m5ChaosPG, critM5Bought, func(in *m5Input) { in.K6["buy_bought"] = one(499) }},
		{"pg verify sai", m5ChaosPG, critM5Verify, func(in *m5Input) { in.VerifyExit = 1 }},
		{"pg đơn trùng", m5ChaosPG, critM5NoDupRef, func(in *m5Input) { in.Dups = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := goodChaos(tc.mode)
			tc.mod(&in)
			expectOnlyFails(t, in, tc.crit)
		})
	}
}

// expectOnlyFails: tiêu chí crit KHÔNG ĐẠT và cả bài không đạt.
func expectOnlyFails(t *testing.T, in m5Input, crit string) {
	t.Helper()
	vs := evaluateM5(in)
	if got := statusOf(t, vs, crit); got != statusFail || m5Passed(vs) {
		t.Fatalf("%q = %s (bài đạt: %v), muốn %s và bài không đạt", crit, got, m5Passed(vs), statusFail)
	}
}
