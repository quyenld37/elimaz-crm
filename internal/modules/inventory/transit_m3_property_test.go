package inventory_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Loại thao tác thêm của TestTransitM3Invariants (sau 0–8 của TestInventoryInvariants).
const (
	opConsumePartial = 9  // ConsumePartial op.qty của dòng đơn của một giữ hàng
	opReverse        = 10 // ReverseConsumption với ref mới
	opReverseAgain   = 11 // ReverseConsumption lặp lại ref gần nhất (gọi lặp)
	opLoss           = 12 // RecordLoss op.qty
	opLossAgain      = 13 // RecordLoss lặp lại ref gần nhất
	transitOpKinds   = 14
)

// transitRun là trạng thái chung của các goroutine trong một lượt: giữ hàng đã tạo, ref gần nhất, giữ hàng đã đảo
// (Consume của M1 không gọi lại cho dòng đã đảo: sau đảo chỉ có đường hoàn, Q-T96-7). revMu cho Consume của M1 và
// ReverseConsumption chạy lần lượt với nhau (mọi thao tác khác vẫn song song), để cờ "đã đảo" đọc trước Consume không
// lỗi thời.
type transitRun struct {
	e        *invEnv
	vs       []uuid.UUID
	revMu    sync.Mutex
	mu       sync.Mutex
	mine     []uuid.UUID
	items    map[uuid.UUID]uuid.UUID // giữ hàng → dòng đơn
	reversed map[uuid.UUID]bool
	lastRev  struct {
		ref   uuid.UUID
		items []uuid.UUID
	}
	lastLoss struct {
		ref  uuid.UUID
		line inventory.LossLine
	}
}

// TestTransitM3Invariants (rapid, số lượt chung của gói, ≥ 200): 1–3 biến thể, 1–4 goroutine, chuỗi ngẫu nhiên các
// hàm của M1, M2 (giữ, nhả, đi giao, giao, nhập, phiếu kho, nhả in_transit, nhận trả, hỏng) xen ba hàm mới và lần gọi
// lặp → mọi số dư ≥ 0, sổ theo ngăn = số dư, giữ hàng held/in_transit = số dư (checkInvariants); mỗi đơn vị hàng chỉ
// bị trừ on_hand một lần trừ khi có đảo (checkOnceOut).
func TestTransitM3Invariants(t *testing.T) {
	e := newInvEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		vs := e.variants(rapid.IntRange(1, 3).Draw(rt, "variants"))
		for _, v := range vs {
			e.seed(v, rapid.Int32Range(15, 20).Draw(rt, "stock")) // đủ cho 5 đơn × 3 cái giữ sẵn
		}
		r := &transitRun{e: e, vs: vs, items: map[uuid.UUID]uuid.UUID{}, reversed: map[uuid.UUID]bool{}}
		r.setup(rt)
		e.checkConcurrentOps(rt, vs, transitOpKinds, r.run)
		var reversed []uuid.UUID
		for id := range r.reversed {
			reversed = append(reversed, id)
		}
		for _, v := range vs {
			if msg := e.checkOnceOut(v, reversed); msg != "" {
				rt.Fatalf("biến thể %v: %s", v, msg)
			}
		}
	})
}

// setup dựng 1–5 đơn, mỗi đơn một dòng (biến thể, 1–3 cái) đã giữ; phần lớn đã đi giao (StartTransitByItems của M1),
// để các thao tác sau chạm giữ hàng in_transit và consumed thường xuyên (phân bố của rapid lệch về số nhỏ).
func (r *transitRun) setup(rt *rapid.T) {
	e := r.e
	for range rapid.IntRange(1, 5).Draw(rt, "orders") {
		l := e.line(r.vs[rapid.IntRange(0, len(r.vs)-1).Draw(rt, "orderVariant")], rapid.Int32Range(1, 3).Draw(rt, "orderQty"))
		ids, err := e.reserve(sysActor(), l)
		if err != nil {
			rt.Fatalf("giữ hàng ban đầu: %v", err)
		}
		r.mine = append(r.mine, ids[0])
		r.items[ids[0]] = l.OrderItemID
		if rapid.IntRange(0, 3).Draw(rt, "orderHeld") == 0 {
			continue
		}
		if err := e.startTransit(ids, sysActor()); err != nil {
			rt.Fatalf("đi giao ban đầu: %v", err)
		}
	}
}

// run chạy ops tuần tự (mỗi thao tác một transaction); thiếu hàng, sai trạng thái là kết quả hợp lệ.
func (r *transitRun) run(ops []propOp) error {
	for _, op := range ops {
		err := r.op(op)
		if err != nil && !errors.Is(err, inventory.ErrInsufficient) && !errors.Is(err, inventory.ErrStateConflict) {
			return err
		}
	}
	return nil
}

// pick trả giữ hàng thứ op.target (vòng) và dòng đơn của nó; chưa có → false.
func (r *transitRun) pick(op propOp) (id, item uuid.UUID, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.mine) == 0 {
		return uuid.Nil, uuid.Nil, false
	}
	id = r.mine[op.target%len(r.mine)]
	return id, r.items[id], true
}

