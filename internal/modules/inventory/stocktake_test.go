package inventory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
)

func (e *invEnv) startStocktake() (inventory.Stocktake, error) {
	var s inventory.Stocktake
	err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = inventory.StartStocktake(e.t.Context(), tx, e.warehouse, staffActor())
		return err //nolint:wrapcheck // test
	})
	return s, err
}

func (e *invEnv) mustStart() inventory.Stocktake {
	e.t.Helper()
	s, err := e.startStocktake()
	if err != nil {
		e.t.Fatalf("StartStocktake: %v", err)
	}
	return s
}

func (e *invEnv) recordCount(id, v uuid.UUID, counted int32, lineVersion *int64) (inventory.StocktakeLine, error) {
	var l inventory.StocktakeLine
	err := e.app(func(tx pgx.Tx) error {
		var err error
		l, err = inventory.RecordCount(e.t.Context(), tx, id, v, counted, lineVersion, staffActor())
		return err //nolint:wrapcheck // test
	})
	return l, err
}

func (e *invEnv) mustCount(id, v uuid.UUID, counted int32) inventory.StocktakeLine {
	e.t.Helper()
	l, err := e.recordCount(id, v, counted, nil)
	if err != nil {
		e.t.Fatalf("RecordCount: %v", err)
	}
	return l
}

func (e *invEnv) getStocktake(id uuid.UUID) (inventory.Stocktake, []inventory.StocktakeLine) {
	e.t.Helper()
	var s inventory.Stocktake
	var ls []inventory.StocktakeLine
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		s, ls, err = inventory.GetStocktake(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetStocktake: %v", err)
	}
	return s, ls
}

// applyStocktake áp dụng phiên id ở version hiện tại (đọc lại trước).
func (e *invEnv) applyStocktake(id uuid.UUID) (inventory.ApplyResult, error) {
	e.t.Helper()
	s, _ := e.getStocktake(id)
	return e.applyAt(id, s.Version)
}

func (e *invEnv) applyAt(id uuid.UUID, version int64) (inventory.ApplyResult, error) {
	var r inventory.ApplyResult
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		var err error
		r, err = w.ApplyStocktake(ctx, tx, id, version, staffActor())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *invEnv) cancelStocktake(id uuid.UUID, version int64) error {
	return e.app(func(tx pgx.Tx) error {
		return inventory.CancelStocktake(e.t.Context(), tx, id, version, staffActor())
	})
}

func (e *invEnv) stocktakeLedger(id uuid.UUID) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'stocktake' AND doc_id = $1", id)
}

func (e *invEnv) shortageJobs(id uuid.UUID) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2",
		inventory.ShortageAlertKind, id.String())
}

// inTransit tạo biến thể on_hand = onHand, rồi giữ q và chuyển sang đang giao; trả biến thể và mã giữ hàng.
func (e *invEnv) inTransit(onHand, q int32) (uuid.UUID, uuid.UUID) {
	e.t.Helper()
	v := e.stock(onHand)
	ids, err := e.reserve(sysActor(), e.line(v, q))
	if err != nil {
		e.t.Fatalf("Reserve: %v", err)
	}
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.StartTransit(ctx, tx, ids, sysActor())
	}); err != nil {
		e.t.Fatalf("StartTransit: %v", err)
	}
	return v, ids[0]
}

// TestStocktake_InTransitNotAdjusted (KH05): on_hand 10, in_transit 3, đếm 7 → không chỉnh.
func TestStocktake_InTransitNotAdjusted(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v, _ := e.inTransit(10, 3)
	s := e.mustStart()
	l := e.mustCount(s.ID, v, 7)
	if l.ExpectedOnShelf != 7 || l.Delta != 0 || l.Counted != 7 {
		t.Fatalf("dòng đếm sai: %+v", l)
	}
	before := e.balance(v)
	r, err := e.applyStocktake(s.ID)
	if err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	if r.Adjusted != 0 || len(r.Shortages) != 0 || r.Stocktake.Status != inventory.StocktakeApplied {
		t.Fatalf("kết quả sai: %+v", r)
	}
	want(t, e.balance(v), before)
	if n := e.stocktakeLedger(s.ID); n != 0 {
		t.Fatalf("có %d dòng sổ kiểm kho, mong 0", n)
	}
}

