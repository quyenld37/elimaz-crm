package reconcile_test

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
	"elimaz.com.vn/crm/internal/modules/cash/internal/reconcile"
)

// ledger là sổ trong bộ nhớ của một vận đơn: áp việc của postings.Rules như consumer cash.
type ledger struct {
	t   *rapid.T
	id  uuid.UUID
	cod int64
	col *int64
	seq int64
	b   postings.Balances
	trs []postings.Transfer
}

func (l *ledger) add(es []postings.Entry) {
	for _, e := range es {
		switch e.Account {
		case postings.AccountToCollect:
			l.b.ToCollect += e.Amount
		case postings.AccountCarrierHolding:
			l.b.CarrierHolding += e.Amount
		case postings.AccountShopReceived:
			l.b.ShopReceived += e.Amount
		case postings.AccountCarrierFee:
			l.b.CarrierFee += e.Amount
		case postings.AccountSalesClearing:
			l.b.SalesClearing += e.Amount
		}
	}
}

// step áp một sự kiện có các tác động effects.
func (l *ledger) step(effects ...string) {
	l.seq++
	acts, err := postings.Rules(postings.Input{
		ShipmentID: l.id, Seq: l.seq, Status: "x", Effects: effects, COD: l.cod, CODCollected: l.col,
		Balances: l.b, Transfers: l.trs,
	})
	if err != nil {
		l.t.Fatal(err)
	}
	for _, a := range acts {
		switch {
		case a.Post != nil:
			l.add(a.Post.Entries)
			l.trs = append(l.trs, postings.Transfer{ID: uuid.New(), Kind: a.Post.Kind, Key: a.Post.Key, Entries: a.Post.Entries})
		case a.Reverse != uuid.Nil:
			for i := range l.trs {
				if l.trs[i].ID == a.Reverse {
					l.trs[i].Reversed = true
					neg := make([]postings.Entry, len(l.trs[i].Entries))
					for j, e := range l.trs[i].Entries {
						neg[j] = postings.Entry{Account: e.Account, Amount: -e.Amount}
					}
					l.add(neg)
					l.trs = append(l.trs, postings.Transfer{ID: uuid.New(), Kind: "reversal", Entries: neg})
				}
			}
		}
	}
}

// Quy luật: với mọi COD, số đã thu và chuỗi sự kiện tới một trạng thái, số dư mà luật ghi sổ (postings.Rules) sinh ra
// khớp Target của trạng thái đó (bảng đích và luật ghi sổ không lệch nhau).
func TestTarget_MatchesPostingRules(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		cod := rapid.Int64Range(0, 50_000_000).Draw(t, "cod")
		collected := rapid.Int64Range(0, 2*cod+1).Draw(t, "collected")
		l := &ledger{t: t, id: uuid.New(), cod: cod}
		if rapid.Bool().Draw(t, "carrierReportsCollected") {
			l.col = &collected
		} else {
			collected = cod
		}
		var status string
		partial := false
		switch path := rapid.IntRange(0, 9).Draw(t, "path"); path {
		case 0, 1, 2, 3:
			status = []string{"creating", "awaiting_pickup", "cancel_requested", "cancelled"}[path]
		case 4, 5:
			status = []string{"shipping", "delivery_failed"}[path-4]
			l.step(postings.EffPickedUp)
		case 6:
			status = "delivered"
			l.step(postings.EffPickedUp, postings.EffDelivered)
		case 7:
			status = rapid.SampledFrom([]string{"returning", "returned"}).Draw(t, "st")
			l.step(postings.EffPickedUp)
			l.step(postings.EffReturning)
		case 8:
			status = rapid.SampledFrom([]string{"returning", "returned"}).Draw(t, "st")
			l.step(postings.EffPickedUp, postings.EffDelivered)
			l.step(postings.EffDeliveryReversed, postings.EffReturning)
		case 9:
			status, partial = "returned", true
			l.step(postings.EffPickedUp, postings.EffDelivered)
			l.step(postings.EffReturning)
		}
		if x := reconcile.Target(status, partial, cod, collected); !x.Match(l.b) {
			t.Fatalf("%s partial=%v cod %d thu %d: số dư %+v không khớp đích %+v", status, partial, cod, collected, l.b, x)
		}
	})
}

// Bảng đích theo trạng thái (kế hoạch Task M3-33), các ca biên: ngăn phí-only bù trừ nhau trong tổng; ngăn sau giao
// phải 0 từng ngăn trước khi giao; trạng thái lạ không khớp gì.
func TestTarget_Table(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		status  string
		partial bool
		b       postings.Balances
		want    bool
	}{
		{"đang lấy: mọi ngăn 0", "creating", false, postings.Balances{}, true},
		{"chờ lấy: còn sổ", "awaiting_pickup", false, postings.Balances{ToCollect: 1000, SalesClearing: -1000}, false},
		{"hủy: bù trừ vẫn sai", "cancelled", false, postings.Balances{CarrierHolding: 5, CarrierFee: -5}, false},
		{"đang giao", "shipping", false, postings.Balances{ToCollect: 1000, SalesClearing: -1000}, true},
		{
			"giao lỗi: tiền ở hãng", "delivery_failed", false,
			postings.Balances{ToCollect: 1000, CarrierHolding: 1, SalesClearing: -1001},
			false,
		},
		{
			"đã giao, đã về shop, phí", "delivered", false,
			postings.Balances{ShopReceived: 970, CarrierFee: 30, SalesClearing: -1000},
			true,
		},
		{
			"đã giao, phí-only bù trừ", "delivered", false,
			postings.Balances{CarrierHolding: -30, ShopReceived: 1000, CarrierFee: 30, SalesClearing: -1000},
			true,
		},
		{"đã giao: còn đi thu", "delivered", false, postings.Balances{ToCollect: 1, CarrierHolding: 999, SalesClearing: -1000}, false},
		{"hoàn: tiền đã về", "returned", false, postings.Balances{ShopReceived: 1000, SalesClearing: -1000}, false},
		{"hoàn một phần", "returned", true, postings.Balances{CarrierHolding: 400, SalesClearing: -400}, true},
		{"trạng thái lạ", "lost", false, postings.Balances{}, false},
	} {
		got := reconcile.Target(c.status, c.partial, 1000, 1000).Match(c.b)
		if c.partial {
			got = reconcile.Target(c.status, c.partial, 1000, 400).Match(c.b)
		}
		if got != c.want {
			t.Errorf("%s: khớp = %v, mong %v", c.name, got, c.want)
		}
	}
}
