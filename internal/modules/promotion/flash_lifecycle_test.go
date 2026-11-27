package promotion_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Lên lịch, thêm suất, đóng đợt, nhịp mở/đóng (kế hoạch M5, Task 200; ca FS01, FS08, FS09, FS17 phần đóng đợt).

// beforeStart là lúc nhân viên lên lịch: một giờ trước giờ bắt đầu của flashInput.
func beforeStart() time.Time { return flashStart().Add(-time.Hour) }

// stockVariant tạo biến thể giá 350.000 có onHand ở kho MAIN.
func (e *env) stockVariant(onHand int32) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = e.shop.Variant(e.t, tx, e.shop.Seller, 350000, onHand)
		return nil
	})
	return id
}

// draftWith tạo đợt nháp dài length từ flashStart với một món mỗi (biến thể, số suất) và trả đợt đọc lại.
func (e *env) draftWith(length time.Duration, items map[uuid.UUID]int) promotion.FlashSale {
	e.t.Helper()
	return e.draftAt(flashStart(), flashStart().Add(length), items)
}

// draftAt tạo đợt nháp [start, end) với một món mỗi (biến thể, số suất) và trả đợt đọc lại.
func (e *env) draftAt(start, end time.Time, items map[uuid.UUID]int) promotion.FlashSale {
	e.t.Helper()
	s, err := e.createFlash(promotion.FlashSaleInput{Name: "Đợt " + uuid.NewString()[:8], StartsAt: start, EndsAt: end})
	if err != nil {
		e.t.Fatal(err)
	}
	for v, q := range items {
		if _, err := e.addFlashItem(s.ID, flashItem(v, 199000, q)); err != nil {
			e.t.Fatal(err)
		}
	}
	return e.getFlash(s.ID)
}

func (e *env) schedule(s promotion.FlashSale, now time.Time) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().ScheduleFlashSale(e.t.Context(), tx, s.ID, s.Version, e.actor(), now)
		return err //nolint:wrapcheck // test
	})
}

func (e *env) removeItem(sale, item uuid.UUID) (promotion.FlashItem, error) {
	var it promotion.FlashItem
	err := e.app(func(tx pgx.Tx) (err error) {
		it, err = promotion.RemoveFlashItem(e.t.Context(), tx, sale, item)
		return err //nolint:wrapcheck // test
	})
	return it, err
}

func (e *env) closeFlash(id uuid.UUID, reason string) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().CloseFlashSale(e.t.Context(), tx, id, reason, e.actor())
		return err //nolint:wrapcheck // test
	})
}

func (e *env) addSlots(sale, item uuid.UUID, add int) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().AddSlots(e.t.Context(), tx, sale, item, add, e.actor())
		return err //nolint:wrapcheck // test
	})
}

func (e *env) tick(now time.Time) (opened, closed int) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) (err error) {
		opened, closed, err = e.writer().Tick(e.t.Context(), tx, now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("nhịp: %v", err)
	}
	return opened, closed
}

