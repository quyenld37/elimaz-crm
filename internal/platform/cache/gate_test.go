package cache_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

// gateClock là đồng hồ giả của cổng ở ms thứ 300 của một giây tròn (RetryAfter của "busy" nằm trong [700, 1200] ms).
func gateClock() *fakeClock { return &fakeClock{t: windowBase().Add(300 * time.Millisecond)} }

// admitN gọi Admit n lần (không Release), trả số vé được nhận và các vé đó.
func admitN(t *testing.T, g cache.AdmissionGate, tenant, item uuid.UUID, n int) (int, []*cache.Admission, *cache.Admission) {
	t.Helper()
	var held []*cache.Admission
	var last *cache.Admission
	for range n {
		last = g.Admit(t.Context(), tenant, item)
		if last.Admitted {
			held = append(held, last)
		}
	}
	return len(held), held, last
}

// startGate dựng cổng trên một Valkey thật riêng với đồng hồ gateClock (o.Now bị thay).
func startGate(t *testing.T, o cache.GateOptions, reg *metrics.Registry, opTimeout time.Duration) (cache.AdmissionGate, *fakeClock, *testkit.Valkey) { //nolint:ireturn // cổng chỉ có dạng interface
	t.Helper()
	vk := testkit.StartValkey(t)
	clk := gateClock()
	o.Now = clk.now
	return cache.NewAdmissionGate(dialGate(t, vk.URL("crm_gate"), nil, opTimeout), o, reg), clk, vk
}

func busyRetryOK(a *cache.Admission) bool {
	return a.RetryAfter >= 700*time.Millisecond && a.RetryAfter <= 1200*time.Millisecond
}

// PerSecond 3: 3 lần đầu trong giây được, lần 4 "busy" với RetryAfter = phần còn lại của giây (700 ms) + 0–500 ms;
// giây kế tiếp lại được. Chỉ số gate_admitted, gate_busy{busy}.
func TestAdmissionGate_PerSecond(t *testing.T) {
	reg := metrics.NewRegistry()
	g, clk, _ := startGate(t, cache.GateOptions{PerSecond: 3, GlobalPerSecond: 100, MaxInflight: 100}, reg, testOpTimeout)
	tenant, item := uuid.New(), uuid.New()

	ok, _, last := admitN(t, g, tenant, item, 3)
	if ok != 3 || last.Degraded || last.Reason != "" {
		t.Fatalf("3 lần đầu: %d được, cuối %+v; muốn 3, không Degraded", ok, last)
	}
	a := g.Admit(t.Context(), tenant, item)
	if a.Admitted || a.Reason != cache.ReasonBusy || a.Degraded || !busyRetryOK(a) {
		t.Fatalf("lần 4 = %+v; muốn busy, RetryAfter trong [700 ms, 1200 ms]", a)
	}
	a.Release() // vé không được nhận: Release không làm gì
	// Món khác cùng giây: bộ đếm riêng.
	if ok, _, _ := admitN(t, g, tenant, uuid.New(), 3); ok != 3 {
		t.Fatalf("món khác: %d được, muốn 3", ok)
	}
	clk.add(time.Second)
	if a := g.Admit(t.Context(), tenant, item); !a.Admitted {
		t.Fatalf("giây kế tiếp: %+v, muốn được", a)
	}
	c := counts(reg.Drain())
	if c["gate_admitted/"] != 7 || c["gate_busy/busy"] != 1 || c["gate_degraded/"] != 0 {
		t.Fatalf("chỉ số %v; muốn gate_admitted 7, gate_busy{busy} 1, không degraded", c)
	}
}

