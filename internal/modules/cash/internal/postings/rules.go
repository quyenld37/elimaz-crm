// Package postings là luật ghi sổ COD theo sự kiện vận đơn (kế hoạch M3, Task M3-29): từ tác động của một bước vận
// đơn (payload shipment.status_changed), số COD và trạng thái sổ hiện tại của vận đơn, chọn các bút toán cần ghi,
// bút toán cần đảo và phát hiện cần mở. Hàm thuần (không database): consumer cash đọc sổ, gọi Rules rồi ghi bằng
// cash.PostWithID, cash.Reverse trong cùng transaction.
package postings

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Ngăn tiền của sổ COD (05 mục 2; cash.Account* là bí danh của các hằng này).
const (
	AccountToCollect      = "to_collect"
	AccountCarrierHolding = "carrier_holding"
	AccountShopReceived   = "shop_received"
	AccountCarrierFee     = "carrier_fee"
	AccountSalesClearing  = "sales_clearing"
)

// Loại bút toán luật này ghi hay đọc (cash.Kind* là bí danh).
const (
	KindExpected  = "expected"
	KindCollected = "collected"
	KindRemitted  = "remitted"
	KindReturned  = "returned"
)

// Tác động của bước vận đơn trong payload (cùng chuỗi với shipstate.Effect của fulfillment; docs/views.md). Tác động
// khác (delivery_failed, carrier_returned, cancel_rejected, close_callbacks) không đụng tiền.
const (
	EffPickedUp         = "picked_up"
	EffDelivered        = "delivered"
	EffReturning        = "returning"
	EffCancelConfirmed  = "cancel_confirmed"
	EffDeliveryReversed = "delivery_reversed"
)

// Loại phát hiện luật này mở (cash.reconcile_findings.kind).
const (
	// FindingRemittedThenReturned: hãng sửa "đã giao" thành "hoàn" khi tiền đã về shop — không tự ghi tiền (Q-1).
	FindingRemittedThenReturned = "remitted_then_returned"
	// FindingUnexpectedCancelAfterPickup: hãng xác nhận hủy khi sổ đã ghi lấy hàng (không xảy ra theo máy trạng thái).
	FindingUnexpectedCancelAfterPickup = "unexpected_cancel_after_pickup"
)

// ErrInvalidInput báo đầu vào không dùng được (vận đơn rỗng, COD âm, tiền hãng báo đã thu âm): không đoán tiền.
var ErrInvalidInput = errors.New("postings: đầu vào không hợp lệ")

// Balances là số dư từng ngăn của vận đơn (đồng, có dấu).
type Balances struct {
	ToCollect, CarrierHolding, ShopReceived, CarrierFee, SalesClearing int64
}

// Entry là một vế bút toán.
type Entry struct {
	Account string
	Amount  int64
}

// Transfer là một bút toán đã ghi của vận đơn: Reversed = đã có bút toán đảo trỏ tới nó; Entries là các vế (cần cho
// bút toán sẽ bị đảo).
type Transfer struct {
	ID       uuid.UUID
	Kind     string
	Key      string
	Reversed bool
	Entries  []Entry
}

// Input là một sự kiện shipment.status_changed cùng trạng thái sổ của vận đơn: Seq = shipments.version của bước;
// Effects rỗng (sự kiện của M1, hay bước không có tác động) → suy từ Status như M1 (shipping → lấy hàng; delivered →
// lấy hàng rồi giao xong); COD = X (cod_amount của vận đơn); CODCollected = tiền hãng báo đã thu (nil = X); Transfers
// theo thứ tự ghi.
type Input struct {
	ShipmentID   uuid.UUID
	Seq          int64
	Status       string
	Effects      []string
	COD          int64
	CODCollected *int64
	Balances     Balances
	Transfers    []Transfer
}

// Post là một bút toán cần ghi (tác nhân hãng).
type Post struct {
	Kind, Key string
	Entries   []Entry
}

// Action là một việc consumer làm, theo thứ tự: đúng một trong Post (ghi bút toán), Reverse (đảo bút toán có mã này
// bằng cash.Reverse), Finding (mở phát hiện loại này, khóa FindingKey).
type Action struct {
	Post       *Post
	Reverse    uuid.UUID
	Finding    string
	FindingKey string
}