// Đếm lúc on_hand 10, in_transit 3 (đếm 7) → 3 cái giao xong → áp dụng: on_hand 7, không chỉnh (bản cũ ra 4).
func TestStocktake_DeliveredAfterCount(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v, res := e.inTransit(10, 3)
	s := e.mustStart()
	e.mustCount(s.ID, v, 7)
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.Consume(ctx, tx, []uuid.UUID{res}, sysActor())
	}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := e.applyStocktake(s.ID); err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 7, Available: 7})
	if n := e.stocktakeLedger(s.ID); n != 0 {
		t.Fatalf("có %d dòng sổ kiểm kho, mong 0", n)
	}
}

// TestStocktake_ShortageKeepsHolds (KH07): on_hand 5, held 4 (2 + 2), đếm 2 → on_hand 2, held giữ nguyên, giữ hàng vẫn
// held, Shortages có available −2, một lệnh inventory.shortage_alert.
func TestStocktake_ShortageKeepsHolds(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	ids, err := e.reserve(sysActor(), e.line(v, 2), e.line(v, 2))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	s := e.mustStart()
	e.mustCount(s.ID, v, 2)
	_, ls := e.getStocktake(s.ID)
	if len(ls) != 1 || ls[0].Delta != -3 || ls[0].AvailableAfter != -2 {
		t.Fatalf("xem trước sai: %+v", ls)
	}
	r, err := e.applyStocktake(s.ID)
	if err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, Held: 4, Available: -2})
	for _, id := range ids {
		if st := e.reservationState(id); st != inventory.StateHeld {
			t.Fatalf("giữ hàng %v thành %s", id, st)
		}
	}
	if r.Adjusted != 1 || len(r.Shortages) != 1 || r.Shortages[0].VariantID != v || r.Shortages[0].Available != -2 {
		t.Fatalf("kết quả sai: %+v", r)
	}
	if n := e.shortageJobs(s.ID); n != 1 {
		t.Fatalf("có %d lệnh cảnh báo thiếu, mong 1", n)
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'stocktake' AND doc_id = $1 AND bucket = 'on_hand' AND delta = -3", s.ID); n != 1 {
		t.Fatalf("sổ kiểm kho: %d dòng −3, mong 1", n)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_id = $1",
		v.String()+":"+e.warehouse.String()); n < 1 {
		t.Fatal("không có stock.changed")
	}
}

// Đếm thừa: on_hand 5, đếm 6 → on_hand 6, sổ stocktake +1; không thiếu → không có lệnh cảnh báo.
func TestStocktake_Overcount(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	s := e.mustStart()
	e.mustCount(s.ID, v, 6)
	r, err := e.applyStocktake(s.ID)
	if err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 6, Available: 6})
	if r.Adjusted != 1 || len(r.Shortages) != 0 {
		t.Fatalf("kết quả sai: %+v", r)
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'stocktake' AND doc_id = $1 AND delta = 1", s.ID); n != 1 {
		t.Fatalf("sổ: %d dòng +1, mong 1", n)
	}
	if n := e.shortageJobs(s.ID); n != 0 {
		t.Fatalf("có %d lệnh cảnh báo, mong 0", n)
	}
}

// Biến thể chưa có dòng số dư: đếm 3 → expected 0, áp dụng → on_hand 3.
func TestStocktake_NewBalanceRow(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.variants(1)[0]
	s := e.mustStart()
	if l := e.mustCount(s.ID, v, 3); l.ExpectedOnShelf != 0 || l.Delta != 3 {
		t.Fatalf("dòng đếm sai: %+v", l)
	}
	if _, err := e.applyStocktake(s.ID); err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 3, Available: 3})
}

