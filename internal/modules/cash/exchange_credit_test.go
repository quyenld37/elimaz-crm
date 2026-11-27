package cash_test

import (
	"context"
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

// exchangeCase là đơn D1 đã giao (vA ×1 245.902, vB ×2 177.049), phiếu đổi 1 vB (tiền hoàn 177.049) và đơn thay thế
// 1 vB' (ship báo khách 0) của kế hoạch Task M3-25.
type exchangeCase struct {
	orig        order.Order
	ret         order.Return
	replacement uuid.UUID
	itemB       uuid.UUID
}

// exchanged dựng D1 đã giao bằng vận đơn thật rồi phiếu đổi 1 vB lấy 1 vB' giá price (createReturn của M2, địa chỉ của
// đơn gốc → đơn thay thế new) và xác nhận đơn thay thế (confirmed, chưa gửi hãng).
func (e *payEnv) exchanged(price int64) exchangeCase {
	e.t.Helper()
	var va, vb, vb2 uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		va, vb = e.shop.Variant(e.t, tx, e.seller, 245902, 5), e.shop.Variant(e.t, tx, e.seller, 177049, 5)
		vb2 = e.shop.Variant(e.t, tx, e.seller, price, 5)
		return nil
	})
	ctx, a, now := e.t.Context(), e.orderActor(), time.Now().UTC()
	var c exchangeCase
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		if c.orig, c.itemB, err = e.deliveredD1(ctx, tx, va, vb); err != nil {
			return err
		}
		c.ret, err = e.w.CreateReturn(ctx, tx, e.seller, c.orig.ID, order.CreateReturnInput{
			Kind: order.ReturnExchange, Lines: []order.ReturnLineInput{{OrderItemID: c.itemB, Quantity: 1}},
			Reason: "Đổi size", Replacement: &order.CreateInput{
				IdempotencyKey: uuid.New(), Lines: []order.LineInput{{VariantID: vb2, Quantity: 1}},
			},
		}, a, now)
		if err != nil || c.ret.ReplacementOrderID == nil {
			return err //nolint:wrapcheck // test
		}
		return e.confirm(ctx, tx, *c.ret.ReplacementOrderID)
	}); err != nil {
		e.t.Fatalf("dựng phiếu đổi: %v", err)
	}
	if c.ret.ReplacementOrderID == nil || c.ret.RefundAmount != 177049 {
		e.t.Fatalf("phiếu đổi: %+v", c.ret)
	}
	c.replacement = *c.ret.ReplacementOrderID
	if m := e.money(c.replacement); m.cod != price || m.status != order.StatusConfirmed {
		e.t.Fatalf("đơn thay thế trước consumer: %+v", m)
	}
	return c
}

// confirm xác nhận đơn new id trong tx.
func (e *payEnv) confirm(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	r, err := order.Get(ctx, tx, e.seller, id)
	if err != nil {
		return err //nolint:wrapcheck // test
	}
	_, err = e.w.Confirm(ctx, tx, e.seller, r.ID, r.Version, e.orderActor(), time.Now().UTC())
	return err //nolint:wrapcheck // test
}

// credited là exchanged(price) rồi giao order.created của đơn thay thế một lần; trả cả sự kiện để giao lại.
func (e *payEnv) credited(price int64) (exchangeCase, events.StoredEvent) {
	e.t.Helper()
	c := e.exchanged(price)
	ev := e.orderEvent(c.replacement, order.TopicOrderCreated)
	e.mustDeliver(ev)
	return c, ev
}

// mustReceiveExchange là receiveExchange phải không lỗi.
func (e *payEnv) mustReceiveExchange(c exchangeCase) {
	e.t.Helper()
	if err := e.receiveExchange(c); err != nil {
		e.t.Fatalf("giao sự kiện nhận phiếu đổi: %v", err)
	}
}