// Rules chọn việc cho một sự kiện, xét từng tác động theo thứ tự trên số dư đã cộng các việc trước đó:
//
//   - picked_up, COD X > 0, chưa có expected chưa bị đảo → expected: to_collect +X, sales_clearing −X
//     (khóa expected:<vận đơn>).
//   - delivered, X > 0 → expected như trên nếu chưa có; chưa có collected chưa bị đảo → C = CODCollected (nil: X):
//     C ≤ X: collected to_collect −C, carrier_holding +C (khi C > 0) và returned to_collect −(X−C),
//     sales_clearing +(X−C) (khi C < X); C > X: collected to_collect −X, carrier_holding +C, sales_clearing −(C−X)
//     (sổ khớp số hãng báo đang giữ; bảng kê đối chiếu sau). Khóa collected:<vận đơn>:<seq>, returned:<vận đơn>:<seq>.
//   - returning → to_collect còn dương thì returned toàn bộ phần đó về sales_clearing (Q-10).
//   - delivery_reversed: tiền đã về shop (shop_received ≠ 0 hay có remitted) → không ghi tiền cho cả sự kiện, phát
//     hiện remitted_then_returned (Q-1); ngược lại đảo bút toán collected gần nhất chưa bị đảo.
//   - cancel_confirmed khi đã có expected chưa bị đảo → phát hiện unexpected_cancel_after_pickup, không ghi.
//
// Khóa phát hiện: <loại>:<vận đơn>:<seq>. Bút toán đã ghi với khóa trùng thì bên ghi bỏ qua (giao lại sự kiện).
func Rules(in Input) ([]Action, error) {
	if in.ShipmentID == uuid.Nil || in.COD < 0 || (in.CODCollected != nil && *in.CODCollected < 0) {
		return nil, fmt.Errorf("%w: vận đơn %s, COD %d", ErrInvalidInput, in.ShipmentID, in.COD)
	}
	r := run{in: in, b: in.Balances}
	r.live = map[string]bool{}
	for _, t := range in.Transfers {
		if !t.Reversed {
			r.live[t.Kind] = true
		}
	}
	effects := in.Effects
	if len(effects) == 0 {
		effects = inferEffects(in.Status)
	}
	for _, ef := range effects {
		if r.blocked {
			break
		}
		r.effect(ef)
	}
	return r.acts, nil
}

// inferEffects là tác động của sự kiện M1 (chưa có effects) theo trạng thái mới, như consumer của M1.
func inferEffects(status string) []string {
	switch status {
	case "shipping":
		return []string{EffPickedUp}
	case "delivered":
		return []string{EffPickedUp, EffDelivered}
	}
	return nil
}

type run struct {
	in      Input
	b       Balances
	live    map[string]bool
	acts    []Action
	blocked bool
}

func (r *run) effect(ef string) {
	x := r.in.COD
	switch ef {
	case EffPickedUp:
		r.expect()
	case EffDelivered:
		r.expect()
		if x == 0 || r.live[KindCollected] {
			return
		}
		c := x
		if r.in.CODCollected != nil {
			c = *r.in.CODCollected
		}
		switch {
		case c > x:
			r.post(KindCollected, r.seqKey(KindCollected),
				Entry{AccountToCollect, -x}, Entry{AccountCarrierHolding, c}, Entry{AccountSalesClearing, -(c - x)})
		case c > 0:
			r.post(KindCollected, r.seqKey(KindCollected), Entry{AccountToCollect, -c}, Entry{AccountCarrierHolding, c})
		}
		r.live[KindCollected] = true
		if c < x {
			r.post(KindReturned, r.seqKey(KindReturned), Entry{AccountToCollect, -(x - c)}, Entry{AccountSalesClearing, x - c})
		}
	case EffReturning:
		if rest := r.b.ToCollect; rest > 0 {
			r.post(KindReturned, r.seqKey(KindReturned), Entry{AccountToCollect, -rest}, Entry{AccountSalesClearing, rest})
		}
	case EffDeliveryReversed:
		if r.b.ShopReceived != 0 || r.live[KindRemitted] {
			r.find(FindingRemittedThenReturned)
			r.blocked = true
			return
		}
		r.reverseLastCollected()
	case EffCancelConfirmed:
		if r.live[KindExpected] {
			r.find(FindingUnexpectedCancelAfterPickup)
		}
	}
}

// expect ghi expected khi COD > 0 và chưa có expected chưa bị đảo.
func (r *run) expect() {
	if x := r.in.COD; x > 0 && !r.live[KindExpected] {
		r.post(KindExpected, KindExpected+":"+r.in.ShipmentID.String(),
			Entry{AccountToCollect, x}, Entry{AccountSalesClearing, -x})
		r.live[KindExpected] = true
	}
}

func (r *run) reverseLastCollected() {
	for i := len(r.in.Transfers) - 1; i >= 0; i-- {
		t := r.in.Transfers[i]
		if t.Kind != KindCollected || t.Reversed {
			continue
		}
		r.acts = append(r.acts, Action{Reverse: t.ID})
		for _, e := range t.Entries {
			r.add(e.Account, -e.Amount)
		}
		r.live[KindCollected] = false
		return
	}
}

func (r *run) seqKey(kind string) string {
	return fmt.Sprintf("%s:%s:%d", kind, r.in.ShipmentID, r.in.Seq)
}

func (r *run) post(kind, key string, es ...Entry) {
	r.acts = append(r.acts, Action{Post: &Post{Kind: kind, Key: key, Entries: es}})
	for _, e := range es {
		r.add(e.Account, e.Amount)
	}
}

func (r *run) find(kind string) {
	r.acts = append(r.acts, Action{Finding: kind, FindingKey: r.seqKey(kind)})
}

func (r *run) add(account string, amount int64) {
	switch account {
	case AccountToCollect:
		r.b.ToCollect += amount
	case AccountCarrierHolding:
		r.b.CarrierHolding += amount
	case AccountShopReceived:
		r.b.ShopReceived += amount
	case AccountCarrierFee:
		r.b.CarrierFee += amount
	case AccountSalesClearing:
		r.b.SalesClearing += amount
	}
}
