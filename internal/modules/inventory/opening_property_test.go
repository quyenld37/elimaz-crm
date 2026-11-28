package inventory_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"
)

// TestOpeningProperty (rapid, số lượt của TestMain ≥ 100; MO02): mỗi lượt một tenant mới (biến thể phải đếm tính theo
// tenant), 1–40 biến thể active, số đếm ngẫu nhiên 0–500 → sau khi đăng on_hand = số đếm với mọi biến thể,
// Σ sổ của phiên = Σ số đếm = Units, số dòng sổ = số biến thể đếm > 0, CheckOpening luôn ok.
func TestOpeningProperty(t *testing.T) {
	t.Parallel()
	base := newInvEnv(t)
	run := 0
	rapid.Check(t, func(rt *rapid.T) {
		run++
		e := newInvEnvOn(t, base.staff, fmt.Sprintf("P%d", run))
		vs := e.variants(rapid.IntRange(1, 40).Draw(rt, "variants"))
		counts := make([]int32, len(vs))
		for i := range counts {
			counts[i] = rapid.Int32Range(0, 500).Draw(rt, "counted")
		}
		e.openingRun(rt, vs, counts)
	})
}

// openingRun mở, đếm vs theo counts, đăng rồi đòi on_hand = counted, Σ sổ = Σ đếm, CheckOpening khớp.
func (e *invEnv) openingRun(rt *rapid.T, vs []uuid.UUID, counts []int32) {
	units, positive := sumCounts(counts)
	s := e.mustCreateOpening(e.warehouse)
	e.countAll(s.ID, vs, counts)
	r, err := e.postOpening(s.ID)
	if err != nil || r.CountedLines != len(vs) || r.LinesExpected != len(vs) || r.Units != units {
		rt.Fatalf("PostOpening: %+v, %v (mong %d dòng, %d cái)", r, err, len(vs), units)
	}
	got := e.onHandMap(e.warehouse)
	for i, v := range vs {
		if got[v] != counts[i] {
			rt.Fatalf("biến thể %d: on_hand %d, mong %d", i, got[v], counts[i])
		}
	}
	sum := e.count(`SELECT coalesce(sum(delta), 0)::int FROM inventory.stock_ledger
		WHERE doc_type = 'stocktake' AND doc_id = $1`, s.ID)
	if int64(sum) != units || e.stocktakeLedger(s.ID) != positive {
		rt.Fatalf("sổ: tổng %d (mong %d), %d dòng (mong %d)", sum, units, e.stocktakeLedger(s.ID), positive)
	}
	c, err := e.checkOpening(s.ID)
	if err != nil || !c.OK || len(c.Mismatches) != 0 {
		rt.Fatalf("CheckOpening: %+v, %v", c, err)
	}
}

// onHandMap trả on_hand theo biến thể của kho wh.
func (e *invEnv) onHandMap(wh uuid.UUID) map[uuid.UUID]int32 {
	e.t.Helper()
	got := map[uuid.UUID]int32{}
	if err := e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT variant_id, on_hand FROM inventory.stock_balances
			WHERE warehouse_id = $1`, wh)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var v uuid.UUID
			var n int32
			if err := rows.Scan(&v, &n); err != nil {
				return err //nolint:wrapcheck // test
			}
			got[v] = n
		}
		return rows.Err()
	}); err != nil {
		e.t.Fatal(err)
	}
	return got
}
