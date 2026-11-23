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

type propOp struct {
	kind int // 0 reserve, 1 release, 2 start transit, 3 consume, 4 nhập kho, 5 ghi sổ phiếu kho (Task M1-29),
	// 6 nhả giữ hàng in_transit, 7 nhận hàng khách trả (sign: hỏng), 8 chuyển sang hỏng (Task M2-22)
	docKind int // loại phiếu khi kind = 5: chỉ số trong docKinds
	sign    bool
	variant int
	qty     int32
	target  int
}

// TestInventoryInvariants (rapid, 300 lượt): 1–4 biến thể, chuỗi ngẫu nhiên Reserve, Release, StartTransit, Consume,
// nhập kho, ghi sổ phiếu kho mọi loại (Task M1-29), ba đường trả hàng (Task M2-22: ReleaseInTransitByItems,
// ReceiveCustomerReturn, MoveToDamaged), chạy xen kẽ 1–4 goroutine → (1) mọi cột ≥ 0; (2) mỗi ngăn: tổng
// delta trong sổ = số dư; (3) held = tổng giữ hàng held, in_transit = tổng giữ hàng in_transit; (4) available ≥ 0;
// (5) damaged ≤ on_hand (đường không có kiểm kho).
func TestInventoryInvariants(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		vs := e.variants(rapid.IntRange(1, 4).Draw(rt, "variants"))
		for _, v := range vs {
			if n := rapid.Int32Range(0, 6).Draw(rt, "stock"); n > 0 {
				e.seed(v, n) // 0: chưa có dòng số dư
			}
		}
		e.checkConcurrentOps(rt, vs, 9, func(ops []propOp) error { return e.runOps(vs, ops) })
	})
}

// checkConcurrentOps (trong rapid.Check): 1–4 goroutine, mỗi goroutine 1–8 thao tác ngẫu nhiên (kind trong [0, kinds))
// chạy bằng run song song → lỗi không mong đợi hay bất biến sai (checkInvariants) của biến thể nào trong vs → rt.Fatalf.
func (e *invEnv) checkConcurrentOps(rt *rapid.T, vs []uuid.UUID, kinds int, run func(ops []propOp) error) {
	workers := make([][]propOp, rapid.IntRange(1, 4).Draw(rt, "goroutines"))
	for g := range workers {
		workers[g] = rapid.SliceOfN(rapid.Custom(func(rt *rapid.T) propOp {
			return propOp{
				kind: rapid.IntRange(0, kinds-1).Draw(rt, "kind"), docKind: rapid.IntRange(0, len(docKinds)-1).Draw(rt, "docKind"),
				sign: rapid.Bool().Draw(rt, "sign"), variant: rapid.IntRange(0, len(vs)-1).Draw(rt, "variant"),
				qty: rapid.Int32Range(1, 3).Draw(rt, "qty"), target: rapid.IntRange(0, 7).Draw(rt, "target"),
			}
		}), 1, 8).Draw(rt, "ops")
	}
	errs := make([]error, len(workers))
	var wg sync.WaitGroup
	for g, ops := range workers {
		wg.Go(func() { errs[g] = run(ops) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		rt.Fatalf("lỗi không mong đợi: %v", err)
	}
	for _, v := range vs {
		if msg := e.checkInvariants(v); msg != "" {
			rt.Fatalf("biến thể %v: %s", v, msg)
		}
	}
}

// runOps chạy ops tuần tự, mỗi thao tác một transaction; thiếu hàng và sai trạng thái là kết quả hợp lệ.
func (e *invEnv) runOps(vs []uuid.UUID, ops []propOp) error {
	var mine []uuid.UUID
	for _, op := range ops {
		v := vs[op.variant]
		var err error
		switch op.kind {
		case 0:
			var ids []uuid.UUID
			ids, err = e.reserve(sysActor(), e.line(v, op.qty))
			mine = append(mine, ids...)
		case 4:
			// Nhập kho qua applyDelta (export_test.go): helper invtest gọi t.Fatal, không dùng được ngoài goroutine test.
			err = e.app(func(tx pgx.Tx) error { return inventory.ImportForTest(e.t.Context(), tx, v, e.warehouse, op.qty) })
		case 5:
			err = e.postRandomDoc(v, op)
		case 6:
			err = e.releaseInTransitOf(mine, op)
		case 7, 8:
			err = e.returnOp(v, op)
		default:
			if len(mine) == 0 {
				continue
			}
			ids := []uuid.UUID{mine[op.target%len(mine)]}
			switch op.kind {
			case 1:
				err = e.release(ids, "prop")
			case 2:
				err = e.startTransit(ids, sysActor())
			default:
				err = e.consume(ids, sysActor())
			}
		}
		if err != nil && !errors.Is(err, inventory.ErrInsufficient) && !errors.Is(err, inventory.ErrStateConflict) {
			return err
		}
	}
	return nil
}

// checkInvariants trả mô tả chỗ sai, rỗng khi mọi bất biến đúng.
func (e *invEnv) checkInvariants(v uuid.UUID) string {
	e.t.Helper()
	var msg string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `
			SELECT CASE
			  WHEN b.on_hand < 0 OR b.held < 0 OR b.in_transit < 0 OR b.damaged < 0 THEN 'có cột âm'
			  WHEN b.on_hand <> l.on_hand OR b.held <> l.held OR b.in_transit <> l.in_transit OR b.damaged <> l.damaged
			    THEN format('sổ (%s, %s, %s, %s) khác số dư (%s, %s, %s, %s)', l.on_hand, l.held, l.in_transit, l.damaged,
			      b.on_hand, b.held, b.in_transit, b.damaged)
			  WHEN b.held <> r.held OR b.in_transit <> r.in_transit
			    THEN format('giữ hàng (held %s, in_transit %s) khác số dư (%s, %s)', r.held, r.in_transit, b.held, b.in_transit)
			  WHEN b.available < 0 THEN 'available âm'
			  WHEN b.damaged > b.on_hand THEN 'damaged lớn hơn on_hand'
			  ELSE '' END
			FROM inventory.stock_available_v AS b,
			LATERAL (SELECT
			  coalesce(sum(delta) FILTER (WHERE bucket = 'on_hand'), 0) AS on_hand,
			  coalesce(sum(delta) FILTER (WHERE bucket = 'held'), 0) AS held,
			  coalesce(sum(delta) FILTER (WHERE bucket = 'in_transit'), 0) AS in_transit,
			  coalesce(sum(delta) FILTER (WHERE bucket = 'damaged'), 0) AS damaged
			  FROM inventory.stock_ledger WHERE variant_id = b.variant_id AND warehouse_id = b.warehouse_id) AS l,
			LATERAL (SELECT
			  coalesce(sum(quantity) FILTER (WHERE state = 'held'), 0) AS held,
			  coalesce(sum(quantity) FILTER (WHERE state = 'in_transit'), 0) AS in_transit
			  FROM inventory.stock_reservations WHERE variant_id = b.variant_id AND warehouse_id = b.warehouse_id) AS r
			WHERE b.variant_id = $1 AND b.warehouse_id = $2`, v, e.warehouse).Scan(&msg)
	}); errors.Is(err, pgx.ErrNoRows) {
		return "" // chưa có dòng số dư: không có gì để kiểm
	} else if err != nil {
		return "đọc bất biến: " + err.Error()
	}
	return msg
}

