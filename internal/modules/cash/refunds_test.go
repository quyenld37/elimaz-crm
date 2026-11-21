package cash_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// orderEvent đọc sự kiện topic mới nhất của đơn id từ sổ sự kiện (dạng consumer nhận).
func (e *payEnv) orderEvent(id uuid.UUID, topic string) events.StoredEvent {
	e.t.Helper()
	ev := events.StoredEvent{TenantID: e.tenant, AggregateType: "order", AggregateID: id.String(), Topic: topic}
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, seq, payload FROM platform.domain_events
			WHERE aggregate_type = 'order' AND aggregate_id = $1 AND topic = $2 ORDER BY seq DESC LIMIT 1`,
			id.String(), topic).Scan(&ev.ID, &ev.Seq, &ev.Payload)
	}); err != nil {
		e.t.Fatalf("đọc sự kiện %s: %v", topic, err)
	}
	return ev
}

// deliver giao ev cho consumer cash_refunds trong một transaction crm_app.
func (e *payEnv) deliver(ev events.StoredEvent) error {
	return e.app(func(tx pgx.Tx) error { return e.refunds.Handle(e.t.Context(), ev, tx) })
}

func (e *payEnv) mustDeliver(ev events.StoredEvent) {
	e.t.Helper()
	if err := e.deliver(ev); err != nil {
		e.t.Fatalf("giao %s: %v", ev.Topic, err)
	}
}

// obligations đọc nghĩa vụ hoàn của đơn id (mọi trạng thái).
func (e *payEnv) obligations(id uuid.UUID) []cash.RefundObligation {
	e.t.Helper()
	var out []cash.RefundObligation
	if err := e.app(func(tx pgx.Tx) error {
		all, _, err := e.svc.ListRefundObligations(e.t.Context(), tx, "", nil, 100)
		for _, r := range all {
			if r.OrderID == id {
				out = append(out, r)
			}
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *payEnv) pay(id uuid.UUID, amount int64) (cash.RefundObligation, error) {
	var out cash.RefundObligation
	err := e.app(func(tx pgx.Tx) (err error) {
		out, err = e.svc.PayRefund(e.t.Context(), tx, e.tenant, id, amount, "CK-HOAN", e.actor())
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *payEnv) waive(id uuid.UUID, reason string) (cash.RefundObligation, error) {
	var out cash.RefundObligation
	err := e.app(func(tx pgx.Tx) (err error) {
		out, err = e.svc.WaiveRefund(e.t.Context(), tx, e.tenant, id, reason, e.actor())
		return err //nolint:wrapcheck // test
	})
	return out, err
}

// cancelledD2 là D2 có cọc 200.000, hủy trước khi gửi hãng, order.cancelled đã giao cho cash_refunds; trả đơn và
// nghĩa vụ duy nhất của nó.
func (e *payEnv) cancelledD2() (order.Order, cash.RefundObligation) {
	e.t.Helper()
	o := e.orderD2()
	e.mustRecord(o.ID, cash.MethodDeposit, 200000)
	e.cancel(o.ID)
	e.mustDeliver(e.orderEvent(o.ID, "order.cancelled"))
	obs := e.obligations(o.ID)
	if len(obs) != 1 {
		e.t.Fatalf("%d nghĩa vụ, muốn 1", len(obs))
	}
	return o, obs[0]
}

// heldReservations đếm giữ hàng còn held của đơn id (chủ bảng).
func (e *payEnv) heldReservations(id uuid.UUID) int64 {
	e.t.Helper()
	var n int64
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT count(*) FROM inventory.stock_reservations r
			JOIN ord.order_items i ON i.id = r.order_item_id WHERE i.order_id = $1 AND r.state = 'held'`, id).Scan(&n)
	})
	return n
}

// TI09/AT17: D2 cọc 200.000, hủy trước khi gửi hãng → hàng nhả, một nghĩa vụ order_cancel 200.000 open (giao lại
// order.cancelled → vẫn một); danh sách nghĩa vụ mở có D2; chưa chi thì payment_status không phải refunded. Chi
// 150.000 → paid_amount 150.000, open; chi 60.000 → ErrRefundExceedsRemaining; chi 50.000 → paid, đơn refunded, tổng
// phiếu 0; chi thêm → ErrObligationClosed.
func TestRefund_CancelledDepositTI09(t *testing.T) {
	e := newPayEnv(t)
	o, ob := e.cancelledD2()
	checkCancelOpened(t, e, o, ob)
	checkPaySequence(t, e, o, ob)
}

