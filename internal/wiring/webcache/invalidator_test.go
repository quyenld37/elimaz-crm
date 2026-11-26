package webcache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// call là một lời gọi ghi của ReadCache.
type call struct {
	op     string // "inv" | "bump"
	tenant uuid.UUID
	space  cache.Space
	id     string
}

// fakeRC ghi lại Invalidate, BumpSpace; fail > 0 thì fail lần gọi tới trả lỗi; gate khác nil thì mỗi lời gọi chờ gate.
type fakeRC struct {
	mu    sync.Mutex
	calls []call
	fail  int
	gate  chan struct{}
	ch    chan call
}

func newFakeRC() *fakeRC { return &fakeRC{ch: make(chan call, 100000)} }

var errValkey = fmt.Errorf("valkey ngừng: %w", cache.ErrUnavailable)

func (f *fakeRC) record(c call) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return errValkey
	}
	f.calls = append(f.calls, c)
	f.ch <- c
	return nil
}

func (f *fakeRC) GetOrLoad(context.Context, uuid.UUID, cache.Space, string, time.Duration,
	func(context.Context) ([]byte, error),
) ([]byte, cache.Source, error) {
	return nil, "", errors.New("không dùng")
}

func (f *fakeRC) Invalidate(_ context.Context, tenant uuid.UUID, space cache.Space, ids ...string) error {
	for _, id := range ids {
		if err := f.record(call{"inv", tenant, space, id}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeRC) BumpSpace(_ context.Context, tenant uuid.UUID, space cache.Space) error {
	return f.record(call{"bump", tenant, space, ""})
}

// waitCalls chờ đủ n lời gọi thành công (hangGuard chống treo) và trả chúng.
func (f *fakeRC) waitCalls(t *testing.T, n int) []call {
	t.Helper()
	out := make([]call, 0, n)
	for range n {
		select {
		case c := <-f.ch:
			out = append(out, c)
		case <-time.After(hangGuard):
			t.Fatalf("chỉ có %d/%d lời gọi: %v", len(out), n, out)
		}
	}
	return out
}

// pushBus là Bus giả: test tự gọi hàm nhận của người đăng ký hệ thống.
type pushBus struct {
	realtimetest.RecordingBus
	mu     sync.Mutex
	prefix string
	fn     func(realtime.Signal)
}

func (b *pushBus) SubscribeSystem(prefix string, fn func(realtime.Signal)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prefix, b.fn = prefix, fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.fn = nil
	}
}

func (b *pushBus) push(s realtime.Signal) {
	b.mu.Lock()
	fn := b.fn
	b.mu.Unlock()
	if fn != nil {
		fn(s)
	}
}

func inv(tenant uuid.UUID, typ, id string) realtime.Signal {
	return realtime.Signal{TenantID: tenant, Topic: webcache.TopicInvalidate, System: true, Entity: realtime.Entity{Type: typ, ID: id}}
}

func quiet() *slog.Logger { return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)) }

