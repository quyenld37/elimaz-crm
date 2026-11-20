package postings_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash/internal/postings"
)

// shipID là mã vận đơn của mọi ca (chuỗi hằng: khóa bút toán mong đợi ghép thẳng).
const shipID = "01900000-0000-7000-8000-000000000001"

func ship() uuid.UUID { return uuid.MustParse(shipID) }

// apply cộng các bút toán của acts vào b (đảo: trừ lại các vế của bút toán bị đảo trong ts) — mô phỏng bên ghi.
func apply(b postings.Balances, ts []postings.Transfer, acts []postings.Action) postings.Balances {
	add := func(es []postings.Entry, sign int64) {
		for _, e := range es {
			switch e.Account {
			case postings.AccountToCollect:
				b.ToCollect += sign * e.Amount
			case postings.AccountCarrierHolding:
				b.CarrierHolding += sign * e.Amount
			case postings.AccountShopReceived:
				b.ShopReceived += sign * e.Amount
			case postings.AccountCarrierFee:
				b.CarrierFee += sign * e.Amount
			case postings.AccountSalesClearing:
				b.SalesClearing += sign * e.Amount
			}
		}
	}
	for _, a := range acts {
		if a.Post != nil {
			add(a.Post.Entries, 1)
		}
		for _, t := range ts {
			if a.Reverse != uuid.Nil && t.ID == a.Reverse {
				add(t.Entries, -1)
			}
		}
	}
	return b
}

func kinds(acts []postings.Action) []string {
	out := []string{}
	for _, a := range acts {
		switch {
		case a.Post != nil:
			out = append(out, a.Post.Kind+"@"+a.Post.Key)
		case a.Reverse != uuid.Nil:
			out = append(out, "reverse")
		case a.Finding != "":
			out = append(out, "finding:"+a.Finding)
		}
	}
	return out
}

// balanced báo lỗi khi bút toán của a có dòng 0 đồng hay tổng khác 0.
func balanced(a postings.Action) error {
	if a.Post == nil {
		return nil
	}
	var sum int64
	for _, e := range a.Post.Entries {
		if e.Amount == 0 {
			return fmt.Errorf("%s có dòng 0 đồng", a.Post.Kind)
		}
		sum += e.Amount
	}
	if sum != 0 {
		return fmt.Errorf("%s lệch %d", a.Post.Kind, sum)
	}
	return nil
}

func i64(v int64) *int64 { return new(v) }

// ruleCase là một ca của luật: đầu vào (vận đơn gán sau), việc mong đợi theo thứ tự, số dư sau khi ghi.
type ruleCase struct {
	name  string
	in    postings.Input
	kinds []string
	want  postings.Balances
}

// ruleCases là các ca của bảng luật (V1 COD 630.000).
// fixtures là bút toán expected, collected của V1 (COD 630.000) và số dư sau khi giao xong.
func fixtures() (expected, collected postings.Transfer, delivered postings.Balances) {
	const x = 630000
	expected = postings.Transfer{ID: uuid.New(), Kind: postings.KindExpected, Entries: []postings.Entry{
		{Account: postings.AccountToCollect, Amount: x}, {Account: postings.AccountSalesClearing, Amount: -x},
	}}
	collected = postings.Transfer{ID: uuid.New(), Kind: postings.KindCollected, Entries: []postings.Entry{
		{Account: postings.AccountToCollect, Amount: -x}, {Account: postings.AccountCarrierHolding, Amount: x},
	}}
	return expected, collected, postings.Balances{CarrierHolding: x, SalesClearing: -x}
}

