package adjust_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash/internal/adjust"
	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
)

var accounts = []string{ //nolint:gochecknoglobals // hằng của test
	postings.AccountToCollect, postings.AccountCarrierHolding, postings.AccountShopReceived, postings.AccountCarrierFee,
	postings.AccountSalesClearing,
}

func sum(b postings.Balances) int64 {
	return b.ToCollect + b.CarrierHolding + b.ShopReceived + b.CarrierFee + b.SalesClearing
}

// addTo cộng vế e vào số dư b (qua bảng con trỏ theo ngăn).
func addTo(b *postings.Balances, e postings.Entry) {
	*map[string]*int64{
		postings.AccountToCollect: &b.ToCollect, postings.AccountCarrierHolding: &b.CarrierHolding,
		postings.AccountShopReceived: &b.ShopReceived, postings.AccountCarrierFee: &b.CarrierFee,
		postings.AccountSalesClearing: &b.SalesClearing,
	}[e.Account] += e.Amount
}

// drawState sinh sổ của một vận đơn: 1–4 bút toán hai vế cân.
func drawState(t *rapid.T) adjust.State {
	st := adjust.State{ShipmentID: uuid.New(), Status: rapid.SampledFrom([]string{"delivered", "shipping", "returned"}).Draw(t, "status")}
	for range rapid.IntRange(1, 4).Draw(t, "transfers") {
		x := rapid.Int64Range(1, 10_000_000).Draw(t, "x")
		a := rapid.Permutation(accounts).Draw(t, "acc")
		es := []postings.Entry{{Account: a[0], Amount: x}, {Account: a[1], Amount: -x}}
		kind := rapid.SampledFrom([]string{"collected", "remitted", "adjustment"}).Draw(t, "kind")
		st.Transfers = append(st.Transfers, adjust.Transfer{ID: uuid.New(), Kind: kind, Entries: es})
		for _, e := range es {
			addTo(&st.Balances, e)
		}
	}
	return st
}

// drawRequest sinh yêu cầu đảo một bút toán của st, hay chuyển một số tiền giữa hai ngăn khác nhau.
func drawRequest(t *rapid.T, st adjust.State) adjust.Request {
	if rapid.Bool().Draw(t, "reverse") {
		target := rapid.SampledFrom(st.Transfers).Draw(t, "target").ID
		return adjust.Request{Mode: adjust.ModeReverse, ReversesTransferID: &target, Reason: "sai"}
	}
	a := rapid.Permutation(accounts).Draw(t, "move")
	return adjust.Request{Mode: adjust.ModeMove, From: a[0], To: a[1], Amount: rapid.Int64Range(1, 1<<53-1).Draw(t, "amount"), Reason: "sai"}
}

// checkReversed: vế đảo đúng dấu từng vế của bút toán gốc.
func checkReversed(t *rapid.T, st adjust.State, r adjust.Request, p adjust.Plan) {
	for _, tr := range st.Transfers {
		if tr.ID != *r.ReversesTransferID {
			continue
		}
		for i, e := range tr.Entries {
			if p.Entries[i].Account != e.Account || p.Entries[i].Amount != -e.Amount {
				t.Fatalf("vế đảo %d: %+v, gốc %+v", i, p.Entries[i], e)
			}
		}
	}
}

// Quy luật: bản xem trước luôn có bút toán cân (tổng vế 0, ≥ 2 vế), tổng số dư sau = tổng trước; đảo một bút toán thì
// vế đảo đúng dấu từng vế gốc; cùng sổ, cùng yêu cầu → cùng token; thêm một bút toán vào sổ → token khác.
func TestPreview_Properties(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		st := drawState(t)
		r := drawRequest(t, st)
		p, err := adjust.Preview(st, r)
		if err != nil {
			t.Fatalf("xem trước: %v", err)
		}
		var total int64
		for _, e := range p.Entries {
			total += e.Amount
		}
		if len(p.Entries) < 2 || total != 0 || sum(p.After) != sum(p.Before) || p.Before != st.Balances {
			t.Fatalf("bản xem trước lệch: %+v", p)
		}
		if r.Mode == adjust.ModeReverse {
			checkReversed(t, st, r, p)
		}
		again, _ := adjust.Preview(st, r)
		st.Transfers = append(st.Transfers, adjust.Transfer{ID: uuid.New(), Kind: "fee"})
		changed, err := adjust.Preview(st, r)
		if again.Token != p.Token || err != nil || changed.Token == p.Token ||
			!errors.Is(p.Check(changed.Token), adjust.ErrStale) || p.Check(p.Token) != nil {
			t.Fatalf("token: %s %s %s %v", p.Token, again.Token, changed.Token, err)
		}
	})
}
