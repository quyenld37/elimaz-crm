package order_test

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// 50 lần hủy song song cùng version → đúng một lần được, các lần khác ErrVersionConflict hoặc ErrInvalidTransition;
// một dòng lịch sử sang cancelled, used_count giảm đúng 1.
func TestCancel_Parallel50SameVersion(t *testing.T) {
	e := newM2Env(t)
	cid := e.coupon(sale10("SALE10", 0))
	e.coupon(sale10("KHAC", 0))
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	o := e.mustCreateM2(e.dh01(v1, v2, "SALE10")).Order
	e.mustCreateM2(e.dh01(v1, v2, "KHAC")) // một lượt khác của mã khác: không bị đụng
	errs := make([]error, 50)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range errs {
		wg.Go(func() { <-start; _, errs[i] = e.cancel(o) })
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, order.ErrVersionConflict), errors.Is(err, order.ErrInvalidTransition):
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d lần hủy được, mong 1", ok)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND to_status = 'cancelled'`,
		o.ID); n != 1 {
		t.Errorf("%d dòng lịch sử sang cancelled", n)
	}
	if n := e.usedCount(cid); n != 0 {
		t.Errorf("used_count = %d, mong 0", n)
	}
	if b := e.balance(v1); b.Held != 2 {
		t.Errorf("held V1 = %d, mong 2 (đơn còn lại)", b.Held)
	}
}

// confirmDraft chốt đơn nháp d bằng Writer.Confirm (Task M2-19) ở version lúc tạo.
func (e *orderEnv) confirmDraft(d order.Order) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().Confirm(e.t.Context(), tx, e.seller, d.ID, d.Version, e.actor, e.now().Add(time.Hour))
		return err //nolint:wrapcheck // test
	})
}

// Đua giữa hết hạn và chốt: 100 lần, ExpireDrafts và chốt cùng một đơn nháp (có mã) đồng thời → mỗi lần đúng một bên
// thắng: confirmed (giữ hàng held, lượt active) hoặc cancelled (không giữ, lượt released; Confirm thua nhận
// ErrVersionConflict hay ErrInvalidTransition). Đếm số lần mỗi bên thắng để thấy cả hai nhánh đều xảy ra.
func TestExpireDrafts_RaceWithConfirm(t *testing.T) {
	e := newM2Env(t)
	cid := e.coupon(sale10("SALE10", 0))
	v := e.variant(250000, 1000)
	at := e.now().Add(25 * time.Hour)
	confirmed := 0
	for i := range 100 {
		d := e.draftWith(v, 1, "SALE10")
		var wg sync.WaitGroup
		start := make(chan struct{})
		var n int
		var expErr, confErr error
		wg.Go(func() { <-start; n, expErr = e.expire(at, 200) })
		wg.Go(func() { <-start; confErr = e.confirmDraft(d) })
		close(start)
		wg.Wait()
		if expErr != nil {
			t.Fatalf("lần %d: ExpireDrafts: %v", i, expErr)
		}
		g := e.get(d.ID)
		held, red := e.heldOf(d.ID, inventory.StateHeld), e.redemption(d.ID)
		switch g.Status {
		case order.StatusConfirmed:
			confirmed++
			if confErr != nil || n != 0 || held != 1 || red != "active" {
				t.Fatalf("lần %d confirmed: chốt %v, hết hạn %d, giữ %d, lượt %s", i, confErr, n, held, red)
			}
		case order.StatusCancelled:
			lost := errors.Is(confErr, order.ErrVersionConflict) || errors.Is(confErr, order.ErrInvalidTransition)
			if !lost || n != 1 || held != 0 || red != "released" {
				t.Fatalf("lần %d cancelled: chốt %v, hết hạn %d, giữ %d, lượt %s", i, confErr, n, held, red)
			}
		default:
			t.Fatalf("lần %d: trạng thái %s", i, g.Status)
		}
	}
	t.Logf("chốt thắng %d/100, hết hạn thắng %d/100", confirmed, 100-confirmed)
	if n := e.usedCount(cid); n != confirmed {
		t.Errorf("used_count = %d, mong %d (số đơn đã chốt)", n, confirmed)
	}
	if b := e.balance(v); b.Held != int32(confirmed) {
		t.Errorf("held = %d, mong %d", b.Held, confirmed)
	}
}

