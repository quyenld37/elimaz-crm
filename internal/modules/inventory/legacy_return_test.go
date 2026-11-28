package inventory_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Hàng hoàn của đơn Nhanh cũ về kho (kế hoạch M7, Task 264 = M7-19; MO08, Q11, KH08).

func (e *invEnv) legacyReturn(in inventory.LegacyReturnInput) (inventory.LegacyReturn, error) {
	if in.WarehouseID == uuid.Nil {
		in.WarehouseID = e.warehouse
	}
	var out inventory.LegacyReturn
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		var err error
		out, err = w.CreateLegacyReturn(ctx, tx, in, staffActor())
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func legacy(v uuid.UUID, q int32, cond string) inventory.LegacyLine {
	return inventory.LegacyLine{VariantID: v, Qty: q, Condition: cond}
}

// Step 1 kế hoạch: A tốt 2, B hỏng 1 → A on_hand +2, có thể bán +2; B on_hand +1, damaged +1, có thể bán +0; sổ có
// dòng import trỏ phiếu nhập, dòng damage trỏ phiếu hỏng; hai phiếu đã ghi sổ trong cùng transaction.
func TestLegacyReturn_SellableAndDamaged(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.variants(2)
	a, b := vs[0], vs[1]
	e.seed(a, 5)
	r, err := e.legacyReturn(inventory.LegacyReturnInput{
		Reference: " VTP 0001 ", Note: "Khách Lan gọi trả",
		Lines: []inventory.LegacyLine{legacy(a, 2, inventory.ConditionSellable), legacy(b, 1, inventory.ConditionDamaged)},
	})
	if err != nil {
		t.Fatalf("CreateLegacyReturn: %v", err)
	}
	want(t, e.balance(a), inventory.Balance{OnHand: 7, Available: 7})
	want(t, e.balance(b), inventory.Balance{OnHand: 1, Damaged: 1, Available: 0})
	if r.Reference != "VTP 0001" || r.WarehouseID != e.warehouse || r.DamageDocumentID == nil || r.CreatedBy == uuid.Nil {
		t.Fatalf("phiếu hàng hoàn sai: %+v", r)
	}
	imp, dmg := e.getDoc(r.StockDocumentID), e.getDoc(*r.DamageDocumentID)
	if imp.Kind != inventory.KindImport || imp.Status != inventory.StatusPosted || len(imp.Lines) != 2 ||
		dmg.Kind != inventory.KindDamage || dmg.Status != inventory.StatusPosted || len(dmg.Lines) != 1 {
		t.Fatalf("hai phiếu sai: nhập %+v, hỏng %+v", imp, dmg)
	}
	if !strings.HasPrefix(imp.Note, "Hàng hoàn đơn cũ VTP 0001") || !strings.Contains(imp.Note, "Khách Lan gọi trả") ||
		!strings.HasPrefix(dmg.Note, "Hàng hoàn đơn cũ VTP 0001") {
		t.Fatalf("ghi chú phiếu: nhập %q, hỏng %q", imp.Note, dmg.Note)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'import' AND doc_id = $1
		AND bucket = 'on_hand'`, imp.ID); n != 2 {
		t.Fatalf("sổ import: mong 2 dòng, có %d", n)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'damage' AND doc_id = $1
		AND bucket = 'damaged' AND delta = 1 AND variant_id = $2`, dmg.ID, b); n != 1 {
		t.Fatalf("sổ damage: mong 1 dòng, có %d", n)
	}
	wantLines(t, r.Lines, legacy(a, 2, inventory.ConditionSellable), legacy(b, 1, inventory.ConditionDamaged))
	var got inventory.LegacyReturn
	if err := e.app(func(tx pgx.Tx) error {
		got, err = inventory.GetLegacyReturn(t.Context(), tx, r.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("GetLegacyReturn: %v", err)
	}
	wantLines(t, got.Lines, r.Lines...)
	if got.StockDocumentID != r.StockDocumentID || *got.DamageDocumentID != *r.DamageDocumentID || got.Reference != r.Reference {
		t.Fatalf("đọc lại khác: %+v / %+v", got, r)
	}
}

// Số bán được của tồn cũ đã âm (giữ 3, kiểm kho thiếu còn on_hand 1): hàng hoàn hỏng vẫn nhập được — phiếu hỏng chỉ
// chuyển phần vừa nhập, không xét phần đang giữ (reviewer Task 264).
func TestLegacyReturn_DamagedWhenOversold(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	e.seed(v, 3)
	if _, err := e.reserve(sysActor(), e.line(v, 3)); err != nil {
		t.Fatalf("giữ hàng: %v", err)
	}
	e.owner("UPDATE inventory.stock_balances SET on_hand = 1 WHERE variant_id = $1", v)
	if _, err := e.legacyReturn(inventory.LegacyReturnInput{
		Reference: "VTP-OVERSOLD", Lines: []inventory.LegacyLine{legacy(v, 2, inventory.ConditionDamaged)},
	}); err != nil {
		t.Fatalf("CreateLegacyReturn khi số bán được âm: %v", err)
	}
	if b := e.balance(v); b.OnHand != 3 || b.Held != 3 || b.Damaged != 2 {
		t.Fatalf("số dư sau hàng hoàn: %+v", b)
	}
}

// Cùng biến thể vừa tốt vừa hỏng: một dòng nhập 3, một dòng hỏng 1; đọc lại ra đúng hai dòng. Mọi dòng tốt → không có
// phiếu hỏng.
func TestLegacyReturn_SameVariantBothConditions(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.variants(2)
	r, err := e.legacyReturn(inventory.LegacyReturnInput{
		Reference: "GHN-1",
		Lines:     []inventory.LegacyLine{legacy(vs[0], 2, inventory.ConditionSellable), legacy(vs[0], 1, inventory.ConditionDamaged)},
	})
	if err != nil {
		t.Fatalf("CreateLegacyReturn: %v", err)
	}
	want(t, e.balance(vs[0]), inventory.Balance{OnHand: 3, Damaged: 1, Available: 2})
	wantLines(t, r.Lines, legacy(vs[0], 2, inventory.ConditionSellable), legacy(vs[0], 1, inventory.ConditionDamaged))
	if d := e.getDoc(r.StockDocumentID); len(d.Lines) != 1 || d.Lines[0].Quantity != 3 {
		t.Fatalf("phiếu nhập phải một dòng 3: %+v", d.Lines)
	}
	r2, err := e.legacyReturn(inventory.LegacyReturnInput{
		Reference: "GHN-2",
		Lines:     []inventory.LegacyLine{legacy(vs[1], 4, inventory.ConditionSellable)},
	})
	if err != nil {
		t.Fatalf("CreateLegacyReturn tốt: %v", err)
	}
	if r2.DamageDocumentID != nil || e.count("SELECT count(*) FROM inventory.stock_documents WHERE kind = 'damage'") != 1 {
		t.Fatalf("mọi dòng tốt không được có phiếu hỏng: %+v", r2)
	}
	want(t, e.balance(vs[1]), inventory.Balance{OnHand: 4, Available: 4})
}

// Một kiện không nhập hai lần: "vtp 1234 5678" rồi "VTP12345678" → DuplicateReferenceError kèm id cũ; không phiếu, số
// dư nào đổi ở lần hai.
func TestLegacyReturn_DuplicateReference(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	first, err := e.legacyReturn(inventory.LegacyReturnInput{
		Reference: "vtp 1234 5678",
		Lines:     []inventory.LegacyLine{legacy(v, 1, inventory.ConditionSellable)},
	})
	if err != nil {
		t.Fatalf("lần đầu: %v", err)
	}
	for _, ref := range []string{"VTP12345678", "VTP\t1234 5678", " v t p 12345678 "} {
		_, err = e.legacyReturn(inventory.LegacyReturnInput{
			Reference: ref,
			Lines:     []inventory.LegacyLine{legacy(v, 1, inventory.ConditionDamaged)},
		})
		wantDuplicate(t, err, first.ID)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 1, Available: 1})
	if n := e.count("SELECT count(*) FROM inventory.stock_documents"); n != 1 {
		t.Fatalf("trùng tham chiếu không được để lại phiếu: %d phiếu", n)
	}
}

// Nhiều yêu cầu cùng tham chiếu cùng lúc → đúng một thành công, còn lại DuplicateReferenceError trỏ đúng phiếu thắng.
func TestLegacyReturn_ConcurrentSameReference(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	const n = 6
	res := make([]inventory.LegacyReturn, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			res[i], errs[i] = e.legacyReturn(inventory.LegacyReturnInput{
				Reference: "SPX 77",
				Lines:     []inventory.LegacyLine{legacy(v, 2, inventory.ConditionSellable)},
			})
		})
	}
	close(start)
	wg.Wait()
	var winner uuid.UUID
	for i, err := range errs {
		if err == nil {
			if winner != uuid.Nil {
				t.Fatal("hai yêu cầu cùng tham chiếu đều thành công")
			}
			winner = res[i].ID
		}
	}
	if winner == uuid.Nil {
		t.Fatalf("không yêu cầu nào thành công: %v", errs)
	}
	for _, err := range errs {
		if err != nil {
			wantDuplicate(t, err, winner)
		}
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, Available: 2})
	if c := e.count("SELECT count(*) FROM inventory.legacy_returns"); c != 1 {
		t.Fatalf("mong 1 phiếu hàng hoàn, có %d", c)
	}
}