// wantExchangeObligation: đơn gốc của c có đúng một nghĩa vụ exchange amount, open, gắn phiếu đổi của c.
func (e *payEnv) wantExchangeObligation(c exchangeCase, amount int64) {
	e.t.Helper()
	obs := e.obligations(c.orig.ID)
	if len(obs) != 1 || obs[0].Source != cash.RefundExchange || obs[0].Amount != amount || obs[0].ReturnID == nil ||
		*obs[0].ReturnID != c.ret.ID || obs[0].Status != cash.RefundOpen {
		e.t.Errorf("nghĩa vụ của đơn gốc: %+v; muốn một exchange %d", obs, amount)
	}
}

// wantOneCredit: đơn thay thế của c có đúng một phiếu exchange_credit.
func (e *payEnv) wantOneCredit(c exchangeCase) {
	e.t.Helper()
	if n := e.creditsOf(c.replacement, cash.MethodExchangeCredit); n != 1 {
		e.t.Errorf("%d phiếu cấn trừ, muốn 1", n)
	}
}

// deliverConcurrently giao ev n lần song song, mỗi lần một transaction; mọi lần phải không lỗi.
func (e *payEnv) deliverConcurrently(ev events.StoredEvent, n int) {
	e.t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range errs {
		wg.Go(func() { errs[i] = e.deliver(ev) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			e.t.Errorf("giao song song %d: %v", i, err)
		}
	}
}

// receiveExchange là kho nhận đủ món của phiếu đổi c (1 vB tốt) rồi giao order.partially_returned của đơn gốc cho
// cash_refunds.
func (e *payEnv) receiveExchange(c exchangeCase) error {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.w.ReceiveReturnQty(e.t.Context(), tx, e.seller, c.ret.ID, c.ret.Version,
			[]order.ReceiveQtyLine{{OrderItemID: c.itemB, Sellable: 1}}, e.orderActor(), time.Now().UTC())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("kho nhận phiếu đổi: %v", err)
	}
	return e.deliver(e.orderEvent(c.orig.ID, order.TopicOrderPartiallyReturned))
}

// creditsOf đếm phiếu của đơn id theo phương thức method.
func (e *payEnv) creditsOf(id uuid.UUID, method string) int64 {
	e.t.Helper()
	return e.count("SELECT count(*) FROM cash.payments WHERE order_id = $1 AND method = $2", id, method)
}

