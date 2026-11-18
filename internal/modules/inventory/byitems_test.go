package inventory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// snapshot là số dư, dòng sổ (bỏ mã, doc, thời gian) và trạng thái giữ hàng của một biến thể, để so hai đường.
type snapshot struct {
	bal    inventory.Balance
	ledger []string
	states []string
}

func (e *invEnv) snapshot(v uuid.UUID) snapshot {
	e.t.Helper()
	s := snapshot{bal: e.balance(v)}
	s.bal.VariantID, s.bal.WarehouseID = uuid.Nil, uuid.Nil
	if err := e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT bucket || ':' || delta || ':' || doc_type || ':' || step || ':' || actor_type
			FROM inventory.stock_ledger WHERE variant_id = $1 ORDER BY 1`, v)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if s.ledger, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err //nolint:wrapcheck // test
		}
		rows, err = tx.Query(e.t.Context(), "SELECT state FROM inventory.stock_reservations WHERE variant_id = $1 ORDER BY 1", v)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		s.states, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (s snapshot) equal(o snapshot) bool {
	if s.bal != o.bal || len(s.ledger) != len(o.ledger) || len(s.states) != len(o.states) {
		return false
	}
	for i := range s.ledger {
		if s.ledger[i] != o.ledger[i] {
			return false
		}
	}
	for i := range s.states {
		if s.states[i] != o.states[i] {
			return false
		}
	}
	return true
}

// TestByItems_SameAsCore: ba hàm theo order_item_id cho cùng số dư, sổ, trạng thái như bản theo mã giữ hàng; gọi
// lại lần hai → ErrStateConflict.
func TestByItems_SameAsCore(t *testing.T) {
	e := newInvEnv(t)
	type path struct {
		release, transit, consume func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, ids []uuid.UUID) error
	}
	core := path{
		release: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, _, ids []uuid.UUID) error {
			return w.Release(ctx, tx, ids, "order_cancelled", sysActor())
		},
		transit: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, _, ids []uuid.UUID) error {
			return w.StartTransit(ctx, tx, ids, sysActor())
		},
		consume: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, _, ids []uuid.UUID) error {
			return w.Consume(ctx, tx, ids, sysActor())
		},
	}
	byItems := path{
		release: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, _ []uuid.UUID) error {
			return w.ReleaseHeldByItems(ctx, tx, items, "order_cancelled", sysActor())
		},
		transit: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, _ []uuid.UUID) error {
			return w.StartTransitByItems(ctx, tx, items, sysActor())
		},
		consume: func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, _ []uuid.UUID) error {
			return w.ConsumeByItems(ctx, tx, items, sysActor())
		},
	}
	// run: biến thể mới on_hand 10; hai dòng đơn (2 và 3 cái); dòng 1 nhả; dòng 2 đi giao rồi giao xong.
	run := func(p path) snapshot {
		v := e.stock(10)
		l1, l2 := e.line(v, 2), e.line(v, 3)
		ids, err := e.reserve(sysActor(), l1, l2)
		if err != nil {
			t.Fatal(err)
		}
		step := func(name string, f func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items, ids []uuid.UUID) error,
			items, rids []uuid.UUID,
		) {
			t.Helper()
			call := func() error {
				return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error { return f(ctx, tx, w, items, rids) })
			}
			if err := call(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err := call(); !errors.Is(err, inventory.ErrStateConflict) {
				t.Fatalf("%s lần hai: mong ErrStateConflict, có %v", name, err)
			}
		}
		step("release", p.release, []uuid.UUID{l1.OrderItemID}, ids[:1])
		step("transit", p.transit, []uuid.UUID{l2.OrderItemID}, ids[1:])
		step("consume", p.consume, []uuid.UUID{l2.OrderItemID}, ids[1:])
		return e.snapshot(v)
	}
	a, b := run(core), run(byItems)
	if !a.equal(b) {
		t.Fatalf("hai đường khác nhau:\nlõi    %+v\ntheo dòng %+v", a, b)
	}
	if a.bal.OnHand != 7 || a.bal.Held != 0 || a.bal.InTransit != 0 || a.bal.Available != 7 {
		t.Errorf("số dư cuối sai: %+v", a.bal)
	}
	// Dòng đơn không có giữ hàng → ErrStateConflict.
	if err := e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		return w.ConsumeByItems(ctx, tx, []uuid.UUID{uuid.New()}, sysActor())
	}); !errors.Is(err, inventory.ErrStateConflict) {
		t.Errorf("dòng đơn lạ: mong ErrStateConflict, có %v", err)
	}
}
