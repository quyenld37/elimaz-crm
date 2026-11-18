package inventory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

func (e *invEnv) release(ids []uuid.UUID, reason string) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.Release(ctx, tx, ids, reason, sysActor())
	})
}

func (e *invEnv) startTransit(ids []uuid.UUID, a inventory.Actor) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.StartTransit(ctx, tx, ids, a)
	})
}

func (e *invEnv) consume(ids []uuid.UUID, a inventory.Actor) error {
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.Consume(ctx, tx, ids, a)
	})
}

// Release hai lần → lần hai ErrStateConflict, held chỉ giảm một lần; lý do được lưu.
func TestRelease_Twice(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(5)
	ids, err := e.reserve(sysActor(), e.line(v, 3))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.release(ids, "order_cancelled"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := e.release(ids, "order_cancelled"); !errors.Is(err, inventory.ErrStateConflict) {
		t.Fatalf("Release lần hai: mong ErrStateConflict, có %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Available: 5})
	if s := e.reservationState(ids[0]); s != "released" {
		t.Errorf("trạng thái %q, mong released", s)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_reservations WHERE id = $1 AND release_reason = 'order_cancelled'
		AND state_changed_at >= created_at`, ids[0]); n != 1 {
		t.Errorf("lý do nhả không được lưu")
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND bucket = 'held' AND delta = -3
		AND step = 'release'`, ids[0]); n != 1 {
		t.Errorf("mong 1 dòng sổ nhả, có %d", n)
	}
	for _, r := range []string{"", "Hủy đơn", strings.Repeat("a", 65)} {
		ids2, err := e.reserve(sysActor(), e.line(v, 1))
		if err != nil {
			t.Fatal(err)
		}
		if err := e.release(ids2, r); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("lý do %q: mong ErrInvalidInput, có %v", r, err)
		}
		if err := e.release(ids2, "cleanup"); err != nil {
			t.Fatal(err)
		}
	}
}

// StartTransit rồi Consume → held 0, in_transit 0, on_hand giảm đúng qty; Consume dòng đang held → ErrStateConflict.
func TestTransitThenConsume(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(7)
	ids, err := e.reserve(sysActor(), e.line(v, 3))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.consume(ids, sysActor()); !errors.Is(err, inventory.ErrStateConflict) {
		t.Fatalf("Consume dòng held: mong ErrStateConflict, có %v", err)
	}
	carrier := inventory.Actor{Type: "carrier"}
	if err := e.startTransit(ids, carrier); err != nil {
		t.Fatalf("StartTransit: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 7, InTransit: 3, Available: 4})
	if err := e.startTransit(ids, carrier); !errors.Is(err, inventory.ErrStateConflict) {
		t.Fatalf("StartTransit lần hai: mong ErrStateConflict, có %v", err)
	}
	if err := e.release(ids, "x"); !errors.Is(err, inventory.ErrStateConflict) {
		t.Fatalf("Release dòng in_transit: mong ErrStateConflict, có %v", err)
	}
	if err := e.consume(ids, carrier); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 4, Available: 4})
	if s := e.reservationState(ids[0]); s != "consumed" {
		t.Errorf("trạng thái %q, mong consumed", s)
	}
	if err := e.consume(ids, carrier); !errors.Is(err, inventory.ErrStateConflict) {
		t.Fatalf("Consume lần hai: mong ErrStateConflict, có %v", err)
	}
	// Sổ: reserve held+3; start_transit held−3, in_transit+3; consume in_transit−3, on_hand−3 (đều của carrier, actor NULL).
	for _, c := range []struct {
		bucket, step string
		delta        int
	}{{"held", "start_transit", -3}, {"in_transit", "start_transit", 3}, {"in_transit", "consume", -3}, {"on_hand", "consume", -3}} {
		if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND doc_id = $1 AND bucket = $2
			AND step = $3 AND delta = $4 AND actor_type = 'carrier' AND actor_id IS NULL`, ids[0], c.bucket, c.step, c.delta); n != 1 {
			t.Errorf("sổ %s/%s: mong 1 dòng, có %d", c.bucket, c.step, n)
		}
	}
	// Một stock.changed mỗi lần xử lý dòng giữ hàng (Q-T28-5): reserve, start_transit, consume (seed không phát).
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'stock.changed'"); n != 3 {
		t.Errorf("mong 3 sự kiện stock.changed, có %d", n)
	}
}

// Một lần gọi có mã không tồn tại (hoặc của tenant khác) → ErrStateConflict, không đổi gì.
func TestCore_UnknownIDAndMixed(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(5)
	ids, err := e.reserve(sysActor(), e.line(v, 1), e.line(v, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.release(append(ids, uuid.New()), "x"); !errors.Is(err, inventory.ErrStateConflict) {
		t.Fatalf("mong ErrStateConflict, có %v", err)
	}
	if err := e.release(nil, "x"); err != nil {
		t.Fatalf("danh sách rỗng: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 2, Available: 3})
	// Mã trùng trong danh sách đếm một lần.
	if err := e.release([]uuid.UUID{ids[0], ids[0], ids[1]}, "x"); err != nil {
		t.Fatalf("mã trùng: %v", err)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Available: 5})
	// Trạng thái trộn (một held, một đã released): lỗi, và không dòng nào bị đổi kể cả khi bên gọi vẫn commit.
	mixed, err := e.reserve(sysActor(), e.line(v, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		if err := w.Release(ctx, tx, []uuid.UUID{mixed[0], ids[0]}, "x", sysActor()); !errors.Is(err, inventory.ErrStateConflict) {
			t.Errorf("trạng thái trộn: mong ErrStateConflict, có %v", err)
		}
		return nil // commit dù có lỗi: phải chưa ghi gì
	}); err != nil {
		t.Fatal(err)
	}
	if s := e.reservationState(mixed[0]); s != "held" {
		t.Errorf("dòng held bị đổi thành %q dù lỗi", s)
	}
	want(t, e.balance(v), inventory.Balance{OnHand: 5, Held: 1, Available: 4})
}

// Dòng sổ do hệ thống ghi có actor_id NULL; do nhân viên ghi có đúng uuid người đó.
func TestLedger_Actor(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(5)
	staff := inventory.Actor{Type: "staff", ID: uuid.New()}
	sys, err := e.reserve(sysActor(), e.line(v, 1))
	if err != nil {
		t.Fatal(err)
	}
	st, err := e.reserve(staff, e.line(v, 1))
	if err != nil {
		t.Fatal(err)
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND actor_type = 'system' AND actor_id IS NULL", sys[0]); n != 1 {
		t.Errorf("dòng sổ hệ thống phải có actor_id NULL")
	}
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1 AND actor_type = 'staff' AND actor_id = $2", st[0], staff.ID); n != 1 {
		t.Errorf("dòng sổ nhân viên phải có actor_id của người đó")
	}
	// Đọc qua view công bố.
	if n := e.count("SELECT count(*) FROM inventory.stock_ledger_v WHERE doc_id = $1 AND actor_id = $2", st[0], staff.ID); n != 1 {
		t.Errorf("stock_ledger_v thiếu dòng")
	}
}

func pgCode(err error) string {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code
	}
	return ""
}

// Sổ chỉ ghi thêm: trùng (doc, bucket, step) → unique; UPDATE bằng crm_app → lỗi quyền; bằng chủ bảng → append-only.
func TestLedger_AppendOnly(t *testing.T) {
	e := newInvEnv(t)
	v := e.stock(5)
	ids, err := e.reserve(sysActor(), e.line(v, 1))
	if err != nil {
		t.Fatal(err)
	}
	dup := func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO inventory.stock_ledger
			(tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type)
			VALUES ($1, $2, $3, 'held', 1, 'order', $4, 'reserve', 'system')`, e.tenant, v, e.warehouse, ids[0])
		return err //nolint:wrapcheck // test
	}
	if err := e.app(dup); pgCode(err) != "23505" {
		t.Errorf("ghi trùng bước: mong 23505, có %v", err)
	}
	upd := func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE inventory.stock_ledger SET delta = 100")
		return err //nolint:wrapcheck // test
	}
	if err := e.app(upd); pgCode(err) != "42501" || strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE bằng crm_app: mong lỗi quyền 42501, có %v", err)
	}
	if err := e.staff.TryExec(t.Context(), e.tenant, upd); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE bằng chủ bảng: mong lỗi append-only, có %v", err)
	}
	del := func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "DELETE FROM inventory.stock_ledger")
		return err //nolint:wrapcheck // test
	}
	if err := e.staff.TryExec(t.Context(), e.tenant, del); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE bằng chủ bảng: mong lỗi append-only, có %v", err)
	}
	// Số âm bị CHECK chặn ở database.
	neg := func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE inventory.stock_balances SET held = held - 100 WHERE variant_id = $1", v)
		return err //nolint:wrapcheck // test
	}
	if err := e.app(neg); pgCode(err) != "23514" {
		t.Errorf("held âm: mong 23514, có %v", err)
	}
}