// ruleCases là các ca của bảng luật (V1 COD 630.000): lấy hàng, giao xong; rồi hoàn, đảo, hủy.
func ruleCases() []ruleCase {
	const x = 630000
	expected, _, delivered := fixtures()
	return append([]ruleCase{
		{
			name:  "lấy hàng",
			in:    postings.Input{Seq: 3, Status: "shipping", Effects: []string{postings.EffPickedUp}, COD: x},
			kinds: []string{"expected@expected:" + shipID},
			want:  postings.Balances{ToCollect: x, SalesClearing: -x},
		},
		{
			name: "giao xong sau lấy hàng",
			in: postings.Input{
				Seq: 4, Status: "delivered", Effects: []string{postings.EffDelivered, "close_callbacks"},
				COD: x, Balances: postings.Balances{ToCollect: x, SalesClearing: -x},
				Transfers: []postings.Transfer{expected},
			},
			kinds: []string{"collected@collected:" + shipID + ":4"},
			want:  delivered,
		},
		{
			name: "giao xong thẳng từ chờ lấy",
			in: postings.Input{
				Seq: 3, Status: "delivered", Effects: []string{postings.EffPickedUp, postings.EffDelivered},
				COD: x,
			},
			kinds: []string{"expected@expected:" + shipID, "collected@collected:" + shipID + ":3"},
			want:  delivered,
		},
		{
			name:  "sự kiện M1 không effects: delivered suy ra lấy hàng + giao",
			in:    postings.Input{Seq: 2, Status: "delivered", COD: x},
			kinds: []string{"expected@expected:" + shipID, "collected@collected:" + shipID + ":2"},
			want:  delivered,
		},
		{
			name:  "sự kiện M1 không effects: trạng thái khác không ghi",
			in:    postings.Input{Seq: 2, Status: "awaiting_pickup", COD: x},
			kinds: []string{},
		},
		{
			name: "đã có expected chưa đảo (M1 khóa evt:) → không ghi lặp",
			in: postings.Input{
				Seq: 5, Status: "shipping", Effects: []string{postings.EffPickedUp}, COD: x,
				Balances: postings.Balances{ToCollect: x, SalesClearing: -x}, Transfers: []postings.Transfer{expected},
			},
			kinds: []string{},
			want:  postings.Balances{ToCollect: x, SalesClearing: -x},
		},
		{
			name: "giao một phần 430.000",
			in: postings.Input{
				Seq: 4, Status: "delivered", Effects: []string{postings.EffDelivered}, COD: x,
				CODCollected: i64(430000), Balances: postings.Balances{ToCollect: x, SalesClearing: -x},
				Transfers: []postings.Transfer{expected},
			},
			kinds: []string{"collected@collected:" + shipID + ":4", "returned@returned:" + shipID + ":4"},
			want:  postings.Balances{CarrierHolding: 430000, SalesClearing: -430000},
		},
		{
			name: "hãng báo thu nhiều hơn COD",
			in: postings.Input{
				Seq: 4, Status: "delivered", Effects: []string{postings.EffDelivered}, COD: x,
				CODCollected: i64(700000), Balances: postings.Balances{ToCollect: x, SalesClearing: -x},
				Transfers: []postings.Transfer{expected},
			},
			kinds: []string{"collected@collected:" + shipID + ":4"},
			want:  postings.Balances{CarrierHolding: 700000, SalesClearing: -700000},
		},
		{
			name: "giao xong mà hãng báo thu 0",
			in: postings.Input{
				Seq: 4, Status: "delivered", Effects: []string{postings.EffDelivered}, COD: x,
				CODCollected: i64(0), Balances: postings.Balances{ToCollect: x, SalesClearing: -x},
				Transfers: []postings.Transfer{expected},
			},
			kinds: []string{"returned@returned:" + shipID + ":4"},
		},
	}, returnCases()...)
}