// checkCancelOpened: nghĩa vụ order_cancel 200.000 open, hàng đã nhả, giao lại vẫn một, danh sách mở có, chưa refunded.
func checkCancelOpened(t *testing.T, e *payEnv, o order.Order, ob cash.RefundObligation) {
	t.Helper()
	if ob.Source != cash.RefundOrderCancel || ob.Amount != 200000 || ob.PaidAmount != 0 || ob.Status != cash.RefundOpen ||
		ob.ReturnID != nil || ob.OrderCode != o.Code {
		t.Errorf("nghĩa vụ: %+v", ob)
	}
	if n := e.heldReservations(o.ID); n != 0 {
		t.Errorf("còn %d giữ hàng held sau khi hủy", n)
	}
	e.mustDeliver(e.orderEvent(o.ID, "order.cancelled"))
	if obs := e.obligations(o.ID); len(obs) != 1 {
		t.Errorf("giao lại order.cancelled: %d nghĩa vụ", len(obs))
	}
	var open []cash.RefundObligation
	if err := e.app(func(tx pgx.Tx) (err error) {
		open, _, err = e.svc.ListRefundObligations(t.Context(), tx, cash.RefundOpen, nil, 100)
		return err //nolint:wrapcheck // test
	}); err != nil || len(open) != 1 || open[0].ID != ob.ID {
		t.Errorf("danh sách mở: %+v %v", open, err)
	}
	if m := e.money(o.ID); m.paymentStatus == order.PaymentRefunded || m.status != order.StatusCancelled {
		t.Errorf("chưa chi mà đơn %+v", m)
	}
}

// checkPaySequence: chi 150.000, 60.000 (vượt), 50.000 (đủ → refunded, tổng phiếu 0), chi thêm, số sai, nghĩa vụ lạ.
func checkPaySequence(t *testing.T, e *payEnv, o order.Order, ob cash.RefundObligation) {
	t.Helper()
	got, err := e.pay(ob.ID, 150000)
	if err != nil || got.PaidAmount != 150000 || got.Status != cash.RefundOpen || got.Version != ob.Version+1 {
		t.Fatalf("chi 150.000: %+v %v", got, err)
	}
	if m := e.money(o.ID); m.paymentStatus == order.PaymentRefunded || m.prepaid != 50000 {
		t.Errorf("sau chi 150.000: %+v", m)
	}
	if _, err := e.pay(ob.ID, 60000); !errors.Is(err, cash.ErrRefundExceedsRemaining) {
		t.Errorf("chi 60.000: %v; muốn ErrRefundExceedsRemaining", err)
	}
	got, err = e.pay(ob.ID, 50000)
	if err != nil || got.PaidAmount != 200000 || got.Status != cash.RefundPaid {
		t.Fatalf("chi 50.000: %+v %v", got, err)
	}
	if m := e.money(o.ID); m.paymentStatus != order.PaymentRefunded || m.prepaid != 0 {
		t.Errorf("chi đủ: %+v", m)
	}
	if n := e.paymentsTotal(o.ID); n != 0 {
		t.Errorf("tổng phiếu của D2 %d, muốn 0", n)
	}
	if n := e.count("SELECT count(*) FROM cash.payments WHERE refund_obligation_id = $1 AND method = 'refund' AND amount < 0",
		ob.ID); n != 2 {
		t.Errorf("%d phiếu chi, muốn 2", n)
	}
	if _, err := e.pay(ob.ID, 1); !errors.Is(err, cash.ErrObligationClosed) {
		t.Errorf("chi khi đã paid: %v; muốn ErrObligationClosed", err)
	}
	for _, bad := range []int64{0, -1} {
		if _, err := e.pay(ob.ID, bad); !errors.Is(err, cash.ErrInvalidInput) {
			t.Errorf("chi %d: %v; muốn ErrInvalidInput", bad, err)
		}
	}
	if _, err := e.pay(uuid.New(), 1); !errors.Is(err, cash.ErrObligationNotFound) {
		t.Errorf("nghĩa vụ lạ: %v; muốn ErrObligationNotFound", err)
	}
}

