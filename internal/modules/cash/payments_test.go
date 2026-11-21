package cash_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// payEnv là codEnv cộng module tiền (Service, consumer cash_refunds) và order.Writer dùng chung sổ sự kiện.
type payEnv struct {
	*codEnv
	svc     *cash.Service
	refunds events.Consumer
	w       order.Writer
}

func newPayEnv(t *testing.T) *payEnv {
	t.Helper()
	e := &payEnv{codEnv: newCODEnv(t)}
	reg, bus := events.NewRegistry(), &realtimetest.RecordingBus{}
	m := cash.New(cash.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Events: reg, Bus: bus})
	e.svc = m.Service
	for _, c := range m.Consumers() {
		if c.Name() == cash.RefundsConsumerName {
			e.refunds = c
		}
	}
	if e.refunds == nil {
		t.Fatalf("không có consumer %s", cash.RefundsConsumerName)
	}
	e.w = order.Writer{Events: reg, Queue: queue.NewQueue(), Realtime: bus}
	return e
}

func (e *payEnv) actor() cash.Actor { return cash.Actor{Type: cash.ActorStaff, ID: e.user} }

func (e *payEnv) orderActor() order.Actor { return order.Actor{TenantID: e.tenant, UserID: e.user} }

// variant tạo biến thể giá price, tồn onHand ở kho chính của người bán e.seller (chủ bảng).
func (e *payEnv) variant(price int64, onHand int32) uuid.UUID {
	e.t.Helper()
	var v uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		v = e.shop.Variant(e.t, tx, e.seller, price, onHand)
		return nil
	})
	return v
}