func (e *env) live(id uuid.UUID) promotion.FlashLive {
	e.t.Helper()
	var l promotion.FlashLive
	if err := e.app(func(tx pgx.Tx) (err error) {
		l, err = promotion.Live(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("số liệu đợt: %v", err)
	}
	return l
}

func (e *env) stockOf(v uuid.UUID) inventory.Balance {
	e.t.Helper()
	var b []inventory.Balance
	if err := e.app(func(tx pgx.Tx) (err error) {
		b, err = inventory.Availability(e.t.Context(), tx, e.shop.Warehouse, []uuid.UUID{v})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return b[0]
}

func (e *env) events(topic string, sale uuid.UUID) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM platform.domain_events
		WHERE topic = $1 AND aggregate_type = 'flash_sale' AND aggregate_id = $2`, topic, sale.String())
}

// checkBooks: đối chiếu đêm không lệch sổ, và held của mỗi biến thể = giữ hàng đang held + đơn vị flash còn trống
// (công thức M5-19 viết trong test).
func (e *env) checkBooks(vs ...uuid.UUID) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		run, err := inventory.Reconcile(e.t.Context(), tx, 0)
		for _, m := range run.Mismatches {
			if m.Check == inventory.CheckLedgerVsBalance {
				e.t.Errorf("sổ lệch số dư: %+v", m)
			}
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	for _, v := range vs {
		reserved := e.staff.CountIn(e.t, e.tenant, `SELECT coalesce(sum(quantity), 0) FROM inventory.stock_reservations
			WHERE variant_id = $1 AND state = 'held'`, v)
		avail := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM inventory.flash_sale_units AS u
			JOIN promotion.flash_sale_items AS i ON i.id = u.flash_sale_item_id
			WHERE i.variant_id = $1 AND u.status = 'available'`, v)
		if held := int(e.stockOf(v).Held); held < 0 || held != reserved+avail {
			e.t.Errorf("biến thể %s: held %d, mong giữ hàng %d + đơn vị trống %d", v, held, reserved, avail)
		}
	}
}

// Tồn 10 → lên lịch món 6 suất: đợt scheduled (version +1), held 6, có thể bán 4, 6 đơn vị trống (số liệu live), một
// flash_sale.changed [FS01] [FS09]; lên lịch lần nữa → not_editable; nhịp đúng starts_at → open, một
// flash_sale.opened; nhịp lần nữa → không gì; thêm 3 suất khi open → quantity 9, held 9; quá ends_at → nhịp đóng với
// closed_reason ended, held về 0, sổ +9 −9 [FS08].
func TestFlashLifecycle_ScheduleOpenAddClose(t *testing.T) {
	t.Parallel()
	e, v, s := newSixOfTen(t)
	if err := e.schedule(s, beforeStart()); err != nil {
		t.Fatal(err)
	}
	got := e.getFlash(s.ID)
	if got.Status != promotion.FlashScheduled || got.Version != s.Version+1 {
		t.Fatalf("sau lên lịch: %+v", got)
	}
	item := got.Items[0].ID
	e.wantStock("sau lên lịch", v, 6, 4)
	e.wantLive("sau lên lịch", s.ID, promotion.FlashLiveItem{ItemID: item, Quantity: 6, Available: 6})
	e.wantEvents(promotion.TopicFlashSaleChanged, s.ID, 1)
	e.checkBooks(v)
	if err := e.schedule(got, beforeStart()); !errors.Is(err, promotion.ErrNotEditable) {
		t.Fatalf("lên lịch lại: %v, mong ErrNotEditable", err)
	}
	e.wantTick("trước giờ", flashStart().Add(-time.Second), 0, 0)
	e.wantTick("đúng giờ", flashStart(), 1, 0)
	e.wantTick("lần hai", flashStart().Add(time.Minute), 0, 0)
	if st := e.getFlash(s.ID).Status; st != promotion.FlashOpen {
		t.Fatalf("trạng thái %s, mong open", st)
	}
	e.wantEvents(promotion.TopicFlashSaleOpened, s.ID, 1)
	if err := e.addSlots(s.ID, item, 3); err != nil {
		t.Fatal(err)
	}
	e.wantStock("sau thêm suất", v, 9, 1)
	e.wantLive("sau thêm suất", s.ID, promotion.FlashLiveItem{ItemID: item, Quantity: 9, Available: 9})
	e.wantEvents(promotion.TopicFlashSaleChanged, s.ID, 2)
	e.checkBooks(v)
	e.wantTick("hết giờ", flashStart().Add(2*time.Hour), 0, 1)
	if end := e.getFlash(s.ID); end.Status != promotion.FlashClosed || end.ClosedReason != promotion.FlashClosedEnded {
		t.Fatalf("sau hết giờ: %+v", end)
	}
	e.wantStock("sau đóng", v, 0, 10)
	e.wantLive("sau đóng", s.ID, promotion.FlashLiveItem{ItemID: item, Quantity: 9, Released: 9})
	e.wantEvents(promotion.TopicFlashSaleClosed, s.ID, 1)
	if plus, minus := e.heldLedger(v); plus != 9 || minus != 9 {
		t.Fatalf("sổ held: +%d −%d, mong +9 −9", plus, minus)
	}
	e.checkBooks(v)
	e.wantTick("sau đóng", flashStart().Add(3*time.Hour), 0, 0)
}

// itemOf là món của biến thể v trong đợt s.
func itemOf(s promotion.FlashSale, v uuid.UUID) promotion.FlashItem {
	for _, it := range s.Items {
		if it.VariantID == v {
			return it
		}
	}
	return promotion.FlashItem{}
}

// newSixOfTen: env có một biến thể tồn 10 và một đợt nháp 2 giờ có món 6 suất của biến thể đó.
func newSixOfTen(t *testing.T) (*env, uuid.UUID, promotion.FlashSale) {
	t.Helper()
	e := newEnv(t)
	v := e.stockVariant(10)
	return e, v, e.draftWith(2*time.Hour, map[uuid.UUID]int{v: 6})
}

func (e *env) wantStock(step string, v uuid.UUID, held, available int32) {
	e.t.Helper()
	if b := e.stockOf(v); b.Held != held || b.Available != available {
		e.t.Fatalf("số dư %s: %+v, mong held %d có thể bán %d", step, b, held, available)
	}
}

func (e *env) wantLive(step string, sale uuid.UUID, want promotion.FlashLiveItem) {
	e.t.Helper()
	if l := e.live(sale); len(l.Items) != 1 || l.Items[0] != want {
		e.t.Fatalf("live %s: %+v, mong %+v", step, l, want)
	}
}

func (e *env) wantEvents(topic string, sale uuid.UUID, n int) {
	e.t.Helper()
	if got := e.events(topic, sale); got != n {
		e.t.Fatalf("%s: %d sự kiện, mong %d", topic, got, n)
	}
}

func (e *env) wantTick(step string, now time.Time, opened, closed int) {
	e.t.Helper()
	if o, c := e.tick(now); o != opened || c != closed {
		e.t.Fatalf("nhịp %s: mở %d đóng %d, mong %d %d", step, o, c, opened, closed)
	}
}

// heldLedger là tổng các dòng sổ held dương và (trị tuyệt đối) âm của biến thể.
func (e *env) heldLedger(v uuid.UUID) (plus, minus int) {
	e.t.Helper()
	plus = e.staff.CountIn(e.t, e.tenant, `SELECT coalesce(sum(delta), 0) FROM inventory.stock_ledger
		WHERE variant_id = $1 AND bucket = 'held' AND delta > 0`, v)
	minus = -e.staff.CountIn(e.t, e.tenant, `SELECT coalesce(sum(delta), 0) FROM inventory.stock_ledger
		WHERE variant_id = $1 AND bucket = 'held' AND delta < 0`, v)
	return plus, minus
}

// Đóng tay đợt đã lên lịch còn 6 trống → closed, lý do manual, held về 0 [FS08]; đóng lại, thêm suất sau đóng → ErrFlashNotActive;
// thêm suất, đóng đợt nháp → ErrFlashNotActive. Đợt hết giờ đã qua lúc lên lịch → ErrFlashEnded; đợt không món →
// ErrFlashEmpty; version cũ → ErrVersionConflict.
func TestFlashLifecycle_CloseAndRejections(t *testing.T) {
	t.Parallel()
	e, v, s := newSixOfTen(t)
	item := s.Items[0].ID
	if err := e.addSlots(s.ID, item, 1); !errors.Is(err, promotion.ErrFlashNotActive) {
		t.Fatalf("thêm suất đợt nháp: %v", err)
	}
	if err := e.closeFlash(s.ID, promotion.FlashClosedManual); !errors.Is(err, promotion.ErrFlashNotActive) {
		t.Fatalf("đóng đợt nháp: %v", err)
	}
	stale := s
	stale.Version--
	if err := e.schedule(stale, beforeStart()); !errors.Is(err, promotion.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if err := e.schedule(s, flashStart().Add(2*time.Hour)); !errors.Is(err, promotion.ErrFlashEnded) {
		t.Fatalf("lên lịch sau giờ kết thúc: %v", err)
	}
	empty := e.draftWith(time.Hour, nil)
	if err := e.schedule(empty, beforeStart()); !errors.Is(err, promotion.ErrFlashEmpty) {
		t.Fatalf("đợt không món: %v", err)
	}
	if err := e.schedule(s, beforeStart()); err != nil {
		t.Fatal(err)
	}
	if err := e.addSlots(s.ID, item, 0); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("thêm 0 suất: %v", err)
	}
	if err := e.addSlots(s.ID, item, 20000); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("vượt 20.000 suất: %v", err)
	}
	if err := e.closeFlash(s.ID, promotion.FlashClosedManual); err != nil {
		t.Fatal(err)
	}
	got := e.getFlash(s.ID)
	if got.Status != promotion.FlashClosed || got.ClosedReason != promotion.FlashClosedManual {
		t.Fatalf("sau đóng tay: %+v", got)
	}
	if b := e.stockOf(v); b.Held != 0 || b.Available != 10 {
		t.Fatalf("số dư sau đóng tay: %+v", b)
	}
	if err := e.closeFlash(s.ID, promotion.FlashClosedManual); !errors.Is(err, promotion.ErrFlashNotActive) {
		t.Fatalf("đóng lại: %v", err)
	}
	if err := e.addSlots(s.ID, item, 1); !errors.Is(err, promotion.ErrFlashNotActive) {
		t.Fatalf("thêm suất sau đóng: %v", err)
	}
	if err := e.closeFlash(s.ID, "lý do lạ"); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Fatalf("lý do lạ: %v", err)
	}
	e.checkBooks(v)
}

