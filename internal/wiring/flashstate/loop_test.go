package flashstate_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
	"elimaz.com.vn/crm/internal/wiring/flashstate"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// Vòng dựng lại cờ hết suất và số còn trên cổng flash sale (Task 204, M5-21): PostgreSQL thật (crm_app), hub LISTEN
// thật, Valkey gate thật (container riêng của test). Ca [FS06], [FS07], [HT27].

// hangGuard là chốt chống treo khi chờ trạng thái đổi; không phải tiêu chí đúng/sai về thời gian (Task 400).
const hangGuard = 30 * time.Second

// fastEvery là nhịp vòng của ca cần vòng chạy đều; slowEvery: nhịp đủ dài để chỉ báo hiệu làm đổi trạng thái.
const (
	fastEvery = 50 * time.Millisecond
	slowEvery = time.Hour
)

type env struct {
	t      *testing.T
	staff  *testkit.Staff
	hub    *realtime.PGHub
	vk     *testkit.Valkey
	gate   cache.AdmissionGate
	shift  atomic.Int64 // độ lệch đồng hồ của cổng (ns)
	tenant uuid.UUID
	reg    *events.Registry
	worker *queue.Worker
	sale   flashtest.Sale
	start  time.Time
	seq    atomic.Int64
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// newEnv: database đã migrate, một shop có một biến thể tồn 20, đợt đang trong giờ mở (đã lên lịch) một món slots suất
// giới hạn 1, giữ 10 phút; hub đã nghe; consumer webcache đăng ký để order.cancelled thành báo hiệu flash_item.
func newEnv(t *testing.T, slots int) *env {
	t.Helper()
	e := &env{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.startHub(e.staff.App.Pool.Config().ConnString())
	e.vk = testkit.StartValkey(t)
	conn, err := cache.Dial(t.Context(), e.vk.URL("crm_gate"), cache.DialOptions{Name: "gate", OpTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	e.gate = cache.NewAdmissionGate(conn, cache.GateOptions{
		Now: func() time.Time { return time.Now().Add(time.Duration(e.shift.Load())) },
	}, nil)
	e.reg = events.NewRegistry()
	if err := e.reg.Register(webcache.NewConsumer(e.hub, quiet())); err != nil {
		t.Fatal(err)
	}
	e.worker = queue.NewWorker(e.staff.App, e.reg.JobHandlers(), queue.WorkerOptions{Logger: quiet()})
	cat := shoptest.SeedCatalog(t, e.staff.Owner, e.tenant, shoptest.SeedOptions{Variants: []shoptest.SeedVariant{
		{SKU: "FS1", Price: 350000, Stock: 20},
	}})
	e.start = time.Now().UTC().Add(-time.Minute)
	e.app(func(tx pgx.Tx) error {
		if err := order.EnsureDefaults(t.Context(), tx, e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		e.sale = flashtest.Schedule(t, tx, flashtest.Item{
			Variant: cat.Variants["FS1"], Price: 199000, Slots: slots, Limit: 1, HoldMinutes: 10, StartsAt: e.start,
			TenantID: e.tenant, UserID: uuid.New(),
		})
		return nil
	})
	return e
}

// startHub chạy hub LISTEN tới khi test xong và chờ nó nghe được (Status listening).
func (e *env) startHub(appURL string) {
	e.hub = realtime.NewPGHub(realtime.PGHubOptions{URL: appURL, Logger: quiet()})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { e.hub.Run(ctx) })
	e.t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	e.waitFor("hub nghe được", func() bool { listening, _ := e.hub.Status(); return listening })
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của env.
func (e *env) app(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	must(e.t, tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn, db.TxAPI))
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// loop dựng và chạy vòng với nhịp every; dừng khi test xong (Stop gọi nhiều lần không sao).
func (e *env) loop(every time.Duration) *flashstate.Loop {
	l := flashstate.New(e.staff.App, e.gate, e.hub, every)
	l.Start(e.t.Context())
	e.t.Cleanup(l.Stop)
	return l
}

// waitFor chờ cond đúng (hỏi mỗi 10 ms); quá hangGuard thì test đỏ.
func (e *env) waitFor(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(hangGuard)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("chờ quá lâu: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// state đọc trạng thái món của cổng: số còn (-1 khi không biết), cờ, biết hay không.
func (e *env) state() (remaining int, soldOut, known bool) {
	soldOut, known = e.gate.SoldOut(e.t.Context(), e.tenant, e.sale.Item)
	remaining = -1
	if n, ok := e.gate.Remaining(e.t.Context(), e.tenant, []uuid.UUID{e.sale.Item})[e.sale.Item]; ok {
		remaining = n
	}
	return remaining, soldOut, known
}

func (e *env) waitState(what string, remaining int, soldOut bool) {
	e.t.Helper()
	e.waitFor(what, func() bool {
		r, s, k := e.state()
		return k && r == remaining && s == soldOut
	})
}

func (e *env) writer() order.Writer {
	return order.Writer{Events: e.reg, Queue: queue.NewQueue(), Realtime: e.hub}
}

// buy: một khách mới bấm mua một suất (khách tạo trong transaction riêng như lớp HTTP); trả kết quả.
func (e *env) buy() string {
	e.t.Helper()
	ctx := e.t.Context()
	n := e.seq.Add(1)
	var cust uuid.UUID
	e.app(func(tx pgx.Tx) error {
		c, err := customer.Writer{Events: e.reg, Queue: queue.NewQueue(), Realtime: e.hub}.FindOrCreateForOrder(ctx, tx,
			customer.OrderCustomerInput{
				FullName: "Khách website", Phone: fmt.Sprintf("+8490%07d", n), PhoneIsCustomers: true,
				Source: customer.SourceWeb,
			}, customer.Actor{Type: customer.ActorService, ID: uuid.New()}, time.Now())
		cust = c.CustomerID
		return err //nolint:wrapcheck // test
	})
	var res order.FlashBuyResult
	if err := tenancy.WithTenant(ctx, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		res, err = e.writer().BuyFlash(ctx, tx, order.FlashBuyInput{
			ItemID: e.sale.Item, CustomerID: cust, Qty: 1, ExternalRef: fmt.Sprintf("FS-%d", n), Now: time.Now().UTC(),
		}, order.Actor{TenantID: e.tenant, UserID: uuid.New(), Type: order.ActorService})
		return err //nolint:wrapcheck // test
	}, db.TxFlashBuy); err != nil {
		e.t.Fatalf("mua: %v", err)
	}
	return res.Result
}

// [FS07] Vòng chạy đều: lúc đầu số còn = số suất, cờ tắt; mua hết 2 suất → vòng ghi số còn 0, cờ bật.
func TestLoop_SoldOutFlag(t *testing.T) {
	t.Parallel()
	e := newEnv(t, 2)
	e.loop(fastEvery)
	e.waitState("số còn ban đầu", 2, false)
	for range 2 {
		if r := e.buy(); r != order.FlashBought {
			t.Fatalf("mua: %s", r)
		}
	}
	e.waitState("hết suất", 0, true)
	if r := e.buy(); r != order.FlashSoldOut {
		t.Fatalf("lượt sau khi hết: %s", r)
	}
}

// [FS06] Nhịp dài (chỉ lượt đầu khi Start và báo hiệu làm đổi trạng thái): mua hết 1 suất, cờ bật (như lớp HTTP ghi
// sau sold_out); đơn nháp hết hạn → order.cancelled → worker chạy consumer webcache → báo hiệu flash_item → vòng tính
// lại đúng món: cờ tắt, số còn 1. Thêm 2 suất (flash_sale.changed → báo hiệu flash_sale) → số còn 3.
func TestLoop_SignalsRecompute(t *testing.T) {
	t.Parallel()
	e := newEnv(t, 1)
	e.loop(slowEvery)
	e.waitState("lượt đầu khi Start", 1, false)
	if r := e.buy(); r != order.FlashBought {
		t.Fatalf("mua: %s", r)
	}
	if err := e.gate.SetState(t.Context(), e.tenant, e.sale.Item, 0, true); err != nil {
		t.Fatal(err)
	}
	e.staff.DrainJobs(t, e.worker) // sự kiện của lượt mua (không báo hiệu flash)
	if r, s, _ := e.state(); r != 0 || !s {
		t.Fatalf("trước khi hết hạn: còn %d, cờ %v", r, s)
	}
	e.app(func(tx pgx.Tx) error {
		n, err := e.writer().ExpireDrafts(t.Context(), tx, time.Now().Add(11*time.Minute), 500)
		if err == nil && n != 1 {
			err = fmt.Errorf("hết hạn %d đơn, muốn 1", n)
		}
		return err
	})
	e.staff.DrainJobs(t, e.worker)
	e.waitState("suất trả lại sau khi đơn nháp hết hạn", 1, false)

	e.app(func(tx pgx.Tx) error {
		_, err := promotion.Writer{Events: e.reg, Queue: queue.NewQueue(), Realtime: e.hub}.AddSlots(t.Context(), tx,
			e.sale.ID, e.sale.Item, 2, promotion.Actor{TenantID: e.tenant, UserID: uuid.New()})
		return err //nolint:wrapcheck // test
	})
	e.staff.DrainJobs(t, e.worker)
	e.waitState("thêm suất", 3, false)
}

// clearSpy bọc cổng thật, ghi các món đã ClearState thành công (phân biệt "vòng xóa" với "tự hết hạn 10 s").
type clearSpy struct {
	cache.AdmissionGate
	mu      sync.Mutex
	cleared []uuid.UUID
}

func (c *clearSpy) ClearState(ctx context.Context, tenant, item uuid.UUID) error {
	err := c.AdmissionGate.ClearState(ctx, tenant, item)
	if err == nil {
		c.mu.Lock()
		c.cleared = append(c.cleared, item)
		c.mu.Unlock()
	}
	return err //nolint:wrapcheck // test
}

func (c *clearSpy) clearedItems() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]uuid.UUID(nil), c.cleared...)
}

// [Task 520] Nhịp dài: lượt đầu ghi trạng thái món, chưa xóa gì; đóng đợt (flash_sale.closed → báo hiệu flash_sale)
// → vòng gọi ClearState đúng món đó, ngay sau đó SoldOut không biết, số còn không có (không phải tự hết hạn 10 s).
func TestLoop_CloseClearsState(t *testing.T) {
	t.Parallel()
	e := newEnv(t, 2)
	spy := &clearSpy{AdmissionGate: e.gate}
	l := flashstate.New(e.staff.App, spy, e.hub, slowEvery)
	l.Start(t.Context())
	t.Cleanup(l.Stop)
	e.waitState("lượt đầu khi Start", 2, false)
	if got := spy.clearedItems(); len(got) != 0 {
		t.Fatalf("trước khi đóng đợt đã xóa %v", got)
	}
	e.app(func(tx pgx.Tx) error {
		_, err := promotion.Writer{Events: e.reg, Queue: queue.NewQueue(), Realtime: e.hub}.CloseFlashSale(t.Context(), tx,
			e.sale.ID, promotion.FlashClosedManual, promotion.Actor{TenantID: e.tenant, UserID: uuid.New()})
		return err //nolint:wrapcheck // test
	})
	e.staff.DrainJobs(t, e.worker)
	e.waitFor("vòng xóa trạng thái sau khi đóng đợt", func() bool { return len(spy.clearedItems()) > 0 })
	if got := spy.clearedItems(); len(got) != 1 || got[0] != e.sale.Item {
		t.Fatalf("món đã xóa %v, muốn [%s]", got, e.sale.Item)
	}
	if r, s, k := e.state(); k || s || r != -1 {
		t.Fatalf("sau khi xóa: còn %d, cờ %v, biết %v", r, s, k)
	}
}

// [HT27] FLUSHALL Valkey → cổng mất trạng thái; vòng ghi lại đúng số còn và cờ.
func TestLoop_FlushAllRebuilds(t *testing.T) {
	t.Parallel()
	e := newEnv(t, 1)
	e.loop(fastEvery)
	if r := e.buy(); r != order.FlashBought {
		t.Fatalf("mua: %s", r)
	}
	e.waitState("hết suất", 0, true)
	e.vk.FlushAll(t)
	e.waitState("dựng lại sau FLUSHALL", 0, true)
}

// Dừng vòng (giả lập tiến trình api chết): quá 10 s theo đồng hồ của cổng → trạng thái hết hạn, SoldOut không biết,
// số còn không có; lượt mua vẫn đi xuống database và đúng (sold_out).
func TestLoop_StopExpiresState(t *testing.T) {
	t.Parallel()
	e := newEnv(t, 1)
	l := e.loop(fastEvery)
	if r := e.buy(); r != order.FlashBought {
		t.Fatalf("mua: %s", r)
	}
	e.waitState("hết suất", 0, true)
	l.Stop()
	l.Stop() // gọi lần hai không sao
	e.shift.Store(int64(11 * time.Second))
	if r, s, k := e.state(); k || s || r != -1 {
		t.Fatalf("sau khi vòng dừng 11 s: còn %d, cờ %v, biết %v", r, s, k)
	}
	if r := e.buy(); r != order.FlashSoldOut {
		t.Fatalf("lượt mua khi cổng không biết: %s", r)
	}
}