// GlobalPerSecond 4, hai món mỗi món PerSecond 3: tổng được trong một giây là 4; lượt bị món từ chối không tiêu lượt
// của shop; tenant khác có bộ đếm shop riêng.
func TestAdmissionGate_GlobalPerSecond(t *testing.T) {
	g, _, _ := startGate(t, cache.GateOptions{PerSecond: 3, GlobalPerSecond: 4, MaxInflight: 100}, nil, testOpTimeout)
	tenant, a, b := uuid.New(), uuid.New(), uuid.New()

	okA, _, _ := admitN(t, g, tenant, a, 5) // 3 được, 2 bị món từ chối (không tính vào shop)
	okB, _, lastB := admitN(t, g, tenant, b, 3)
	if okA+okB != 4 || okA != 3 || okB != 1 {
		t.Fatalf("món a %d, món b %d được; muốn 3 + 1 = 4", okA, okB)
	}
	if lastB.Reason != cache.ReasonBusy || !busyRetryOK(lastB) {
		t.Fatalf("món b bị shop từ chối: %+v", lastB)
	}
	if ok, _, _ := admitN(t, g, uuid.New(), a, 3); ok != 3 {
		t.Fatalf("tenant khác: %d được, muốn 3", ok)
	}
}

// MaxInflight 2: hai vé chưa Release → lần 3 "inflight" (RetryAfter 1 s); Release một vé → lần 4 được; Release hai
// lần (kể cả song song) không làm âm. Vé "inflight" không chạm bộ đếm giây. Gauge gate_inflight.
func TestAdmissionGate_MaxInflight(t *testing.T) {
	reg := metrics.NewRegistry()
	g, _, _ := startGate(t, cache.GateOptions{PerSecond: 100, GlobalPerSecond: 100, MaxInflight: 2}, reg, testOpTimeout)
	tenant, item := uuid.New(), uuid.New()

	ok, held, _ := admitN(t, g, tenant, item, 2)
	if ok != 2 {
		t.Fatalf("hai vé đầu: %d được", ok)
	}
	a := g.Admit(t.Context(), tenant, item)
	if a.Admitted || a.Reason != cache.ReasonInflight || a.RetryAfter != time.Second {
		t.Fatalf("lần 3 = %+v; muốn inflight, RetryAfter 1 s", a)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(held[0].Release)
	}
	wg.Wait()
	if g := gaugeOf(reg, "gate_inflight"); g != 1 {
		t.Fatalf("gate_inflight = %v sau Release, muốn 1", g)
	}
	a4 := g.Admit(t.Context(), tenant, item)
	if !a4.Admitted {
		t.Fatalf("lần 4 sau Release = %+v, muốn được", a4)
	}
	// Đang giữ held[1], a4: đầy; Release lại held[0] không mở thêm chỗ.
	held[0].Release()
	if a := g.Admit(t.Context(), tenant, item); a.Reason != cache.ReasonInflight {
		t.Fatalf("Release lần hai mở thêm chỗ: %+v", a)
	}
	held[1].Release()
	a4.Release()
	if ok, _, _ := admitN(t, g, tenant, item, 3); ok != 2 {
		t.Fatalf("sau khi trả hết: %d được, muốn 2", ok)
	}
	c := counts(reg.Drain())
	// Từ lần đọc gauge (Drain) tới đây: vé a4 và 2 vé cuối được, 2 lần inflight.
	if c["gate_busy/inflight"] != 2 || c["gate_admitted/"] != 3 || c["gate_busy/busy"] != 0 {
		t.Fatalf("chỉ số %v; muốn gate_busy{inflight} 2, gate_admitted 3", c)
	}
	var nilA *cache.Admission
	nilA.Release() // vé nil: không panic
}

