package inventory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Kiểm kho mở sổ (kế hoạch M7, Task 262 = M7-17; MO02). Dữ liệu chung: 3 mẫu × 4 biến thể active (mẫu thứ ba của
// người bán PARTNER1), thêm 1 biến thể discontinued và 1 mẫu đã xóa có 1 biến thể active — không phải đếm → 12 dòng.

// openingFixture tạo dữ liệu chung và trả 12 biến thể phải đếm (theo thứ tự tạo).
func (e *invEnv) openingFixture() []uuid.UUID {
	e.t.Helper()
	sellers := e.sellers()
	var out, products []uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		for i, seller := range []uuid.UUID{sellers[0], sellers[0], sellers[1]} {
			p, err := e.fixtureProduct(tx, seller, fmt.Sprintf("MS%d", i+1), false)
			if err != nil {
				return err
			}
			products = append(products, p)
			for j := range 4 {
				v, err := e.fixtureVariant(tx, seller, p, fmt.Sprintf("MS%d-%d", i+1, j+1), "active")
				if err != nil {
					return err
				}
				out = append(out, v)
			}
		}
		if _, err := e.fixtureVariant(tx, sellers[0], products[0], "MS1-NGUNG", "discontinued"); err != nil {
			return err
		}
		p, err := e.fixtureProduct(tx, sellers[0], "MSXOA", true)
		if err != nil {
			return err
		}
		_, err = e.fixtureVariant(tx, sellers[0], p, "MSXOA-1", "active")
		return err
	})
	return out
}

func (e *invEnv) fixtureProduct(tx pgx.Tx, seller uuid.UUID, code string, deleted bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(e.t.Context(), `INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status,
		deleted_at) VALUES ($1, $2, $3, 'Mẫu mở sổ', 'mau mo so', 'active', CASE WHEN $4 THEN now() END)
		RETURNING id`, e.tenant, seller, code, deleted).Scan(&id)
	return id, err //nolint:wrapcheck // test
}

func (e *invEnv) fixtureVariant(tx pgx.Tx, seller, p uuid.UUID, sku, status string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(e.t.Context(), `INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color,
		size, price, status) VALUES ($1, $2, $3, $4, $4, 'Đỏ', $4, 100000, $5) RETURNING id`,
		e.tenant, seller, p, sku, status).Scan(&id)
	return id, err //nolint:wrapcheck // test
}

func (e *invEnv) createOpening(wh uuid.UUID) (inventory.Stocktake, int, error) {
	var s inventory.Stocktake
	var n int
	err := e.app(func(tx pgx.Tx) error {
		var err error
		s, n, err = inventory.CreateOpening(e.t.Context(), tx, wh, staffActor())
		return err //nolint:wrapcheck // test
	})
	return s, n, err
}

func (e *invEnv) mustCreateOpening(wh uuid.UUID) inventory.Stocktake {
	e.t.Helper()
	s, _, err := e.createOpening(wh)
	if err != nil {
		e.t.Fatalf("CreateOpening: %v", err)
	}
	return s
}

