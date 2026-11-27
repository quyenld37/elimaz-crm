package promotion_test

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// TestFlashLifecycle_ConcurrentInvariants (rapid, số lượt của gói): mỗi lượt 2–3 biến thể (tồn 0–12), 2 đợt (cùng
// giờ hoặc nối tiếp, mỗi đợt 1–3 món của các biến thể đó, 1–6 suất), rồi 3–8 thao tác lên lịch, thêm suất, đóng đợt
// chạy đồng thời theo thứ tự ngẫu nhiên, mỗi thao tác một transaction thẳng (không chạy lại: deadlock không bị che).
// Sau đó đóng mọi đợt. Quy luật: không thao tác nào lỗi database (kể cả deadlock 40P01) và pg_stat_database.deadlocks
// không tăng; held ≥ 0 và bằng giữ hàng + đơn vị trống; sổ khớp số dư; sau khi đóng không còn đơn vị available.
func TestFlashLifecycle_ConcurrentInvariants(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	before := e.staff.Count(t, deadlocksQuery)
	rapid.Check(t, func(rt *rapid.T) {
		vs, sales := e.drawFlashRound(rt)
		ops := drawFlashOps(rt)
		for i, err := range e.runFlashOps(sales, ops) {
			if pg, ok := errors.AsType[*pgconn.PgError](err); ok {
				rt.Fatalf("thao tác %d (%+v): lỗi database %s %s", i, ops[i], pg.Code, pg.Message)
			}
			if err != nil && !expectedFlashRefusal(err) {
				rt.Fatalf("thao tác %d (%+v): %v", i, ops[i], err)
			}
		}
		e.checkBooks(vs...)
		e.closeAll(rt, sales)
		e.checkBooks(vs...)
	})
	time.Sleep(1500 * time.Millisecond) // số liệu thống kê của backend đẩy lên tối đa mỗi giây
	if after := e.staff.Count(t, deadlocksQuery); after != before {
		t.Errorf("pg_stat_database.deadlocks %d → %d", before, after)
	}
}

// deadlocksQuery đếm deadlock của database thử (chủ bảng, ngoài transaction nghiệp vụ).
const deadlocksQuery = `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`

// flashOp là một thao tác ngẫu nhiên: kind 0 lên lịch, 1 thêm add suất cho món item, 2 đóng đợt sale.
type flashOp struct{ kind, sale, item, add int }

// drawFlashRound tạo 2–3 biến thể (tồn 0–12) và 2 đợt nháp có món của các biến thể đó.
func (e *env) drawFlashRound(rt *rapid.T) ([]uuid.UUID, []promotion.FlashSale) {
	nv := rapid.IntRange(2, 3).Draw(rt, "variants")
	vs := make([]uuid.UUID, nv)
	for i := range vs {
		if stock := rapid.Int32Range(0, 12).Draw(rt, fmt.Sprintf("stock%d", i)); stock > 0 {
			vs[i] = e.stockVariant(stock)
		} else {
			vs[i] = e.variant(e.shop.Seller, 350000)
		}
	}
	apart := rapid.Bool().Draw(rt, "apart")
	sales := make([]promotion.FlashSale, 2)
	for si := range sales {
		start := flashStart()
		if apart && si == 1 {
			start = start.Add(time.Hour)
		}
		s, err := e.createFlash(promotion.FlashSaleInput{
			Name: fmt.Sprintf("Đợt %d", si), StartsAt: start, EndsAt: start.Add(time.Hour),
		})
		if err != nil {
			rt.Fatal(err)
		}
		picks := rapid.SliceOfNDistinct(rapid.IntRange(0, nv-1), 1, 3, rapid.ID[int]).Draw(rt, fmt.Sprintf("items%d", si))
		for _, vi := range picks {
			if _, err := e.addFlashItem(s.ID, flashItem(vs[vi], 199000, rapid.IntRange(1, 6).Draw(rt, "qty"))); err != nil {
				rt.Fatal(err)
			}
		}
		sales[si] = e.getFlash(s.ID)
	}
	return vs, sales
}

func drawFlashOps(rt *rapid.T) []flashOp {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) flashOp {
		return flashOp{
			kind: rapid.IntRange(0, 2).Draw(t, "kind"), sale: rapid.IntRange(0, 1).Draw(t, "sale"),
			item: rapid.IntRange(0, 2).Draw(t, "item"), add: rapid.IntRange(1, 4).Draw(t, "add"),
		}
	}), 3, 8).Draw(rt, "ops")
}

// runFlashOps chạy mọi thao tác đồng thời, mỗi thao tác một transaction thẳng; trả lỗi theo thứ tự ops.
func (e *env) runFlashOps(sales []promotion.FlashSale, ops []flashOp) []error {
	var wg sync.WaitGroup
	errs := make([]error, len(ops))
	for i, o := range ops {
		s := sales[o.sale]
		wg.Go(func() { errs[i] = e.rawTx(func(tx pgx.Tx) error { return e.applyFlashOp(tx, s, o) }) })
	}
	wg.Wait()
	return errs
}

