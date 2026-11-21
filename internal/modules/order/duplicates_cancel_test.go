package order_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
)

// Đơn trùng khi một đơn của cặp bị hủy (Task 432, review M2 B-03, B-04, E-08).

// cancelPlain hủy tay đơn id (lý do khách đổi ý) bằng writer có consumer order_duplicates.
func (d *dupEnv) cancelPlain(id uuid.UUID) order.Order {
	d.t.Helper()
	var got order.Order
	if err := d.app(func(tx pgx.Tx) error {
		var err error
		got, err = d.w().Cancel(d.t.Context(), tx, d.seller, id, d.get(id).Version, order.CancelCustomerChangedMind, "",
			d.actor, at(14))
		return err //nolint:wrapcheck // test
	}); err != nil {
		d.t.Fatalf("hủy tay: %v", err)
	}
	return got
}

// carrierAnswer gọi OnShipmentCancelConfirmed (ok = true) hay OnShipmentCancelRejected của vận đơn s lên đơn o bằng
// writer có consumer order_duplicates; người làm là hãng.
func (d *dupEnv) carrierAnswer(o, s uuid.UUID, ok bool) {
	d.t.Helper()
	carrier := order.Actor{TenantID: d.tenant, Type: inventory.ActorCarrier}
	var applied bool
	if err := d.app(func(tx pgx.Tx) error {
		var err error
		if ok {
			applied, err = d.w().OnShipmentCancelConfirmed(d.t.Context(), tx, d.seller, o, s, carrier, at(21))
		} else {
			applied, err = d.w().OnShipmentCancelRejected(d.t.Context(), tx, d.seller, o, s, carrier, at(21))
		}
		return err //nolint:wrapcheck // test
	}); err != nil || !applied {
		d.t.Fatalf("hãng trả lời hủy (%v): %v %v", ok, applied, err)
	}
}

// attachShipment gắn vận đơn mới (dòng giả) làm vận đơn hiện hành của đơn o lúc 15:00 bằng writer có consumer
// order_duplicates (phát order.updated); trả mã vận đơn.
func (d *dupEnv) attachShipment(o uuid.UUID) uuid.UUID {
	d.t.Helper()
	s := uuid.New()
	d.stubShipment(o, s)
	if err := d.app(func(tx pgx.Tx) error {
		return d.w().AttachShipmentBy(d.t.Context(), tx, d.seller, o, s, d.actor, at(15))
	}); err != nil {
		d.t.Fatal(err)
	}
	return s
}

func (d *dupEnv) openLinks() int {
	d.t.Helper()
	return d.count(`SELECT count(*) FROM ord.order_duplicate_links WHERE resolution IS NULL`)
}

// B-03 (DH09, TD03): O2 đã gửi hãng; "hủy đơn kia" → O2 cancel_requested, không nhả hàng, cặp vẫn mở (không kết
// luận, còn ở danh sách, hai đơn còn cờ). Hãng từ chối → O2 confirmed, cặp vẫn hiện. Hủy lại, hãng xác nhận → O2
// cancelled, nhả hàng; sau worker cặp rời danh sách, hai đơn hết cờ; cặp không bị ghi kết luận thay nhân viên.
func TestDuplicates_CancelShipped(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	s := d.attachShipment(o2.ID)
	d.drain()
	stillOpen := func(when string, status string) {
		t.Helper()
		if got := d.get(o2.ID).Status; got != status {
			t.Errorf("%s: O2 %s, mong %s", when, got, status)
		}
		if d.openLinks() != 1 || len(d.list()) != 1 || !d.suspect(o1.ID) || !d.suspect(o2.ID) {
			t.Errorf("%s: cặp mở %d, danh sách %d, cờ %v %v", when, d.openLinks(), len(d.list()), d.suspect(o1.ID),
				d.suspect(o2.ID))
		}
		if n := d.heldOf(o2.ID, inventory.StateHeld); n != 1 {
			t.Errorf("%s: O2 giữ %d, mong 1", when, n)
		}
	}
	if err := d.resolve(o1.ID, o2.ID, order.DuplicateCancelOther); err != nil {
		t.Fatalf("cancel_other đơn đã gửi hãng: %v", err)
	}
	d.drain()
	stillOpen("sau yêu cầu hủy", order.StatusCancelRequested)

	d.carrierAnswer(o2.ID, s, false)
	d.drain()
	stillOpen("hãng từ chối", order.StatusConfirmed)

	if err := d.resolve(o2.ID, o1.ID, order.DuplicateCancelThis); err != nil {
		t.Fatalf("cancel_this lần hai: %v", err)
	}
	d.carrierAnswer(o2.ID, s, true)
	if got := d.get(o2.ID); got.Status != order.StatusCancelled || got.CancelReason == nil ||
		*got.CancelReason != order.CancelDuplicate {
		t.Fatalf("hãng xác nhận: O2 %s %v", got.Status, got.CancelReason)
	}
	d.drain()
	if d.suspect(o1.ID) || d.suspect(o2.ID) || len(d.list()) != 0 {
		t.Errorf("sau hãng xác nhận: cờ %v %v, danh sách %d", d.suspect(o1.ID), d.suspect(o2.ID), len(d.list()))
	}
	if d.openLinks() != 1 || d.heldOf(o2.ID, inventory.StateHeld) != 0 {
		t.Errorf("kết luận tự ghi hay hàng chưa nhả: mở %d, giữ %d", d.openLinks(), d.heldOf(o2.ID, inventory.StateHeld))
	}
}