// Tồn 5, món 6 suất → *FlashShortageError {need 6, sellable 5, sku}; đợt vẫn draft, 0 đơn vị, held không đổi. Hai món,
// một món thiếu → lỗi chỉ liệt kê món thiếu, món đủ cũng không bị giữ. Thêm suất vượt số có thể bán → lỗi thiếu.
func TestFlashSchedule_Insufficient(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	short, ok := e.stockVariant(5), e.stockVariant(10)
	s := e.draftWith(time.Hour, map[uuid.UUID]int{short: 6, ok: 3})
	err := e.schedule(s, beforeStart())
	se, isShort := errors.AsType[*promotion.FlashShortageError](err)
	if !isShort || !errors.Is(err, promotion.ErrFlashInsufficient) || len(se.Items) != 1 {
		t.Fatalf("lên lịch thiếu hàng: %v", err)
	}
	shortItem, okItem := itemOf(s, short), itemOf(s, ok)
	sku := shortItem.SKU
	if got := se.Items[0]; got != (promotion.FlashShortItem{VariantID: short, SKU: sku, Need: 6, Sellable: 5}) {
		t.Fatalf("món thiếu: %+v (sku %s)", got, sku)
	}
	if st := e.getFlash(s.ID).Status; st != promotion.FlashDraft {
		t.Fatalf("trạng thái %s, mong draft", st)
	}
	e.wantStock("món thiếu", short, 0, 5)
	e.wantStock("món đủ", ok, 0, 10)
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM inventory.flash_sale_units`); n != 0 {
		t.Fatalf("đơn vị %d, mong 0", n)
	}
	// Bỏ món thiếu thì lên lịch được; thêm 8 suất cho món còn 7 có thể bán → thiếu {need 8, sellable 7}.
	if _, err := e.removeItem(s.ID, shortItem.ID); err != nil {
		t.Fatal(err)
	}
	item := okItem.ID
	if err := e.schedule(e.getFlash(s.ID), beforeStart()); err != nil {
		t.Fatal(err)
	}
	err = e.addSlots(s.ID, item, 8)
	if se, ok := errors.AsType[*promotion.FlashShortageError](err); !ok || len(se.Items) != 1 ||
		se.Items[0].Need != 8 || se.Items[0].Sellable != 7 {
		t.Fatalf("thêm suất thiếu hàng: %v", err)
	}
	if it := e.getFlash(s.ID).Items[0]; it.Quantity != 3 {
		t.Fatalf("số suất %d, mong 3", it.Quantity)
	}
	e.checkBooks(short, ok)
}

// Cùng biến thể ở đợt khác đang scheduled chồng giờ → ErrFlashOverlap (không giữ gì); không chồng giờ (bắt đầu đúng
// lúc đợt kia kết thúc) → được; đợt kia đã đóng → chồng giờ cũng được.
func TestFlashSchedule_Overlap(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	v := e.stockVariant(20)
	a := e.draftWith(2*time.Hour, map[uuid.UUID]int{v: 5})
	if err := e.schedule(a, beforeStart()); err != nil {
		t.Fatal(err)
	}
	b := e.draftWith(time.Hour, map[uuid.UUID]int{v: 5}) // 12:00–13:00 chồng 12:00–14:00
	if err := e.schedule(b, beforeStart()); !errors.Is(err, promotion.ErrFlashOverlap) {
		t.Fatalf("chồng giờ: %v", err)
	}
	if h := e.stockOf(v).Held; h != 5 {
		t.Fatalf("held %d, mong 5", h)
	}
	c, err := e.createFlash(promotion.FlashSaleInput{
		Name: "Sau", StartsAt: flashStart().Add(2 * time.Hour), EndsAt: flashStart().Add(3 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.addFlashItem(c.ID, flashItem(v, 199000, 5)); err != nil {
		t.Fatal(err)
	}
	if err := e.schedule(e.getFlash(c.ID), beforeStart()); err != nil {
		t.Fatalf("không chồng giờ: %v", err)
	}
	if err := e.closeFlash(a.ID, promotion.FlashClosedManual); err != nil {
		t.Fatal(err)
	}
	if err := e.schedule(e.getFlash(b.ID), beforeStart()); err != nil {
		t.Fatalf("đợt chồng đã đóng: %v", err)
	}
	if h := e.stockOf(v).Held; h != 10 {
		t.Fatalf("held %d, mong 10", h)
	}
	e.checkBooks(v)
}

// Số khách đã lấy: dòng claims quantity > 0 (đặt bằng chủ bảng; lượt mua ở Task 201); quantity 0 không tính. Đợt
// không có → ErrNotFound.
func TestFlashLive_Buyers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	v := e.stockVariant(10)
	s := e.draftWith(time.Hour, map[uuid.UUID]int{v: 4})
	c1, c2 := e.customer(), e.customer()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO promotion.flash_sale_claims (tenant_id, flash_sale_item_id,
			customer_id, quantity) VALUES ($1, $2, $3, 1), ($1, $2, $4, 0)`, e.tenant, s.Items[0].ID, c1, c2)
		return err //nolint:wrapcheck // test
	})
	if l := e.live(s.ID); l.Items[0].Buyers != 1 || l.Items[0].Available != 0 {
		t.Fatalf("live: %+v", l)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.Live(t.Context(), tx, uuid.New())
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, promotion.ErrNotFound) {
		t.Fatalf("đợt không có: %v", err)
	}
}

// Lịch promotion.flash_tick: 15 s, theo tenant; handler chạy nhịp trong tx việc và trả số đợt mở, đóng.
func TestFlashTickSchedule(t *testing.T) {
	t.Parallel()
	parts := promotion.Background(background.Deps{})
	found := false
	for _, s := range parts.Schedules {
		if s.Name == promotion.FlashTickSchedule {
			found = true
			if s.Every != 15*time.Second || !s.PerTenant {
				t.Fatalf("lịch: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("thiếu lịch promotion.flash_tick")
	}
	e := newEnv(t)
	v := e.stockVariant(3)
	s := e.draftWith(time.Hour, map[uuid.UUID]int{v: 2})
	if err := e.schedule(s, beforeStart()); err != nil {
		t.Fatal(err)
	}
	h := promotion.FlashTickHandler(flashStart)
	var out []byte
	if err := e.app(func(tx pgx.Tx) (err error) {
		out, err = h.Handle(t.Context(), queue.Job{TenantID: &e.tenant, Payload: json.RawMessage(`{}`)}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"closed":0,"opened":1}` {
		t.Fatalf("kết quả %s", out)
	}
}
