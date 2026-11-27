package inventory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Chứng từ giữ cho đợt flash sale (kế hoạch M5, Task 200; ca FS01, FS08, FS09).

// flashItem chèn một đợt và một món (qty suất) của biến thể v bằng chủ bảng (promotion soạn đợt; inventory chỉ cần mã
// món cho khóa ngoại) và trả tham chiếu món.
func (e *invEnv) flashItem(v uuid.UUID, qty int) inventory.FlashItemRef {
	e.t.Helper()
	var item uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		ctx := e.t.Context()
		var sale uuid.UUID
		start := time.Date(2026, 12, 12, 12, 0, 0, 0, time.UTC)
		if err := tx.QueryRow(ctx, `INSERT INTO promotion.flash_sales (tenant_id, name, starts_at, ends_at, created_by)
			VALUES ($1, 'Đợt thử', $2, $3, $4) RETURNING id`, e.tenant, start, start.Add(time.Hour), uuid.New()).
			Scan(&sale); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO promotion.flash_sale_items (tenant_id, flash_sale_id, seller_id, variant_id,
			warehouse_id, sale_price, quantity) VALUES ($1, $2, $3, $4, $5, 50000, $6) RETURNING id`,
			e.tenant, sale, e.seller, v, e.warehouse, qty).Scan(&item)
	})
	return inventory.FlashItemRef{ItemID: item, VariantID: v, WarehouseID: e.warehouse, Quantity: qty, Step: "schedule"}
}

// holdUnits đặt n đơn vị đầu của món là đang giữ cho một dòng đơn thật của biến thể v (chủ bảng; lượt mua là Task 201).
func (e *invEnv) holdUnits(item, v uuid.UUID, n int) {
	e.t.Helper()
	line, err := e.orderItem(v)
	if err != nil {
		e.t.Fatal(err)
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE inventory.flash_sale_units SET status = 'held', order_item_id = $2
			WHERE id IN (SELECT id FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1 ORDER BY id LIMIT $3)`,
			item, line, n)
		return err //nolint:wrapcheck // test
	})
}

// units đếm đơn vị suất của món theo trạng thái.
func (e *invEnv) units(item uuid.UUID, status string) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1 AND status = $2`,
		item, status)
}

// ledgerSum là tổng delta ngăn held của sổ theo doc_type cho món.
func (e *invEnv) ledgerSum(item uuid.UUID, docType string) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(delta), 0) FROM inventory.stock_ledger
		WHERE doc_id = $1 AND doc_type = $2 AND bucket = 'held'`, item, docType)
}

// checkFlashBooks: đối chiếu đêm không lệch sổ (ledger_vs_balance), và held = giữ hàng đang held + đơn vị flash còn
// trống (công thức M5-19 viết trong test: đơn vị held có dòng giữ hàng riêng ở Task 201).
func (e *invEnv) checkFlashBooks(v uuid.UUID, extraHeld int) {
	e.t.Helper()
	for _, m := range e.reconcile(0).Mismatches {
		if m.Check == inventory.CheckLedgerVsBalance {
			e.t.Errorf("sổ lệch số dư: %+v", m)
		}
	}
	reserved := e.count(`SELECT coalesce(sum(quantity), 0) FROM inventory.stock_reservations
		WHERE variant_id = $1 AND state = 'held'`, v)
	avail := e.count(`SELECT count(*) FROM inventory.flash_sale_units AS u
		JOIN promotion.flash_sale_items AS i ON i.id = u.flash_sale_item_id
		WHERE i.variant_id = $1 AND u.status = 'available'`, v)
	if got := int(e.balance(v).Held); got != reserved+avail+extraHeld {
		e.t.Errorf("held %d, mong giữ hàng %d + đơn vị trống %d + %d", got, reserved, avail, extraHeld)
	}
}

// Tồn 10 → giữ cho đợt 6 suất: held 6, 6 đơn vị trống, có thể bán thường còn 4; đơn thường 5 cái hết hàng, 4 cái được
// [FS01] [FS09]. Sổ có flash_hold +6, một stock.changed; chạy lại cùng bước → lỗi unique của sổ, không áp hai lần.
func TestFlashReserve_HoldsForSale(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	r := e.flashItem(v, 6)
	var short []inventory.FlashShortage
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) (err error) {
		if short, err = inventory.LockFlashStock(ctx, tx, []inventory.FlashItemRef{r}); err != nil {
			return err //nolint:wrapcheck // test
		}
		return w.ReserveForFlash(ctx, tx, r, sysActor())
	}); err != nil {
		t.Fatal(err)
	}
	if len(short) != 0 {
		t.Fatalf("thiếu: %+v", short)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 10, Held: 6, Available: 4})
	if n := e.units(r.ItemID, "available"); n != 6 {
		t.Fatalf("đơn vị trống %d, mong 6", n)
	}
	if n := e.ledgerSum(r.ItemID, "flash_hold"); n != 6 {
		t.Fatalf("sổ flash_hold %d, mong 6", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed' AND aggregate_id = $1`,
		v.String()+":"+e.warehouse.String()); n != 1 { // nạp tồn (invtest) không phát
		t.Fatalf("stock.changed %d, mong 1", n)
	}
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ReserveForFlash(ctx, tx, inventory.FlashItemRef{
			ItemID: r.ItemID, VariantID: v, WarehouseID: e.warehouse, Quantity: 1, Step: "schedule",
		}, sysActor())
	})
	if pg, ok := errors.AsType[*pgconn.PgError](err); !ok || pg.ConstraintName != "stock_ledger_step_key" {
		t.Fatalf("áp lại bước schedule: %v, mong lỗi stock_ledger_step_key", err)
	}
	if _, err := e.reserve(sysActor(), e.line(v, 5)); !errors.Is(err, inventory.ErrInsufficient) {
		t.Fatalf("đơn thường 5 cái: %v, mong ErrInsufficient", err)
	}
	if _, err := e.reserve(sysActor(), e.line(v, 4)); err != nil {
		t.Fatalf("đơn thường 4 cái: %v", err)
	}
	e.checkFlashBooks(v, 0)
}

