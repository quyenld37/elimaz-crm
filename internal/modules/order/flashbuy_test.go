package order_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Lõi lượt mua flash sale (kế hoạch M5, Task 201 / M5-18; ca FS03 quy mô nhỏ, FS05, FS07, FS12).

// flashEnv là orderEnv có nguồn đơn mặc định (nguồn "Website" key web), actor dịch vụ của khóa website và giờ bắt đầu
// của các đợt thử (đợt dài 2 giờ, lên lịch một giờ trước giờ bắt đầu).
type flashEnv struct {
	*orderEnv
	service order.Actor
	start   time.Time
	refSeq  atomic.Int64
}

func newFlashEnv(t *testing.T) *flashEnv {
	t.Helper()
	f := &flashEnv{orderEnv: newOrderEnv(t)}
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error { return order.EnsureDefaults(t.Context(), tx, f.tenant) })
	f.service = order.Actor{TenantID: f.tenant, UserID: uuid.New(), Type: order.ActorService}
	f.start = time.Now().UTC().Truncate(time.Second).Add(2 * time.Hour)
	return f
}

// flashSale là một đợt đã lên lịch có một món.
type flashSale struct {
	sale, item, variant uuid.UUID
}

// schedule tạo biến thể có slots+5 cái, đợt [start, start+2h) có một món slots suất, giới hạn limit mỗi khách, giữ
// hold phút, rồi lên lịch đợt (held + slots, trải slots đơn vị trống).
func (f *flashEnv) schedule(slots, limit, hold int) flashSale {
	f.t.Helper()
	v := f.variant(350000, int32(slots+5)) //nolint:gosec // test: slots nhỏ
	var s flashSale
	s.variant = v
	f.mustApp(func(tx pgx.Tx) error {
		ctx := f.t.Context()
		pa := promotion.Actor{TenantID: f.tenant, UserID: f.actor.UserID}
		sale, err := promotion.CreateFlashSale(ctx, tx, pa, promotion.FlashSaleInput{
			Name: "Đợt " + uuid.NewString()[:8], StartsAt: f.start, EndsAt: f.start.Add(2 * time.Hour),
		})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		it, err := promotion.AddFlashItem(ctx, tx, sale.ID, promotion.FlashItemInput{
			VariantID: v, SalePrice: 199000, Quantity: slots, PerCustomerLimit: limit, HoldMinutes: hold,
		})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		got, err := promotion.GetFlashSale(ctx, tx, sale.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		pw := promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		if _, err := pw.ScheduleFlashSale(ctx, tx, sale.ID, got.Version, pa, f.start.Add(-time.Hour)); err != nil {
			return err //nolint:wrapcheck // test
		}
		s.sale, s.item = sale.ID, it.ID
		return nil
	})
	return s
}

func (f *flashEnv) mustApp(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := f.app(fn); err != nil {
		f.t.Fatal(err)
	}
}

// customers tạo n khách mới.
func (f *flashEnv) customers(n int) []uuid.UUID {
	f.t.Helper()
	out := make([]uuid.UUID, n)
	f.mustApp(func(tx pgx.Tx) error {
		for i := range out {
			id, err := customer.Create(f.t.Context(), tx, fmt.Sprintf("Khách %d", i))
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			out[i] = id
		}
		return nil
	})
	return out
}

// in là lượt mua qty của khách c ở món của s, mã ngoài mới, lúc start + 1 phút.
func (f *flashEnv) in(s flashSale, c uuid.UUID, qty int) order.FlashBuyInput {
	return order.FlashBuyInput{
		ItemID: s.item, CustomerID: c, Qty: qty, ExternalRef: fmt.Sprintf("FS-%d", f.refSeq.Add(1)),
		Now: f.start.Add(time.Minute),
	}
}

// buyOnce chạy một lượt mua trong RetryTx với TxFlashBuy bằng crm_app (một lần, lỗi trả nguyên).
func (f *flashEnv) buyOnce(ctx context.Context, in order.FlashBuyInput) (order.FlashBuyResult, error) {
	var res order.FlashBuyResult
	err := tenancy.WithTenant(ctx, f.staff.App, f.tenant, func(tx pgx.Tx) error {
		var err error
		res, err = f.writer().BuyFlash(ctx, tx, in, f.service)
		return err //nolint:wrapcheck // test
	}, db.TxFlashBuy)
	return res, err //nolint:wrapcheck // test
}

// buy chạy lượt mua như điểm cuối: ErrFlashRetry hay hết lượt chạy lại của RetryTx → thử lại sau chốc lát.
func (f *flashEnv) buy(in order.FlashBuyInput) (order.FlashBuyResult, error) {
	for range 2000 {
		res, err := f.buyOnce(f.t.Context(), in)
		if errors.Is(err, order.ErrFlashRetry) || errors.Is(err, db.ErrRetryLater) {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		return res, err
	}
	return order.FlashBuyResult{}, errors.New("lượt mua thử lại quá 2000 lần")
}

func (f *flashEnv) mustBuy(in order.FlashBuyInput) order.FlashBuyResult {
	f.t.Helper()
	res, err := f.buy(in)
	if err != nil {
		f.t.Fatalf("mua: %v", err)
	}
	return res
}

// units đếm đơn vị suất của món theo trạng thái.
func (f *flashEnv) units(item uuid.UUID, status string) int {
	f.t.Helper()
	return f.count(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1 AND status = $2`,
		item, status)
}

// claimed là tổng số đã lấy của món (mọi khách).
func (f *flashEnv) claimed(item uuid.UUID) int {
	f.t.Helper()
	return f.count(`SELECT coalesce(sum(quantity), 0) FROM promotion.flash_sale_claims WHERE flash_sale_item_id = $1`,
		item)
}

// buyAll chạy các lượt mua đồng thời và trả kết quả theo thứ tự.
func (f *flashEnv) buyAll(ins []order.FlashBuyInput) []order.FlashBuyResult {
	f.t.Helper()
	res := make([]order.FlashBuyResult, len(ins))
	errs := make([]error, len(ins))
	var wg sync.WaitGroup
	for i, in := range ins {
		wg.Go(func() { res[i], errs[i] = f.buy(in) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			f.t.Fatalf("lượt %d: %v", i, err)
		}
	}
	return res
}

func countResults(res []order.FlashBuyResult) map[string]int {
	out := map[string]int{}
	for _, r := range res {
		out[r.Result]++
	}
	return out
}

// [FS03] 10 suất, 50 khách mỗi người một lượt qty 1 đồng thời → đúng 10 bought, 40 sold_out; 10 đơn vị held, 10 dòng
// giữ hàng held số lượng 1, held của số dư không đổi so với sau lên lịch; 10 đơn draft kênh web, hạn now + 10 phút,
// dòng giá flash, món flash; mỗi đơn có lịch sử và sự kiện order.created.
func TestFlashBuy_SoldOutUnderContention(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(10, 1, 10)
	heldBefore := f.balance(s.variant).Held
	cs := f.customers(50)
	ins := make([]order.FlashBuyInput, len(cs))
	for i, c := range cs {
		ins[i] = f.in(s, c, 1)
	}
	res := f.buyAll(ins)
	if got := countResults(res); got[order.FlashBought] != 10 || got[order.FlashSoldOut] != 40 || len(got) != 2 {
		t.Fatalf("kết quả %v, muốn 10 bought, 40 sold_out", got)
	}
	if n := f.units(s.item, "held"); n != 10 {
		t.Fatalf("đơn vị held %d, muốn 10", n)
	}
	if n := f.units(s.item, "available"); n != 0 {
		t.Fatalf("đơn vị trống %d, muốn 0", n)
	}
	if b := f.balance(s.variant); b.Held != heldBefore {
		t.Fatalf("held của số dư %d, muốn %d (không đổi sau lên lịch)", b.Held, heldBefore)
	}
	if n := f.count(`SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1 AND state = 'held'
		AND quantity = 1 AND order_item_id IS NOT NULL`, s.variant); n != 10 {
		t.Fatalf("dòng giữ hàng held %d, muốn 10", n)
	}
	exp := f.start.Add(time.Minute + 10*time.Minute)
	for _, r := range res {
		if r.Result != order.FlashBought {
			if r.OrderID != uuid.Nil {
				t.Fatalf("lượt %s có đơn %s", r.Result, r.OrderID)
			}
			continue
		}
		if r.Code == "" || !r.ExpiresAt.Equal(exp) {
			t.Fatalf("kết quả %+v, muốn mã đơn và hạn %s", r, exp)
		}
	}
	if n := f.count(`SELECT count(*) FROM ord.orders AS o JOIN ord.order_items AS i ON i.order_id = o.id
		WHERE o.status = 'draft' AND o.channel = 'web' AND o.draft_expires_at = $1 AND o.external_ref IS NOT NULL
		AND o.traffic_source_id = (SELECT id FROM ord.traffic_sources WHERE key = 'web')
		AND o.warehouse_id = $2 AND o.grand_total = 199000 AND o.cod_amount = 199000
		AND i.flash_sale_item_id = $3 AND i.unit_price = 199000 AND i.quantity = 1 AND i.net_amount = 199000`,
		exp, f.warehouse, s.item); n != 10 {
		t.Fatalf("đơn nháp flash đúng cột %d, muốn 10", n)
	}
	if n := f.count(`SELECT count(*) FROM ord.order_status_history AS h JOIN ord.orders AS o ON o.id = h.order_id
		WHERE o.channel = 'web' AND h.from_status IS NULL AND h.to_status = 'draft' AND h.actor_type = 'service'`); n != 10 {
		t.Fatalf("lịch sử tạo đơn %d, muốn 10", n)
	}
	if n := f.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'order.created'`); n != 10 {
		t.Fatalf("sự kiện order.created %d, muốn 10", n)
	}
	if n := f.claimed(s.item); n != 10 {
		t.Fatalf("tổng đã lấy %d, muốn 10", n)
	}
}

// [FS05] Giới hạn 2, một khách, 20 lượt đồng thời khác mã ngoài → đúng 2 bought, 18 over_limit; claims = 2.
func TestFlashBuy_PerCustomerLimit(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(30, 2, 10)
	c := f.customers(1)[0]
	ins := make([]order.FlashBuyInput, 20)
	for i := range ins {
		ins[i] = f.in(s, c, 1)
	}
	res := f.buyAll(ins)
	if got := countResults(res); got[order.FlashBought] != 2 || got[order.FlashOverLimit] != 18 || len(got) != 2 {
		t.Fatalf("kết quả %v, muốn 2 bought, 18 over_limit", got)
	}
	if n := f.claimed(s.item); n != 2 {
		t.Fatalf("đã lấy %d, muốn 2", n)
	}
	if n := f.units(s.item, "held"); n != 2 {
		t.Fatalf("đơn vị held %d, muốn 2", n)
	}
	// qty lớn hơn giới hạn của món: over_limit ngay, không ghi gì.
	if r := f.mustBuy(f.in(s, f.customers(1)[0], 3)); r.Result != order.FlashOverLimit {
		t.Fatalf("qty 3 > giới hạn 2: %+v", r)
	}
	if n := f.claimed(s.item); n != 2 {
		t.Fatalf("đã lấy sau qty 3: %d, muốn 2", n)
	}
}

// Cùng mã ngoài gọi hai lần (lần lượt, rồi đồng thời) → lần sau trả cùng đơn, không lấy thêm đơn vị, không tăng claim.
func TestFlashBuy_SameExternalRef(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(10, 3, 10)
	cs := f.customers(2)
	in := f.in(s, cs[0], 1)
	in.Now = in.Now.Add(123456789 * time.Nanosecond) // giờ có phần dưới micro giây: lần lặp vẫn trả đúng kết quả
	first := f.mustBuy(in)
	again := f.mustBuy(in)
	if first.Result != order.FlashBought || again != first {
		t.Fatalf("lần đầu %+v, lần hai %+v: muốn cùng đơn", first, again)
	}
	if f.units(s.item, "held") != 1 || f.claimed(s.item) != 1 {
		t.Fatalf("sau lặp: held %d, đã lấy %d; muốn 1, 1", f.units(s.item, "held"), f.claimed(s.item))
	}
	// Đồng thời: 8 lượt cùng một mã ngoài của khách thứ hai (giới hạn 3) → mọi lượt cùng một đơn.
	in2 := f.in(s, cs[1], 1)
	ins := make([]order.FlashBuyInput, 8)
	for i := range ins {
		ins[i] = in2
	}
	res := f.buyAll(ins)
	for _, r := range res {
		if r.Result != order.FlashBought || r.OrderID != res[0].OrderID {
			t.Fatalf("lượt cùng mã ngoài: %+v, muốn cùng đơn %s", r, res[0].OrderID)
		}
	}
	if f.units(s.item, "held") != 2 || f.claimed(s.item) != 2 {
		t.Fatalf("sau đồng thời: held %d, đã lấy %d; muốn 2, 2", f.units(s.item, "held"), f.claimed(s.item))
	}
	// Mã ngoài của đơn flash món khác → ErrDuplicateOrder (Quyết định 3).
	other := f.schedule(5, 1, 10)
	dup := f.in(other, cs[0], 1)
	dup.ExternalRef = in.ExternalRef
	if _, err := f.buy(dup); !errors.Is(err, order.ErrDuplicateOrder) {
		t.Fatalf("mã ngoài của món khác: %v, muốn ErrDuplicateOrder", err)
	}
}

// [Task 520] Mã ngoài đã có đơn flash cùng món của khách A, lượt của khách B (SĐT khác) cùng mã → ErrDuplicateOrder,
// không bao giờ trả đơn của A; claim, đơn vị không đổi; A gửi lại vẫn nhận đúng đơn của mình.
func TestFlashBuy_RefOfOtherCustomer(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(10, 3, 10)
	cs := f.customers(2)
	inA := f.in(s, cs[0], 1)
	first := f.mustBuy(inA)
	inB := inA
	inB.CustomerID = cs[1]
	if res, err := f.buy(inB); !errors.Is(err, order.ErrDuplicateOrder) || res.Code != "" || res.OrderID != uuid.Nil {
		t.Fatalf("mã ngoài của khách khác: %+v, %v; muốn ErrDuplicateOrder, không có đơn", res, err)
	}
	if f.units(s.item, "held") != 1 || f.claimed(s.item) != 1 {
		t.Fatalf("sau lượt bị từ chối: held %d, đã lấy %d; muốn 1, 1", f.units(s.item, "held"), f.claimed(s.item))
	}
	if again := f.mustBuy(inA); again != first {
		t.Fatalf("khách A gửi lại: %+v, muốn %+v", again, first)
	}
}

// refOutcome là kết quả một lượt mua của khách thứ who (0, 1).
type refOutcome struct {
	who int
	res order.FlashBuyResult
	err error
}

// buyRefRace chạy n lượt của mỗi khách cs[0], cs[1] đồng thời cùng một mã ngoài mới ở món của s.
func (f *flashEnv) buyRefRace(s flashSale, cs []uuid.UUID, n int) []refOutcome {
	ref := f.in(s, cs[0], 1).ExternalRef
	outs := make([]refOutcome, 2*n)
	var wg sync.WaitGroup
	for i := range outs {
		in := f.in(s, cs[i%2], 1)
		in.ExternalRef = ref
		wg.Go(func() {
			res, err := f.buy(in)
			outs[i] = refOutcome{who: i % 2, res: res, err: err}
		})
	}
	wg.Wait()
	return outs
}

// [Task 520] Song song: 8 lượt của khách A và 8 lượt của khách B cùng một mã ngoài mới → mọi lượt của một khách bought
// cùng một đơn, mọi lượt của khách kia ErrDuplicateOrder (không có đơn); đúng 1 đơn vị held, đã lấy 1.
func TestFlashBuy_RefOfOtherCustomerParallel(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(10, 3, 10)
	outs := f.buyRefRace(s, f.customers(2), 8)
	winner := slices.IndexFunc(outs, func(o refOutcome) bool { return o.err == nil && o.res.Result == order.FlashBought })
	if winner < 0 {
		t.Fatalf("song song: không lượt nào bought: %+v", outs)
	}
	w := outs[winner]
	for _, o := range outs {
		okWinner := o.who == w.who && o.err == nil && o.res.Result == order.FlashBought && o.res.OrderID == w.res.OrderID
		okLoser := o.who != w.who && errors.Is(o.err, order.ErrDuplicateOrder) && o.res.OrderID == uuid.Nil
		if !okWinner && !okLoser {
			t.Fatalf("lượt của khách %d: %+v, %v; khách có đơn %d, đơn %s", o.who, o.res, o.err, w.who, w.res.OrderID)
		}
	}
	if f.units(s.item, "held") != 1 || f.claimed(s.item) != 1 {
		t.Fatalf("sau song song: held %d, đã lấy %d; muốn 1, 1", f.units(s.item, "held"), f.claimed(s.item))
	}
}

// [ADR-40] Một transaction khác khóa 1 trong 2 đơn vị cuối 300 ms, khách mua qty 2 → ErrFlashRetry; sau khi lùi:
// claims không đổi, 2 đơn vị vẫn available.
func TestFlashBuy_LockedUnitsRetry(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(2, 2, 10)
	c := f.customers(1)[0]
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.app(func(tx pgx.Tx) error {
			if _, err := tx.Exec(f.t.Context(), `SELECT id FROM inventory.flash_sale_units
				WHERE flash_sale_item_id = $1 ORDER BY id DESC LIMIT 1 FOR UPDATE`, s.item); err != nil {
				return err //nolint:wrapcheck // test
			}
			close(locked)
			select {
			case <-release:
			case <-time.After(300 * time.Millisecond):
			}
			return nil
		})
	}()
	<-locked
	_, err := f.buyOnce(t.Context(), f.in(s, c, 2))
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, order.ErrFlashRetry) {
		t.Fatalf("mua khi 1/2 đơn vị bị khóa: %v, muốn ErrFlashRetry", err)
	}
	if f.claimed(s.item) != 0 || f.units(s.item, "available") != 2 {
		t.Fatalf("sau lùi: đã lấy %d, trống %d; muốn 0, 2", f.claimed(s.item), f.units(s.item, "available"))
	}
	if n := f.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Fatalf("có %d đơn sau lùi", n)
	}
	// Khóa đã nhả: lượt sau mua được cả 2.
	if r := f.mustBuy(f.in(s, c, 2)); r.Result != order.FlashBought {
		t.Fatalf("sau khi nhả khóa: %+v", r)
	}
}