// returnCases là các ca hoàn, đảo "Thành công", hủy, COD 0.
func returnCases() []ruleCase {
	const x = 630000
	expected, collected, delivered := fixtures()
	return []ruleCase{
		{
			name: "hoàn trước khi giao",
			in: postings.Input{
				Seq: 5, Status: "returning", Effects: []string{postings.EffReturning, "close_callbacks"},
				COD: x, Balances: postings.Balances{ToCollect: x, SalesClearing: -x},
				Transfers: []postings.Transfer{expected},
			},
			kinds: []string{"returned@returned:" + shipID + ":5"},
		},
		{
			name: "đảo khi chưa có tiền về",
			in: postings.Input{
				Seq: 6, Status: "returning",
				Effects: []string{postings.EffDeliveryReversed, postings.EffReturning}, COD: x, Balances: delivered,
				Transfers: []postings.Transfer{expected, collected},
			},
			kinds: []string{"reverse", "returned@returned:" + shipID + ":6"},
		},
		{
			name: "đảo khi đã có tiền về → chỉ phát hiện",
			in: postings.Input{
				Seq: 6, Status: "returning",
				Effects: []string{postings.EffDeliveryReversed, postings.EffReturning}, COD: x,
				Balances:  postings.Balances{ShopReceived: 598000, CarrierFee: 32000, SalesClearing: -x},
				Transfers: []postings.Transfer{expected, collected, {ID: uuid.New(), Kind: postings.KindRemitted}},
			},
			kinds: []string{"finding:" + postings.FindingRemittedThenReturned},
			want:  postings.Balances{ShopReceived: 598000, CarrierFee: 32000, SalesClearing: -x},
		},
		{
			name: "đảo lần hai (collected đã bị đảo) → chỉ hoàn phần còn lại",
			in: postings.Input{
				Seq: 6, Status: "returning",
				Effects: []string{postings.EffDeliveryReversed, postings.EffReturning}, COD: x,
				Transfers: []postings.Transfer{expected, func() postings.Transfer {
					c := collected
					c.Reversed = true
					return c
				}()},
			},
			kinds: []string{},
		},
		{
			name: "hủy sau khi đã expected → phát hiện, không ghi",
			in: postings.Input{
				Seq: 4, Status: "cancelled", Effects: []string{postings.EffCancelConfirmed}, COD: x,
				Balances: postings.Balances{ToCollect: x, SalesClearing: -x}, Transfers: []postings.Transfer{expected},
			},
			kinds: []string{"finding:" + postings.FindingUnexpectedCancelAfterPickup},
			want:  postings.Balances{ToCollect: x, SalesClearing: -x},
		},
		{
			name:  "hủy trước lấy hàng → không gì",
			in:    postings.Input{Seq: 3, Status: "cancelled", Effects: []string{postings.EffCancelConfirmed}, COD: x},
			kinds: []string{},
		},
		{
			name: "COD 0: lấy hàng, giao xong không ghi",
			in: postings.Input{
				Seq: 3, Status: "delivered", Effects: []string{postings.EffPickedUp, postings.EffDelivered},
				CODCollected: i64(0),
			},
			kinds: []string{},
		},
	}
}

