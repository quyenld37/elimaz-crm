package fulfillment_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Báo phí thật (luồng Task M3-13: RequestQuotes → lệnh nền → dòng ok) rồi gửi hãng với quoteId đó (Task M3-12): kiện
// dựng bằng cùng QuoteParcel nên InputHash khớp; đổi cân nặng hay địa chỉ giao sau khi so phí → ErrQuoteStale.

// quoteShip gọi Service.Ship của quoteEnv (registry có các hãng báo phí).
func (e *quoteEnv) quoteShip(in fulfillment.ShipInput) (fulfillment.ShipmentView, error) {
	return e.shipVia(e.svc, in)
}

// okQuote so phí đơn o (đầu vào mặc định), chạy worker, trả dòng ok rẻ nhất của tài khoản account.
func (e *quoteEnv) okQuote(o order.Order, account uuid.UUID) fulfillment.QuoteRow {
	e.t.Helper()
	acc := e.mustRequest(o, fulfillment.QuoteRequest{AccountIDs: []uuid.UUID{account}})
	e.drain()
	_, rows := e.list(o, &acc.RequestID)
	for _, r := range rows {
		if r.CarrierAccountID == account && r.Status == "ok" {
			return r
		}
	}
	e.t.Fatalf("không có dòng ok của tài khoản %s: %+v", account, rows)
	return fulfillment.QuoteRow{}
}

func TestShip_WithRealQuote(t *testing.T) {
	a, b, _ := vc02Carriers()
	e := newQuoteEnv(t, a, b)
	o, _ := e.order(2)
	q := e.okQuote(o, e.accounts["qa"])
	v, err := e.quoteShip(fulfillment.ShipInput{
		OrderID: o.ID, CarrierAccountID: e.accounts["qa"], Service: q.Service, QuoteID: &q.QuoteID,
	})
	if err != nil {
		t.Fatalf("gửi hãng với báo phí vừa so: %v, muốn thành công", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND quote_id = $2 AND weight_grams = 600",
		v.ID, q.QuoteID); n != 1 {
		t.Fatal("vận đơn không nhớ báo phí hay cân mặc định 300 g × 2 món")
	}
}

func TestShip_RealQuoteStale(t *testing.T) {
	a, b, _ := vc02Carriers()
	e := newQuoteEnv(t, a, b)

	t.Run("cân nặng khác lúc so phí", func(t *testing.T) {
		o, _ := e.order(2)
		q := e.okQuote(o, e.accounts["qa"])
		_, err := e.quoteShip(fulfillment.ShipInput{
			OrderID: o.ID, CarrierAccountID: e.accounts["qa"], Service: q.Service, QuoteID: &q.QuoteID, WeightGrams: 900,
		})
		if !errors.Is(err, fulfillment.ErrQuoteStale) {
			t.Fatalf("đổi cân: %v, muốn ErrQuoteStale", err)
		}
	})
	t.Run("địa chỉ giao đổi sau khi so phí", func(t *testing.T) {
		o, _ := e.order(2)
		q := e.okQuote(o, e.accounts["qb"])
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(),
				`UPDATE ord.orders SET ship_address = jsonb_set(ship_address, '{line}', '"99 Hai Bà Trưng"') WHERE id = $1`, o.ID)
			return err //nolint:wrapcheck // test
		})
		_, err := e.quoteShip(fulfillment.ShipInput{
			OrderID: o.ID, CarrierAccountID: e.accounts["qb"], Service: q.Service, QuoteID: &q.QuoteID,
		})
		if !errors.Is(err, fulfillment.ErrQuoteStale) {
			t.Fatalf("đổi địa chỉ: %v, muốn ErrQuoteStale", err)
		}
	})
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE tenant_id = $1", e.tenant); n != 0 {
		t.Fatalf("%d vận đơn, muốn 0", n)
	}
}