func (e *invEnv) postOpening(id uuid.UUID) (inventory.PostResult, error) {
	var r inventory.PostResult
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		var err error
		r, err = w.PostOpening(ctx, tx, id, staffActor())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *invEnv) checkOpening(id uuid.UUID) (inventory.CheckResult, error) {
	var r inventory.CheckResult
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = inventory.CheckOpening(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *invEnv) getOpening(id uuid.UUID) inventory.OpeningStatus {
	e.t.Helper()
	var o inventory.OpeningStatus
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		o, err = inventory.GetOpening(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetOpening: %v", err)
	}
	return o
}

// countAll ghi số đếm counts[i] cho vs[i] trong một transaction (RecordCount của M1).
func (e *invEnv) countAll(id uuid.UUID, vs []uuid.UUID, counts []int32) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		for i, v := range vs {
			if _, err := inventory.RecordCount(e.t.Context(), tx, id, v, counts[i], nil, staffActor()); err != nil {
				return fmt.Errorf("RecordCount %d: %w", i, err)
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *invEnv) onHandAt(v, wh uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(on_hand), 0)::int FROM inventory.stock_balances
		WHERE variant_id = $1 AND warehouse_id = $2`, v, wh)
}

// MO02, KH04: mở trên kho trống (12 dòng phải đếm), mở trùng → ErrOpeningExists, thiếu một dòng → UncountedLinesError,
// đủ dòng → đăng: on_hand = số đếm, sổ đúng số dòng có đếm > 0, có thể bán = số đếm; kiểm lại khớp, sổ bị sửa → lệch
// đúng biến thể; sau đăng không đếm được nữa, phiên mở sổ thứ hai bị từ chối, phiên kiểm thường so on_hand − in_transit.
func TestOpening_CreateCountPostCheck(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	s, n, err := e.createOpening(e.warehouse)
	if err != nil || n != 12 || s.Kind != inventory.StocktakeKindOpening || s.Status != inventory.StocktakeOpen {
		t.Fatalf("CreateOpening: %+v, %d dòng, %v", s, n, err)
	}
	if o := e.getOpening(s.ID); o.LinesExpected != 12 || o.LinesCounted != 0 || o.UnitsCounted != 0 ||
		o.WarehouseID != e.warehouse || o.AppliedAt != nil {
		t.Fatalf("trạng thái mới mở: %+v", o)
	}
	if _, _, err := e.createOpening(e.warehouse); !errors.Is(err, inventory.ErrOpeningExists) {
		t.Fatalf("mở lần hai: mong ErrOpeningExists, có %v", err)
	}

	counts := make([]int32, len(vs))
	for i := range vs[:len(vs)-1] { // dòng cuối đếm 0
		counts[i] = int32((i * 7) % 5)
	}
	units, positive := sumCounts(counts)
	e.countAll(s.ID, vs[:len(vs)-1], counts[:len(vs)-1])
	_, err = e.postOpening(s.ID)
	if ue, ok := errors.AsType[*inventory.UncountedLinesError](err); !ok || ue.N != 1 || !errors.Is(err, inventory.ErrUncountedLines) {
		t.Fatalf("đăng khi thiếu 1 dòng: mong UncountedLinesError{1}, có %v", err)
	}
	e.countAll(s.ID, vs[len(vs)-1:], counts[len(vs)-1:])
	if o := e.getOpening(s.ID); o.LinesCounted != 12 || o.UnitsCounted != units {
		t.Fatalf("trạng thái đã đếm đủ: %+v, mong 12 dòng, %d cái", o, units)
	}
	r, err := e.postOpening(s.ID)
	if err != nil || r.LinesExpected != 12 || r.CountedLines != 12 || r.Units != units ||
		r.Stocktake.Status != inventory.StocktakeApplied {
		t.Fatalf("PostOpening: %+v, %v", r, err)
	}
	e.assertOpeningPosted(s.ID, vs, counts, units, positive)
	e.assertOpeningTamper(s.ID, vs[2], counts[2], "MS1-3")
	e.assertOpeningClosed(s.ID, vs[0])
	// KH05 không đổi: phiên kiểm thường sau mở sổ chụp expected_on_shelf = on_hand − in_transit.
	reg := e.mustStart()
	if reg.Kind != inventory.StocktakeKindRegular {
		t.Fatalf("phiên thường: kind %q", reg.Kind)
	}
	if l := e.mustCount(reg.ID, vs[1], 1); l.ExpectedOnShelf != counts[1] || l.Delta != 1-counts[1] {
		t.Fatalf("phiên thường sau mở sổ: %+v, mong expected %d", l, counts[1])
	}
}

// sumCounts trả tổng số đếm và số dòng có đếm > 0.
func sumCounts(counts []int32) (units int64, positive int) {
	for _, c := range counts {
		units += int64(c)
		if c > 0 {
			positive++
		}
	}
	return units, positive
}

// assertOpeningPosted: on_hand và có thể bán (KH04) = số đếm, sổ có đúng positive dòng, tổng delta = units, phiên
// applied, CheckOpening khớp.
func (e *invEnv) assertOpeningPosted(id uuid.UUID, vs []uuid.UUID, counts []int32, units int64, positive int) {
	e.t.Helper()
	for i, v := range vs {
		if got := e.onHandAt(v, e.warehouse); got != int(counts[i]) {
			e.t.Fatalf("biến thể %d: on_hand %d, mong %d", i, got, counts[i])
		}
		if b := e.balance(v); b.Available != counts[i] {
			e.t.Fatalf("biến thể %d: có thể bán %d, mong %d", i, b.Available, counts[i])
		}
	}
	if got := e.stocktakeLedger(id); got != positive {
		e.t.Fatalf("sổ: %d dòng, mong %d", got, positive)
	}
	if got := e.count(`SELECT coalesce(sum(delta), 0)::int FROM inventory.stock_ledger
		WHERE doc_type = 'stocktake' AND doc_id = $1`, id); int64(got) != units {
		e.t.Fatalf("tổng sổ %d, mong %d", got, units)
	}
	if o := e.getOpening(id); o.Status != inventory.StocktakeApplied || o.AppliedAt == nil {
		e.t.Fatalf("trạng thái sau đăng: %+v", o)
	}
	if c, err := e.checkOpening(id); err != nil || !c.OK || c.ChangedSince || len(c.Mismatches) != 0 {
		e.t.Fatalf("CheckOpening: %+v, %v", c, err)
	}
}

// assertOpeningTamper chèn tay (chủ bảng) một dòng sổ +1 của phiên cho biến thể v → CheckOpening nêu đúng v.
func (e *invEnv) assertOpeningTamper(id, v uuid.UUID, counted int32, sku string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket,
			delta, doc_type, doc_id, step, actor_type) VALUES ($1, $2, $3, 'on_hand', 1, 'stocktake', $4, 'tamper', 'system')`,
			e.tenant, v, e.warehouse, id)
		return err //nolint:wrapcheck // test
	})
	c, err := e.checkOpening(id)
	if err != nil || c.OK || c.ChangedSince || len(c.Mismatches) != 1 {
		e.t.Fatalf("CheckOpening sau khi sửa sổ: %+v, %v", c, err)
	}
	if m := c.Mismatches[0]; m.VariantID != v || m.Counted != counted || m.LedgerOpening != counted+1 ||
		m.OnHand != counted || m.SKU != sku {
		e.t.Fatalf("biến thể lệch: %+v", m)
	}
}

