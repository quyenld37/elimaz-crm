package cash_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash"
)

// propPosted là một bút toán đã ghi trong mô hình: vận đơn, các vế, đã bị đảo chưa, có phải bút toán đảo không.
type propPosted struct {
	id       uuid.UUID
	shipment int
	entries  []cash.Entry
	reversed bool
	reversal bool
}

// propEntries là các vế của bút toán kind với số tiền x (05 mục 2).
func propEntries(kind string, x int64) []cash.Entry {
	switch kind {
	case cash.KindExpected:
		return []cash.Entry{{Account: cash.AccountToCollect, Amount: x}, {Account: cash.AccountSalesClearing, Amount: -x}}
	case cash.KindCollected:
		return []cash.Entry{{Account: cash.AccountToCollect, Amount: -x}, {Account: cash.AccountCarrierHolding, Amount: x}}
	case cash.KindReturned:
		return []cash.Entry{{Account: cash.AccountToCollect, Amount: -x}, {Account: cash.AccountSalesClearing, Amount: x}}
	default: // remitted
		return []cash.Entry{{Account: cash.AccountCarrierHolding, Amount: -x}, {Account: cash.AccountShopReceived, Amount: x}}
	}
}

// Quy luật sổ COD M3 (300 lượt): chuỗi 1–30 thao tác trên 1–5 vận đơn — PostWithID các loại expected, collected,
// returned, remitted (1–50.000.000 đồng), xen Reverse (kể cả đảo lại bút toán đã đảo → ErrAlreadyReversed), xen bút
// toán cố ý lệch (qua PostWithID → ErrUnbalanced; ghi thẳng SQL → 23514) → sau mỗi lần commit: tổng mọi dòng của
// tenant bằng 0, tổng mỗi bút toán bằng 0, không bút toán nào bị đảo hai lần; số dư từng vận đơn khớp mô hình.
func TestCODLedgerM3Property(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	pool := make([]uuid.UUID, 5)
	for i := range pool {
		pool[i] = e.shipment(1)
	}
	want := make([]cash.Balances, len(pool))
	rapid.Check(t, func(rt *rapid.T) {
		ships := rapid.IntRange(1, len(pool)).Draw(rt, "shipments")
		var posted []*propPosted
		for range rapid.IntRange(1, 30).Draw(rt, "ops") {
			switch op := rapid.IntRange(0, 9).Draw(rt, "op"); {
			case op <= 5:
				posted = append(posted, e.propPost(rt, ships, pool, want))
			case op <= 7 && len(posted) > 0:
				if r := e.propReverse(rt, posted, want); r != nil {
					posted = append(posted, r)
				}
			case op == 8:
				e.propUnbalancedGo(rt, pool[rapid.IntRange(0, ships-1).Draw(rt, "ship")])
			default:
				e.propUnbalancedSQL(rt, pool[rapid.IntRange(0, ships-1).Draw(rt, "ship")])
			}
			e.propCheckInvariants(rt)
		}
		for i := range ships {
			if b := e.balancesFor(pool[i]); b != want[i] {
				rt.Fatalf("vận đơn %d: số dư %+v, mong %+v", i, b, want[i])
			}
		}
	})
}

func (e *codEnv) propPost(rt *rapid.T, ships int, pool []uuid.UUID, want []cash.Balances) *propPosted {
	kind := rapid.SampledFrom([]string{cash.KindExpected, cash.KindCollected, cash.KindReturned, cash.KindRemitted}).
		Draw(rt, "kind")
	x := rapid.Int64Range(1, 50_000_000).Draw(rt, "amount")
	p := &propPosted{shipment: rapid.IntRange(0, ships-1).Draw(rt, "ship"), entries: propEntries(kind, x)}
	id, created, err := e.postID(transfer(pool[p.shipment], kind, uuid.NewString(), p.entries...))
	if err != nil || !created {
		rt.Fatalf("PostWithID %s %d: created=%v err=%v", kind, x, created, err)
	}
	p.id = id
	apply(&want[p.shipment], p.entries, 1)
	return p
}

