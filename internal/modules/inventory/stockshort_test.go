package inventory_test

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Kiểm kho thiếu dưới phần giữ (Task 406, A-01 của review độc lập M1): kiểm kho ghi đúng số đếm (Q8), còn bước lấy
// hàng, giao chặn bằng ErrStockShort thay vì lỗi CHECK; không trừ âm, không tự nhả giữ.

// shortAfterStocktake: biến thể on_hand 5, giữ 2 + 2 (hai dòng đơn), đếm 2 rồi áp dụng → on_hand 2, held 4. Trả biến
// thể, mã giữ hàng, dòng giữ hàng, kết quả áp dụng.
func (e *invEnv) shortAfterStocktake() (uuid.UUID, []uuid.UUID, []inventory.ReserveLine, inventory.ApplyResult) {
	e.t.Helper()
	v := e.stock(5)
	lines := []inventory.ReserveLine{e.line(v, 2), e.line(v, 2)}
	ids, err := e.reserve(sysActor(), lines...)
	if err != nil {
		e.t.Fatalf("Reserve: %v", err)
	}
	s := e.mustStart()
	e.mustCount(s.ID, v, 2)
	r, err := e.applyStocktake(s.ID)
	if err != nil {
		e.t.Fatalf("ApplyStocktake: %v", err)
	}
	want(e.t, e.balance(v), inventory.Balance{OnHand: 2, Held: 4, Available: -2})
	return v, ids, lines, r
}

// Đúng ca của A-01: sau kiểm kho thiếu, đơn 1 lấy hàng và giao được; đơn 2 lấy hàng → ErrStockShort (kèm biến thể, kho,
// mã giữ hàng, cần 2, trên kệ 0), giữ hàng vẫn held, số dư và sổ không đổi; đếm lại được (không lỗi CHECK của
// expected_on_shelf); nhập thêm 2 thì đơn 2 lấy hàng, giao xong; cuối cùng mọi ngăn về 0 và sổ = số dư.
func TestStockShort_PickupBlockedAfterStocktake(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v, ids, _, _ := e.shortAfterStocktake()
	if err := e.startTransit(ids[:1], sysActor()); err != nil {
		t.Fatalf("lấy hàng đơn 1: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, Held: 2, InTransit: 2, Available: -2})
	ledger := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1", v)

	err := e.startTransit(ids[1:], sysActor())
	var se *inventory.StockShortError
	if !errors.Is(err, inventory.ErrStockShort) || !errors.As(err, &se) {
		t.Fatalf("lấy hàng đơn 2: %v; mong ErrStockShort", err)
	}
	if se.ReservationID != ids[1] || se.VariantID != v || se.WarehouseID != e.warehouse || se.Requested != 2 || se.OnShelf != 0 {
		t.Fatalf("chi tiết lỗi sai: %+v", se)
	}
	if errors.Is(err, inventory.ErrStateConflict) || errors.Is(err, inventory.ErrInsufficient) {
		t.Fatalf("ErrStockShort không được trùng nghĩa lỗi khác: %v", err)
	}
	if st := e.reservationState(ids[1]); st != inventory.StateHeld {
		t.Fatalf("giữ hàng đơn 2 thành %s, mong held", st)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, Held: 2, InTransit: 2, Available: -2})
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1", v); n != ledger {
		t.Fatalf("sổ đổi khi bị chặn: %d → %d dòng", ledger, n)
	}

	if err := e.consume(ids[:1], sysActor()); err != nil {
		t.Fatalf("giao đơn 1: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 0, Held: 2, Available: -2})

	s := e.mustStart() // đếm lại được: expected_on_shelf = 0, không âm
	if l := e.mustCount(s.ID, v, 0); l.ExpectedOnShelf != 0 || l.Delta != 0 {
		t.Fatalf("đếm lại: %+v", l)
	}
	cur, _ := e.getStocktake(s.ID)
	if err := e.cancelStocktake(s.ID, cur.Version); err != nil {
		t.Fatalf("hủy phiên đếm lại: %v", err)
	}

	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindImport}, line(v, 2)), staffActor()); err != nil {
		t.Fatalf("nhập thêm: %v", err)
	}
	if err := e.startTransit(ids[1:], sysActor()); err != nil {
		t.Fatalf("lấy hàng đơn 2 sau khi nhập: %v", err)
	}
	if err := e.consume(ids[1:], sysActor()); err != nil {
		t.Fatalf("giao đơn 2: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{})
	if msg := e.checkInvariants(v); msg != "" {
		t.Fatal(msg)
	}
}

// Áp dụng phiên thiếu trả, cho từng biến thể thiếu, người bán của biến thể và mọi dòng giữ hàng held (theo mã giữ hàng
// tăng dần) kèm dòng đơn, số lượng — để nhân viên xử lý; dòng đã đi giao không vào danh sách.
func TestStockShort_ApplyListsHeldOrders(t *testing.T) {
	t.Parallel()
	eachSeller(t, func(t *testing.T, e *invEnv) {
		v := e.stock(6)
		lines := []inventory.ReserveLine{e.line(v, 2), e.line(v, 1), e.line(v, 3)}
		ids, err := e.reserve(sysActor(), lines...)
		if err != nil {
			t.Fatalf("Reserve: %v", err)
		}
		if err := e.startTransit(ids[2:], sysActor()); err != nil { // dòng 3 đã đi giao: on_shelf 3
			t.Fatalf("StartTransit: %v", err)
		}
		other := e.stock(1) // biến thể không thiếu: không vào danh sách
		s := e.mustStart()
		e.mustCount(s.ID, v, 1)
		e.mustCount(s.ID, other, 1)
		r, err := e.applyStocktake(s.ID)
		if err != nil {
			t.Fatalf("ApplyStocktake: %v", err)
		}
		if len(r.Shortages) != 1 {
			t.Fatalf("Shortages: %+v", r.Shortages)
		}
		sh := r.Shortages[0]
		if sh.VariantID != v || sh.Available != -2 || sh.SellerID != e.seller {
			t.Fatalf("thiếu: %+v (người bán mong %v)", sh, e.seller)
		}
		type hold struct {
			id, item uuid.UUID
			q        int32
		}
		exp := []hold{{ids[0], lines[0].OrderItemID, 2}, {ids[1], lines[1].OrderItemID, 1}}
		slices.SortFunc(exp, func(a, b hold) int { return compareUUIDs(a.id, b.id) })
		got := make([]hold, 0, len(sh.Holds))
		for _, h := range sh.Holds {
			got = append(got, hold{h.ReservationID, h.OrderItemID, h.Quantity})
		}
		if !slices.Equal(got, exp) {
			t.Fatalf("giữ hàng thiếu: %+v, mong %+v", got, exp)
		}
	})
}

// Hai đơn cùng lấy phần cuối trên kệ song song (on_hand 2, giữ 2 + 2 sau kiểm kho): đúng một qua, một ErrStockShort;
// không lỗi khác, số dư khớp.
func TestStockShort_ConcurrentPickup(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v, ids, _, _ := e.shortAfterStocktake()
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() { errs[i] = e.startTransit(ids[i:i+1], sysActor()) })
	}
	wg.Wait()
	ok, short := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, inventory.ErrStockShort):
			short++
		default:
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
	}
	if ok != 1 || short != 1 {
		t.Fatalf("qua %d, chặn %d; mong 1 và 1", ok, short)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, Held: 2, InTransit: 2, Available: -2})
	if msg := e.checkLedgerAndHolds(v); msg != "" {
		t.Fatal(msg)
	}
}

