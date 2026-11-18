package cash_test

import (
	"maps"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
)

// propShip là một vận đơn của một lượt: COD, hàng sự kiện chưa giao cho consumer, trạng thái cuối đã phát.
type propShip struct {
	id    uuid.UUID
	cod   int64
	queue []events.StoredEvent
	last  fulfillment.Status
}

// Quy luật sổ COD (300 lượt): 1–5 vận đơn, mỗi vận đơn một chuỗi trạng thái chỉ tiến (như fulfillment phát), sự kiện
// có thể bị giao lại (cả sau sự kiện mới hơn), sự kiện của các vận đơn đan xen ngẫu nhiên → tổng mọi dòng sổ = 0 và số
// dư từng vận đơn khớp trạng thái cuối: shipping → to_collect = X; delivered → carrier_holding = X, to_collect = 0;
// chưa lấy hàng hay COD 0 → không bút toán.
func TestCODLedgerProperty(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		ships := make([]*propShip, rapid.IntRange(1, 5).Draw(rt, "shipments"))
		for i := range ships {
			ships[i] = e.drawShip(rt)
		}
		e.deliverInterleaved(rt, ships)
		for _, s := range ships {
			if b, want := e.balances(s.id), wantBalances(s); !maps.Equal(b, want) {
				rt.Fatalf("vận đơn %s (COD %d, %s): số dư %v, mong %v", s.id, s.cod, s.last, b, want)
			}
		}
		if got := e.total(); got != 0 {
			rt.Fatalf("tổng mọi dòng sổ = %d", got)
		}
	})
}

// drawShip tạo vận đơn COD ngẫu nhiên (có thể 0) và chuỗi sự kiện chỉ tiến của nó, xen các lần giao lại.
func (e *codEnv) drawShip(rt *rapid.T) *propShip {
	cod := rapid.OneOf(rapid.Just(int64(0)), rapid.Int64Range(1, 1<<45)).Draw(rt, "cod")
	s := &propShip{id: e.shipment(cod), cod: cod}
	var sent []events.StoredEvent
	for _, st := range []fulfillment.Status{
		fulfillment.StatusAwaitingPickup, fulfillment.StatusShipping, fulfillment.StatusDelivered,
	} {
		if !rapid.Bool().Draw(rt, "emit") {
			continue
		}
		ev := e.event(uuid.New(), s.id, st)
		sent = append(sent, ev)
		s.queue = append(s.queue, ev)
		s.last = st
		for range rapid.IntRange(0, 2).Draw(rt, "replays") {
			s.queue = append(s.queue, sent[rapid.IntRange(0, len(sent)-1).Draw(rt, "replay")])
		}
	}
	return s
}

// deliverInterleaved giao sự kiện cho consumer: mỗi bước chọn ngẫu nhiên một vận đơn còn sự kiện, giao sự kiện đầu hàng
// của nó (thứ tự trong một vận đơn giữ nguyên như việc event.cash thứ tự nghiêm).
func (e *codEnv) deliverInterleaved(rt *rapid.T, ships []*propShip) {
	for {
		var open []*propShip
		for _, s := range ships {
			if len(s.queue) > 0 {
				open = append(open, s)
			}
		}
		if len(open) == 0 {
			return
		}
		s := open[rapid.IntRange(0, len(open)-1).Draw(rt, "next")]
		if err := e.handle(s.queue[0]); err != nil {
			rt.Fatalf("Handle: %v", err)
		}
		s.queue = s.queue[1:]
	}
}

// wantBalances là số dư từng ngăn mong đợi của s theo trạng thái cuối (ngăn đã về 0 vẫn có mặt trong view).
func wantBalances(s *propShip) map[string]int64 {
	if s.cod == 0 {
		return map[string]int64{}
	}
	switch s.last {
	case fulfillment.StatusShipping:
		return map[string]int64{cash.AccountToCollect: s.cod, cash.AccountSalesClearing: -s.cod}
	case fulfillment.StatusDelivered:
		return map[string]int64{cash.AccountToCollect: 0, cash.AccountCarrierHolding: s.cod, cash.AccountSalesClearing: -s.cod}
	default:
		return map[string]int64{}
	}
}