//nolint:gochecknoglobals // bảng loại phiếu cố định của test thuộc tính.
var docKinds = []string{
	inventory.KindImport, inventory.KindExport, inventory.KindDamage, inventory.KindDamageExport, inventory.KindAdjust,
}

// postRandomDoc tạo, thêm một dòng và ghi sổ một phiếu loại op.docKind cho v trong một transaction (adjust âm khi
// op.sign); thiếu hàng là kết quả hợp lệ (runOps bỏ qua ErrInsufficient).
func (e *invEnv) postRandomDoc(v uuid.UUID, op propOp) error {
	kind, q := docKinds[op.docKind], op.qty
	if kind == inventory.KindAdjust && op.sign {
		q = -q
	}
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		a := sysActor()
		d, err := inventory.CreateDocument(ctx, tx, inventory.DocumentInput{WarehouseID: e.warehouse, Kind: kind}, a)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if d, err = inventory.ReplaceLines(ctx, tx, d.ID, d.Version, []inventory.DocumentLine{line(v, q)}); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = w.PostDocument(ctx, tx, d.ID, d.Version, a)
		return err //nolint:wrapcheck // test
	})
}

// releaseInTransitOf nhả giữ hàng in_transit của dòng đơn của một giữ hàng trong mine (Task M2-22); không có → bỏ qua.
func (e *invEnv) releaseInTransitOf(mine []uuid.UUID, op propOp) error {
	if len(mine) == 0 {
		return nil
	}
	id := mine[op.target%len(mine)]
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		var item uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT order_item_id FROM inventory.stock_reservations WHERE id = $1", id).
			Scan(&item); err != nil {
			return err //nolint:wrapcheck // test
		}
		return w.ReleaseInTransitByItems(ctx, tx, []uuid.UUID{item}, sysActor())
	})
}

// returnOp: kind 7 nhận op.qty cái khách trả (op.sign: hỏng), kind 8 chuyển op.qty cái sang hỏng; mỗi lần một mã phiếu
// mới.
func (e *invEnv) returnOp(v uuid.UUID, op propOp) error {
	doc := uuid.New()
	return e.do(func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error {
		if op.kind == 7 {
			return w.ReceiveCustomerReturn(ctx, tx, doc, []inventory.ReturnLine{
				{VariantID: v, WarehouseID: e.warehouse, Qty: op.qty, Damaged: op.sign},
			}, sysActor())
		}
		return w.MoveToDamaged(ctx, tx, doc, v, e.warehouse, op.qty, sysActor())
	})
}