// Hàng bị xuất sau lúc đếm, áp dụng sẽ làm phần trên kệ âm → cả phiên không áp, *InsufficientError, không ghi gì.
func TestStocktake_ApplyWouldGoNegative(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	s := e.mustStart()
	e.mustCount(s.ID, v, 1) // delta −4
	d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(v, 3))
	if _, err := e.post(d, staffActor()); err != nil {
		t.Fatalf("PostDocument: %v", err)
	}
	_, err := e.applyStocktake(s.ID)
	if ie, ok := errors.AsType[*inventory.InsufficientError](err); !ok || ie.VariantID != v || ie.Available != 2 {
		t.Fatalf("mong InsufficientError (có 2), có %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 2, Available: 2})
	if st, _ := e.getStocktake(s.ID); st.Status != inventory.StocktakeOpen {
		t.Fatalf("phiên thành %s", st.Status)
	}
}

// Áp dụng hai lần song song cùng version → một thành công, một ErrStocktakeNotOpen; sổ chỉ có dòng của một lần.
func TestStocktake_ApplyTwiceConcurrent(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	s := e.mustStart()
	e.mustCount(s.ID, v, 3)
	cur, _ := e.getStocktake(s.ID)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.applyAt(s.ID, cur.Version) })
	}
	wg.Wait()
	ok, notOpen := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, inventory.ErrStocktakeNotOpen):
			notOpen++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || notOpen != 1 {
		t.Fatalf("thành công %d, không còn mở %d", ok, notOpen)
	}
	if n := e.stocktakeLedger(s.ID); n != 1 {
		t.Fatalf("sổ có %d dòng, mong 1", n)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 3, Available: 3})
}

func TestStocktake_Cancel(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	s := e.mustStart()
	e.mustCount(s.ID, v, 1)
	cur, _ := e.getStocktake(s.ID)
	if err := e.cancelStocktake(s.ID, cur.Version-1); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if err := e.cancelStocktake(s.ID, cur.Version); err != nil {
		t.Fatalf("CancelStocktake: %v", err)
	}
	if got, _ := e.getStocktake(s.ID); got.Status != inventory.StocktakeCancelled {
		t.Fatalf("trạng thái %s", got.Status)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Available: 5})
	if _, err := e.recordCount(s.ID, v, 2, nil); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		t.Fatalf("đếm sau hủy: mong ErrStocktakeNotOpen, có %v", err)
	}
	// Sau khi hủy mở được phiên mới; phiên đã áp dụng thì không hủy được.
	s2 := e.mustStart()
	if _, err := e.applyStocktake(s2.ID); err != nil {
		t.Fatalf("áp dụng phiên rỗng: %v", err)
	}
	got, _ := e.getStocktake(s2.ID)
	if err := e.cancelStocktake(s2.ID, got.Version); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		t.Fatalf("hủy phiên đã áp dụng: mong ErrStocktakeNotOpen, có %v", err)
	}
	if _, err := e.applyAt(s2.ID, got.Version); !errors.Is(err, inventory.ErrStocktakeNotOpen) {
		t.Fatalf("áp dụng lại: mong ErrStocktakeNotOpen, có %v", err)
	}
}

func TestStocktake_OneOpenPerWarehouse(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	s := e.mustStart()
	if !strings.HasPrefix(s.Code, "KK-") || s.Status != inventory.StocktakeOpen || s.WarehouseID != e.warehouse {
		t.Fatalf("phiên sai: %+v", s)
	}
	if _, err := e.startStocktake(); !errors.Is(err, inventory.ErrStocktakeOpen) {
		t.Fatalf("mở phiên thứ hai: mong ErrStocktakeOpen, có %v", err)
	}
}

// Kho đã đóng không mở được phiên; kho của tenant khác → ErrNotFound.
func TestStocktake_WarehouseChecks(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	other := newInvEnvOn(t, e.staff, "B")
	err := e.app(func(tx pgx.Tx) error {
		_, err := inventory.StartStocktake(t.Context(), tx, other.warehouse, staffActor())
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("kho tenant khác: mong ErrNotFound, có %v", err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE inventory.warehouses SET status = 'closed' WHERE id = $1", e.warehouse)
		return err //nolint:wrapcheck // test
	})
	if _, err := e.startStocktake(); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("kho đóng: mong ErrInvalidInput, có %v", err)
	}
}

