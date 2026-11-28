package cash_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Task 524: luật "đơn đổi còn phải cấn trừ" lúc gửi hãng là luật của consumer cash_refunds (một nguồn, creditDue).

// pending đọc cash.ExchangeCreditPending của đơn id trong một transaction crm_app.
func (e *payEnv) pending(id uuid.UUID) bool {
	e.t.Helper()
	var p bool
	if err := e.app(func(tx pgx.Tx) (err error) {
		p, err = cash.ExchangeCreditPending(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("đọc còn phải cấn trừ: %v", err)
	}
	return p
}

// Đơn đổi vừa tạo (phiếu đổi có tiền hoàn 177.049, chưa có phiếu cấn trừ) → còn phải cấn trừ; sau consumer → không.
// Đơn thường → không. Đơn đổi lấy món rẻ hơn sau consumer (đã thu đủ) → không.
func TestExchangeCreditPending_Rules(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(180000)
	if !e.pending(c.replacement) {
		t.Fatal("đơn đổi chưa cấn trừ: muốn còn phải cấn trừ")
	}
	if e.pending(c.orig.ID) {
		t.Error("đơn gốc (không phải đơn đổi): muốn không")
	}
	e.mustDeliver(e.orderEvent(c.replacement, order.TopicOrderCreated))
	if e.pending(c.replacement) {
		t.Error("đã có phiếu cấn trừ: muốn không")
	}
	if e.pending(e.orderD2().ID) {
		t.Error("đơn thường: muốn không")
	}
	cheap := e.exchanged(150000)
	e.mustDeliver(e.orderEvent(cheap.replacement, order.TopicOrderCreated))
	if e.pending(cheap.replacement) {
		t.Error("đơn đổi rẻ hơn đã cấn trừ đủ: muốn không")
	}
}

// Kho đã nhận phiếu đổi khi đơn thay thế đang gửi hãng (phần tiền chốt thành nghĩa vụ exchange đủ tiền hoàn), vận đơn
// bị gỡ → gửi lại không bị chặn (không trả tiền hoàn hai lần, như applyCredit). Đơn thay thế đã hủy → không.
func TestExchangeCreditPending_SettledOrCancelled(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(180000)
	e.shipReplacement(c)
	e.mustReceiveExchange(c)
	e.wantExchangeObligation(c, 177049)
	e.detachShipment(c.replacement)
	if e.pending(c.replacement) {
		t.Error("đã chốt nghĩa vụ exchange: muốn không")
	}
	d := e.exchanged(180000)
	e.cancel(d.replacement)
	if e.pending(d.replacement) {
		t.Error("đơn thay thế đã hủy: muốn không")
	}
}

// errStillPending là lỗi test dùng để lùi lần gửi hãng khi còn phải cấn trừ (như createShipment trả 409).
var errStillPending = errors.New("còn phải cấn trừ")

// shipIfCredited gửi hãng đơn id như createShipment: Ship (khóa đơn) rồi kiểm còn phải cấn trừ trong cùng
// transaction; còn → errStillPending, transaction lùi.
func (e *payEnv) shipIfCredited(id uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		if _, err := e.shop.Shipment(e.t.Context(), tx, e.seller, id, e.user); err != nil {
			return err //nolint:wrapcheck // test
		}
		p, err := cash.ExchangeCreditPending(e.t.Context(), tx, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if p {
			return errStillPending
		}
		return nil
	})
}

// Gửi hãng (Ship rồi kiểm dưới khóa đơn, như createShipment) chạy song song với consumer cấn trừ: vận đơn đã commit thì
// luôn đã có đúng một phiếu cấn trừ; consumer không bao giờ gặp ErrCODLockedByShipment; COD của vận đơn là COD sau cấn
// trừ.
func TestExchangeCreditPending_ConcurrentShipAndCredit(t *testing.T) {
	t.Parallel()
	e := newPayEnv(t)
	c := e.exchanged(180000)
	ev := e.orderEvent(c.replacement, order.TopicOrderCreated)
	const n = 4
	var wg sync.WaitGroup
	shipErrs, creditErrs := make([]error, n), make([]error, n)
	for i := range n {
		wg.Go(func() { shipErrs[i] = e.shipIfCredited(c.replacement) })
		wg.Go(func() { creditErrs[i] = e.deliver(ev) })
	}
	wg.Wait()
	for i, err := range creditErrs {
		if err != nil {
			t.Errorf("consumer lần %d: %v", i, err)
		}
	}
	shipped := 0
	for _, err := range shipErrs {
		if err == nil {
			shipped++
		}
	}
	e.wantOneCredit(c)
	var cod int64
	var count int
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*), coalesce(max(cod_amount), 0) FROM fulfillment.shipments
			WHERE order_id = $1`, c.replacement).Scan(&count, &cod)
	})
	if count != shipped || shipped > 1 {
		t.Fatalf("%d vận đơn, %d lần gửi qua", count, shipped)
	}
	if shipped == 1 && cod != 2951 {
		t.Errorf("COD vận đơn %d, muốn 2.951 (sau cấn trừ)", cod)
	}
}