// Hai kế toán chi song song mỗi người 200.000 cho nghĩa vụ 200.000 → đúng một thành công, một phiếu chi.
func TestPayRefund_Concurrent(t *testing.T) {
	e := newPayEnv(t)
	o, ob := e.cancelledD2()
	var (
		wg   sync.WaitGroup
		errs [2]error
	)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.pay(ob.ID, 200000) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, cash.ErrObligationClosed) && !errors.Is(err, cash.ErrRefundExceedsRemaining):
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d lần chi thành công, muốn 1 (%v)", ok, errs)
	}
	if n := e.paymentsTotal(o.ID); n != 0 {
		t.Errorf("tổng phiếu %d, muốn 0", n)
	}
}

// WaiveRefund: lý do rỗng, chỉ khoảng trắng → ErrReasonRequired; có lý do → waived, lý do lưu, đơn giữ tiền đã cọc
// (khách đồng ý mất cọc); miễn lại, chi sau khi miễn → ErrObligationClosed.
func TestWaiveRefund(t *testing.T) {
	e := newPayEnv(t)
	o, ob := e.cancelledD2()
	for _, bad := range []string{"", "   "} {
		if _, err := e.waive(ob.ID, bad); !errors.Is(err, cash.ErrReasonRequired) {
			t.Errorf("lý do %q: %v; muốn ErrReasonRequired", bad, err)
		}
	}
	got, err := e.waive(ob.ID, "Khách đồng ý mất cọc")
	if err != nil || got.Status != cash.RefundWaived || got.WaiveReason != "Khách đồng ý mất cọc" || got.PaidAmount != 0 {
		t.Fatalf("miễn: %+v %v", got, err)
	}
	if m := e.money(o.ID); m.paymentStatus == order.PaymentRefunded || m.prepaid != 200000 {
		t.Errorf("đơn sau miễn: %+v", m)
	}
	if _, err := e.waive(ob.ID, "lần hai"); !errors.Is(err, cash.ErrObligationClosed) {
		t.Errorf("miễn lại: %v; muốn ErrObligationClosed", err)
	}
	if _, err := e.pay(ob.ID, 1000); !errors.Is(err, cash.ErrObligationClosed) {
		t.Errorf("chi sau miễn: %v; muốn ErrObligationClosed", err)
	}
	if _, err := e.waive(uuid.New(), "x"); !errors.Is(err, cash.ErrObligationNotFound) {
		t.Errorf("nghĩa vụ lạ: %v; muốn ErrObligationNotFound", err)
	}
}

// Đơn hủy chưa thu đồng nào → không nghĩa vụ. Phiếu trả loại exchange, carrier_return và đơn tạo thường → không
// nghĩa vụ (exchange chuyển cho handleExchange, Task M3-25). Payload hỏng → queue.ErrPermanent.
func TestRefundsConsumer_NoObligation(t *testing.T) {
	e := newPayEnv(t)
	o := e.orderD2()
	e.mustDeliver(e.orderEvent(o.ID, order.TopicOrderCreated))
	e.cancel(o.ID)
	e.mustDeliver(e.orderEvent(o.ID, "order.cancelled"))
	for _, kind := range []string{order.ReturnExchange, order.ReturnCarrier} {
		p, err := json.Marshal(order.ReturnReceived{
			Changed:  order.Changed{OrderID: o.ID, SellerID: e.seller, Version: 9, Status: order.StatusDelivered},
			ReturnID: uuid.New(), Kind: kind, RefundAmount: 100000,
		})
		if err != nil {
			t.Fatal(err)
		}
		e.mustDeliver(events.StoredEvent{
			ID: uuid.New(), TenantID: e.tenant, AggregateType: "order", AggregateID: o.ID.String(), Seq: 9,
			Topic: order.TopicOrderPartiallyReturned, Payload: p,
		})
	}
	if obs := e.obligations(o.ID); len(obs) != 0 {
		t.Errorf("có nghĩa vụ: %+v", obs)
	}
	bad := events.StoredEvent{ID: uuid.New(), TenantID: e.tenant, Topic: "order.cancelled", Payload: []byte(`{"x":`)}
	if err := e.deliver(bad); !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("payload hỏng: %v; muốn ErrPermanent", err)
	}
}

