package statement_test

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/testkit"
)

// statementParallel là số test chạy song song mặc định của gói (Task 475, như order, fulfillment, inventory, httpapi).
const statementParallel = 6

// TestMain đặt số lượt rapid 200 (kế hoạch M3-32: quy luật khớp và chốt bảng kê 200 lượt) và -parallel thành
// min(statementParallel, GOMAXPROCS), trừ khi người chạy đã đặt.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(200)
	testkit.SetParallel(statementParallel)
	os.Exit(m.Run())
}

// Quy luật (rapid): tập vận đơn (giao xong hay đang giao, COD tùy ý) và bảng kê ngẫu nhiên (thiếu, thừa, đúng, phí,
// trùng, ngoài hệ thống), mỗi bảng kê chốt song song bởi 1–3 người cùng version, ghi tiền bởi nhiều worker song song →
// mỗi bảng kê đúng một lần chốt thành công; tổng mọi ngăn của sổ bằng 0; mỗi dòng bảng kê có tối đa một bút toán và
// chỉ dòng có vận đơn được ghi; mỗi vận đơn shop_received + carrier_fee ≤ COD đã thu, trừ khi có dòng ghi tiền là
// amount_mismatch dương — dòng đó có trong "Lệch đối soát" khi vận đơn còn lệch.
func TestProperty_MatchConfirm(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	run := 0
	rapid.Check(t, func(rt *rapid.T) {
		run++
		ships := e.drawShips(rt, run)
		stmts := e.drawStatements(rt, run, ships)
		for _, id := range stmts {
			e.confirmByMany(rt, id, rapid.IntRange(1, 3).Draw(rt, "people"))
		}
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(e.drainQuiet)
		}
		wg.Wait()
		e.drain()
		e.checkLedger(rt)
		e.checkPerShipment(rt, ships, stmts)
	})
}

// propShip là một vận đơn của một lượt: mã vận đơn, COD hãng đã thu (0 khi đang giao).
type propShip struct {
	id        uuid.UUID
	tracking  string
	collected int64
}

func (e *flowEnv) drawShips(rt *rapid.T, run int) []propShip {
	ships := make([]propShip, rapid.IntRange(1, 3).Draw(rt, "ships"))
	for i := range ships {
		cod := int64(rapid.IntRange(0, 5).Draw(rt, "cod")) * 10000
		st, collected := fulfillment.StatusShipping, int64(0)
		if rapid.Bool().Draw(rt, "delivered") {
			st, collected = fulfillment.StatusDelivered, cod
		}
		tr := fmt.Sprintf("P%d-%d", run, i)
		ships[i] = propShip{id: e.shipment(ship{tracking: tr, cod: cod, status: st}), tracking: tr, collected: collected}
	}
	return ships
}

// drawStatements ghi 1–3 bảng kê đã khớp, mỗi bảng kê 1–4 dòng: ngoài hệ thống, hay của một vận đơn với COD đúng,
// thiếu, thừa (bội 5.000), phí âm, 0, dương, phí khác 0 hay 500.
func (e *flowEnv) drawStatements(rt *rapid.T, run int, ships []propShip) []uuid.UUID {
	stmts := make([]uuid.UUID, rapid.IntRange(1, 3).Draw(rt, "statements"))
	for k := range stmts {
		ls := make([]line, rapid.IntRange(1, 4).Draw(rt, "lines"))
		for j := range ls {
			fee := int64(rapid.IntRange(-1, 3).Draw(rt, "fee")) * 1000
			if rapid.IntRange(0, 5).Draw(rt, "external") == 0 {
				ls[j] = line{fmt.Sprintf("NHANH-%d-%d-%d", run, k, j), 10000, fee, 0}
				continue
			}
			s := ships[rapid.IntRange(0, len(ships)-1).Draw(rt, "ship")]
			cod := max(s.collected+int64(rapid.IntRange(-2, 2).Draw(rt, "delta"))*5000, 0)
			ls[j] = line{s.tracking, cod, fee, int64(rapid.IntRange(0, 1).Draw(rt, "other")) * 500}
		}
		stmts[k] = e.statement(uuid.Nil, nil, ls...)
	}
	return stmts
}

// confirmByMany cho people người cùng chốt bảng kê id với cùng version: đúng một người thành công.
func (e *flowEnv) confirmByMany(rt *rapid.T, id uuid.UUID, people int) {
	v := e.get(id).Statement.Version
	errs := make([]error, people)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			errs[i] = e.app(func(tx pgx.Tx) error {
				_, err := e.mod.Service.ConfirmStatement(e.t.Context(), tx, e.tenant, id, v, e.user)
				return err //nolint:wrapcheck // test
			})
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, cash.ErrStatementChanged):
			rt.Fatalf("chốt: %v", err)
		}
	}
	if ok != 1 {
		rt.Fatalf("bảng kê %s: %d lần chốt thành công, mong 1", id, ok)
	}
}

// checkLedger: tổng mọi ngăn 0; mỗi dòng bảng kê tối đa một bút toán; dòng ngoài hệ thống, trùng không ghi tiền.
func (e *flowEnv) checkLedger(rt *rapid.T) {
	if s := e.count("SELECT coalesce(sum(amount), 0)::int FROM cash.cod_ledger_entries"); s != 0 {
		rt.Fatalf("tổng mọi ngăn %d, mong 0", s)
	}
	if n := e.count(`SELECT count(*) FROM (SELECT statement_line_id FROM cash.cod_transfers
		WHERE statement_line_id IS NOT NULL GROUP BY statement_line_id HAVING count(*) > 1) x`); n != 0 {
		rt.Fatalf("%d dòng bảng kê có hơn một bút toán", n)
	}
	if n := e.count(`SELECT count(*) FROM cash.cod_statement_lines
		WHERE posted_transfer_id IS NOT NULL AND (shipment_id IS NULL OR match_status IN ('external', 'duplicate'))`); n != 0 {
		rt.Fatalf("%d dòng ngoài hệ thống hay trùng đã ghi tiền", n)
	}
}

// checkPerShipment: shop_received + carrier_fee ≤ COD đã thu trừ khi có dòng ghi tiền amount_mismatch dương; dòng đó có
// trong "Lệch đối soát" khi vận đơn còn lệch.
func (e *flowEnv) checkPerShipment(rt *rapid.T, ships []propShip, stmts []uuid.UUID) {
	listed := map[uuid.UUID]bool{}
	for _, d := range e.discrepanciesBy(200) {
		listed[d.ID] = true
	}
	over := map[uuid.UUID][]uuid.UUID{}
	for _, id := range stmts {
		for _, l := range e.get(id).Lines {
			if l.ShipmentID != nil && l.PostedTransferID != nil && l.MatchStatus == statement.MatchAmountMismatch &&
				l.DiffAmount != nil && *l.DiffAmount > 0 {
				over[*l.ShipmentID] = append(over[*l.ShipmentID], l.ID)
			}
		}
	}
	for _, s := range ships {
		b := e.bal(s.id)
		if len(over[s.id]) == 0 && b[2]+b[3] > s.collected {
			rt.Fatalf("vận đơn %s: shop_received + carrier_fee = %d > đã thu %d, không dòng thừa nào", s.tracking,
				b[2]+b[3], s.collected)
		}
		for _, l := range over[s.id] {
			if b[1] != 0 && !listed[l] {
				rt.Fatalf("dòng thừa %s của vận đơn %s (còn lệch %d) không có trong lệch đối soát", l, s.tracking, b[1])
			}
		}
	}
}