// TD06/AT14: đổi 1 vB (177.049) lấy 1 vB' 180.000 → sau consumer đơn thay thế có đúng một phiếu exchange_credit 177.049
// (mã exchange:<phiếu>, người ghi hệ thống), cod 2.951, partial; không phiếu tiền mặt nào; đơn gốc D1 không đổi dòng,
// tổng, trạng thái, tiền. Kho nhận phiếu đổi (đủ món) → không nghĩa vụ hoàn (không dư).
func TestExchangeCredit_TD06(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(180000)
	before := e.money(c.orig.ID)
	e.mustDeliver(e.orderEvent(c.replacement, order.TopicOrderCreated))
	var ps []cash.PaymentView
	if err := e.app(func(tx pgx.Tx) (err error) {
		ps, _, err = e.svc.ListOrderPayments(t.Context(), tx, c.replacement, nil, 100)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Method != cash.MethodExchangeCredit || ps[0].Amount != 177049 ||
		ps[0].Reference != "exchange:"+c.ret.ID.String() || ps[0].ActorID != uuid.Nil || ps[0].RefundObligationID != nil {
		t.Fatalf("phiếu của đơn thay thế: %+v", ps)
	}
	if m := e.money(c.replacement); m.cod != 2951 || m.prepaid != 177049 || m.paymentStatus != order.PaymentPartial {
		t.Errorf("đơn thay thế sau cấn trừ: %+v", m)
	}
	if n := e.count("SELECT count(*) FROM cash.payments WHERE method = 'cash'"); n != 0 {
		t.Errorf("%d phiếu tiền mặt", n)
	}
	if n := e.count("SELECT count(*) FROM cash.payments WHERE order_id = $1", c.orig.ID); n != 0 {
		t.Errorf("đơn gốc có %d phiếu", n)
	}
	if m := e.money(c.orig.ID); m != before {
		t.Errorf("đơn gốc đổi: %+v (trước %+v)", m, before)
	}
	var lines int64
	var grand int64
	if err := e.app(func(tx pgx.Tx) error {
		o, err := order.Get(t.Context(), tx, e.seller, c.orig.ID)
		lines, grand = int64(len(o.Items)), o.GrandTotal
		return err //nolint:wrapcheck // test
	}); err != nil || lines != 2 || grand != c.orig.GrandTotal {
		t.Errorf("dòng, tổng của đơn gốc: %d %d %v", lines, grand, err)
	}
	e.mustReceiveExchange(c)
	if obs := e.obligations(c.orig.ID); len(obs) != 0 {
		t.Errorf("đổi lấy món đắt hơn mà có nghĩa vụ: %+v", obs)
	}
	e.wantOneCredit(c)
}

// Đổi lấy món rẻ hơn (150.000): exchange_credit 150.000, cod 0, paid; phần dư 27.049 chưa thành nghĩa vụ khi phiếu đổi
// chưa nhận; kho nhận món trả → một nghĩa vụ exchange 27.049 của đơn gốc gắn phiếu (giao lại → vẫn một).
func TestExchangeCredit_CheaperReplacement(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(150000)
	e.mustDeliver(e.orderEvent(c.replacement, order.TopicOrderCreated))
	if m := e.money(c.replacement); m.cod != 0 || m.prepaid != 150000 || m.paymentStatus != order.PaymentPaid {
		t.Errorf("đơn thay thế: %+v", m)
	}
	if n := e.paymentsTotal(c.replacement); n != 150000 {
		t.Errorf("tổng phiếu đơn thay thế %d", n)
	}
	if obs := e.obligations(c.orig.ID); len(obs) != 0 {
		t.Errorf("chưa nhận mà có nghĩa vụ: %+v", obs)
	}
	e.mustReceiveExchange(c)
	e.mustDeliver(e.orderEvent(c.orig.ID, order.TopicOrderPartiallyReturned))
	e.wantExchangeObligation(c, 27049)
	if obs := e.obligations(c.replacement); len(obs) != 0 {
		t.Errorf("đơn thay thế có nghĩa vụ: %+v", obs)
	}
}

// Sự kiện order.created của đơn thay thế giao hai lần liền, rồi hai lần song song → đúng một phiếu cấn trừ; tổng của
// đơn không đổi sau lần đầu.
func TestExchangeCredit_Idempotent(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c, ev := e.credited(180000)
	after := e.money(c.replacement)
	e.mustDeliver(ev)
	e.deliverConcurrently(ev, 2)
	e.wantOneCredit(c)
	if m := e.money(c.replacement); m != after {
		t.Errorf("giao lại làm đổi đơn: %+v (sau lần đầu %+v)", m, after)
	}
}

// Hai lần giao đầu tiên của order.created chạy song song (chưa có phiếu) → đúng một phiếu cấn trừ.
func TestExchangeCredit_ConcurrentFirst(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(180000)
	e.deliverConcurrently(e.orderEvent(c.replacement, order.TopicOrderCreated), 4)
	e.wantOneCredit(c)
	if m := e.money(c.replacement); m.cod != 2951 {
		t.Errorf("đơn thay thế: %+v", m)
	}
}

// Đơn thay thế đã gửi hãng trước khi consumer chạy → không cấn trừ được (COD khóa theo vận đơn) → việc dừng có lỗi rõ
// (cash.ErrCODLockedByShipment, lỗi vĩnh viễn: dead ngay, cảnh báo dead thường), không phiếu nào, COD giữ 180.000. Ghi
// lại cho màn đổi hàng (Task 138): chỉ cho gửi hãng sau khi có phiếu cấn trừ. Kho nhận phiếu đổi sau đó: không cấn trừ
// được (khách trả đủ COD 180.000) → nghĩa vụ exchange bằng đủ tiền hoàn 177.049, sự kiện nhận không dừng. Vận đơn bị
// gỡ rồi chạy lại order.created → không phiếu cấn trừ (không hoàn hai lần).
func TestExchangeCredit_ShippedBeforeConsumer(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(180000)
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.shop.Shipment(t.Context(), tx, e.seller, c.replacement, e.user)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("gửi hãng đơn thay thế: %v", err)
	}
	err := e.deliver(e.orderEvent(c.replacement, order.TopicOrderCreated))
	if !errors.Is(err, cash.ErrCODLockedByShipment) || !errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("giao order.created: %v; muốn ErrCODLockedByShipment, ErrPermanent", err)
	}
	if n := e.paymentsTotal(c.replacement); n != 0 {
		t.Errorf("tổng phiếu %d, muốn 0", n)
	}
	if m := e.money(c.replacement); m.cod != 180000 || m.prepaid != 0 {
		t.Errorf("đơn thay thế: %+v", m)
	}
	e.mustReceiveExchange(c)
	e.wantExchangeObligation(c, 177049)
	if n := e.paymentsTotal(c.replacement); n != 0 {
		t.Errorf("tổng phiếu sau khi nhận %d, muốn 0", n)
	}
	// Vận đơn bị gỡ (như DetachCurrentShipment khi hủy vận đơn: đơn về confirmed không vận đơn) rồi nhân viên chạy lại
	// việc dead → không cấn trừ nữa (đã chốt lúc nhận), không trả tiền hoàn hai lần.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE ord.orders SET current_shipment_id = NULL WHERE id = $1", c.replacement)
		return err //nolint:wrapcheck // test
	})
	e.mustDeliver(e.orderEvent(c.replacement, order.TopicOrderCreated))
	if n := e.paymentsTotal(c.replacement); n != 0 {
		t.Errorf("chạy lại sau khi gỡ vận đơn: tổng phiếu %d, muốn 0", n)
	}
	e.wantExchangeObligation(c, 177049)
}

