package customer_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// riskWith trả ngưỡng mặc định của cài đặt fulfillment (Task M3-04: theo dõi từ 1 đơn hoàn; cao từ 2 đơn) với tỷ lệ
// cao highRateBP (mặc định 3000).
func riskWith(highRateBP int) customer.ReturnRisk {
	return customer.ReturnRisk{WatchMinReturned: 1, HighMinReturned: 2, HighRateBP: highRateBP}
}

type wantDelivery struct {
	delivered, returned, rate int
	risk                      string
}

func (e *statsEnv) deliveryStats(id uuid.UUID, rr customer.ReturnRisk) (customer.DeliveryStats, error) {
	e.t.Helper()
	var s customer.DeliveryStats
	err := e.appErr(func(tx pgx.Tx) error {
		var err error
		s, err = customer.DeliveryStatsFor(e.t.Context(), tx, id, rr)
		return err //nolint:wrapcheck // test
	})
	return s, err
}

func (e *statsEnv) appErr(fn func(tx pgx.Tx) error) error {
	e.t.Helper()
	var inner error
	e.app(func(tx pgx.Tx) error {
		inner = fn(tx)
		return nil
	})
	return inner
}

func (e *statsEnv) expectDelivery(label string, id uuid.UUID, rr customer.ReturnRisk, w wantDelivery) {
	e.t.Helper()
	s, err := e.deliveryStats(id, rr)
	if err != nil {
		e.t.Fatalf("%s: %v", label, err)
	}
	hint := map[string]string{
		customer.RiskNone: "", customer.RiskWatch: customer.HintWatch,
		customer.RiskHigh: customer.HintHigh,
	}[w.risk]
	if s.Delivered != w.delivered || s.Returned != w.returned || s.ReturnRateBP != w.rate || s.Risk != w.risk ||
		s.Hint != hint {
		e.t.Errorf("%s: %+v, muốn %+v (hint %q)", label, s, w, hint)
	}
}

// deliveredWith tạo đơn của khách c rồi giao xong, trả cả mã vận đơn.
func (e *statsEnv) deliveredWith(c uuid.UUID, price int64) (order.Order, uuid.UUID) {
	e.t.Helper()
	o := e.confirmed(c, price)
	s := e.attach(o)
	e.step(o, s, order.StepDelivered)
	return o, s
}

// Kế hoạch M3-21 (DH03): số nhận/hoàn do consumer customer_stats của M2 tính; M3 chỉ phân mức theo ngưỡng.
// K1 3 giao, 2 hoàn → {3, 2, 4000, high}; K2 9 giao, 1 hoàn → {9, 1, 1000, watch}; K3 chưa có đơn kết thúc →
// {0, 0, 0, none}. Đảo "đã giao" một đơn của K2 → {8, 1}; đơn đó hoàn → {8, 2, 2000}: với ngưỡng mặc định 30% là
// watch, ngưỡng 20% (đúng biên) là high. Khách trả hàng sau khi nhận không đổi returned (Q-13). HighRateBP 5000 → K1
// watch. Hồ sơ con đọc số của gốc. Không có → ErrNotFound.
func TestDeliveryStatsFor(t *testing.T) {
	t.Parallel()
	e := newStatsEnv(t)
	k1, k2, k3, k4 := e.newCustomer("Khách K1"), e.newCustomer("Khách K2"), e.newCustomer("Khách K3"),
		e.newCustomer("Khách K4")
	for range 3 {
		e.delivered(k1, 100000)
	}
	for range 2 {
		e.carrierReturned(k1, 100000)
	}
	var o2 order.Order
	var s2 uuid.UUID
	for i := range 9 {
		if i == 0 {
			o2, s2 = e.deliveredWith(k2, 100000)
			continue
		}
		e.delivered(k2, 100000)
	}
	e.carrierReturned(k2, 100000)
	e.confirmed(k3, 100000) // chưa kết thúc: không đếm
	o4 := e.delivered(k4, 100000)
	e.feed(false)

	e.expectDelivery("K1", k1, riskWith(3000), wantDelivery{3, 2, 4000, customer.RiskHigh})
	e.expectDelivery("K2", k2, riskWith(3000), wantDelivery{9, 1, 1000, customer.RiskWatch})
	e.expectDelivery("K3", k3, riskWith(3000), wantDelivery{0, 0, 0, customer.RiskNone})
	e.expectDelivery("K1, HighRateBP 5000", k1, riskWith(5000), wantDelivery{3, 2, 4000, customer.RiskWatch})

	// Q-13: khách trả hàng sau khi nhận (phiếu customer_return đã nhận kho) không phải đơn hoàn.
	e.customerReturn(o4, 0)
	e.feed(false)
	e.expectDelivery("K4 sau khi khách trả", k4, riskWith(3000), wantDelivery{1, 0, 0, customer.RiskNone})

	// Hãng đảo "đã giao" đơn o2 của K2 (order.delivery_reversed, payload Changed) → consumer M2 tính lại.
	carrier := order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}
	e.app(func(tx pgx.Tx) error {
		applied, err := e.ow().ReverseDelivery(t.Context(), tx, e.shop.Seller, o2.ID, s2, uuid.New(),
			"carrier_corrected", carrier, e.now)
		if err == nil && !applied {
			t.Error("đảo giao không áp")
		}
		return err //nolint:wrapcheck // test
	})
	e.feed(false)
	e.expectDelivery("K2 sau đảo giao", k2, riskWith(3000), wantDelivery{8, 1, 1111, customer.RiskWatch})

	// Đơn vừa đảo đi đường hoàn của M2 → returned.
	e.app(func(tx pgx.Tx) error {
		if _, err := e.ow().OnShipmentReturning(t.Context(), tx, e.shop.Seller, o2.ID, s2, carrier, e.now); err != nil {
			return err //nolint:wrapcheck // test
		}
		r, err := order.CarrierReturnFor(t.Context(), tx, e.shop.Seller, s2)
		return e.receiveAll(tx, r, err)
	})
	e.feed(false)
	e.expectDelivery("K2 sau hoàn, ngưỡng mặc định", k2, riskWith(3000), wantDelivery{8, 2, 2000, customer.RiskWatch})
	e.expectDelivery("K2 sau hoàn, ngưỡng 20%", k2, riskWith(2000), wantDelivery{8, 2, 2000, customer.RiskHigh})

	// Hồ sơ con (K3 gộp vào K1) đọc số của gốc.
	e.mergeAndFeed(k1, k3)
	e.expectDelivery("K3 sau gộp vào K1", k3, riskWith(3000), wantDelivery{3, 2, 4000, customer.RiskHigh})

	if _, err := e.deliveryStats(uuid.New(), riskWith(3000)); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("khách không có: %v; muốn ErrNotFound", err)
	}
}
