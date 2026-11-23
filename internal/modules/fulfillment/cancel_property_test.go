package fulfillment_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Quy luật (Task M3-14): chuỗi ngẫu nhiên gửi hãng, hủy đơn, đổi hãng, kết quả hãng (hủy được, từ chối, chờ, không
// rõ, chưa gửi), hãng lấy hàng, hãng tự hủy, tra lại, tin muộn của vận đơn cũ → không bao giờ: hàng được nhả khi hãng
// còn giữ kiện chưa hủy; đơn có hai vận đơn chưa hủy; hàng đã in_transit bị nhả.
func TestCancel_Property(t *testing.T) {
	t.Parallel()
	e := newCancelEnv(t)
	ops := []string{
		"ship", "order_cancel", "replace", "cancel_mode", "create_mode", "pickup", "carrier_cancel", "run",
		"sync", "resolve", "late",
	}
	rapid.Check(t, func(rt *rapid.T) {
		e.carrier.SetMode(fake.ModeOK)
		e.carrier.SetCancelMode(fake.CancelModeOK)
		e.carrier.SetCreateHook(nil)
		o, v := e.order(1)
		m := &cancelModel{orderID: o.ID, variant: v}
		steps := rapid.IntRange(1, 10).Draw(rt, "steps")
		for i := range steps {
			e.cancelStep(rt, m, rapid.SampledFrom(ops).Draw(rt, "op"))
			e.checkCancelInvariants(rt, m, i)
		}
	})
}

type cancelModel struct {
	orderID, variant uuid.UUID
	inTransit        bool // hàng đã in_transit một lần (không có bước giao: không được về held hay nhả)
}

// current trả vận đơn hiện hành của đơn (nil: không có).
func (e *shipEnv) current(m *cancelModel) *uuid.UUID { return e.getOrder(m.orderID).CurrentShipmentID }

func (e *shipEnv) cancelStep(rt *rapid.T, m *cancelModel, op string) {
	cur := e.current(m)
	switch op {
	case "ship":
		if _, err := e.ship(fulfillment.ShipInput{OrderID: m.orderID, CarrierAccountID: e.account}); err != nil &&
			!errors.Is(err, fulfillment.ErrShipmentExists) && !errors.Is(err, fulfillment.ErrOrderNotShippable) {
			rt.Fatalf("gửi hãng: %v", err)
		}
	case "order_cancel":
		if err := e.tryRequestCancel(m.orderID); err != nil && !errors.Is(err, order.ErrInvalidTransition) {
			rt.Fatalf("hủy đơn: %v", err)
		}
		if err := e.deliverCancelRequested(m.orderID); err != nil {
			rt.Fatalf("giao order.cancel_requested: %v", err)
		}
	case "replace":
		e.propReplace(rt, cur)
	case "cancel_mode":
		e.carrier.SetCancelMode(rapid.SampledFrom([]string{
			fake.CancelModeOK, fake.CancelModePending,
			fake.CancelModeTimeoutAfterSend, fake.CancelModeNotSent, fake.CancelModeReject,
		}).Draw(rt, "cmode"))
	case "create_mode":
		e.carrier.SetMode(rapid.SampledFrom([]string{
			fake.ModeOK, fake.ModeTimeoutAfterCreate, fake.ModeFailBeforeCreate,
			fake.ModeTimeoutNoCreate,
		}).Draw(rt, "mode"))
	case "pickup":
		e.advanceCurrent(cur, fake.RawPickedUp)
	case "carrier_cancel":
		e.advanceCurrent(cur, fake.RawCancelled)
	case "run":
		e.drain()
	case "sync":
		if cur != nil && e.view(*cur).TrackingCode != "" {
			e.syncNow(*cur)
		}
	case "resolve":
		e.resolveInDoubt()
		e.drain()
	case "late":
		e.lateEvents(rt, m)
	}
}

// propReplace: nhân viên đổi hãng vận đơn hiện hành cur; chỉ xác nhận "hãng không có kiện" khi đúng là hãng không có
// (tra trang hãng nói thật).
func (e *shipEnv) propReplace(rt *rapid.T, cur *uuid.UUID) {
	if cur == nil {
		return
	}
	checked := rapid.Bool().Draw(rt, "checked") && !e.carrier.Has(e.view(*cur).Code)
	if _, err := e.cancelShipment(*cur, checked); err != nil &&
		!errors.Is(err, fulfillment.ErrShipmentNotCancellable) && !errors.Is(err, fulfillment.ErrCreateInProgress) {
		rt.Fatalf("đổi hãng: %v", err)
	}
}

// advanceCurrent: hãng đổi kiện của vận đơn hiện hành cur (còn chờ thu gom) sang raw.
func (e *shipEnv) advanceCurrent(cur *uuid.UUID, raw string) {
	if cur == nil {
		return
	}
	if tr := e.carrier.Tracking(e.view(*cur).Code); tr != "" && e.carrier.Status(tr) == fake.RawCreated {
		e.carrier.Advance(tr, raw)
	}
}

// lateEvents áp tin muộn (hủy, lấy hàng) cho một vận đơn đã hủy của đơn.
func (e *shipEnv) lateEvents(rt *rapid.T, m *cancelModel) {
	for _, id := range e.orderShipmentIDs(m.orderID) {
		if e.view(id).Status != "cancelled" {
			continue
		}
		now := time.Now().UTC()
		canon := rapid.SampledFrom([]fulfillment.Canonical{fulfillment.CanonCancelled, fulfillment.CanonPickedUp}).Draw(rt, "late")
		if err := e.app(func(tx pgx.Tx) error {
			_, err := e.svc.ApplyTracking(e.t.Context(), tx, fulfillment.ApplyInput{
				ShipmentID: id, Source: fulfillment.SrcCarrierAPI, SyncedAt: now,
				Tracking: fulfillment.Tracking{Events: []fulfillment.CarrierEvent{
					{Key: "late-" + uuid.NewString(), RawStatus: string(canon), Canonical: canon, OccurredAt: now},
				}},
			})
			return err //nolint:wrapcheck // test
		}); err != nil {
			rt.Fatalf("tin muộn: %v", err)
		}
		return
	}
}

func (e *shipEnv) checkCancelInvariants(rt *rapid.T, m *cancelModel, step int) {
	ids := e.orderShipmentIDs(m.orderID)
	active := 0
	for _, id := range ids {
		if e.view(id).Status != "cancelled" {
			active++
		}
	}
	if active > 1 {
		rt.Fatalf("bước %d: đơn có %d vận đơn chưa hủy", step, active)
	}
	st := e.stock(m.variant)
	if st[2] > 0 {
		m.inTransit = true
	}
	if m.inTransit && st[2] != 1 {
		rt.Fatalf("bước %d: hàng in_transit bị nhả hay trả về held: %v", step, st)
	}
	if st[1]+st[2] == 1 {
		return
	}
	// Hàng đã nhả: mọi kiện của đơn ở hãng phải đã hủy.
	for _, id := range ids {
		code := e.view(id).Code
		if tr := e.carrier.Tracking(code); tr != "" && e.carrier.Status(tr) != fake.RawCancelled {
			rt.Fatalf("bước %d: nhả hàng khi hãng còn giữ kiện %s (%s)", step, tr, e.carrier.Status(tr))
		}
	}
}