func TestInvalidator_Mapping(t *testing.T) {
	bus, rc := &pushBus{}, newFakeRC()
	v := webcache.NewInvalidator(bus, rc, nil, quiet())
	v.Start()
	t.Cleanup(v.Stop)
	if bus.prefix != webcache.TopicInvalidate {
		t.Fatalf("đăng ký tiền tố %q", bus.prefix)
	}
	tn := uuid.New()
	cases := []struct {
		sig  realtime.Signal
		want []call
	}{
		{inv(tn, "product", "p1"), []call{{"inv", tn, cache.SpaceProduct, "p1"}, {"bump", tn, cache.SpaceProductsPage, ""}}},
		{inv(tn, "variant", "v1"), []call{{"inv", tn, cache.SpaceAvailability, "v1"}}},
		{inv(tn, "flash_sale", "f1"), []call{{"bump", tn, cache.SpaceFlash, ""}}},
		{inv(tn, "flash_item", "i1"), []call{{"bump", tn, cache.SpaceFlash, ""}}},
		{inv(tn, "carrier_settings", "ghn"), []call{{"bump", tn, cache.SpaceCarriers, ""}, {"bump", tn, cache.SpaceShipQuote, ""}}},
		{inv(tn, "web_campaign", "c1"), []call{{"bump", tn, cache.SpaceCampaigns, ""}, {"bump", tn, cache.SpaceBanners, ""}}},
		{inv(tn, "web_intake", tn.String()), []call{{"bump", tn, cache.SpaceWebIntake, ""}}},
		{inv(tn, "home_banners", tn.String()), []call{{"bump", tn, cache.SpaceBanners, ""}}},
		{inv(tn, "home_looks", tn.String()), []call{{"bump", tn, cache.SpaceLooks, ""}}},
	}
	for _, c := range cases {
		bus.push(inv(tn, "loai_la", "x")) // loại chưa biết: bỏ qua, không gọi gì
		bus.push(c.sig)
		if got := rc.waitCalls(t, len(c.want)); !slices.Equal(got, c.want) {
			t.Fatalf("%s: gọi %v, muốn %v", c.sig.Entity.Type, got, c.want)
		}
	}
	v.Stop()
	v.Stop()
	bus.push(inv(tn, "product", "p2"))
	select {
	case c := <-rc.ch:
		t.Fatalf("đã Stop mà vẫn gọi %v", c)
	case <-time.After(100 * time.Millisecond): // chỉ để bắt lỗi; Stop đã hủy đăng ký nên không có đường tới rc
	}
}

// bumpsByTenant đếm số space được BumpSpace của mỗi tenant.
func bumpsByTenant(calls []call) map[uuid.UUID]map[cache.Space]bool {
	out := map[uuid.UUID]map[cache.Space]bool{}
	for _, c := range calls {
		if c.op != "bump" {
			continue
		}
		if out[c.tenant] == nil {
			out[c.tenant] = map[cache.Space]bool{}
		}
		out[c.tenant][c.space] = true
	}
	return out
}

// waitFullResync chờ tới khi mọi tenant trong tenants đã được BumpSpace đủ 10 space.
func waitFullResync(t *testing.T, rc *fakeRC, tenants ...uuid.UUID) {
	t.Helper()
	var seen []call
	for {
		seen = append(seen, rc.waitCalls(t, 1)...)
		b := bumpsByTenant(seen)
		done := true
		for _, tn := range tenants {
			if len(b[tn]) != len(cache.Spaces()) {
				done = false
			}
		}
		if done {
			return
		}
	}
}

func TestInvalidator_SystemResync(t *testing.T) {
	e := newEnv(t)
	t1, t2 := e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	if len(cache.Spaces()) != 14 {
		t.Fatalf("có %d space, kế hoạch có 14 (Task 452 thêm campaigns, Task 456 thêm web_intake, Task 453 thêm banners, "+
			"Task 454 thêm looks)",
			len(cache.Spaces()))
	}
	bus, rc := &pushBus{}, newFakeRC()
	v := webcache.NewInvalidator(bus, rc, e.staff.App, quiet())
	v.Start()
	t.Cleanup(v.Stop)
	bus.push(realtime.Signal{Topic: realtime.TopicSystemResync, System: true})
	waitFullResync(t, rc, t1, t2)
}

// Valkey lỗi khi xóa khóa → resync, thử lại tới khi Valkey trả lời (bản cũ không sống tới hết hạn).
func TestInvalidator_FailureLeadsToResync(t *testing.T) {
	e := newEnv(t)
	t1 := e.staff.Tenant(t, "A")
	bus, rc := &pushBus{}, newFakeRC()
	rc.fail = 3 // Invalidate lỗi, rồi hai lượt resync đầu cũng lỗi
	v := webcache.NewInvalidator(bus, rc, e.staff.App, quiet())
	v.SetResyncRetry(20 * time.Millisecond)
	v.Start()
	t.Cleanup(v.Stop)
	bus.push(inv(t1, "product", "p1"))
	waitFullResync(t, rc, t1)
}