// Phiếu khách trả (createReturn của M2, nhận theo số lượng ở M3-24) nhận xong 1 tốt 1 thiếu của dòng vB ×2 net
// 354.098 → order.partially_returned refundAmount 177.049 → một nghĩa vụ customer_return 177.049 gắn phiếu (giao lại →
// vẫn một). Chi đủ → paid; đơn đã giao giữ nguyên tiền (không refunded, COD không đổi; Quyết định 1).
func TestRefundsConsumer_CustomerReturn(t *testing.T) {
	e := newPayEnv(t)
	o, r := e.receivedReturn()
	ev := e.orderEvent(o.ID, order.TopicOrderPartiallyReturned)
	e.mustDeliver(ev)
	e.mustDeliver(ev)
	obs := e.obligations(o.ID)
	if len(obs) != 1 || obs[0].Source != cash.RefundCustomerReturn || obs[0].Amount != 177049 ||
		obs[0].ReturnID == nil || *obs[0].ReturnID != r || obs[0].Status != cash.RefundOpen {
		t.Fatalf("nghĩa vụ: %+v", obs)
	}
	before := e.money(o.ID)
	got, err := e.pay(obs[0].ID, 177049)
	if err != nil || got.Status != cash.RefundPaid {
		t.Fatalf("chi: %+v %v", got, err)
	}
	if m := e.money(o.ID); m.paymentStatus != before.paymentStatus || m.cod != before.cod || m.prepaid != before.prepaid {
		t.Errorf("đơn đổi sau khi chi hoàn phiếu trả: %+v (trước %+v)", m, before)
	}
}

// receivedReturn dựng đơn D1 (vA ×1 245.902, vB ×2 177.049) đã giao bằng vận đơn thật, phiếu khách trả 2 vB và kho
// nhận 1 tốt 1 thiếu; trả đơn và mã phiếu.
func (e *payEnv) receivedReturn() (order.Order, uuid.UUID) {
	e.t.Helper()
	var va, vb uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		va, vb = e.shop.Variant(e.t, tx, e.seller, 245902, 5), e.shop.Variant(e.t, tx, e.seller, 177049, 5)
		return nil
	})
	ctx, a, now := e.t.Context(), e.orderActor(), time.Now().UTC()
	var (
		o   order.Order
		ret uuid.UUID
	)
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		if o, err = e.shop.Order(ctx, tx, e.seller, e.user, 0, order.ItemInput{VariantID: va, Qty: 1},
			order.ItemInput{VariantID: vb, Qty: 2}); err != nil {
			return err //nolint:wrapcheck // test
		}
		s, err := e.shop.Shipment(ctx, tx, e.seller, o.ID, e.user)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, step := range []order.ShipmentStep{order.StepPickedUp, order.StepDelivered} {
			if _, err := e.w.OnShipmentEvent(ctx, tx, e.seller, o.ID, s.ID, step, a, now); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		var ib uuid.UUID
		for _, it := range o.Items {
			if it.VariantID == vb {
				ib = it.ID
			}
		}
		r, err := e.w.CreateReturn(ctx, tx, e.seller, o.ID, order.CreateReturnInput{
			Kind: order.ReturnCustomer, Lines: []order.ReturnLineInput{{OrderItemID: ib, Quantity: 2}}, Reason: "Khách đổi ý",
		}, a, now)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		got, err := e.w.ReceiveReturnQty(ctx, tx, e.seller, r.ID, r.Version,
			[]order.ReceiveQtyLine{{OrderItemID: ib, Sellable: 1, Missing: 1}}, a, now)
		if err == nil && got.RefundAmount != 177049 {
			e.t.Errorf("phiếu nhận: tiền hoàn %d, muốn 177.049", got.RefundAmount)
		}
		ret = r.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("dựng phiếu trả đã nhận: %v", err)
	}
	return o, ret
}