func TestRules_Table(t *testing.T) {
	t.Parallel()
	for _, tc := range ruleCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.in.ShipmentID = ship()
			acts, err := postings.Rules(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := kinds(acts); !reflect.DeepEqual(got, tc.kinds) {
				t.Errorf("hành động %v, muốn %v", got, tc.kinds)
			}
			if got := apply(tc.in.Balances, tc.in.Transfers, acts); got != tc.want {
				t.Errorf("số dư sau %+v, muốn %+v", got, tc.want)
			}
			for _, a := range acts {
				if err := balanced(a); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestRules_InvalidInput(t *testing.T) {
	t.Parallel()
	for _, in := range []postings.Input{
		{
			ShipmentID: ship(), Seq: 4, Status: "delivered", Effects: []string{postings.EffDelivered}, COD: 1000,
			CODCollected: i64(-1),
		},
		{ShipmentID: ship(), Seq: 4, Status: "shipping", Effects: []string{postings.EffPickedUp}, COD: -1},
		{ShipmentID: uuid.Nil, Seq: 4, Status: "shipping", COD: 1000},
	} {
		if _, err := postings.Rules(in); !errors.Is(err, postings.ErrInvalidInput) {
			t.Errorf("%+v: mong ErrInvalidInput, có %v", in, err)
		}
	}
}

// sim là sổ mô phỏng của bên ghi: bút toán đã ghi (khóa trùng thì bỏ qua như PostWithID), số dư.
type sim struct {
	ts   []postings.Transfer
	b    postings.Balances
	keys map[string]bool
}

// write ghi các việc acts như consumer; trả lỗi khi bút toán lệch hay đảo một bút toán đã đảo.
func (s *sim) write(acts []postings.Action) error {
	var fresh []postings.Action
	for _, a := range acts {
		if err := balanced(a); err != nil {
			return err
		}
		if a.Post != nil {
			if s.keys[a.Post.Key] {
				continue
			}
			s.keys[a.Post.Key] = true
			s.ts = append(s.ts, postings.Transfer{ID: uuid.New(), Kind: a.Post.Kind, Key: a.Post.Key, Entries: a.Post.Entries})
		}
		if err := s.markReversed(a.Reverse); err != nil {
			return err
		}
		fresh = append(fresh, a)
	}
	s.b = apply(s.b, s.ts, fresh)
	return nil
}

func (s *sim) markReversed(id uuid.UUID) error {
	if id == uuid.Nil {
		return nil
	}
	for i := range s.ts {
		if s.ts[i].ID == id {
			if s.ts[i].Reversed {
				return errors.New("đảo bút toán đã đảo")
			}
			s.ts[i].Reversed = true
		}
	}
	return nil
}

// fresh báo acts có bút toán khóa mới hay lần đảo nào.
func (s *sim) fresh(acts []postings.Action) bool {
	for _, a := range acts {
		if (a.Post != nil && !s.keys[a.Post.Key]) || a.Reverse != uuid.Nil {
			return true
		}
	}
	return false
}

// realEdges là các bộ tác động có thật của máy trạng thái (fulfillment/internal/shipstate/graph.go), theo thứ tự.
func realEdges() [][]string {
	return [][]string{
		{postings.EffPickedUp},
		{postings.EffPickedUp, postings.EffDelivered},
		{postings.EffPickedUp, "delivery_failed"},
		{postings.EffPickedUp, postings.EffReturning},
		{postings.EffPickedUp, postings.EffReturning, "carrier_returned"},
		{postings.EffCancelConfirmed},
		{postings.EffPickedUp, "cancel_rejected"},
		{postings.EffDelivered, "close_callbacks"},
		{"delivery_failed"},
		{postings.EffReturning, "close_callbacks"},
		{postings.EffDeliveryReversed, postings.EffReturning},
		{postings.EffDeliveryReversed, postings.EffReturning, "carrier_returned"},
		nil,
	}
}

// Quy luật thuần: chuỗi bước với bộ tác động có thật → mọi bút toán cân hai vế, không dòng 0, không đảo hai lần; áp
// lại đúng bước đó trên sổ sau khi ghi không sinh bút toán khóa mới hay lần đảo nào; tổng các ngăn luôn 0.
func TestRules_PropertyBalancedAndStable(t *testing.T) {
	t.Parallel()
	edges := realEdges()
	rapid.Check(t, func(rt *rapid.T) {
		cod := rapid.Int64Range(0, 1<<40).Draw(rt, "cod")
		s := &sim{keys: map[string]bool{}}
		steps := int64(rapid.IntRange(1, 8).Draw(rt, "số bước"))
		for seq := int64(2); seq < 2+steps; seq++ {
			in := postings.Input{
				ShipmentID: ship(), Seq: seq, Status: "x", COD: cod,
				Effects: edges[rapid.IntRange(0, len(edges)-1).Draw(rt, "tác động")],
			}
			if rapid.Bool().Draw(rt, "có codCollected") {
				in.CODCollected = i64(rapid.Int64Range(0, cod+1000).Draw(rt, "codCollected"))
			}
			in.Balances, in.Transfers = s.b, s.ts
			acts, err := postings.Rules(in)
			if err != nil {
				rt.Fatal(err)
			}
			if err := s.write(acts); err != nil {
				rt.Fatalf("seq %d: %v", seq, err)
			}
			in.Balances, in.Transfers = s.b, s.ts
			again, err := postings.Rules(in)
			if err != nil {
				rt.Fatal(err)
			}
			if s.fresh(again) {
				rt.Fatalf("áp lại seq %d sinh việc mới: %+v", seq, again)
			}
		}
		if b := s.b; b.ToCollect+b.CarrierHolding+b.ShopReceived+b.CarrierFee+b.SalesClearing != 0 {
			rt.Fatalf("tổng các ngăn khác 0: %+v", b)
		}
	})
}