// assertOpeningClosed: sau đăng không đếm, không đăng lại, không mở sổ lần hai được.
func (e *invEnv) assertOpeningClosed(id, v uuid.UUID) {
	e.t.Helper()
	if _, err := e.recordCount(id, v, 9, nil); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		e.t.Fatalf("đếm sau đăng: mong ErrStocktakeNotOpen, có %v", err)
	}
	if _, err := e.postOpening(id); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		e.t.Fatalf("đăng lần hai: mong ErrStocktakeNotOpen, có %v", err)
	}
	if _, _, err := e.createOpening(e.warehouse); !errors.Is(err, inventory.ErrOpeningExists) {
		e.t.Fatalf("mở sổ lần hai sau đăng: mong ErrOpeningExists, có %v", err)
	}
}

// Kho đã có dòng sổ (phiếu nhập) → không mở được; phiên kiểm thường đang mở → ErrStocktakeOpen; kho lạ → ErrNotFound.
func TestOpening_CreateRefused(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	w2 := e.secondWarehouse()
	if err := e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(t.Context(), tx, vs[0], w2, 3) }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.createOpening(w2); !errors.Is(err, inventory.ErrWarehouseNotEmpty) {
		t.Fatalf("kho có phiếu nhập: mong ErrWarehouseNotEmpty, có %v", err)
	}
	reg := e.mustStart()
	if _, _, err := e.createOpening(e.warehouse); !errors.Is(err, inventory.ErrStocktakeOpen) {
		t.Fatalf("kho có phiên thường mở: mong ErrStocktakeOpen, có %v", err)
	}
	if err := e.cancelStocktake(reg.ID, reg.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.createOpening(uuid.New()); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("kho lạ: mong ErrNotFound, có %v", err)
	}
	// Hủy phiên mở sổ rồi mở lại được (index bỏ qua phiên đã hủy).
	s := e.mustCreateOpening(e.warehouse)
	if err := e.cancelStocktake(s.ID, s.Version); err != nil {
		t.Fatal(err)
	}
	e.mustCreateOpening(e.warehouse)
}