func (r *transitRun) op(op propOp) error {
	switch op.kind {
	case 1, 2, 3, 6, opConsumePartial, opReverse:
		return r.onReservation(op)
	}
	e, v := r.e, r.vs[op.variant]
	switch op.kind {
	case 0:
		l := e.line(v, op.qty)
		ids, err := e.reserve(sysActor(), l)
		if err == nil {
			r.mu.Lock()
			r.mine = append(r.mine, ids...)
			r.items[ids[0]] = l.OrderItemID
			r.mu.Unlock()
		}
		return err
	case opLoss:
		ref, l := uuid.New(), inventory.LossLine{VariantID: v, WarehouseID: e.warehouse, Qty: op.qty}
		err := e.recordLoss(ref, "missing_items", l)
		if err == nil {
			r.mu.Lock()
			r.lastLoss.ref, r.lastLoss.line = ref, l
			r.mu.Unlock()
		}
		return err
	case opLossAgain:
		r.mu.Lock()
		ref, l := r.lastLoss.ref, r.lastLoss.line
		r.mu.Unlock()
		if ref == uuid.Nil {
			return nil
		}
		return e.recordLoss(ref, "missing_items", l)
	case opReverseAgain:
		r.mu.Lock()
		ref, items := r.lastRev.ref, r.lastRev.items
		r.mu.Unlock()
		if ref == uuid.Nil {
			return nil
		}
		return e.reverse(ref, "carrier_corrected", items...)
	}
	return e.runOps(r.vs, []propOp{op}) // 4, 5, 7, 8: không dùng giữ hàng của lượt
}

// onReservation chạy thao tác op trên giữ hàng thứ op.target (vòng) của lượt; chưa có giữ hàng → bỏ qua.
func (r *transitRun) onReservation(op propOp) error {
	e := r.e
	id, item, ok := r.pick(op)
	if !ok {
		return nil
	}
	switch op.kind {
	case 1:
		return e.release([]uuid.UUID{id}, "prop")
	case 2:
		return e.startTransit([]uuid.UUID{id}, sysActor())
	case 3:
		r.revMu.Lock()
		defer r.revMu.Unlock()
		r.mu.Lock()
		reversed := r.reversed[id]
		r.mu.Unlock()
		if reversed {
			return nil // sau đảo chỉ đi đường hoàn (Q-T96-7)
		}
		return e.consume([]uuid.UUID{id}, sysActor())
	case 6:
		return e.byItems(func(ctx context.Context, tx pgx.Tx, w inventory.Writer, items []uuid.UUID) error {
			return w.ReleaseInTransitByItems(ctx, tx, items, sysActor())
		}, item)
	case opConsumePartial:
		return e.consumePartial(inventory.PartialLine{OrderItemID: item, Qty: op.qty})
	default: // opReverse
		r.revMu.Lock()
		defer r.revMu.Unlock()
		ref := uuid.New()
		err := e.reverse(ref, "carrier_corrected", item)
		if err == nil {
			r.mu.Lock()
			r.reversed[id] = true
			r.lastRev.ref, r.lastRev.items = ref, []uuid.UUID{item}
			r.mu.Unlock()
		}
		return err
	}
}

// checkOnceOut kiểm mỗi dòng giữ hàng của v: tổng on_hand bị trừ theo sổ của chính nó (consume, consume_partial)
// không vượt số đã giữ (reserve) — trừ dòng trong reversed (đã đảo: phần đảo cộng lại on_hand qua sổ adjust), và dòng còn held/in_transit có quantity = số đã giữ − phần đã giao một phần. Trả mô
// tả chỗ sai, rỗng khi đúng.
func (e *invEnv) checkOnceOut(v uuid.UUID, reversed []uuid.UUID) string {
	e.t.Helper()
	var msg string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `
			SELECT coalesce(string_agg(format('giữ hàng %s (%s ×%s): giữ %s, đã trừ %s, giao một phần %s',
			         r.id, r.state, r.quantity, l.reserved, l.out, l.part), '; '), '')
			FROM inventory.stock_reservations AS r,
			LATERAL (SELECT
			  coalesce(sum(delta) FILTER (WHERE bucket = 'held' AND step = 'reserve'), 0) AS reserved,
			  coalesce(-sum(delta) FILTER (WHERE bucket = 'on_hand'), 0) AS out,
			  coalesce(-sum(delta) FILTER (WHERE bucket = 'on_hand' AND step = 'consume_partial'), 0) AS part
			  FROM inventory.stock_ledger WHERE doc_type = 'order' AND doc_id = r.id) AS l
			WHERE r.variant_id = $1 AND r.warehouse_id = $2
			  AND ((l.out > l.reserved AND r.id <> ALL ($3::uuid[])) OR (r.state IN ('held', 'in_transit') AND r.quantity <> l.reserved - l.part))`,
			v, e.warehouse, reversed).Scan(&msg)
	}); err != nil {
		return "đọc giữ hàng: " + err.Error()
	}
	return msg
}