// [FS07] 0 đơn vị trống, 3 held → sold_out ("hết suất" là không còn suất trống); còn 1 trống mà mua 2 → sold_out, không
// giữ đơn vị nào, claim trả lại.
func TestFlashBuy_SoldOutMeansNoFreeSlot(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(4, 2, 10)
	cs := f.customers(5)
	for _, c := range cs[:3] {
		if r := f.mustBuy(f.in(s, c, 1)); r.Result != order.FlashBought {
			t.Fatalf("mua: %+v", r)
		}
	}
	if r := f.mustBuy(f.in(s, cs[3], 2)); r.Result != order.FlashSoldOut {
		t.Fatalf("còn 1 mua 2: %+v, muốn sold_out", r)
	}
	if f.units(s.item, "available") != 1 || f.claimed(s.item) != 3 {
		t.Fatalf("sau sold_out: trống %d, đã lấy %d; muốn 1, 3", f.units(s.item, "available"), f.claimed(s.item))
	}
	if r := f.mustBuy(f.in(s, cs[3], 1)); r.Result != order.FlashBought {
		t.Fatalf("mua suất cuối: %+v", r)
	}
	if r := f.mustBuy(f.in(s, cs[4], 1)); r.Result != order.FlashSoldOut {
		t.Fatalf("0 trống, 4 held: %+v, muốn sold_out", r)
	}
	if f.units(s.item, "held") != 4 || f.claimed(s.item) != 4 {
		t.Fatalf("held %d, đã lấy %d; muốn 4, 4", f.units(s.item, "held"), f.claimed(s.item))
	}
}