// DefaultWarehouse trả kho MAIN; tenant chưa có kho → ErrNoWarehouse. EnsureDefaultWarehouse chạy lại được.
func TestDefaultWarehouse(t *testing.T) {
	e := newInvEnv(t)
	var got uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = inventory.DefaultWarehouse(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil || got != e.warehouse {
		t.Fatalf("DefaultWarehouse = %v, %v; mong %v", got, err, e.warehouse)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		again, err := inventory.EnsureDefaultWarehouse(t.Context(), tx, e.tenant, e.seller)
		if err == nil && again != e.warehouse {
			t.Errorf("EnsureDefaultWarehouse lần hai trả kho khác: %v", again)
		}
		return err //nolint:wrapcheck // test
	})
	if n := e.count(`SELECT count(*) FROM inventory.warehouses_v WHERE code = 'MAIN' AND name = 'Kho chính'
		AND status = 'active' AND operated_by_seller_id = $1`, e.seller); n != 1 {
		t.Errorf("mong đúng 1 kho MAIN \"Kho chính\" do ELIMAZ vận hành, có %d", n)
	}
	other := e.staff.Tenant(t, "Trống")
	err := tenancyApp(e, other, func(tx pgx.Tx) error {
		_, err := inventory.DefaultWarehouse(t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, inventory.ErrNoWarehouse) {
		t.Errorf("tenant chưa có kho: mong ErrNoWarehouse, có %v", err)
	}
}
