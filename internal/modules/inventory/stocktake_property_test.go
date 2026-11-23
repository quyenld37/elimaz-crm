package inventory_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

type countSnap struct {
	counted, shelfAtCount int32 // số đếm, on_hand − in_transit ngay sau lúc ghi dòng
}

// TestStocktakeInvariants (rapid, số lượt của TestMain ≥ 200): xen kẽ ngẫu nhiên giữ hàng, gửi đi, giao xong, nhập
// kho, đếm (kể cả đếm lại cùng biến thể) trong một phiên rồi áp dụng →
// (1) áp dụng thành công đúng khi mọi biến thể đã đếm có số đếm + thay đổi sau lúc đếm ≥ 0, ngược lại ErrInsufficient
// và không gì đổi; (2) sau áp dụng on_hand − in_transit = số đếm + thay đổi của on_hand − in_transit xảy ra sau lúc
// đếm dòng đó; (3) held và trạng thái mọi dòng giữ hàng không đổi qua áp dụng; (4) sổ = số dư, giữ hàng khớp số dư.
func TestStocktakeInvariants(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		r := &stkRun{
			e: e, vs: e.variants(rapid.IntRange(1, 3).Draw(rt, "variants")),
			snaps: map[uuid.UUID]countSnap{}, lineVer: map[uuid.UUID]int64{},
		}
		for _, v := range r.vs {
			if n := rapid.Int32Range(0, 6).Draw(rt, "stock"); n > 0 {
				e.seed(v, n)
			}
		}
		r.s = e.mustStart()
		for range rapid.IntRange(1, 15).Draw(rt, "ops") {
			r.step(rt)
		}
		r.applyAndCheck(rt)
	})
}

// stkRun là trạng thái của một lượt: phiên, biến thể, ảnh chụp lúc đếm, version dòng, giữ hàng đã tạo.
type stkRun struct {
	e       *invEnv
	s       inventory.Stocktake
	vs      []uuid.UUID
	snaps   map[uuid.UUID]countSnap
	lineVer map[uuid.UUID]int64
	res     []uuid.UUID
}

// step chạy một thao tác ngẫu nhiên; thiếu hàng, sai trạng thái là kết quả hợp lệ.
func (r *stkRun) step(rt *rapid.T) {
	e := r.e
	v := r.vs[rapid.IntRange(0, len(r.vs)-1).Draw(rt, "variant")]
	q := rapid.Int32Range(1, 3).Draw(rt, "qty")
	var err error
	switch rapid.IntRange(0, 4).Draw(rt, "kind") {
	case 0:
		var ids []uuid.UUID
		ids, err = e.reserve(sysActor(), e.line(v, q))
		r.res = append(r.res, ids...)
	case 1, 2:
		if len(r.res) == 0 {
			return
		}
		ids := []uuid.UUID{r.res[rapid.IntRange(0, len(r.res)-1).Draw(rt, "res")]}
		if rapid.Bool().Draw(rt, "consume") {
			err = e.consume(ids, sysActor())
		} else {
			err = e.startTransit(ids, sysActor())
		}
	case 3:
		err = e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(e.t.Context(), tx, v, e.warehouse, q) })
	default:
		r.count(rt, v)
	}
	if err != nil && !errors.Is(err, inventory.ErrInsufficient) && !errors.Is(err, inventory.ErrStateConflict) {
		rt.Fatalf("lỗi không mong đợi: %v", err)
	}
}

// count ghi một số đếm ngẫu nhiên cho v (ghi lại với version dòng khi đã có) và chụp phần trên kệ lúc đó.
func (r *stkRun) count(rt *rapid.T, v uuid.UUID) {
	c := rapid.Int32Range(0, 8).Draw(rt, "counted")
	var lv *int64
	if ver, ok := r.lineVer[v]; ok {
		lv = &ver
	}
	l, err := r.e.recordCount(r.s.ID, v, c, lv)
	if err != nil {
		rt.Fatalf("RecordCount: %v", err)
	}
	r.lineVer[v] = l.Version
	r.snaps[v] = countSnap{counted: c, shelfAtCount: r.e.shelf(v)}
	if l.ExpectedOnShelf != r.snaps[v].shelfAtCount || l.Delta != c-l.ExpectedOnShelf {
		rt.Fatalf("dòng đếm %+v, phần trên kệ lúc đếm %d", l, r.snaps[v].shelfAtCount)
	}
}

// applyAndCheck áp dụng phiên và kiểm các quy luật (1)–(4); phiên không áp được thì hủy để lượt sau mở phiên mới.
func (r *stkRun) applyAndCheck(rt *rapid.T) {
	e := r.e
	before := map[uuid.UUID]inventory.Balance{}
	expectOK := true
	for _, v := range r.vs {
		before[v] = e.availability(v)
		if sn, ok := r.snaps[v]; ok && sn.counted+e.shelf(v)-sn.shelfAtCount < 0 {
			expectOK = false
		}
	}
	states := e.reservationStates(r.res)
	_, err := e.applyStocktake(r.s.ID)
	switch {
	case expectOK && err != nil:
		rt.Fatalf("mong áp dụng được, có %v", err)
	case !expectOK && !errors.Is(err, inventory.ErrInsufficient):
		rt.Fatalf("mong ErrInsufficient, có %v", err)
	}
	for _, v := range r.vs {
		r.checkVariant(rt, v, before[v], expectOK)
	}
	if got := e.reservationStates(r.res); !slices.Equal(got, states) {
		rt.Fatalf("trạng thái giữ hàng đổi: %v → %v", states, got)
	}
	if !expectOK {
		cur, _ := e.getStocktake(r.s.ID)
		if err := e.cancelStocktake(r.s.ID, cur.Version); err != nil {
			rt.Fatalf("hủy phiên: %v", err)
		}
		return
	}
	r.afterApply(rt)
}