// orderD2 tạo đơn D2 của kế hoạch: 2 cái giá 300.000 + phí ship 30.000 = 630.000, confirmed.
func (e *payEnv) orderD2() order.Order {
	e.t.Helper()
	v := e.variant(300000, 10)
	var o order.Order
	if err := e.app(func(tx pgx.Tx) (err error) {
		o, err = e.shop.Order(e.t.Context(), tx, e.seller, e.user, 30000, order.ItemInput{VariantID: v, Qty: 2})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo đơn D2: %v", err)
	}
	if o.Status != order.StatusConfirmed || o.GrandTotal != 630000 || o.CODAmount != 630000 {
		e.t.Fatalf("đơn D2: %s grand %d cod %d", o.Status, o.GrandTotal, o.CODAmount)
	}
	return o
}

func (e *payEnv) record(orderID uuid.UUID, method string, amount int64) (cash.PaymentView, error) {
	var out cash.PaymentView
	err := e.app(func(tx pgx.Tx) (err error) {
		out, err = e.svc.RecordPayment(e.t.Context(), tx, e.tenant, cash.PaymentInput{
			OrderID: orderID, Method: method, Amount: amount, Reference: "CK-0001",
		}, e.actor())
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *payEnv) mustRecord(orderID uuid.UUID, method string, amount int64) cash.PaymentView {
	e.t.Helper()
	p, err := e.record(orderID, method, amount)
	if err != nil {
		e.t.Fatalf("ghi phiếu %s %d: %v", method, amount, err)
	}
	return p
}

// orderMoney là phần tiền của đơn đọc qua ord.orders_v.
type orderMoney struct {
	status, paymentStatus string
	prepaid, cod          int64
	version               int64
}

func (e *payEnv) money(id uuid.UUID) orderMoney {
	e.t.Helper()
	var m orderMoney
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, payment_status, prepaid_total, cod_amount, version
			FROM ord.orders_v WHERE id = $1`, id).Scan(&m.status, &m.paymentStatus, &m.prepaid, &m.cod, &m.version)
	}); err != nil {
		e.t.Fatalf("đọc đơn: %v", err)
	}
	return m
}

// paymentsTotal là tổng mọi phiếu của đơn.
func (e *payEnv) paymentsTotal(id uuid.UUID) int64 {
	e.t.Helper()
	return e.count("SELECT coalesce(sum(amount), 0)::bigint FROM cash.payments WHERE order_id = $1", id)
}

// D2 cọc 200.000 → cod 430.000, partial, prepaid 200.000, một payment.received; gửi hãng → vận đơn COD 430.000; sau
// khi gửi hãng ghi thêm chuyển khoản → ErrCODLockedByShipment, không thêm phiếu, đơn không đổi.
func TestRecordPayment_DepositBeforeShipment(t *testing.T) {
	e := newPayEnv(t)
	o := e.orderD2()
	p := e.mustRecord(o.ID, cash.MethodDeposit, 200000)
	if p.OrderID != o.ID || p.Method != cash.MethodDeposit || p.Amount != 200000 || p.Reference != "CK-0001" ||
		p.ActorID != e.user || p.ReceivedAt.IsZero() || p.RefundObligationID != nil {
		t.Errorf("phiếu: %+v", p)
	}
	if m := e.money(o.ID); m.cod != 430000 || m.prepaid != 200000 || m.paymentStatus != order.PaymentPartial {
		t.Errorf("đơn sau cọc: %+v", m)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = $1 AND aggregate_type = 'payment'
		AND aggregate_id = $2`, cash.TopicPaymentReceived, p.ID.String()); n != 1 {
		t.Errorf("%d sự kiện %s", n, cash.TopicPaymentReceived)
	}
	var shipCOD int64
	if err := e.app(func(tx pgx.Tx) error {
		s, err := e.shop.Shipment(t.Context(), tx, e.seller, o.ID, e.user)
		shipCOD = s.CODAmount
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if shipCOD != 430000 {
		t.Errorf("vận đơn COD %d, muốn 430.000", shipCOD)
	}
	before := e.money(o.ID)
	if _, err := e.record(o.ID, cash.MethodBankTransfer, 100000); !errors.Is(err, cash.ErrCODLockedByShipment) {
		t.Errorf("chuyển khoản sau khi gửi hãng: %v; muốn ErrCODLockedByShipment", err)
	}
	if got := e.money(o.ID); got != before || e.paymentsTotal(o.ID) != 200000 {
		t.Errorf("đơn bị đổi: %+v (trước %+v), tổng phiếu %d", got, before, e.paymentsTotal(o.ID))
	}
}

// Đầu vào sai, vượt tổng đơn, đơn lạ, đơn đã hủy bị từ chối, không ghi phiếu; trả đủ 630.000 → cod 0, paid.
func TestRecordPayment_Refusals(t *testing.T) {
	e := newPayEnv(t)
	o := e.orderD2()
	for _, c := range []struct {
		name   string
		order  uuid.UUID
		method string
		amount int64
		want   error
	}{
		{"phương thức refund", o.ID, "refund", 1000, cash.ErrInvalidInput},
		{"phương thức exchange_credit", o.ID, "exchange_credit", 1000, cash.ErrInvalidInput},
		{"phương thức lạ", o.ID, "momo", 1000, cash.ErrInvalidInput},
		{"số 0", o.ID, cash.MethodCash, 0, cash.ErrInvalidInput},
		{"số âm", o.ID, cash.MethodCash, -1, cash.ErrInvalidInput},
		{"quá giới hạn tiền", o.ID, cash.MethodCash, 9007199254740992, cash.ErrInvalidInput},
		{"vượt tổng đơn", o.ID, cash.MethodBankTransfer, 630001, cash.ErrPaymentExceedsTotal},
		{"đơn lạ", uuid.New(), cash.MethodCash, 1000, cash.ErrOrderNotFound},
	} {
		if _, err := e.record(c.order, c.method, c.amount); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; muốn %v", c.name, err, c.want)
		}
	}
	if n := e.paymentsTotal(o.ID); n != 0 {
		t.Fatalf("có phiếu sau các lần từ chối: %d", n)
	}
	e.mustRecord(o.ID, cash.MethodCash, 30000)
	e.mustRecord(o.ID, cash.MethodBankTransfer, 600000)
	if m := e.money(o.ID); m.cod != 0 || m.prepaid != 630000 || m.paymentStatus != order.PaymentPaid {
		t.Errorf("trả đủ: %+v", m)
	}
	if _, err := e.record(o.ID, cash.MethodCash, 1); !errors.Is(err, cash.ErrPaymentExceedsTotal) {
		t.Errorf("trả thêm khi đã đủ: %v; muốn ErrPaymentExceedsTotal", err)
	}

	cancelled := e.orderD2()
	e.cancel(cancelled.ID)
	if _, err := e.record(cancelled.ID, cash.MethodCash, 1000); !errors.Is(err, cash.ErrPaymentNotAllowed) {
		t.Errorf("đơn đã hủy: %v; muốn ErrPaymentNotAllowed", err)
	}
}

// Phiếu của đơn mới nhất trước, trang theo mã phiếu cuối; đơn lạ → ErrOrderNotFound.
func TestListOrderPayments(t *testing.T) {
	e := newPayEnv(t)
	o := e.orderD2()
	a := e.mustRecord(o.ID, cash.MethodDeposit, 100000)
	b := e.mustRecord(o.ID, cash.MethodCash, 50000)
	var (
		page1, page2 []cash.PaymentView
		next1, next2 *uuid.UUID
	)
	if err := e.app(func(tx pgx.Tx) (err error) {
		if page1, next1, err = e.svc.ListOrderPayments(t.Context(), tx, o.ID, nil, 1); err != nil {
			return err //nolint:wrapcheck // test
		}
		page2, next2, err = e.svc.ListOrderPayments(t.Context(), tx, o.ID, next1, 1)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(page1) != 1 || page1[0].ID != b.ID || next1 == nil || *next1 != b.ID {
		t.Errorf("trang 1: %+v next %v", page1, next1)
	}
	if len(page2) != 1 || page2[0].ID != a.ID || next2 != nil {
		t.Errorf("trang 2: %+v next %v", page2, next2)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := e.svc.ListOrderPayments(t.Context(), tx, uuid.New(), nil, 10)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, cash.ErrOrderNotFound) {
		t.Errorf("đơn lạ: %v; muốn ErrOrderNotFound", err)
	}
}

// cancel hủy đơn id (customer_changed_mind) bằng order.Writer.Cancel của M2.
func (e *payEnv) cancel(id uuid.UUID) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		o, err := order.Get(e.t.Context(), tx, e.seller, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = e.w.Cancel(e.t.Context(), tx, e.seller, id, o.Version, order.CancelCustomerChangedMind, "",
			e.orderActor(), time.Now().UTC())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("hủy đơn: %v", err)
	}
}