// Tồn 5, món 6 suất → LockFlashStock báo {need 6, sellable 5}; ReserveForFlash → *InsufficientError, không đơn vị,
// held không đổi. Biến thể chưa từng có dòng số dư → sellable 0.
func TestFlashReserve_Insufficient(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(5)
	none := e.variants(1)[0]
	r, rn := e.flashItem(v, 6), e.flashItem(none, 1)
	var short []inventory.FlashShortage
	if err := e.do(func(ctx context.Context, tx pgx.Tx, _ inventory.Writer) (err error) {
		short, err = inventory.LockFlashStock(ctx, tx, []inventory.FlashItemRef{r, rn})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	wantShort := map[uuid.UUID]inventory.FlashShortage{
		v:    {ItemID: r.ItemID, VariantID: v, WarehouseID: e.warehouse, Need: 6, Sellable: 5},
		none: {ItemID: rn.ItemID, VariantID: none, WarehouseID: e.warehouse, Need: 1, Sellable: 0},
	}
	if len(short) != 2 {
		t.Fatalf("thiếu: %+v", short)
	}
	for _, s := range short {
		if s != wantShort[s.VariantID] {
			t.Errorf("thiếu %+v, mong %+v", s, wantShort[s.VariantID])
		}
	}
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ReserveForFlash(ctx, tx, r, sysActor())
	})
	if ie, ok := errors.AsType[*inventory.InsufficientError](err); !ok || ie.Requested != 6 || ie.Available != 5 {
		t.Fatalf("giữ 6 khi còn 5: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 0, Available: 5})
	if n := e.units(r.ItemID, "available"); n != 0 {
		t.Fatalf("đơn vị %d, mong 0", n)
	}
}

// Thêm 3 suất → held 9, 9 đơn vị trống, sổ flash_hold +6 +3 (bước add_2); thêm vượt số có thể bán → *InsufficientError.
// Đóng món có 4 trống và 2 đang giữ (đặt bằng chủ bảng) → 4 released, held còn 2, sổ flash_release −4 [FS08]; đóng
// lần nữa → 0, không thêm dòng sổ.
func TestFlashAddAndClose(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	r := e.flashItem(v, 3)
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		if err := w.ReserveForFlash(ctx, tx, r, sysActor()); err != nil {
			return err //nolint:wrapcheck // test
		}
		add := r
		add.Step = "add_2"
		return w.AddFlashUnits(ctx, tx, add, 3, sysActor())
	}); err != nil {
		t.Fatal(err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 10, Held: 6, Available: 4})
	err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		add := r
		add.Step = "add_3"
		return w.AddFlashUnits(ctx, tx, add, 5, sysActor())
	})
	if !errors.Is(err, inventory.ErrInsufficient) {
		t.Fatalf("thêm 5 khi còn 4: %v", err)
	}
	if n := e.units(r.ItemID, "available"); n != 6 {
		t.Fatalf("đơn vị trống %d, mong 6", n)
	}
	e.holdUnits(r.ItemID, v, 2)
	for i, wantReleased := range []int{4, 0} {
		var released int
		if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) (err error) {
			released, err = w.CloseFlashItem(ctx, tx, r, sysActor())
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatalf("đóng lần %d: %v", i+1, err)
		}
		if released != wantReleased {
			t.Fatalf("đóng lần %d trả %d, mong %d", i+1, released, wantReleased)
		}
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 10, Held: 2, Available: 8})
	if a, h, rl := e.units(r.ItemID, "available"), e.units(r.ItemID, "held"), e.units(r.ItemID, "released"); a != 0 || h != 2 || rl != 4 {
		t.Fatalf("đơn vị trống %d giữ %d trả %d, mong 0 2 4", a, h, rl)
	}
	if hold, rel := e.ledgerSum(r.ItemID, "flash_hold"), e.ledgerSum(r.ItemID, "flash_release"); hold != 6 || rel != -4 {
		t.Fatalf("sổ flash_hold %d flash_release %d, mong 6 −4", hold, rel)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND doc_type = 'flash_release'`,
		r.ItemID); n != 1 {
		t.Fatalf("dòng sổ flash_release %d, mong 1", n)
	}
	e.checkFlashBooks(v, 2) // 2 đơn vị giữ bằng chủ bảng, không có dòng giữ hàng
}
