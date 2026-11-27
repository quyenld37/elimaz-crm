package cache_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/testkit"
)

// gateModel là mô hình của cổng trong một lượt rapid: số vé được của mỗi (món, giây), của shop mỗi giây, vé đang giữ.
type gateModel struct {
	o        cache.GateOptions // ngưỡng đang áp (dự phòng thì là ngưỡng dự phòng)
	valkeyUp bool
	clk      *fakeClock
	g        cache.AdmissionGate
	tenant   uuid.UUID
	items    []uuid.UUID
	perItem  map[gateCell]int
	perShop  map[int64]int
	held     []*cache.Admission
	released []*cache.Admission
}

type gateCell struct {
	item int
	sec  int64
}

// step chạy một thao tác ngẫu nhiên: sang giây mới, Release một vé đang giữ, Release lại vé đã trả, hoặc Admit.
func (m *gateModel) step(t *testing.T, rt *rapid.T, i int) {
	switch rapid.IntRange(0, 9).Draw(rt, fmt.Sprintf("op%d", i)) {
	case 0:
		m.clk.add(time.Duration(rapid.IntRange(1, 1500).Draw(rt, fmt.Sprintf("dt%d", i))) * time.Millisecond)
	case 1, 2:
		if len(m.held) > 0 {
			k := rapid.IntRange(0, len(m.held)-1).Draw(rt, fmt.Sprintf("rel%d", i))
			m.held[k].Release()
			m.released = append(m.released, m.held[k])
			m.held = append(m.held[:k], m.held[k+1:]...)
		}
	case 3:
		if len(m.released) > 0 {
			m.released[rapid.IntRange(0, len(m.released)-1).Draw(rt, fmt.Sprintf("again%d", i))].Release()
		}
	default:
		m.admit(t, rt, i, rapid.IntRange(0, len(m.items)-1).Draw(rt, fmt.Sprintf("item%d", i)))
	}
	if len(m.held) > m.o.MaxInflight {
		rt.Fatalf("đang giữ %d vé > MaxInflight %d", len(m.held), m.o.MaxInflight)
	}
}

// admit gọi Admit cho món it và so với lý do mô hình muốn.
func (m *gateModel) admit(t *testing.T, rt *rapid.T, i, it int) {
	sec := m.clk.now().Unix()
	c := gateCell{it, sec}
	var want string
	switch {
	case len(m.held) >= m.o.MaxInflight:
		want = cache.ReasonInflight
	case m.perItem[c] >= m.o.PerSecond || m.perShop[sec] >= m.o.GlobalPerSecond:
		want = cache.ReasonBusy
	}
	a := m.g.Admit(t.Context(), m.tenant, m.items[it])
	if a.Reason != cache.ReasonInflight && a.Degraded == m.valkeyUp { // inflight không chạm Valkey
		rt.Fatalf("Degraded = %v khi Valkey chạy = %v", a.Degraded, m.valkeyUp)
	}
	if a.Reason != want || a.Admitted != (want == "") {
		rt.Fatalf("bước %d: %+v; mô hình muốn lý do %q (giữ %d/%d, món %d/%d, shop %d/%d)", i, a, want,
			len(m.held), m.o.MaxInflight, m.perItem[c], m.o.PerSecond, m.perShop[sec], m.o.GlobalPerSecond)
	}
	if a.Admitted {
		m.perItem[c]++
		m.perShop[sec]++
		m.held = append(m.held, a)
	}
}

// checkCeilings: vé mỗi (món, giây) ≤ PerSecond, mỗi giây của shop ≤ GlobalPerSecond.
func (m *gateModel) checkCeilings(rt *rapid.T) {
	for c, n := range m.perItem {
		if n > m.o.PerSecond {
			rt.Fatalf("%v: %d vé > PerSecond %d", c, n, m.o.PerSecond)
		}
	}
	for s, n := range m.perShop {
		if n > m.o.GlobalPerSecond {
			rt.Fatalf("giây %d: %d vé > GlobalPerSecond %d", s, n, m.o.GlobalPerSecond)
		}
	}
}

// checkNoLeak trả mọi vé rồi sang giờ mới: lại giữ được đúng min(MaxInflight, GlobalPerSecond) vé (Release lặp không
// làm âm, không rò chỗ).
func (m *gateModel) checkNoLeak(t *testing.T, rt *rapid.T) {
	for _, a := range m.held {
		a.Release()
	}
	m.held = m.held[:0]
	m.clk.add(time.Hour)
	for range m.o.MaxInflight + 1 {
		if a := m.g.Admit(t.Context(), m.tenant, uuid.New()); a.Admitted {
			m.held = append(m.held, a)
		}
	}
	free := len(m.held)
	for _, a := range m.held {
		a.Release()
	}
	if want := min(m.o.MaxInflight, m.o.GlobalPerSecond); free != want {
		rt.Fatalf("sau khi trả hết: giữ được %d vé, muốn %d", free, want)
	}
}

// Quy luật (100 lượt): chuỗi thao tác ngẫu nhiên (Admit trên vài món, Release một vé đang giữ, Release lại vé đã trả,
// sang giây mới) → số vé Admitted trong mỗi (món, giây) ≤ PerSecond, mỗi giây của shop ≤ GlobalPerSecond, số vé đang
// giữ không bao giờ vượt MaxInflight; kết quả khớp đúng mô hình (khi dự phòng: ngưỡng dự phòng).
func TestAdmissionGate_PropertyLimits(t *testing.T) {
	vk := testkit.StartValkey(t)
	conn := dialGate(t, vk.URL("crm_gate"), nil, time.Second)
	rapid.Check(t, func(rt *rapid.T) {
		per := rapid.IntRange(1, 6).Draw(rt, "per")
		global := rapid.IntRange(1, 10).Draw(rt, "global")
		o := cache.GateOptions{
			PerSecond: per, GlobalPerSecond: global, MaxInflight: rapid.IntRange(1, 8).Draw(rt, "max_inflight"),
			FallbackPerSecond:       rapid.IntRange(1, per).Draw(rt, "fb_per"),
			FallbackGlobalPerSecond: rapid.IntRange(1, global).Draw(rt, "fb_global"),
		}
		m := &gateModel{
			valkeyUp: rapid.Bool().Draw(rt, "valkey"),
			clk:      &fakeClock{t: windowBase().Add(time.Duration(rapid.IntRange(0, 999).Draw(rt, "start_ms")) * time.Millisecond)},
			tenant:   uuid.New(), // mỗi lượt một tenant: khóa Valkey không lẫn giữa các lượt
			items:    []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
			perItem:  map[gateCell]int{}, perShop: map[int64]int{},
		}
		o.Now = m.clk.now
		if m.valkeyUp {
			m.g = cache.NewAdmissionGate(conn, o, nil)
		} else {
			m.g = cache.NewAdmissionGate(nil, o, nil)
			o.PerSecond, o.GlobalPerSecond = o.FallbackPerSecond, o.FallbackGlobalPerSecond
		}
		m.o = o
		steps := rapid.IntRange(1, 80).Draw(rt, "steps")
		for i := range steps {
			m.step(t, rt, i)
		}
		m.checkCeilings(rt)
		m.checkNoLeak(t, rt)
	})
}