// 500 goroutine trong cùng một giây, PerSecond 50, MaxInflight 1000 → đúng 50 vé.
func TestAdmissionGate_Concurrent(t *testing.T) {
	g, _, _ := startGate(t, cache.GateOptions{PerSecond: 50, GlobalPerSecond: 1000, MaxInflight: 1000}, nil, 5*time.Second)
	tenant, item := uuid.New(), uuid.New()
	var admitted, degraded atomic.Int64
	var wg sync.WaitGroup
	for range 500 {
		wg.Go(func() {
			a := g.Admit(t.Context(), tenant, item)
			if a.Admitted {
				admitted.Add(1)
			}
			if a.Degraded {
				degraded.Add(1)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 50 || degraded.Load() != 0 {
		t.Fatalf("%d vé được, %d Degraded; muốn đúng 50, 0", admitted.Load(), degraded.Load())
	}
}

// Khóa bộ đếm sống ≤ 3 s; hash trạng thái sống ≤ 10 s (vòng dựng lại ngừng thì tự hết hạn).
func TestAdmissionGate_KeyTTL(t *testing.T) {
	g, _, vk := startGate(t, cache.GateOptions{}, nil, testOpTimeout)
	admin := dial(t, vk.URL("crm_admin"), nil)
	tenant, item := uuid.New(), uuid.New()
	if a := g.Admit(t.Context(), tenant, item); !a.Admitted {
		t.Fatalf("Admit: %+v", a)
	}
	if err := g.SetState(t.Context(), tenant, item, 5, false); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	unix := strconv.FormatInt(windowBase().Unix(), 10)
	pttl := func(key string) int64 {
		t.Helper()
		var n int64
		if err := admin.Do(t.Context(), "pttl", func(ctx context.Context, cl valkey.Client) error {
			var e error
			n, e = cl.Do(ctx, cl.B().Pttl().Key(key).Build()).AsInt64()
			return e //nolint:wrapcheck // Do của gói đang test
		}); err != nil {
			t.Fatalf("PTTL %s: %v", key, err)
		}
		return n
	}
	for _, k := range []string{
		"gt:" + tenant.String() + ":" + item.String() + ":" + unix,
		"gt:" + tenant.String() + ":all:" + unix,
	} {
		if n := pttl(k); n <= 2000 || n > 3000 {
			t.Errorf("PTTL %s = %d ms, muốn trong (2000, 3000]", k, n)
		}
	}
	if n := pttl("fs:" + tenant.String() + ":" + item.String()); n <= 9000 || n > 10_000 {
		t.Errorf("PTTL hash trạng thái = %d ms, muốn trong (9000, 10000]", n)
	}
}

// SetState(item, 0, true) → SoldOut (true, true); hết suất bỏ được; sau 11 s (đồng hồ của cổng) → known = false;
// món chưa có trạng thái → known = false; remaining âm → ErrBadState.
func TestAdmissionGate_SoldOutState(t *testing.T) {
	g, clk, _ := startGate(t, cache.GateOptions{}, nil, testOpTimeout)
	tenant, item := uuid.New(), uuid.New()

	if s, k := g.SoldOut(t.Context(), tenant, item); s || k {
		t.Fatalf("chưa có trạng thái: (%v, %v), muốn (false, false)", s, k)
	}
	if err := g.SetState(t.Context(), tenant, item, 0, true); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if s, k := g.SoldOut(t.Context(), tenant, item); !s || !k {
		t.Fatalf("sau SetState hết: (%v, %v), muốn (true, true)", s, k)
	}
	if s, k := g.SoldOut(t.Context(), uuid.New(), item); s || k {
		t.Fatalf("tenant khác: (%v, %v), muốn (false, false)", s, k)
	}
	if err := g.SetState(t.Context(), tenant, item, 2, false); err != nil {
		t.Fatalf("SetState còn: %v", err)
	}
	if s, k := g.SoldOut(t.Context(), tenant, item); s || !k {
		t.Fatalf("còn suất: (%v, %v), muốn (false, true)", s, k)
	}
	clk.add(9 * time.Second)
	if _, k := g.SoldOut(t.Context(), tenant, item); !k {
		t.Fatal("ở giây thứ 9 trạng thái phải còn")
	}
	clk.add(2 * time.Second)
	if s, k := g.SoldOut(t.Context(), tenant, item); s || k {
		t.Fatalf("sau 11 s: (%v, %v), muốn known = false", s, k)
	}
	if err := g.SetState(t.Context(), tenant, item, -1, false); err == nil {
		t.Fatal("remaining âm phải lỗi ErrBadState")
	} else if !errors.Is(err, cache.ErrBadState) {
		t.Fatalf("remaining âm: %v, muốn ErrBadState", err)
	}
}

// Remaining cho 3 món, một món chưa có trạng thái → map 2 phần tử; trạng thái hết hạn bị bỏ; danh sách rỗng → map rỗng.
func TestAdmissionGate_Remaining(t *testing.T) {
	g, clk, _ := startGate(t, cache.GateOptions{}, nil, testOpTimeout)
	tenant, a, b, c := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := g.SetState(t.Context(), tenant, a, 7, false); err != nil {
		t.Fatal(err)
	}
	if err := g.SetState(t.Context(), tenant, b, 0, true); err != nil {
		t.Fatal(err)
	}
	got := g.Remaining(t.Context(), tenant, []uuid.UUID{a, b, c})
	if len(got) != 2 || got[a] != 7 || got[b] != 0 {
		t.Fatalf("Remaining = %v; muốn {a: 7, b: 0}", got)
	}
	if _, ok := got[c]; ok {
		t.Fatal("món chưa có trạng thái không được có trong map")
	}
	if got := g.Remaining(t.Context(), tenant, nil); got == nil || len(got) != 0 {
		t.Fatalf("danh sách rỗng: %v, muốn map rỗng khác nil", got)
	}
	clk.add(5 * time.Second)
	if err := g.SetState(t.Context(), tenant, c, 3, false); err != nil {
		t.Fatal(err)
	}
	clk.add(6 * time.Second) // a, b quá 10 s; c mới 6 s
	if got := g.Remaining(t.Context(), tenant, []uuid.UUID{a, b, c}); len(got) != 1 || got[c] != 3 {
		t.Fatalf("sau 11 s: %v; muốn {c: 3}", got)
	}
}

// [Task 520] ClearState xóa ngay trạng thái một món: SoldOut known = false, Remaining không có món; món khác, tenant
// khác giữ nguyên; xóa món chưa có trạng thái không lỗi; Valkey dừng → ErrUnavailable.
func TestAdmissionGate_ClearState(t *testing.T) {
	g, _, vk := startGate(t, cache.GateOptions{}, nil, testOpTimeout)
	tenant, other, a, b := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, s := range []struct {
		tenant, item uuid.UUID
		remaining    int
		soldOut      bool
	}{{tenant, a, 0, true}, {tenant, b, 4, false}, {other, a, 2, false}} {
		if err := g.SetState(t.Context(), s.tenant, s.item, s.remaining, s.soldOut); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.ClearState(t.Context(), tenant, a); err != nil {
		t.Fatalf("ClearState: %v", err)
	}
	if s, k := g.SoldOut(t.Context(), tenant, a); s || k {
		t.Fatalf("sau ClearState: (%v, %v), muốn (false, false)", s, k)
	}
	if got := g.Remaining(t.Context(), tenant, []uuid.UUID{a, b}); len(got) != 1 || got[b] != 4 {
		t.Fatalf("Remaining sau ClearState: %v, muốn {b: 4}", got)
	}
	if got := g.Remaining(t.Context(), other, []uuid.UUID{a}); got[a] != 2 {
		t.Fatalf("tenant khác: %v, muốn {a: 2}", got)
	}
	if err := g.ClearState(t.Context(), tenant, uuid.New()); err != nil {
		t.Fatalf("ClearState món chưa có trạng thái: %v", err)
	}
	vk.Stop(t)
	if err := g.ClearState(t.Context(), tenant, b); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("ClearState khi dừng: %v, muốn ErrUnavailable", err)
	}
}

// Valkey dừng: FallbackPerSecond 1 → lần 2 trong giây busy, Degraded; FallbackGlobalPerSecond chặn cả shop; giây sau
// được lại; SoldOut known = false; SetState lỗi ErrUnavailable; Remaining rỗng; chỉ số gate_degraded.
func TestAdmissionGate_ValkeyDown(t *testing.T) {
	reg := metrics.NewRegistry()
	g, clk, vk := startGate(t, cache.GateOptions{
		PerSecond: 10, GlobalPerSecond: 10, FallbackPerSecond: 1, FallbackGlobalPerSecond: 2,
		MaxInflight: 100,
	}, reg, testOpTimeout)
	tenant, item := uuid.New(), uuid.New()
	if err := g.SetState(t.Context(), tenant, item, 0, true); err != nil {
		t.Fatal(err)
	}
	vk.Stop(t)

	if a := g.Admit(t.Context(), tenant, item); !a.Admitted || !a.Degraded {
		t.Fatalf("lần 1 khi dừng: %+v, muốn được, Degraded", a)
	}
	a := g.Admit(t.Context(), tenant, item)
	if a.Admitted || !a.Degraded || a.Reason != cache.ReasonBusy || !busyRetryOK(a) {
		t.Fatalf("lần 2 khi dừng: %+v, muốn busy, Degraded", a)
	}
	// Ngưỡng shop dự phòng 2: món thứ hai được 1, món thứ ba bị shop chặn.
	if a := g.Admit(t.Context(), tenant, uuid.New()); !a.Admitted {
		t.Fatalf("món 2 khi dừng: %+v", a)
	}
	if a := g.Admit(t.Context(), tenant, uuid.New()); a.Admitted || a.Reason != cache.ReasonBusy {
		t.Fatalf("món 3 khi dừng (shop đủ 2): %+v", a)
	}
	clk.add(time.Second)
	if a := g.Admit(t.Context(), tenant, item); !a.Admitted || !a.Degraded {
		t.Fatalf("giây sau khi dừng: %+v", a)
	}
	if s, k := g.SoldOut(t.Context(), tenant, item); s || k {
		t.Fatalf("SoldOut khi dừng: (%v, %v), muốn known = false", s, k)
	}
	if err := g.SetState(t.Context(), tenant, item, 1, false); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("SetState khi dừng: %v, muốn ErrUnavailable", err)
	}
	if got := g.Remaining(t.Context(), tenant, []uuid.UUID{item}); len(got) != 0 {
		t.Fatalf("Remaining khi dừng: %v, muốn rỗng", got)
	}
	if c := counts(reg.Drain()); c["gate_degraded/"] != 5 {
		t.Fatalf("chỉ số %v; muốn gate_degraded 5", c)
	}
}

// Conn nil (không cấu hình Valkey gate): luôn dự phòng; mặc định QĐ23 khi dự phòng là 50% (PerSecond 100 → 50).
func TestAdmissionGate_NilConnDefaults(t *testing.T) {
	clk := gateClock()
	g := cache.NewAdmissionGate(nil, cache.GateOptions{Now: clk.now}, nil)
	ok, held, last := admitN(t, g, uuid.New(), uuid.New(), 60)
	if ok != 50 || !last.Degraded || last.Reason != cache.ReasonBusy {
		t.Fatalf("Conn nil: %d được, cuối %+v; muốn 50 (50%% của 100), Degraded", ok, last)
	}
	for _, a := range held {
		a.Release()
	}
	// MaxInflight mặc định 64: giữ 64 vé ở nhiều món (shop dự phòng 150/s) thì vé 65 là inflight.
	clk.add(time.Second)
	tenant := uuid.New()
	var kept []*cache.Admission
	for range 64 {
		a := g.Admit(t.Context(), tenant, uuid.New())
		if !a.Admitted {
			t.Fatalf("vé %d: %+v", len(kept)+1, a)
		}
		kept = append(kept, a)
	}
	if a := g.Admit(t.Context(), tenant, uuid.New()); a.Reason != cache.ReasonInflight {
		t.Fatalf("vé 65: %+v, muốn inflight (MaxInflight mặc định 64)", a)
	}
	for _, a := range kept {
		a.Release()
	}
}

func gaugeOf(reg *metrics.Registry, name string) float64 {
	v := -1.0
	for _, r := range reg.Drain() {
		if r.Name == name && r.Kind == metrics.KindGauge {
			v = r.Sum
		}
	}
	return v
}