// [QĐ8] Trước starts_at → not_open; sau ends_at với đợt vẫn ghi open (nhịp chưa đóng) → not_open; đợt nháp → not_open.
// Không ghi gì.
func TestFlashBuy_NotOpen(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(5, 1, 10)
	c := f.customers(1)[0]
	early := f.in(s, c, 1)
	early.Now = f.start.Add(-time.Second)
	if r := f.mustBuy(early); r.Result != order.FlashNotOpen {
		t.Fatalf("trước giờ mở: %+v", r)
	}
	pw := promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	f.mustApp(func(tx pgx.Tx) error {
		_, _, err := pw.Tick(t.Context(), tx, f.start.Add(time.Minute))
		return err //nolint:wrapcheck // test
	})
	if n := f.count(`SELECT count(*) FROM promotion.flash_sales WHERE id = $1 AND status = 'open'`, s.sale); n != 1 {
		t.Fatal("đợt chưa mở sau nhịp")
	}
	late := f.in(s, c, 1)
	late.Now = f.start.Add(2 * time.Hour)
	if r := f.mustBuy(late); r.Result != order.FlashNotOpen {
		t.Fatalf("đúng giờ kết thúc, đợt vẫn open: %+v", r)
	}
	var draftItem uuid.UUID
	v := f.variant(350000, 5)
	f.mustApp(func(tx pgx.Tx) error {
		sale, err := promotion.CreateFlashSale(t.Context(), tx, promotion.Actor{TenantID: f.tenant, UserID: f.actor.UserID},
			promotion.FlashSaleInput{Name: "Nháp", StartsAt: f.start, EndsAt: f.start.Add(time.Hour)})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		it, err := promotion.AddFlashItem(t.Context(), tx, sale.ID, promotion.FlashItemInput{
			VariantID: v, SalePrice: 199000, Quantity: 1, PerCustomerLimit: 1, HoldMinutes: 10,
		})
		draftItem = it.ID
		return err //nolint:wrapcheck // test
	})
	draft := f.in(flashSale{item: draftItem}, c, 1)
	if r := f.mustBuy(draft); r.Result != order.FlashNotOpen {
		t.Fatalf("đợt nháp: %+v", r)
	}
	if f.claimed(s.item) != 0 || f.count(`SELECT count(*) FROM ord.orders`) != 0 {
		t.Fatal("lượt not_open đã ghi")
	}
	if _, err := f.buy(f.in(flashSale{item: uuid.New()}, c, 1)); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("món không có: %v, muốn promotion.ErrNotFound", err)
	}
	if _, err := f.buy(f.in(s, c, 0)); !errors.Is(err, order.ErrInvalidInput) {
		t.Fatalf("qty 0: %v, muốn ErrInvalidInput", err)
	}
}