func TestRecordCount_Rules(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	s := e.mustStart()
	if _, err := e.recordCount(s.ID, v, -1, nil); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("số âm: mong ErrInvalidInput, có %v", err)
	}
	if _, err := e.recordCount(s.ID, uuid.New(), 1, nil); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("biến thể không có: mong ErrInvalidInput, có %v", err)
	}
	if _, err := e.recordCount(uuid.New(), v, 1, nil); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("phiên không có: mong ErrNotFound, có %v", err)
	}
	l := e.mustCount(s.ID, v, 4)
	if l.Version != 1 || l.Delta != -1 {
		t.Fatalf("dòng sai: %+v", l)
	}
	// Ghi lại phải gửi đúng version của dòng; ghi lại chụp lại số dư.
	if _, err := e.recordCount(s.ID, v, 3, nil); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("ghi lại không version: mong ErrVersionConflict, có %v", err)
	}
	stale := int64(7)
	if _, err := e.recordCount(s.ID, v, 3, &stale); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("version sai: mong ErrVersionConflict, có %v", err)
	}
	e.seed(v, 2) // on_hand 7
	l2, err := e.recordCount(s.ID, v, 6, &l.Version)
	if err != nil {
		t.Fatalf("ghi lại: %v", err)
	}
	if l2.Version != 2 || l2.ExpectedOnShelf != 7 || l2.Delta != -1 || l2.Counted != 6 {
		t.Fatalf("ghi lại sai: %+v", l2)
	}
	// Đếm tăng version phiên: áp dụng với version trước lần đếm → ErrVersionConflict.
	st, _ := e.getStocktake(s.ID)
	if _, err := e.applyAt(s.ID, st.Version-1); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("áp dụng version cũ: mong ErrVersionConflict, có %v", err)
	}
}

type fakeNotifier struct {
	key, severity, text string
	err                 error
}

func (f *fakeNotifier) Notify(_ context.Context, key, severity, text string) error {
	f.key, f.severity, f.text = key, severity, text
	return f.err
}

// Lệnh cảnh báo: khóa stock-shortage:<id>, mức normal, văn bản chỉ có mã phiếu và số mã hàng (không tên mẫu, SĐT);
// Notify lỗi → not_sent (tự gửi lại).
func TestShortageAlertCommand(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	if _, err := e.reserve(sysActor(), e.line(v, 4)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	s := e.mustStart()
	e.mustCount(s.ID, v, 2)
	if _, err := e.applyStocktake(s.ID); err != nil {
		t.Fatalf("ApplyStocktake: %v", err)
	}
	var payload json.RawMessage
	var kind string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT kind, payload FROM platform.jobs WHERE dedupe_key = $1", s.ID.String()).
			Scan(&kind, &payload)
	})
	n := &fakeNotifier{}
	cmd := inventory.NewShortageAlertCommand(n, nil)
	if cmd.Kind().Name != kind || cmd.Kind().Policy.Mode != queue.ModeCommand || !cmd.AutoRetryNotSent() {
		t.Fatalf("Kind sai: %+v (việc %s)", cmd.Kind(), kind)
	}
	out, err := cmd.Execute(t.Context(), queue.Job{Kind: kind, Payload: payload, TenantID: &e.tenant})
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute: %+v, %v", out, err)
	}
	wantText := "Kiểm kho " + s.Code + ": 1 mã hàng thiếu so với phần đang giữ"
	if n.key != "stock-shortage:"+s.ID.String() || n.severity != "normal" || n.text != wantText {
		t.Fatalf("cảnh báo sai: %+v", n)
	}
	if strings.Contains(n.text, "Áo thử") {
		t.Fatal("văn bản cảnh báo có tên mẫu")
	}
	n.err = errors.New("telegram hỏng")
	if out, err := cmd.Execute(t.Context(), queue.Job{Kind: kind, Payload: payload}); err != nil ||
		out.Status != commands.StatusNotSent || out.RetryAfter != 0 {
		t.Fatalf("Notify lỗi: mong not_sent, có %+v, %v", out, err)
	}
	// Telegram 429: chờ đúng thời gian Telegram yêu cầu (04 mục 5.3).
	n.err = fmt.Errorf("gửi: %w", &observability.TelegramRetryError{After: 7 * time.Second})
	if out, err := cmd.Execute(t.Context(), queue.Job{Kind: kind, Payload: payload}); err != nil ||
		out.Status != commands.StatusNotSent || out.RetryAfter != 7*time.Second {
		t.Fatalf("Telegram 429: mong not_sent chờ 7s, có %+v, %v", out, err)
	}
}