// WB12 (lõi phía CRM): 100 lần, Cancel và AttachShipmentBy đồng thời trên một đơn confirmed → không bao giờ có đơn
// cancelled còn vận đơn hiện hành: hoặc (cancelled, không vận đơn, gắn bị từ chối ErrInvalidTransition, hết giữ hàng)
// hoặc (gắn trước → hủy với version mới → cancel_requested, vẫn vận đơn, giữ hàng).
func TestCancel_RaceWithAttach(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(100000, 1000)
	cancelledFirst := 0
	for i := range 100 {
		o := e.mustCreateM2(e.m2Input(0, ln(v, 1, 0))).Order
		s := uuid.New()
		e.stubShipment(o.ID, s)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var cancelErr, attachErr error
		wg.Go(func() {
			<-start
			_, cancelErr = e.cancel(o)
			if errors.Is(cancelErr, order.ErrVersionConflict) { // gắn vận đơn thắng: nhân viên tải lại rồi hủy
				_, cancelErr = e.cancel(e.get(o.ID))
			}
		})
		wg.Go(func() { <-start; attachErr = e.attachOnly(o.ID, s) })
		close(start)
		wg.Wait()
		if cancelErr != nil {
			t.Fatalf("lần %d: hủy: %v", i, cancelErr)
		}
		g := e.get(o.ID)
		switch g.Status {
		case order.StatusCancelled:
			cancelledFirst++
			if g.CurrentShipmentID != nil || !errors.Is(attachErr, order.ErrInvalidTransition) ||
				e.heldOf(o.ID, inventory.StateHeld) != 0 {
				t.Fatalf("lần %d: cancelled, vận đơn %v, gắn %v", i, g.CurrentShipmentID, attachErr)
			}
		case order.StatusCancelRequested:
			if attachErr != nil || g.CurrentShipmentID == nil || *g.CurrentShipmentID != s ||
				e.heldOf(o.ID, inventory.StateHeld) != 1 {
				t.Fatalf("lần %d: cancel_requested, vận đơn %v, gắn %v", i, g.CurrentShipmentID, attachErr)
			}
		default:
			t.Fatalf("lần %d: trạng thái %s", i, g.Status)
		}
	}
	t.Logf("hủy trước %d/100, gắn vận đơn trước %d/100", cancelledFirst, 100-cancelledFirst)
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE status = 'cancelled' AND current_shipment_id IS NOT NULL`); n != 0 {
		t.Errorf("%d đơn cancelled còn vận đơn hiện hành", n)
	}
}

// cancelModel là trạng thái mong đợi của một đơn trong TestCancel_Invariants.
type cancelModel struct {
	o         order.Order
	shipment  uuid.UUID // uuid.Nil: chưa gắn
	picked    bool
	delivered bool
}

// TestCancel_Invariants (rapid): 1–3 đơn confirmed (có hoặc không mã) của một biến thể, chuỗi thao tác ngẫu nhiên (hủy,
// gắn vận đơn, lấy hàng, giao xong, gỡ vận đơn, hãng xác nhận hay từ chối hủy, vận đơn cũ báo) → sau mỗi bước: held =
// tổng số lượng các đơn chưa hủy, chưa giao, chưa lấy hàng; in_transit = tổng của đơn đã lấy hàng chưa giao;
// used_count = số lượt active; không đơn cancelled nào còn vận đơn hiện hành hay giữ hàng; không đơn nào kẹt ở
// cancel_requested mà mất vận đơn hiện hành (B-02). Lấy hàng, giao xong bằng vận đơn hiện hành không bao giờ lỗi, kể
// cả khi đơn đang cancel_requested (B-01, Task 431).
func TestCancel_Invariants(t *testing.T) {
	e := newM2Env(t)
	rapid.Check(t, func(rt *rapid.T) {
		v := e.variant(1000, 100)
		code := fmt.Sprintf("R%s", uuid.NewString()[:8])
		cid := e.coupon(sale10(code, 0))
		ms := make([]*cancelModel, rapid.IntRange(1, 3).Draw(rt, "orders"))
		for i := range ms {
			in := e.m2Input(0, ln(v, rapid.Int32Range(1, 3).Draw(rt, "qty"), 0))
			if rapid.Bool().Draw(rt, "coupon") {
				in.CouponCode = code
			}
			ms[i] = &cancelModel{o: e.mustCreateM2(in).Order}
		}
		for range rapid.IntRange(1, 8).Draw(rt, "steps") {
			m := ms[rapid.IntRange(0, len(ms)-1).Draw(rt, "order")]
			ops := []string{"cancel", "attach", "pickup", "deliver", "detach", "confirm", "reject", "stale"}
			e.cancelStep(rt, m, rapid.SampledFrom(ops).Draw(rt, "op"))
			if msg := e.checkCancelInvariants(v, cid, ms); msg != "" {
				rt.Fatal(msg)
			}
		}
	})
}

// cancelStep áp thao tác op lên đơn m; lỗi nghiệp vụ được phép (thao tác không hợp trạng thái) bị bỏ qua.
func (e *orderEnv) cancelStep(rt *rapid.T, m *cancelModel, op string) {
	var err error
	switch op {
	case "cancel":
		_, err = e.cancel(e.get(m.o.ID))
	case "attach":
		s := uuid.New()
		if err = e.attachBy(m.o.ID, s); err == nil {
			m.shipment = s
		}
	case "pickup":
		e.shipmentStep(rt, m, order.StepPickedUp)
	case "deliver":
		e.shipmentStep(rt, m, order.StepDelivered)
	case "detach":
		if m.shipment != uuid.Nil {
			_, err = e.detachBy(e.seller, m.o.ID, m.shipment)
		}
	case "confirm", "reject":
		if m.shipment != uuid.Nil {
			_, err = e.carrierCancel(m.o.ID, m.shipment, op == "confirm")
		}
	case "stale":
		_, err = e.carrierCancel(m.o.ID, uuid.New(), rapid.Bool().Draw(rt, "confirmed"))
	}
	if g := e.get(m.o.ID); g.CurrentShipmentID == nil {
		m.shipment = uuid.Nil
	}
	if err != nil && !errors.Is(err, order.ErrInvalidTransition) && !errors.Is(err, order.ErrStatusConflict) &&
		!errors.Is(err, order.ErrShipmentAlreadyAttached) {
		rt.Fatalf("%s: lỗi lạ: %v", op, err)
	}
}

// shipmentStep báo bước step bằng vận đơn hiện hành của m (nếu có): không bao giờ lỗi, kể cả khi đơn cancel_requested
// (B-01); applied thì cập nhật mô hình.
func (e *orderEnv) shipmentStep(rt *rapid.T, m *cancelModel, step order.ShipmentStep) {
	if m.shipment == uuid.Nil {
		return
	}
	applied, err := e.onEvent(m.o.ID, m.shipment, step)
	if err != nil {
		rt.Fatalf("%s bằng vận đơn hiện hành: %v", step, err)
	}
	if applied {
		m.picked = true
		m.delivered = m.delivered || step == order.StepDelivered
	}
}

// orderInvariant kiểm một đơn g (mô hình m): trả thông báo lỗi, hay "" kèm việc đơn có giữ hàng (counted) để cộng
// vào held/in_transit.
func (e *orderEnv) orderInvariant(g order.Order, m *cancelModel) (msg string, counted bool) {
	switch {
	case g.Status == order.StatusCancelRequested && g.CurrentShipmentID == nil:
		return fmt.Sprintf("đơn %s cancel_requested mà không còn vận đơn hiện hành", g.Code), false
	case (g.Status == order.StatusDelivered) != m.delivered:
		return fmt.Sprintf("đơn %s: %s, mô hình giao xong %v", g.Code, g.Status, m.delivered), false
	case m.delivered:
		return "", false
	case g.Status == order.StatusCancelled:
		if g.CurrentShipmentID != nil || e.heldOf(g.ID, inventory.StateHeld)+e.heldOf(g.ID, inventory.StateInTransit) != 0 {
			return fmt.Sprintf("đơn %s cancelled còn vận đơn %v hoặc giữ hàng", g.Code, g.CurrentShipmentID), false
		}
		return "", false
	}
	return "", true
}

func (e *orderEnv) checkCancelInvariants(v, cid uuid.UUID, ms []*cancelModel) string {
	var held, transit int32
	active := 0
	for _, m := range ms {
		g := e.get(m.o.ID)
		if e.redemption(g.ID) == "active" {
			active++
		}
		msg, counted := e.orderInvariant(g, m)
		if msg != "" {
			return msg
		}
		if !counted {
			continue
		}
		if m.picked {
			transit += g.Items[0].Qty
		} else {
			held += g.Items[0].Qty
		}
	}
	if b := e.balance(v); b.Held != held || b.InTransit != transit {
		return fmt.Sprintf("tồn %+v, mong held %d, in_transit %d", b, held, transit)
	}
	if n := e.usedCount(cid); n != active {
		return fmt.Sprintf("used_count %d, lượt active %d", n, active)
	}
	return ""
}

// Không deadlock (ADR-46 cho nhiều đơn một lần): 50 lần, ExpireDrafts hủy hai đơn nháp — đơn tạo trước (mã đơn nhỏ)
// giữ biến thể có mã lớn hơn, đơn sau giữ biến thể mã nhỏ — song song với tạo đơn có cả hai biến thể (khóa số dư theo
// biến thể tăng dần) → không lần nào lỗi, pg_stat_database.deadlocks không tăng (lỗi 40P01 bị db.RetryTx chạy lại nên
// chỉ thấy ở bộ đếm); held khớp số đơn còn mở.
func TestExpireDrafts_ManyOrdersNoDeadlock(t *testing.T) {
	e := newM2Env(t)
	lo, hi := e.variant(100000, 1000), e.variant(100000, 1000)
	if bytes.Compare(lo[:], hi[:]) > 0 {
		lo, hi = hi, lo
	}
	at := e.now().Add(25 * time.Hour)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	before := deadlocks()
	for i := range 50 {
		e.draftWith(hi, 1, "")
		e.draftWith(lo, 1, "")
		var wg sync.WaitGroup
		start := make(chan struct{})
		var n int
		var expErr, createErr error
		wg.Go(func() { <-start; n, expErr = e.expire(at, 200) })
		wg.Go(func() { <-start; _, createErr = e.createM2(e.m2Input(0, ln(lo, 1, 0), ln(hi, 1, 0))) })
		close(start)
		wg.Wait()
		if expErr != nil || createErr != nil || n != 2 {
			t.Fatalf("lần %d: hết hạn %d, %v; tạo đơn %v", i, n, expErr, createErr)
		}
	}
	if a, b := e.balance(lo), e.balance(hi); a.Held != 50 || b.Held != 50 {
		t.Errorf("held %d, %d; mong 50", a.Held, b.Held)
	}
	time.Sleep(1500 * time.Millisecond) // số liệu thống kê của backend đẩy lên tối đa mỗi giây
	if after := deadlocks(); after != before {
		t.Errorf("pg_stat_database.deadlocks %d → %d", before, after)
	}
}