// [FS12] Tiến trình chết giữa chừng: hàm trong RetryTx panic ngay sau lượt mua (bước cuối là HoldFlashUnits) → 0 đơn,
// đơn vị vẫn available, claims 0.
func TestFlashBuy_PanicRollsBack(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(3, 1, 10)
	c := f.customers(1)[0]
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("không panic")
			}
		}()
		_ = tenancy.WithTenant(t.Context(), f.staff.App, f.tenant, func(tx pgx.Tx) error {
			r, err := f.writer().BuyFlash(t.Context(), tx, f.in(s, c, 1), f.service)
			if err != nil || r.Result != order.FlashBought {
				t.Errorf("mua trước panic: %+v, %v", r, err)
			}
			panic("tiến trình chết")
		}, db.TxFlashBuy)
	}()
	if f.count(`SELECT count(*) FROM ord.orders`) != 0 || f.units(s.item, "available") != 3 || f.claimed(s.item) != 0 {
		t.Fatalf("sau panic: đơn %d, trống %d, đã lấy %d", f.count(`SELECT count(*) FROM ord.orders`),
			f.units(s.item, "available"), f.claimed(s.item))
	}
	if n := f.count(`SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1`, s.variant); n != 0 {
		t.Fatalf("còn %d dòng giữ hàng", n)
	}
}