// afterApply (Task 406, A-01): sau khi áp dụng (có thể thiếu dưới phần giữ) chạy ngẫu nhiên lấy hàng, giao, nhả, nhập,
// phiếu kho mọi loại → chỉ có ErrInsufficient, ErrStateConflict, ErrStockShort (không lỗi CHECK); mọi cột ≥ 0,
// in_transit ≤ on_hand, sổ = số dư; rồi đếm lại mọi biến thể trong một phiên mới được (expected_on_shelf ≥ 0).
func (r *stkRun) afterApply(rt *rapid.T) {
	e := r.e
	for range rapid.IntRange(0, 6).Draw(rt, "after") {
		v := r.vs[rapid.IntRange(0, len(r.vs)-1).Draw(rt, "afterVariant")]
		op := propOp{
			docKind: rapid.IntRange(0, len(docKinds)-1).Draw(rt, "afterDocKind"), sign: rapid.Bool().Draw(rt, "afterSign"),
			qty: rapid.Int32Range(1, 3).Draw(rt, "afterQty"),
		}
		var err error
		switch k := rapid.IntRange(0, 4).Draw(rt, "afterKind"); {
		case k == 3:
			err = e.postRandomDoc(v, op)
		case k == 4:
			err = e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(e.t.Context(), tx, v, e.warehouse, op.qty) })
		case len(r.res) > 0:
			ids := []uuid.UUID{r.res[rapid.IntRange(0, len(r.res)-1).Draw(rt, "afterRes")]}
			switch k {
			case 0:
				err = e.startTransit(ids, sysActor())
			case 1:
				err = e.consume(ids, sysActor())
			default:
				err = e.release(ids, "prop")
			}
		}
		if err != nil && !errors.Is(err, inventory.ErrInsufficient) && !errors.Is(err, inventory.ErrStateConflict) &&
			!errors.Is(err, inventory.ErrStockShort) {
			rt.Fatalf("sau áp dụng: lỗi không mong đợi: %v", err)
		}
	}
	for _, v := range r.vs {
		if msg := e.checkLedgerAndHolds(v); msg != "" {
			rt.Fatalf("sau áp dụng, biến thể %v: %s", v, msg)
		}
	}
	// Một transaction, rồi lùi (lỗi giả): phiên đếm lại không để lại gì cho lượt sau.
	errRecounted := errors.New("đã đếm lại")
	err := e.app(func(tx pgx.Tx) error {
		ctx := e.t.Context()
		s, err := inventory.StartStocktake(ctx, tx, e.warehouse, staffActor())
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, v := range r.vs {
			if _, err := inventory.RecordCount(ctx, tx, s.ID, v, 0, nil, staffActor()); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return errRecounted
	})
	if !errors.Is(err, errRecounted) {
		rt.Fatalf("đếm lại sau áp dụng: %v", err)
	}
}

func (r *stkRun) checkVariant(rt *rapid.T, v uuid.UUID, before inventory.Balance, applied bool) {
	after := r.e.availability(v)
	shelfBefore := before.OnHand - before.InTransit
	wantShelf := shelfBefore
	if sn, ok := r.snaps[v]; ok && applied {
		wantShelf = sn.counted + shelfBefore - sn.shelfAtCount
	}
	if got := after.OnHand - after.InTransit; got != wantShelf {
		rt.Fatalf("biến thể %v: on_hand − in_transit %d, mong %d (trước %+v, đếm %+v)", v, got, wantShelf, before, r.snaps[v])
	}
	if after.Held != before.Held || after.InTransit != before.InTransit || after.Damaged != before.Damaged {
		rt.Fatalf("biến thể %v: phiên kiểm đổi ngăn khác on_hand: trước %+v, sau %+v", v, before, after)
	}
	if msg := r.e.checkLedgerAndHolds(v); msg != "" {
		rt.Fatalf("biến thể %v: %s", v, msg)
	}
}

// availability đọc số dư của v (chưa có dòng số dư → 0).
func (e *invEnv) availability(v uuid.UUID) (b inventory.Balance) {
	e.t.Helper()
	err := e.app(func(tx pgx.Tx) error {
		bs, err := inventory.Availability(e.t.Context(), tx, e.warehouse, []uuid.UUID{v})
		if err == nil {
			b = bs[0]
		}
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		e.t.Fatalf("Availability: %v", err)
	}
	return b
}

func (e *invEnv) shelf(v uuid.UUID) int32 {
	b := e.availability(v)
	return b.OnHand - b.InTransit
}

func (e *invEnv) reservationStates(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, e.reservationState(id))
	}
	return out
}

// checkLedgerAndHolds: bất biến còn đúng trên đường kiểm kho (không kiểm available ≥ 0, damaged ≤ on_hand): mọi cột
// ≥ 0, in_transit ≤ on_hand, sổ = số dư, giữ hàng khớp held, in_transit.
func (e *invEnv) checkLedgerAndHolds(v uuid.UUID) string {
	msg := e.checkInvariants(v)
	if msg == "available âm" || msg == "damaged lớn hơn on_hand" {
		msg = ""
	}
	if b := e.availability(v); msg == "" && b.InTransit > b.OnHand {
		msg = "in_transit lớn hơn on_hand"
	}
	return msg
}