// Phiếu nhập ghi sổ vào kho sau khi mở phiên, trước khi đăng (transaction khác) → đăng bị từ chối, không dòng sổ nào
// của phiên, phiên còn mở. Kiểm lại phiên chưa đăng → ErrOpeningNotPosted.
func TestOpening_ImportBeforePost(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	counts := make([]int32, len(vs))
	for i := range counts {
		counts[i] = 2
	}
	e.countAll(s.ID, vs, counts)
	if _, err := e.checkOpening(s.ID); !errors.Is(err, inventory.ErrOpeningNotPosted) {
		t.Fatalf("kiểm phiên chưa đăng: mong ErrOpeningNotPosted, có %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(t.Context(), tx, vs[5], e.warehouse, 1) }); err != nil {
		t.Fatal(err)
	}
	if _, err := e.postOpening(s.ID); !errors.Is(err, inventory.ErrWarehouseNotEmpty) {
		t.Fatalf("đăng sau phiếu nhập: mong ErrWarehouseNotEmpty, có %v", err)
	}
	if n := e.stocktakeLedger(s.ID); n != 0 {
		t.Fatalf("sổ của phiên còn %d dòng", n)
	}
	if o := e.getOpening(s.ID); o.Status != inventory.StocktakeOpen {
		t.Fatalf("phiên phải còn mở: %+v", o)
	}
}

// Dòng đếm chụp expected_on_shelf ≠ 0 (kho có hàng lúc đếm) → ErrWarehouseNotEmpty ở bước đăng.
func TestOpening_LineExpectedNotZero(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	if err := e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(t.Context(), tx, vs[0], e.warehouse, 4) }); err != nil {
		t.Fatal(err)
	}
	counts := make([]int32, len(vs))
	e.countAll(s.ID, vs, counts)
	if _, err := e.postOpening(s.ID); !errors.Is(err, inventory.ErrWarehouseNotEmpty) {
		t.Fatalf("mong ErrWarehouseNotEmpty, có %v", err)
	}
}

// Đường áp dụng của kiểm kho thường không đăng được phiên mở sổ; phiên thường không đăng được bằng PostOpening.
func TestOpening_RoutesSeparated(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	s := e.mustCreateOpening(e.warehouse)
	e.countAll(s.ID, vs, make([]int32, len(vs)))
	if _, err := e.applyStocktake(s.ID); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("ApplyStocktake trên phiên mở sổ: mong ErrInvalidInput, có %v", err)
	}
	if got, _ := e.getStocktake(s.ID); got.Status != inventory.StocktakeOpen {
		t.Fatalf("phiên phải còn mở: %+v", got)
	}
	w2 := e.secondWarehouse()
	var reg inventory.Stocktake
	if err := e.app(func(tx pgx.Tx) (err error) {
		reg, err = inventory.StartStocktake(t.Context(), tx, w2, staffActor())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.postOpening(reg.ID); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("PostOpening trên phiên thường: mong ErrNotFound, có %v", err)
	}
	if _, err := e.checkOpening(reg.ID); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("CheckOpening trên phiên thường: mong ErrNotFound, có %v", err)
	}
}

// Hai lần mở cùng kho song song → một phiên, bên kia ErrOpeningExists; hai lần đăng song song → một lần ghi sổ, bên
// kia ErrStocktakeNotOpen.
func TestOpening_Concurrent(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.openingFixture()
	errs := make([]error, 2)
	ids := make([]inventory.Stocktake, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { ids[i], _, errs[i] = e.createOpening(e.warehouse) })
	}
	wg.Wait()
	ok := 0
	var s inventory.Stocktake
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
			s = ids[i]
		case !errors.Is(err, inventory.ErrOpeningExists):
			t.Fatalf("mở song song: lỗi lạ %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("mở song song: %d phiên, mong 1 (%v)", ok, errs)
	}
	counts := make([]int32, len(vs))
	for i := range counts {
		counts[i] = 3
	}
	e.countAll(s.ID, vs, counts)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.postOpening(s.ID) })
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("đăng song song: %v / %v", errs[0], errs[1])
	}
	for _, err := range errs {
		if err != nil && !errors.Is(err, inventory.ErrStocktakeNotOpen) {
			t.Fatalf("đăng song song: lỗi lạ %v", err)
		}
	}
	if n := e.stocktakeLedger(s.ID); n != len(vs) {
		t.Fatalf("sổ: %d dòng, mong %d", n, len(vs))
	}
	if got := e.onHandAt(vs[0], e.warehouse); got != 3 {
		t.Fatalf("on_hand %d, mong 3", got)
	}
}