// Lớp chặn thứ hai ở bước giao: số dư đã lệch sẵn (on_hand 1 < in_transit 2, dựng bằng chủ bảng như dữ liệu cũ) →
// Consume trả ErrStockShort (trên kệ = on_hand 1), không lỗi CHECK, giữ hàng vẫn in_transit, số dư không đổi.
func TestStockShort_ConsumeGuard(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v, res := e.inTransit(2, 2)
	ids := []uuid.UUID{res}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE inventory.stock_balances SET on_hand = 1 WHERE variant_id = $1`, v)
		return err //nolint:wrapcheck // test
	})
	err := e.consume(ids, sysActor())
	var se *inventory.StockShortError
	if !errors.As(err, &se) || !errors.Is(err, inventory.ErrStockShort) {
		t.Fatalf("Consume: %v; mong ErrStockShort", err)
	}
	if se.ReservationID != ids[0] || se.Requested != 2 || se.OnShelf != 1 {
		t.Fatalf("chi tiết lỗi sai: %+v", se)
	}
	if st := e.reservationState(ids[0]); st != inventory.StateInTransit {
		t.Fatalf("giữ hàng thành %s, mong in_transit", st)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 1, InTransit: 2, Available: -1})
}

// Xuất hàng lỗi không được trừ on_hand xuống dưới phần đang giao (giữ bất biến in_transit ≤ on_hand mà bước giao dựa
// vào): on_hand 3, giữ 2 đã đi giao, hỏng 1, đếm 0 → on_hand 2, damaged 1, in_transit 2; xuất hàng lỗi 1 →
// InsufficientError (có thể xuất 0), số dư không đổi; đơn vẫn giao xong.
func TestStockShort_DamageExportKeepsInTransit(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v, res := e.inTransit(3, 2)
	ids := []uuid.UUID{res}
	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindDamage}, line(v, 1)), staffActor()); err != nil {
		t.Fatalf("ghi hàng lỗi: %v", err)
	}
	s := e.mustStart()
	e.mustCount(s.ID, v, 0)
	if _, err := e.applyStocktake(s.ID); err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, InTransit: 2, Damaged: 1, Available: -1})
	_, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindDamageExport}, line(v, 1)), staffActor())
	var ie *inventory.InsufficientError
	if !errors.As(err, &ie) || ie.Available != 0 || ie.Requested != 1 {
		t.Fatalf("xuất hàng lỗi: %v; mong InsufficientError có thể xuất 0", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, InTransit: 2, Damaged: 1, Available: -1})
	if err := e.consume(ids, sysActor()); err != nil {
		t.Fatalf("giao: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{Damaged: 1, Available: -1})
}

func compareUUIDs(a, b uuid.UUID) int {
	switch {
	case uuidLess(a, b):
		return -1
	case uuidLess(b, a):
		return 1
	}
	return 0
}