func TestLegacyReturn_InvalidInput(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	ok := []inventory.LegacyLine{legacy(v, 1, inventory.ConditionSellable)}
	for name, in := range map[string]inventory.LegacyReturnInput{
		"qty 0":             {Reference: "R-100", Lines: []inventory.LegacyLine{legacy(v, 0, inventory.ConditionSellable)}},
		"qty 1001":          {Reference: "R-101", Lines: []inventory.LegacyLine{legacy(v, 1001, inventory.ConditionSellable)}},
		"tình trạng lạ":     {Reference: "R-102", Lines: []inventory.LegacyLine{legacy(v, 1, "lost")}},
		"không dòng":        {Reference: "R-103"},
		"tham chiếu ngắn":   {Reference: "a b", Lines: ok},
		"tham chiếu dài":    {Reference: strings.Repeat("A", 65), Lines: ok},
		"ghi chú dài":       {Reference: "R-104", Note: strings.Repeat("x", 501), Lines: ok},
		"trùng dòng":        {Reference: "R-105", Lines: append(slices.Clone(ok), ok...)},
		"biến thể không có": {Reference: "R-106", Lines: []inventory.LegacyLine{legacy(uuid.New(), 1, inventory.ConditionDamaged)}},
	} {
		if _, err := e.legacyReturn(in); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	if _, err := e.legacyReturn(inventory.LegacyReturnInput{WarehouseID: uuid.New(), Reference: "R-107", Lines: ok}); !errors.Is(err, inventory.ErrNotFound) {
		t.Errorf("kho không có: mong ErrNotFound, có %v", err)
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_documents"); n != 0 {
		t.Fatalf("dữ liệu sai không được để lại phiếu: %d", n)
	}
}

// ListLegacyReturns: mới nhất trước, lọc kho, lọc khoảng giờ, phân trang theo con trỏ.
func TestListLegacyReturns(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	w2 := e.secondWarehouse()
	v := e.variants(1)[0]
	ids := make([]uuid.UUID, 0, 3)
	for i, wh := range []uuid.UUID{e.warehouse, w2, e.warehouse} {
		r, err := e.legacyReturn(inventory.LegacyReturnInput{
			WarehouseID: wh, Reference: "L-" + string(rune('A'+i)) + "00",
			Lines: []inventory.LegacyLine{legacy(v, 1, inventory.ConditionDamaged)},
		})
		if err != nil {
			t.Fatalf("tạo %d: %v", i, err)
		}
		ids = append(ids, r.ID)
	}
	got, next := e.listLegacy(inventory.LegacyReturnFilter{AllWarehouses: true, Limit: 2})
	if !slices.Equal(got, []uuid.UUID{ids[2], ids[1]}) || next == nil || *next != ids[1] {
		t.Fatalf("trang 1: %v next %v (mong %v)", got, next, ids)
	}
	if got, next = e.listLegacy(inventory.LegacyReturnFilter{AllWarehouses: true, Limit: 2, Cursor: next}); !slices.Equal(got, ids[:1]) || next != nil {
		t.Fatalf("trang 2: %v next %v", got, next)
	}
	if got, _ = e.listLegacy(inventory.LegacyReturnFilter{WarehouseIDs: []uuid.UUID{e.warehouse}, Limit: 10}); !slices.Equal(got, []uuid.UUID{ids[2], ids[0]}) {
		t.Fatalf("lọc kho MAIN: %v", got)
	}
	if got, _ = e.listLegacy(inventory.LegacyReturnFilter{Limit: 10}); len(got) != 0 {
		t.Fatalf("không kho nào được giao → rỗng: %v", got)
	}
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	if got, _ = e.listLegacy(inventory.LegacyReturnFilter{AllWarehouses: true, From: &past, To: &future, Limit: 10}); len(got) != 3 {
		t.Fatalf("khoảng chứa cả ba: %v", got)
	}
	if got, _ = e.listLegacy(inventory.LegacyReturnFilter{AllWarehouses: true, From: &future, Limit: 10}); len(got) != 0 {
		t.Fatalf("từ giờ sau: %v", got)
	}
	if got, _ = e.listLegacy(inventory.LegacyReturnFilter{AllWarehouses: true, To: &past, Limit: 10}); len(got) != 0 {
		t.Fatalf("tới giờ trước: %v", got)
	}
}

func TestGetLegacyReturn_NotFound(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	err := e.app(func(tx pgx.Tx) error {
		_, err := inventory.GetLegacyReturn(t.Context(), tx, uuid.New())
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("phiếu không có: mong ErrNotFound, có %v", err)
	}
}

// listLegacy trả id các phiếu của một trang (mỗi phiếu thử có đúng một dòng hỏng) và con trỏ trang sau.
func (e *invEnv) listLegacy(f inventory.LegacyReturnFilter) ([]uuid.UUID, *uuid.UUID) {
	e.t.Helper()
	var rs []inventory.LegacyReturn
	var next *uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		rs, next, err = inventory.ListLegacyReturns(e.t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("ListLegacyReturns: %v", err)
	}
	out := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		if len(r.Lines) != 1 || r.Lines[0].Condition != inventory.ConditionDamaged {
			e.t.Fatalf("dòng của phiếu trong danh sách: %+v", r.Lines)
		}
		out = append(out, r.ID)
	}
	return out, next
}

func wantDuplicate(t *testing.T, err error, existing uuid.UUID) {
	t.Helper()
	de, ok := errors.AsType[*inventory.DuplicateReferenceError](err)
	if !ok || !errors.Is(err, inventory.ErrDuplicateReference) || de.ExistingID != existing {
		t.Fatalf("mong DuplicateReferenceError(%s), có %v", existing, err)
	}
}

func wantLines(t *testing.T, got []inventory.LegacyLine, exp ...inventory.LegacyLine) {
	t.Helper()
	key := func(l inventory.LegacyLine) string { return l.VariantID.String() + l.Condition }
	g, x := slices.Clone(got), slices.Clone(exp)
	slices.SortFunc(g, func(a, b inventory.LegacyLine) int { return strings.Compare(key(a), key(b)) })
	slices.SortFunc(x, func(a, b inventory.LegacyLine) int { return strings.Compare(key(a), key(b)) })
	if !slices.Equal(g, x) {
		t.Fatalf("dòng: có %+v, mong %+v", g, x)
	}
}
