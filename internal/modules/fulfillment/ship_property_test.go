package fulfillment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Quy luật (Task M3-12, Review Focus 1): chuỗi ngẫu nhiên chế độ hãng giả, worker chết sau khi gửi, tra lại
// (ResolveInDoubt), "Gửi lại", gửi song song hai lần → với mỗi đơn: số vận đơn chưa hủy ≤ 1; hãng (tra theo mã nói
// thật) có ≤ 1 kiện cho các vận đơn của đơn, và kiện đó thuộc vận đơn chưa hủy; vận đơn đã hủy không có kiện; vận đơn
// có mã hãng thì đúng mã kiện của hãng.
func TestShip_Property(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	sweeper := queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "sweeper/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, e.cmds),
	})
	ops := []string{"mode", "ship", "parallel", "run", "crash", "resolve", "resend"}
	modes := []string{fake.ModeOK, fake.ModeTimeoutAfterCreate, fake.ModeTimeoutNoCreate, fake.ModeFailBeforeCreate, fake.ModeReject}
	rapid.Check(t, func(rt *rapid.T) {
		e.carrier.SetMode(fake.ModeOK)
		e.carrier.SetCreateHook(nil)
		o, _ := e.order(1)
		steps := rapid.IntRange(1, 8).Draw(rt, "steps")
		for i := range steps {
			e.propStep(rt, sweeper, o.ID, rapid.SampledFrom(ops).Draw(rt, "op"), modes)
			e.checkShipInvariants(rt, o.ID, i)
		}
	})
}

// propStep chạy một bước op của TestShip_Property trên đơn orderID.
func (e *shipEnv) propStep(rt *rapid.T, sweeper *queue.Worker, orderID uuid.UUID, op string, modes []string) {
	switch op {
	case "mode":
		e.carrier.SetMode(rapid.SampledFrom(modes).Draw(rt, "mode"))
	case "ship":
		if _, err := e.ship(fulfillment.ShipInput{OrderID: orderID, CarrierAccountID: e.account}); err != nil &&
			!errors.Is(err, fulfillment.ErrShipmentExists) {
			rt.Fatalf("ship: %v", err)
		}
	case "parallel":
		e.shipParallel(rt, orderID)
	case "run":
		e.drain()
	case "crash":
		e.armCrash(rt, sweeper)
		e.drain()
	case "resolve":
		if _, _, _, err := commands.ResolveInDoubt(e.t.Context(), e.staff.App, e.cmds, 50); err != nil {
			rt.Fatalf("ResolveInDoubt: %v", err)
		}
	case "resend":
		for _, id := range e.orderShipmentIDs(orderID) {
			if _, err := e.resend(id); err != nil && !errors.Is(err, fulfillment.ErrCreateInProgress) {
				rt.Fatalf("gửi lại: %v", err)
			}
		}
	}
}

// shipParallel gửi hai lần song song cho đơn orderID: chỉ được thành công hoặc ErrShipmentExists.
func (e *shipEnv) shipParallel(rt *rapid.T, orderID uuid.UUID) {
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for k := range 2 {
		wg.Go(func() {
			_, errs[k] = e.ship(fulfillment.ShipInput{OrderID: orderID, CarrierAccountID: e.account})
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, fulfillment.ErrShipmentExists) {
			rt.Fatalf("ship song song: %v", err)
		}
	}
}

// armCrash: lần gọi tạo kế tiếp giả lập worker chết sau khi gửi (giữ chỗ hết hạn, worker khác quét sang in_doubt).
func (e *shipEnv) armCrash(rt *rapid.T, sweeper *queue.Worker) {
	e.carrier.SetCreateHook(func() {
		e.carrier.SetCreateHook(nil)
		e.ownerExec(`UPDATE platform.jobs SET lease_until = now() - interval '1 second'
			WHERE kind = 'fulfillment.create_shipment' AND status = 'running'`)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := sweeper.RunOnce(ctx, 1); err != nil {
			rt.Errorf("worker quét: %v", err)
		}
	})
}

func (e *shipEnv) orderShipmentIDs(orderID uuid.UUID) []uuid.UUID {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
		vs, _, _, err := e.svc.OrderShipments(ctx, tx, orderID, nil, 200)
		ids := make([]uuid.UUID, 0, len(vs))
		for _, v := range vs {
			ids = append(ids, v.ID)
		}
		return ids, err //nolint:wrapcheck // test
	})
}

func (e *shipEnv) checkShipInvariants(rt *rapid.T, orderID uuid.UUID, step int) {
	vs := readAs(e, func(ctx context.Context, tx pgx.Tx) ([]fulfillment.ShipmentView, error) {
		vs, _, _, err := e.svc.OrderShipments(ctx, tx, orderID, nil, 200)
		return vs, err //nolint:wrapcheck // test
	})
	active, parcels := 0, 0
	for _, v := range vs {
		has := e.carrier.Has(v.Code)
		if has {
			parcels++
		}
		if v.Status == "cancelled" {
			if has {
				rt.Fatalf("bước %d: vận đơn đã hủy %s có kiện ở hãng", step, v.Code)
			}
			continue
		}
		active++
		if v.TrackingCode != "" && v.TrackingCode != e.carrier.Tracking(v.Code) {
			rt.Fatalf("bước %d: mã hãng %q khác kiện %q", step, v.TrackingCode, e.carrier.Tracking(v.Code))
		}
	}
	if active > 1 || parcels > 1 {
		rt.Fatalf("bước %d: %d vận đơn chưa hủy, %d kiện ở hãng cho một đơn", step, active, parcels)
	}
}
