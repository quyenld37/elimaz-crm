package order_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// TestShipping_Invariants (rapid, 200 lượt): đơn D1, chuỗi ngẫu nhiên hàm M2 (AttachShipmentBy, OnShipmentEvent,
// DetachShipment) xen hàm mới (ReverseDelivery, MarkDeliveredPartial, SetPackingStatus, ApplyAddressChange) với vận đơn
// hiện hành hoặc không. Lỗi nghiệp vụ được phép (bên gọi lùi cả transaction). Sau mỗi bước: lịch sử không có hai lần
// tới delivered mà không có lần đảo delivered → confirmed ở giữa; seq sự kiện của đơn tăng ngặt và seq lớn nhất bằng
// version; một lần bị từ chối hay applied=false không đổi version; tồn không âm, in_transit ≤ on_hand.
func TestShipping_Invariants(t *testing.T) {
	e := newM2Env(t)
	rapid.Check(t, func(rt *rapid.T) {
		o, va, vb := e.d1()
		ships := []uuid.UUID{uuid.New(), uuid.New()}
		for _, s := range ships {
			e.stubShipment(o.ID, s)
		}
		for range rapid.IntRange(1, 10).Draw(rt, "steps") {
			before := e.get(o.ID).Version
			s := rapid.SampledFrom(ships).Draw(rt, "shipment")
			changed, err := e.shippingStep(rt, o, s)
			after := e.get(o.ID).Version
			if (err != nil || !changed) && after != before {
				rt.Fatalf("bước không áp (%v) đổi version %d → %d", err, before, after)
			}
			if msg := e.shippingInvariants(o.ID, after, va, vb); msg != "" {
				rt.Fatal(msg)
			}
		}
	})
}

// shippingStep áp một thao tác ngẫu nhiên lên đơn o với vận đơn s; trả có đổi đơn không và lỗi.
func (e *orderEnv) shippingStep(rt *rapid.T, o order.Order, s uuid.UUID) (bool, error) {
	switch op := rapid.SampledFrom([]string{
		"attach", "detach", "pickup", "deliver", "reverse", "partial", "pack",
		"address",
	}).Draw(rt, "op"); op {
	case "attach":
		before := e.get(o.ID).Version
		err := e.attachOnly(o.ID, s)
		return err == nil && e.get(o.ID).Version != before, err
	case "detach":
		return e.detachBy(e.seller, o.ID, s)
	case "pickup":
		return e.onEvent(o.ID, s, order.StepPickedUp)
	case "deliver":
		before := e.get(o.ID).Version
		applied, err := e.onEvent(o.ID, s, order.StepDelivered)
		return applied && e.get(o.ID).Version != before, err
	case "reverse":
		return e.reverse(o.ID, s, uuid.New(), "carrier_corrected")
	case "partial":
		i := rapid.IntRange(0, len(o.Items)-1).Draw(rt, "item")
		qty := rapid.Int32Range(1, o.Items[i].Qty).Draw(rt, "qty")
		return e.deliverPartial(o.ID, s, order.DeliveredLine{OrderItemID: o.Items[i].ID, Qty: qty})
	case "pack":
		to := rapid.SampledFrom([]string{order.PackingPicking, order.PackingPacked}).Draw(rt, "to")
		res, err := e.pack(to, o.ID)
		return err == nil && res[0].Result == order.PackingUpdated, err
	case "address":
		addr := *fullAddress()
		addr.Line = logging.Address("Số " + rapid.SampledFrom([]string{"1", "2"}).Draw(rt, "line"))
		return e.changeAddress(o.ID, s, addr)
	default:
		panic(op)
	}
}

// shippingInvariants trả mô tả quy luật bị phá (rỗng: đúng hết) của đơn id có version.
func (e *orderEnv) shippingInvariants(id uuid.UUID, version int64, vs ...uuid.UUID) string {
	seqs, moves := e.eventSeqs(id), e.statusMoves(id)
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			return fmt.Sprintf("seq không tăng ngặt: %v", seqs)
		}
	}
	if len(seqs) == 0 || seqs[len(seqs)-1] != version {
		return fmt.Sprintf("seq cuối %v khác version %d", seqs, version)
	}
	if msg := deliveredTwice(moves); msg != "" {
		return msg
	}
	for _, v := range vs {
		if b := e.balance(v); b.OnHand < 0 || b.InTransit < 0 || b.Held < 0 || b.InTransit > b.OnHand {
			return fmt.Sprintf("tồn sai %+v", b)
		}
	}
	return ""
}

// deliveredTwice: lịch sử có hai lần tới delivered mà không có lần delivered → confirmed ở giữa → mô tả, không thì rỗng.
func deliveredTwice(moves [][2]string) string {
	delivered := false
	for _, m := range moves {
		switch {
		case m[1] == order.StatusDelivered && m[0] != order.StatusDelivered:
			if delivered {
				return fmt.Sprintf("delivered hai lần không đảo ở giữa: %v", moves)
			}
			delivered = true
		case m[0] == order.StatusDelivered && m[1] == order.StatusConfirmed:
			delivered = false
		}
	}
	return ""
}

// eventSeqs trả seq các sự kiện của đơn id theo thứ tự ghi.
func (e *orderEnv) eventSeqs(id uuid.UUID) []int64 {
	var seqs []int64
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT seq FROM platform.domain_events WHERE aggregate_type = 'order'
			AND aggregate_id = $1 ORDER BY id`, id.String())
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		seqs, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		return err //nolint:wrapcheck // test
	})
	return seqs
}

// statusMoves trả các cặp (từ, tới) trong lịch sử trạng thái của đơn id theo thứ tự ghi.
func (e *orderEnv) statusMoves(id uuid.UUID) [][2]string {
	var moves [][2]string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT coalesce(from_status, ''), to_status FROM ord.order_status_history
			WHERE order_id = $1 ORDER BY recorded_at, id`, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		moves, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]string, error) {
			var m [2]string
			return m, r.Scan(&m[0], &m[1])
		})
		return err //nolint:wrapcheck // test
	})
	return moves
}