// Hàng chờ đầy (Valkey chậm) → không chặn hub, không mất: resync.
func TestInvalidator_QueueOverflowResyncs(t *testing.T) {
	e := newEnv(t)
	t1 := e.staff.Tenant(t, "A")
	bus, rc := &pushBus{}, newFakeRC()
	rc.gate = make(chan struct{})
	v := webcache.NewInvalidator(bus, rc, e.staff.App, quiet())
	v.Start()
	t.Cleanup(v.Stop)
	pushed := make(chan struct{})
	go func() {
		defer close(pushed)
		for i := range 3000 {
			bus.push(inv(t1, "variant", fmt.Sprintf("v%d", i)))
		}
	}()
	select {
	case <-pushed: // push không chặn dù rc đang treo
	case <-time.After(hangGuard):
		t.Fatal("hàm nhận của hub bị chặn")
	}
	close(rc.gate)
	waitFullResync(t, rc, t1)
}

// valkeyTestOpTimeout là hạn mỗi lệnh Valkey của test (Task 496): hạn mặc định 50 ms của mã chạy thật hết khi máy chạy
// test đang tải, lệnh coi như Valkey ngừng và test đếm nguồn, lượt gọi hãng đỏ dù mã đúng.
const valkeyTestOpTimeout = 2 * time.Second

// Từ đầu tới cuối (PostgreSQL, Valkey thật): P nằm trong bộ đệm; Emit price.changed P; worker chạy consumer; api nhận
// báo hiệu và xóa khóa → GetOrLoad(product, P) gọi lại load.
func TestEndToEnd_PriceChangedInvalidatesProduct(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	tenant := e.staff.Tenant(t, "A")
	vk := testkit.StartValkey(t)
	conn, err := cache.Dial(ctx, vk.URL("crm_cache"), cache.DialOptions{Name: "cache", OpTimeout: valkeyTestOpTimeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	rc := cache.NewReadCache(conn, cache.ReadCacheOptions{})
	inv := webcache.NewInvalidator(e.hub, rc, e.staff.App, quiet())
	inv.Start()
	t.Cleanup(inv.Stop)

	reg := events.NewRegistry()
	if err := reg.Register(webcache.NewConsumer(e.hub, quiet())); err != nil {
		t.Fatal(err)
	}
	p, v := uuid.New(), uuid.New()
	var loads int
	load := func(context.Context) ([]byte, error) {
		loads++
		return json.Marshal(map[string]int{"n": loads})
	}
	get := func() cache.Source {
		_, src, err := rc.GetOrLoad(ctx, tenant, cache.SpaceProduct, p.String(), time.Minute, load)
		if err != nil {
			t.Fatal(err)
		}
		return src
	}
	get()
	if src := get(); src != cache.SourceCache {
		t.Fatalf("P chưa vào bộ đệm: %q", src)
	}
	if err := tenancy.WithTenant(ctx, e.staff.App, tenant, func(tx pgx.Tx) error {
		_, _, err := reg.Emit(ctx, tx, queue.NewQueue(), events.Event{
			TenantID: tenant, AggregateType: "variant", AggregateID: v.String(), Seq: 1, Topic: catalog.TopicPriceChanged,
			Payload: catalog.PriceChanged{ProductID: p, VariantIDs: []uuid.UUID{v}, Price: 1000},
		})
		if err != nil {
			return fmt.Errorf("emit: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if get() != cache.SourceCache {
		t.Fatal("chưa chạy consumer mà bộ đệm đã đổi")
	}
	w := queue.NewWorker(e.staff.App, reg.JobHandlers(), queue.WorkerOptions{WorkerID: "w", Logger: quiet()})
	testkit.DrainDue(t, e.staff.Owner, w)
	deadline := time.Now().Add(hangGuard)
	for get() == cache.SourceCache {
		if time.Now().After(deadline) {
			t.Fatal("bộ đệm của P không được xóa sau khi consumer chạy")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if loads != 2 {
		t.Fatalf("load gọi %d lần, muốn 2", loads)
	}
}
