package inventory_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

func line(v uuid.UUID, q int32) inventory.DocumentLine {
	return inventory.DocumentLine{VariantID: v, Quantity: q}
}

// createDoc tạo phiếu nháp ở kho MAIN (WarehouseID rỗng → kho MAIN).
func (e *invEnv) createDoc(in inventory.DocumentInput) (inventory.Document, error) {
	if in.WarehouseID == uuid.Nil {
		in.WarehouseID = e.warehouse
	}
	var d inventory.Document
	err := e.app(func(tx pgx.Tx) error {
		var err error
		d, err = inventory.CreateDocument(e.t.Context(), tx, in, staffActor())
		return err //nolint:wrapcheck // test
	})
	return d, err
}

func (e *invEnv) replaceLines(id uuid.UUID, version int64, lines ...inventory.DocumentLine) (inventory.Document, error) {
	var d inventory.Document
	err := e.app(func(tx pgx.Tx) error {
		var err error
		d, err = inventory.ReplaceLines(e.t.Context(), tx, id, version, lines)
		return err //nolint:wrapcheck // test
	})
	return d, err
}

// newDoc tạo phiếu nháp kèm dòng; lỗi làm test dừng.
func (e *invEnv) newDoc(in inventory.DocumentInput, lines ...inventory.DocumentLine) inventory.Document {
	e.t.Helper()
	d, err := e.createDoc(in)
	if err != nil {
		e.t.Fatalf("CreateDocument: %v", err)
	}
	if d, err = e.replaceLines(d.ID, d.Version, lines...); err != nil {
		e.t.Fatalf("ReplaceLines: %v", err)
	}
	return d
}

func (e *invEnv) post(d inventory.Document, a inventory.Actor) (inventory.Document, error) {
	var out inventory.Document
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		var err error
		out, err = w.PostDocument(ctx, tx, d.ID, d.Version, a)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *invEnv) getDoc(id uuid.UUID) inventory.Document {
	e.t.Helper()
	var d inventory.Document
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		d, err = inventory.GetDocument(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetDocument: %v", err)
	}
	return d
}

func wantInsufficient(t *testing.T, err error, v uuid.UUID, available int32) {
	t.Helper()
	ie, ok := errors.AsType[*inventory.InsufficientError](err)
	if !ok || !errors.Is(err, inventory.ErrInsufficient) || ie.VariantID != v || ie.Available != available {
		t.Fatalf("mong InsufficientError{%v, available %d}, có %v", v, available, err)
	}
}

// KH03: phiếu nhập tăng on_hand và ghi đúng một dòng sổ; phiếu còn nháp thì số dư không đổi.
func TestPostImport_IncreasesOnHandAndLedger(t *testing.T) {
	t.Parallel()
	eachSeller(t, testPostImport)
}