// E-08: hủy một đơn qua xử lý đơn trùng cần version của đơn bị hủy: thiếu → ErrInvalidInput; khác version hiện tại (đơn
// vừa được sửa) → ErrVersionConflict; cả hai không đổi đơn, cặp, cờ. not_duplicate không cần version.
func TestDuplicates_CancelVersion(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	if err := d.resolveV(o1.ID, o2.ID, order.DuplicateCancelOther, nil); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("thiếu version: %v", err)
	}
	stale := d.get(o2.ID).Version - 1
	if err := d.resolveV(o1.ID, o2.ID, order.DuplicateCancelOther, &stale); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if err := d.resolveV(o2.ID, o1.ID, order.DuplicateCancelOther, &stale); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ (đơn kia là O1): %v", err)
	}
	if d.get(o1.ID).Status != order.StatusConfirmed || d.get(o2.ID).Status != order.StatusConfirmed ||
		d.openLinks() != 1 || !d.suspect(o1.ID) || !d.suspect(o2.ID) {
		t.Errorf("lỗi version mà đơn, cặp, cờ bị đổi")
	}
	if err := d.resolveV(o1.ID, o2.ID, order.DuplicateNotDuplicate, nil); err != nil {
		t.Errorf("not_duplicate không version: %v", err)
	}
}

// B-04: ba đơn A, B, C cùng SĐT, cùng biến thể (ba cặp). Hủy tay A → sau worker A hết cờ, B, C giữ cờ (cặp B–C còn
// mở). Hủy tay C → B, C hết cờ. Không cặp nào bị ghi kết luận.
func TestDuplicates_OtherCancelClearsFlag(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	a := d.place(shipPhone, at(10), v1)
	b := d.place(shipPhone, at(11), v1)
	c := d.place(shipPhone, at(12), v1)
	d.drain()
	if d.openLinks() != 3 {
		t.Fatalf("cặp: %d", d.openLinks())
	}
	d.cancelPlain(a.ID)
	d.drain()
	if d.suspect(a.ID) || !d.suspect(b.ID) || !d.suspect(c.ID) {
		t.Errorf("sau hủy A: cờ A %v B %v C %v", d.suspect(a.ID), d.suspect(b.ID), d.suspect(c.ID))
	}
	d.cancelPlain(c.ID)
	d.drain()
	if d.suspect(b.ID) || d.suspect(c.ID) || len(d.list()) != 0 || d.openLinks() != 3 {
		t.Errorf("sau hủy C: cờ B %v C %v, danh sách %d, cặp chưa kết luận %d", d.suspect(b.ID), d.suspect(c.ID),
			len(d.list()), d.openLinks())
	}
}

// 6.3: hai đơn của một cặp bị hủy; consumer nhận order.cancelled của cả hai, bốn lần mỗi đơn, song song (mỗi lần khóa
// hai đơn theo id tăng dần) → không lỗi (không deadlock), hai đơn hết cờ.
func TestDuplicates_CancelledConsumerParallel(t *testing.T) {
	d := newDupEnv(t)
	v1 := d.variant(250000, 20)
	o1, o2 := d.pair(shipPhone, v1)
	c := order.DuplicatesConsumer{Now: time.Now}
	evs := make([]events.StoredEvent, 0, 2)
	for _, o := range []order.Order{o1, o2} {
		got := d.cancelPlain(o.ID)
		evs = append(evs, events.StoredEvent{
			ID: uuid.New(), TenantID: d.tenant, AggregateType: "order", AggregateID: o.ID.String(), Seq: got.Version,
			Topic: "order.cancelled",
			Payload: []byte(`{"orderId":"` + o.ID.String() + `","sellerId":"` + d.seller.String() +
				`","status":"cancelled"}`),
		})
	}
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range 8 {
		wg.Go(func() { errs[i] = d.app(func(tx pgx.Tx) error { return c.Handle(t.Context(), evs[i%2], tx) }) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("Handle song song: %v", err)
	}
	if d.suspect(o1.ID) || d.suspect(o2.ID) {
		t.Errorf("còn cờ sau hai đơn hủy")
	}
}