// propReverse đảo một bút toán ngẫu nhiên: chưa đảo và không phải bút toán đảo → thành công; đã đảo →
// ErrAlreadyReversed; là bút toán đảo → ErrInvalidTransfer. Số dư mô hình chỉ đổi khi thành công; trả bút toán đảo
// mới (nil khi bị từ chối).
func (e *codEnv) propReverse(rt *rapid.T, posted []*propPosted, want []cash.Balances) *propPosted {
	p := posted[rapid.IntRange(0, len(posted)-1).Draw(rt, "target")]
	rid, err := e.reverse(p.id, "thử")
	switch {
	case p.reversal:
		if !errors.Is(err, cash.ErrInvalidTransfer) {
			rt.Fatalf("đảo bút toán đảo: mong ErrInvalidTransfer, có %v", err)
		}
	case p.reversed:
		if !errors.Is(err, cash.ErrAlreadyReversed) {
			rt.Fatalf("đảo lần hai: mong ErrAlreadyReversed, có %v", err)
		}
	default:
		if err != nil {
			rt.Fatalf("Reverse: %v", err)
		}
		p.reversed = true
		apply(&want[p.shipment], p.entries, -1)
		return &propPosted{id: rid, shipment: p.shipment, reversal: true}
	}
	return nil
}

func (e *codEnv) propUnbalancedGo(rt *rapid.T, s uuid.UUID) {
	x := rapid.Int64Range(2, 50_000_000).Draw(rt, "amount")
	delta := rapid.SampledFrom([]int64{-1, 1}).Draw(rt, "delta")
	tr := transfer(s, cash.KindExpected, uuid.NewString(),
		cash.Entry{Account: cash.AccountToCollect, Amount: x}, cash.Entry{Account: cash.AccountSalesClearing, Amount: -x + delta})
	if _, _, err := e.postID(tr); !errors.Is(err, cash.ErrUnbalanced) {
		rt.Fatalf("bút toán lệch qua PostWithID: mong ErrUnbalanced, có %v", err)
	}
}

func (e *codEnv) propUnbalancedSQL(rt *rapid.T, s uuid.UUID) {
	x := rapid.Int64Range(2, 50_000_000).Draw(rt, "amount")
	err := e.rawTransfer(s, x, -x+1)
	if pgCode(err) != "23514" || !strings.Contains(err.Error(), "unbalanced") {
		rt.Fatalf("bút toán lệch ghi thẳng: mong 23514 unbalanced, có %v", err)
	}
}

// propCheckInvariants: tổng tenant 0, mỗi bút toán tổng 0 và ≥ 2 dòng, không bút toán nào bị đảo hai lần.
func (e *codEnv) propCheckInvariants(rt *rapid.T) {
	var total, badTransfers, doubleReversed int64
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT
			(SELECT coalesce(sum(amount), 0)::bigint FROM cash.cod_ledger_entries),
			(SELECT count(*) FROM (SELECT t.id FROM cash.cod_transfers t
				LEFT JOIN cash.cod_ledger_entries l ON l.tenant_id = t.tenant_id AND l.transfer_id = t.id
				GROUP BY t.id HAVING count(l.id) < 2 OR coalesce(sum(l.amount), 0) <> 0) x),
			(SELECT count(*) FROM (SELECT reverses_transfer_id FROM cash.cod_transfers
				WHERE reverses_transfer_id IS NOT NULL GROUP BY 1 HAVING count(*) > 1) y)`).
			Scan(&total, &badTransfers, &doubleReversed)
	}); err != nil {
		rt.Fatalf("đọc quy luật: %v", err)
	}
	if total != 0 || badTransfers != 0 || doubleReversed != 0 {
		rt.Fatalf("tổng tenant %d, %d bút toán lệch, %d bút toán bị đảo hai lần", total, badTransfers, doubleReversed)
	}
}

// apply cộng các vế (nhân sign) vào số dư mô hình b.
func apply(b *cash.Balances, entries []cash.Entry, sign int64) {
	for _, en := range entries {
		v := en.Amount * sign
		switch en.Account {
		case cash.AccountToCollect:
			b.ToCollect += v
		case cash.AccountCarrierHolding:
			b.CarrierHolding += v
		case cash.AccountShopReceived:
			b.ShopReceived += v
		case cash.AccountCarrierFee:
			b.CarrierFee += v
		case cash.AccountSalesClearing:
			b.SalesClearing += v
		}
	}
}
