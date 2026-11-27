package reportseed

import (
	"time"

	"github.com/google/uuid"
)

// FakeCarrierFee là phí giả của hãng trên mọi vận đơn của kịch bản.
const FakeCarrierFee = 22_000

// Scenario là kịch bản chín đơn đã dựng: các mã để test so.
type Scenario struct {
	Day                             string
	P1, P2                          uuid.UUID            // Page
	LiveP1, MesP2, Website          uuid.UUID            // nguồn
	An, Binh                        uuid.UUID            // nhân viên
	ASmall, AMedium, PantsB, DressC uuid.UUID            // biến thể
	Orders                          map[string]uuid.UUID // "O1".."O9"
}

// IDs trả mã của các đơn theo tên ("O1"…); tên lạ cho uuid.Nil.
func (sc Scenario) IDs(names ...string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(names))
	for _, n := range names {
		out = append(out, sc.Orders[n])
	}
	return out
}

// NineOrders dựng kịch bản chín đơn của kế hoạch M6 (Task 227) trong ngày day (YYYY-MM-DD giờ Việt Nam), mọi lần đổi
// trạng thái từ 08:00 tới trước 12:00 giờ VN của ngày đó. Ship mỗi đơn có phí 30.000 hoặc 0 đúng bảng của kế hoạch;
// tổng ship thu khách của đơn lên chưa hủy là 150.000 (O1, O2, O4, O8, O9).
func NineOrders(s *Seed, day string) Scenario {
	s.t.Helper()
	d, err := time.ParseInLocation("2006-01-02", day, vn)
	if err != nil {
		s.t.Fatalf("reportseed: ngày kịch bản %q: %v", day, err)
	}
	base := d.Add(8 * time.Hour).UTC()
	own := s.Own()
	sc := Scenario{
		Day: day, P1: s.Page("P1"), P2: s.Page("P2"),
		LiveP1: s.Source("Live trưa P1"), MesP2: s.Source("Mes P2"), Website: s.Source("Website"),
		An: s.Staff("An"), Binh: s.Staff("Bình"),
		ASmall: s.Variant(own, "Áo A", "AOA-S"), AMedium: s.Variant(own, "Áo A", "AOA-M"),
		PantsB: s.Variant(own, "Quần B", "QUANB"), DressC: s.Variant(own, "Váy C", "VAYC"),
		Orders: map[string]uuid.UUID{},
	}
	for i, o := range nineSpecs(sc, base) {
		sc.Orders["O"+string(rune('1'+i))] = s.Order(o)
	}
	return sc
}

// nineSpecs là chín đơn theo thứ tự O1…O9; đơn thứ i bắt đầu lúc base + 20i phút, mỗi bước cách 2 phút.
func nineSpecs(sc Scenario, base time.Time) []OrderSpec {
	at := func(i, step int) time.Time {
		return base.Add(time.Duration(20*i+2*step) * time.Minute)
	}
	steps := func(i int, to ...string) []Step {
		out := make([]Step, len(to))
		for k, st := range to {
			out[k] = Step{To: st, At: at(i, k)}
		}
		return out
	}
	ship := func(i, step int, key, status string, cod int64) ShipmentSpec {
		return ShipmentSpec{CarrierKey: key, Status: status, Fee: FakeCarrierFee, COD: cod, CreatedAt: at(i, step).Add(time.Minute)}
	}
	fbP1 := func(o OrderSpec) OrderSpec {
		o.Channel, o.Page, o.Source, o.CareBy, o.CreatedBy = "facebook", sc.P1, sc.LiveP1, sc.An, sc.An
		return o
	}
	fbP2 := func(o OrderSpec) OrderSpec {
		o.Channel, o.Page, o.Source, o.CareBy, o.CreatedBy = "facebook", sc.P2, sc.MesP2, sc.Binh, sc.Binh
		return o
	}
	received := at(8, 4)
	return []OrderSpec{
		fbP1(OrderSpec{
			Items: []Item{{sc.ASmall, 2, 150_000}}, ShippingFee: 30_000,
			History: steps(0, "new", "confirmed", "delivered"), Shipments: []ShipmentSpec{ship(0, 1, "vtp", "delivered", 330_000)},
		}),
		fbP1(OrderSpec{
			Items: []Item{{sc.PantsB, 1, 250_000}}, ShippingFee: 30_000,
			History: steps(1, "new", "confirmed", "returned"), Shipments: []ShipmentSpec{ship(1, 1, "vtp", "returned", 280_000)},
		}),
		fbP2(OrderSpec{
			Items:     []Item{{sc.AMedium, 1, 150_000}},
			History:   steps(2, "new", "confirmed"),
			Shipments: []ShipmentSpec{ship(2, 1, "spx", "cancelled", 150_000), ship(2, 2, "best", "shipping", 150_000)},
		}),
		{
			Channel: "web", Source: sc.Website, Items: []Item{{sc.DressC, 2, 200_000}}, Discount: 40_000, ShippingFee: 30_000,
			History: steps(3, "new"),
		},
		{
			Channel: "manual", CareBy: sc.Binh, CreatedBy: sc.Binh, Items: []Item{{sc.PantsB, 1, 250_000}},
			History: steps(4, "new", "confirmed"),
		},
		fbP2(OrderSpec{Items: []Item{{sc.DressC, 1, 200_000}}, History: steps(5, "new", "cancelled")}),
		fbP1(OrderSpec{Items: []Item{{sc.ASmall, 1, 150_000}}, History: steps(6, "draft")}),
		fbP1(OrderSpec{
			Items: []Item{{sc.ASmall, 1, 150_000}}, ShippingFee: 30_000,
			History:   steps(7, "draft", "confirmed", "cancel_requested"),
			Shipments: []ShipmentSpec{ship(7, 1, "spx", "cancel_requested", 180_000)},
		}),
		fbP1(OrderSpec{
			Items: []Item{{sc.AMedium, 3, 150_000}}, ShippingFee: 30_000,
			History: steps(8, "new", "confirmed", "delivered"), Shipments: []ShipmentSpec{ship(8, 1, "vtp", "delivered", 480_000)},
			Returns: []ReturnSpec{{
				Kind: "customer_return", Status: "received", ReceivedAt: &received, CreatedAt: at(8, 3),
				Lines: []ReturnLine{{Item: 0, Qty: 1, Sellable: 1, Refund: 150_000}},
			}},
		}),
	}
}