func testPostImport(t *testing.T, e *invEnv) {
	s, err := e.createSupplier(inventory.SupplierInput{Name: "Xưởng Hoa " + e.seller.String()})
	if err != nil {
		t.Fatal(err)
	}
	a := e.variants(1)[0]
	cost := int64(180000)
	d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindImport, SupplierID: &s.ID, Note: "Lô 1"},
		inventory.DocumentLine{VariantID: a, Quantity: 10, UnitCost: &cost})
	if d.Status != inventory.StatusDraft || d.Version != 2 || len(d.Lines) != 1 || *d.Lines[0].UnitCost != cost ||
		!regexp.MustCompile(`^NK-[0-9]{6,}$`).MatchString(d.Code) {
		t.Fatalf("phiếu nháp sai: %+v", d)
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_balances WHERE variant_id = $1", a); n != 0 {
		t.Fatalf("phiếu nháp đã đổi số dư: %d dòng", n)
	}
	actor := staffActor()
	posted, err := e.post(d, actor)
	if err != nil {
		t.Fatalf("PostDocument: %v", err)
	}
	if posted.Status != inventory.StatusPosted || posted.Version != 3 || posted.PostedAt == nil ||
		posted.PostedBy == nil || *posted.PostedBy != actor.ID || posted.SupplierName != s.Name {
		t.Fatalf("phiếu sau ghi sổ sai: %+v", posted)
	}
	want(t, e.balance(a), inventory.Balance{OnHand: 10, Available: 10})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'import' AND doc_id = $1
		AND bucket = 'on_hand' AND delta = 10 AND actor_id = $2`, d.ID, actor.ID); n != 1 {
		t.Fatalf("sổ: mong 1 dòng import +10, có %d", n)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_id = $1",
		a.String()+":"+e.warehouse.String()); n != 1 {
		t.Fatalf("stock.changed: mong 1, có %d", n)
	}
	// Ghi sổ lần hai (version mới) → không còn nháp; sửa dòng → không còn nháp.
	if _, err := e.post(posted, actor); !errors.Is(err, inventory.ErrDocumentNotDraft) {
		t.Fatalf("ghi sổ lần hai: mong ErrDocumentNotDraft, có %v", err)
	}
	if _, err := e.replaceLines(d.ID, posted.Version, line(a, 1)); !errors.Is(err, inventory.ErrDocumentNotDraft) {
		t.Fatalf("sửa dòng phiếu đã ghi: mong ErrDocumentNotDraft, có %v", err)
	}
	// Database cũng chặn sửa dòng của phiếu đã ghi sổ.
	err = e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "DELETE FROM inventory.stock_document_lines WHERE document_id = $1", d.ID)
		return err //nolint:wrapcheck // test
	})
	if err == nil {
		t.Fatal("xóa dòng phiếu đã ghi sổ phải bị database từ chối")
	}
	want(t, e.balance(a), inventory.Balance{OnHand: 10, Available: 10})
}

// KH10: xuất tay không ăn vào phần đã giữ cho đơn.
func TestPostExport_CannotEatHeld(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	if _, err := e.reserve(sysActor(), e.line(v, 3)); err != nil {
		t.Fatal(err)
	}
	d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(v, 3))
	_, err := e.post(d, staffActor())
	wantInsufficient(t, err, v, 2)
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 3, Available: 2})
	if got := e.getDoc(d.ID); got.Status != inventory.StatusDraft || got.Version != d.Version {
		t.Fatalf("phiếu thiếu hàng phải còn nháp: %+v", got)
	}
	d, err = e.replaceLines(d.ID, d.Version, line(v, 2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.post(d, staffActor()); err != nil {
		t.Fatalf("xuất 2: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 3, Held: 3, Available: 0})
	// Biến thể chưa có dòng số dư: xuất → thiếu, có thể bán 0.
	n := e.variants(1)[0]
	_, err = e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(n, 1)), staffActor())
	wantInsufficient(t, err, n, 0)
}

// KH08: hàng lỗi giảm có thể bán; xuất hàng lỗi giảm cả lỗi và tồn.
func TestPostDamage_ReducesAvailable(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindDamage}, line(v, 2)), staffActor()); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 10, Damaged: 2, Available: 8})
	if _, err := e.reserve(sysActor(), e.line(v, 9)); !errors.Is(err, inventory.ErrInsufficient) {
		t.Fatalf("giữ 9 khi có thể bán 8: mong ErrInsufficient, có %v", err)
	}
	_, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindDamageExport}, line(v, 3)), staffActor())
	wantInsufficient(t, err, v, 2) // xuất hàng lỗi: phần xuất được là số hàng lỗi
	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindDamageExport}, line(v, 2)), staffActor()); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 8, Damaged: 0, Available: 8})
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'damage' AND variant_id = $1", v); n != 3 {
		t.Fatalf("sổ damage: mong 3 dòng (damaged +2, damaged −2, on_hand −2), có %d", n)
	}
}

// Phiếu 3 dòng, dòng giữa (theo thứ tự khóa) thiếu → không dòng nào được ghi.
func TestPost_AllOrNothing(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.variants(3)
	slices.SortFunc(vs, func(a, b uuid.UUID) int {
		if uuidLess(a, b) {
			return -1
		}
		return 1
	})
	e.seed(vs[0], 5)
	e.seed(vs[1], 1)
	e.seed(vs[2], 5)
	ledger := e.count("SELECT count(*) FROM inventory.stock_ledger")
	d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(vs[2], 2), line(vs[1], 2), line(vs[0], 2))
	_, err := e.post(d, staffActor())
	wantInsufficient(t, err, vs[1], 1)
	for i, n := range []int32{5, 1, 5} {
		want(t, e.balance(vs[i]), inventory.Balance{OnHand: n, Available: n})
	}
	if got := e.count("SELECT count(*) FROM inventory.stock_ledger"); got != ledger {
		t.Fatalf("sổ có thêm %d dòng", got-ledger)
	}
}

// Điều chỉnh: âm không quá có thể bán; dương cộng tồn (kể cả biến thể chưa có số dư).
func TestPostAdjust(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(4)
	if _, err := e.reserve(sysActor(), e.line(v, 1)); err != nil {
		t.Fatal(err)
	}
	_, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindAdjust}, line(v, -4)), staffActor())
	wantInsufficient(t, err, v, 3)
	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindAdjust}, line(v, 2)), staffActor()); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 6, Held: 1, Available: 5})
	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindAdjust}, line(v, -5)), staffActor()); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 1, Held: 1, Available: 0})
	n := e.variants(1)[0]
	if _, err := e.post(e.newDoc(inventory.DocumentInput{Kind: inventory.KindAdjust}, line(n, 2)), staffActor()); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(n), inventory.Balance{OnHand: 2, Available: 2})
}

// Luật dòng: chỉ phiếu nháp, đúng version, số lượng theo loại, không trùng biến thể, biến thể có thật.
func TestReplaceLines_Rules(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	d, err := e.createDoc(inventory.DocumentInput{Kind: inventory.KindExport})
	if err != nil {
		t.Fatal(err)
	}
	for name, lines := range map[string][]inventory.DocumentLine{
		"số lượng 0":        {line(v, 0)},
		"số lượng âm":       {line(v, -1)},
		"trùng biến thể":    {line(v, 1), line(v, 2)},
		"biến thể không có": {line(uuid.New(), 1)},
		"quá lớn":           {line(v, 1_000_001)},
		"giá nhập âm":       {{VariantID: v, Quantity: 1, UnitCost: ptr(int64(-1))}},
	} {
		if _, err := e.replaceLines(d.ID, d.Version, lines...); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	if _, err := e.replaceLines(d.ID, d.Version+1, line(v, 1)); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("version sai: mong ErrVersionConflict, có %v", err)
	}
	if _, err := e.replaceLines(uuid.New(), 1, line(v, 1)); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("phiếu không có: mong ErrNotFound, có %v", err)
	}
	d2, err := e.replaceLines(d.ID, d.Version, line(v, 1))
	if err != nil || d2.Version != d.Version+1 || len(d2.Lines) != 1 {
		t.Fatalf("thay dòng: %+v %v", d2, err)
	}
	d3, err := e.replaceLines(d.ID, d2.Version)
	if err != nil || len(d3.Lines) != 0 {
		t.Fatalf("thay bằng rỗng: %+v %v", d3, err)
	}
	if _, err := e.post(d3, staffActor()); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("ghi sổ phiếu không dòng: mong ErrInvalidInput, có %v", err)
	}
	if _, err := e.createDoc(inventory.DocumentInput{Kind: "transfer"}); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("loại lạ: mong ErrInvalidInput, có %v", err)
	}
	if _, err := e.createDoc(inventory.DocumentInput{Kind: inventory.KindImport, WarehouseID: uuid.New()}); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("kho không có: mong ErrNotFound, có %v", err)
	}
}

// Hủy chỉ phiếu nháp; phiếu đã hủy không ghi sổ được; tenant khác không thấy phiếu.
func TestCancelDocument(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(3)
	d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(v, 1))
	cancel := func(id uuid.UUID, version int64) error {
		return e.app(func(tx pgx.Tx) error {
			return inventory.CancelDocument(t.Context(), tx, id, version, staffActor())
		})
	}
	if err := cancel(d.ID, d.Version-1); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("hủy version cũ: %v", err)
	}
	if err := cancel(d.ID, d.Version); err != nil {
		t.Fatal(err)
	}
	got := e.getDoc(d.ID)
	if got.Status != inventory.StatusCancelled || got.Version != d.Version+1 {
		t.Fatalf("phiếu sau hủy: %+v", got)
	}
	if err := cancel(d.ID, got.Version); !errors.Is(err, inventory.ErrDocumentNotDraft) {
		t.Fatalf("hủy lần hai: %v", err)
	}
	if _, err := e.post(got, staffActor()); !errors.Is(err, inventory.ErrDocumentNotDraft) {
		t.Fatalf("ghi sổ phiếu đã hủy: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 3, Available: 3})

	other := newInvEnvOn(t, e.staff, "B")
	err := other.app(func(tx pgx.Tx) error {
		_, err := inventory.GetDocument(t.Context(), tx, d.ID)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("tenant B đọc phiếu của A: %v", err)
	}
}

// Hai transaction ghi sổ cùng phiếu song song → đúng một thành công, một ErrDocumentNotDraft; sổ chỉ một lần.
func TestPost_ConcurrentOnce(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(v, 4))
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.post(d, staffActor()) })
	}
	wg.Wait()
	ok, notDraft := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, inventory.ErrDocumentNotDraft):
			notDraft++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || notDraft != 1 {
		t.Fatalf("mong 1 thành công, 1 không còn nháp: %v", errs)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 6, Available: 6})
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1", d.ID); n != 1 {
		t.Fatalf("sổ: mong 1 dòng, có %d", n)
	}
}

// ListDocuments lọc theo kho, trạng thái, loại; phân trang theo id giảm dần.
func TestListDocuments(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	var ids []uuid.UUID
	for range 3 {
		ids = append(ids, e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(v, 1)).ID)
	}
	if _, err := e.post(e.getDoc(ids[0]), staffActor()); err != nil {
		t.Fatal(err)
	}
	list := func(f inventory.DocumentFilter) ([]inventory.Document, *uuid.UUID) {
		var out []inventory.Document
		var next *uuid.UUID
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			out, next, err = inventory.ListDocuments(t.Context(), tx, f)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return out, next
	}
	page, next := list(inventory.DocumentFilter{AllWarehouses: true, Limit: 2})
	if len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] || next == nil {
		t.Fatalf("trang 1: %+v next %v", page, next)
	}
	page, next = list(inventory.DocumentFilter{AllWarehouses: true, Limit: 2, Cursor: next})
	if len(page) != 1 || page[0].ID != ids[0] || next != nil {
		t.Fatalf("trang 2: %+v next %v", page, next)
	}
	if page, _ := list(inventory.DocumentFilter{AllWarehouses: true, Limit: 10, Status: inventory.StatusPosted}); len(page) != 1 {
		t.Fatalf("lọc posted: %+v", page)
	}
	if page, _ := list(inventory.DocumentFilter{AllWarehouses: true, Limit: 10, Kind: inventory.KindImport}); len(page) != 0 {
		t.Fatalf("lọc import: %+v", page)
	}
	if page, _ := list(inventory.DocumentFilter{WarehouseIDs: []uuid.UUID{uuid.New()}, Limit: 10}); len(page) != 0 {
		t.Fatalf("kho khác: %+v", page)
	}
	if page, _ := list(inventory.DocumentFilter{WarehouseIDs: []uuid.UUID{e.warehouse}, Limit: 10}); len(page) != 3 {
		t.Fatalf("kho MAIN: %+v", page)
	}
}