// Kho nhận phiếu đổi trước khi order.created của đơn thay thế được giao (hai đối tượng khác nhau, không có thứ tự):
// lần nhận cấn trừ ngay bằng tiền hoàn cuối; order.created tới sau không ghi thêm. Đổi lấy món rẻ hơn → nghĩa vụ dư.
func TestExchangeCredit_ReceivedBeforeCreated(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(150000)
	e.mustReceiveExchange(c)
	e.mustDeliver(e.orderEvent(c.replacement, order.TopicOrderCreated))
	e.wantOneCredit(c)
	if m := e.money(c.replacement); m.cod != 0 || m.prepaid != 150000 {
		t.Errorf("đơn thay thế: %+v", m)
	}
	e.wantExchangeObligation(c, 27049)
}

// Đơn thay thế bị hủy sau khi đã cấn trừ (khách không lấy món mới): phiếu cấn trừ không còn tính cho khách → kho nhận
// món trả thì nghĩa vụ exchange bằng đủ tiền hoàn 177.049; order.created giao lại sau khi hủy không ghi thêm phiếu.
func TestExchangeCredit_ReplacementCancelled(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c, ev := e.credited(180000)
	e.cancel(c.replacement)
	e.mustDeliver(ev)
	e.wantOneCredit(c)
	e.mustReceiveExchange(c)
	e.wantExchangeObligation(c, 177049)
}

// Phiếu đổi lạ trong sự kiện nhận → queue.ErrPermanent (sự kiện trỏ tới phiếu không có).
func TestExchangeCredit_UnknownReturn(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	o := e.orderD2()
	err := e.deliver(e.returnEvent(o.ID, uuid.New(), order.ReturnExchange))
	if !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("phiếu đổi lạ: %v; muốn ErrPermanent", err)
	}
}