// capturingTx ghi câu và tham số của lần Query cuối, để EXPLAIN đúng câu sqlc ứng dụng chạy.
type capturingTx struct {
	pgx.Tx
	sql  string
	args []any
}

//nolint:ireturn // hiện thực pgx.Tx
func (c *capturingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if c.sql == "" {
		c.sql, c.args = sql, args
	}
	return c.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

// Câu lấy đơn vị (TakeFlashUnits) trên món 15.000 đơn vị bằng crm_app → kế hoạch không có Seq Scan.
func TestFlashBuy_TakePlan(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(15000, 1, 10)
	if _, err := f.staff.Owner.Pool.Exec(t.Context(), "ANALYZE inventory.flash_sale_units"); err != nil {
		t.Fatal(err)
	}
	ct := &capturingTx{}
	f.mustApp(func(tx pgx.Tx) error {
		ct.Tx = tx
		ids, _, err := inventory.TakeFlashUnits(t.Context(), ct, s.item, 2)
		if err == nil && len(ids) != 2 {
			err = fmt.Errorf("lấy %d đơn vị, muốn 2", len(ids))
		}
		return err
	})
	nodes, err := testkit.ExplainAsApp(t.Context(), f.staff.App, f.tenant, ct.sql, ct.args...)
	if err != nil {
		t.Fatal(err)
	}
	testkit.RequireNoSeqScan(t, nodes)
}