func (e *env) applyFlashOp(tx pgx.Tx, s promotion.FlashSale, o flashOp) error {
	w, ctx := e.writer(), e.t.Context()
	var err error
	switch o.kind {
	case 0:
		_, err = w.ScheduleFlashSale(ctx, tx, s.ID, s.Version, e.actor(), beforeStart())
	case 1:
		_, err = w.AddSlots(ctx, tx, s.ID, s.Items[o.item%len(s.Items)].ID, o.add, e.actor())
	default:
		_, err = w.CloseFlashSale(ctx, tx, s.ID, promotion.FlashClosedManual, e.actor())
	}
	return err //nolint:wrapcheck // test
}

// closeAll đóng mọi đợt (đợt nháp, đã đóng: bỏ qua) rồi kiểm không còn đơn vị trống.
func (e *env) closeAll(rt *rapid.T, sales []promotion.FlashSale) {
	for _, s := range sales {
		if err := e.closeFlash(s.ID, promotion.FlashClosedManual); err != nil && !errors.Is(err, promotion.ErrFlashNotActive) {
			rt.Fatal(err)
		}
		if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM inventory.flash_sale_units AS u
			JOIN promotion.flash_sale_items AS i ON i.id = u.flash_sale_item_id
			WHERE i.flash_sale_id = $1 AND u.status = 'available'`, s.ID); n != 0 {
			rt.Fatalf("đợt %s còn %d đơn vị trống sau khi đóng", s.ID, n)
		}
	}
}

// expectedFlashRefusal: lỗi nghiệp vụ hợp lệ khi thao tác chạy theo thứ tự bất kỳ.
func expectedFlashRefusal(err error) bool {
	for _, target := range []error{
		promotion.ErrNotEditable, promotion.ErrFlashNotActive, promotion.ErrFlashInsufficient, promotion.ErrFlashOverlap,
		promotion.ErrInvalidInput,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// rawTx chạy fn trong một transaction mở thẳng bằng crm_app của tenant (không RetryTx: lỗi 40P01 không bị che).
func (e *env) rawTx(fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(e.t.Context(), e.staff.App.Pool, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		if err := tenancy.SetTenant(e.t.Context(), tx, e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		return fn(tx)
	})
}

// Nhịp đóng hai đợt cùng hết giờ (A có biến thể X lớn, B có biến thể W nhỏ, id A < id B) trong khi một transaction
// khác (như Reserve của đơn hai dòng) khóa số dư W rồi X theo đúng thứ tự ADR-46: transaction kia giữ W, nhịp chạy,
// rồi transaction kia khóa X. Nhịp phải khóa số dư mọi đợt cần đóng theo (variant, warehouse) tăng dần trên cả
// transaction (chờ W trước, không giữ X) → không lỗi database (40P01), cả hai đợt đóng, sổ khớp.
func TestFlashTick_CloseManyNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w, x := e.stockVariant(10), e.stockVariant(10)
	if compareIDs(w, x) > 0 {
		w, x = x, w
	}
	end := flashStart().Add(time.Hour)
	a, b := e.draftAt(end.Add(-time.Hour), end, map[uuid.UUID]int{x: 2}), e.draftAt(end.Add(-time.Hour), end, map[uuid.UUID]int{w: 2})
	for _, s := range []promotion.FlashSale{a, b} {
		if err := e.schedule(s, beforeStart()); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var tickErr error
	otherErr := e.rawTx(func(tx pgx.Tx) error {
		ref := func(v uuid.UUID) []inventory.FlashItemRef {
			return []inventory.FlashItemRef{{VariantID: v, WarehouseID: e.shop.Warehouse, Quantity: 1}}
		}
		if _, err := inventory.LockFlashStock(t.Context(), tx, ref(w)); err != nil {
			return err //nolint:wrapcheck // test
		}
		wg.Go(func() {
			tickErr = e.rawTx(func(tx pgx.Tx) error {
				_, _, err := e.writer().Tick(t.Context(), tx, end)
				return err //nolint:wrapcheck // test
			})
		})
		time.Sleep(300 * time.Millisecond) // nhịp đã khóa xong những gì nó khóa được trước W
		_, err := inventory.LockFlashStock(t.Context(), tx, ref(x))
		return err //nolint:wrapcheck // test
	})
	wg.Wait()
	if otherErr != nil || tickErr != nil {
		t.Fatalf("transaction khóa W rồi X: %v; nhịp: %v", otherErr, tickErr)
	}
	for _, s := range []promotion.FlashSale{a, b} {
		if st := e.getFlash(s.ID).Status; st != promotion.FlashClosed {
			t.Fatalf("đợt %s còn %s", s.ID, st)
		}
	}
	e.checkBooks(w, x)
}

func compareIDs(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }
