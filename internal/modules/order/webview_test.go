package order_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

// getWeb đọc đơn web ref của người bán e.seller lúc at (crm_app, tenant của e).
func (e *reqEnv) getWeb(ref string, at time.Time) (order.WebOrderView, error) {
	return inApp(e, func(tx pgx.Tx) (order.WebOrderView, error) {
		return order.GetWebOrder(e.t.Context(), tx, e.seller, ref, at)
	})
}

func (e *reqEnv) mustGetWeb(ref string, at time.Time) order.WebOrderView {
	e.t.Helper()
	v, err := e.getWeb(ref, at)
	if err != nil {
		e.t.Fatalf("GetWebOrder %s: %v", ref, err)
	}
	return v
}

// webOrderIntent tạo đơn kênh web ref (ý định intent: new hay confirmed) chưa giao.
func (e *reqEnv) webOrderIntent(ref, intent string, lines ...order.LineInput) order.Order {
	e.t.Helper()
	in := e.m2Input(30000, lines...)
	in.Channel, in.ExternalRef, in.Intent = order.ChannelWeb, ref, intent
	return e.mustCreateM2(in).Order
}

func (e *reqEnv) cancelWeb(ref, note string) (order.CancelOutcome, error) {
	return inApp(e, func(tx pgx.Tx) (order.CancelOutcome, error) {
		return e.w().CancelFromWeb(e.t.Context(), tx, e.seller, ref, note, e.key, e.now())
	})
}

// [WB03]: đơn web confirmed 2 dòng → đủ trường, ba trục, chưa vận đơn, hủy được; gắn vận đơn → CurrentShipmentID, vẫn
// cancellable (hủy thành yêu cầu). Đơn new chờ duyệt giảm → PendingReason để bên gọi gộp nhãn. Đơn đã giao lúc D: deliveredAt, exchangeUntil = D + 7 ngày, exchangeableQty = số mua trừ
// yêu cầu đổi đang mở; quá hạn → 0.
func TestGetWebOrder_Fields(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	a, b := e.variant(250000, 10), e.variant(180000, 10)
	o := e.webOrderIntent("W-1", order.IntentConfirmed, ln(a, 2, 20000), ln(b, 1, 0))
	v := e.mustGetWeb("W-1", e.now())
	if v.ExternalRef != "W-1" || v.Code != o.Code || v.Status != order.StatusConfirmed ||
		v.PendingReason != "" || v.PackingStatus != "none" || v.CurrentShipmentID != nil || !v.Cancellable || v.PriceReview ||
		v.Merchandise != o.Subtotal || v.Discount != o.DiscountTotal || v.ShippingFee != 30000 ||
		v.GrandTotal != o.GrandTotal || !v.CreatedAt.Equal(o.CreatedAt) || v.DeliveredAt != nil || v.ExchangeUntil != nil {
		t.Fatalf("đơn web: %+v (đơn %+v)", v, o)
	}
	if len(v.Lines) != 2 {
		t.Fatalf("dòng: %+v", v.Lines)
	}
	for i, it := range o.Items {
		l := v.Lines[i]
		if l.VariantID != it.VariantID || l.SKU != it.SKU || l.Name != it.Name || l.Qty != int(it.Qty) ||
			l.UnitPrice != it.UnitPrice || l.Net != it.NetAmount || l.ExchangeableQty != 0 {
			t.Fatalf("dòng %d: %+v, đơn %+v", i, l, it)
		}
	}

	s := e.shipM3(o, m3Ship{carrier: "fake_web", status: "awaiting_pickup", packing: "packed"})
	v = e.mustGetWeb("W-1", e.now())
	if v.CurrentShipmentID == nil || *v.CurrentShipmentID != s || v.PackingStatus != "packed" || !v.Cancellable {
		t.Fatalf("có vận đơn %s: %+v", s, v)
	}

	in := e.m2Input(30000, ln(a, 1, 0))
	in.Channel, in.ExternalRef, in.Intent = order.ChannelWeb, "W-2", order.IntentNew
	e.mustCreateM2(in)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE ord.orders SET pending_reason = 'discount_approval' WHERE external_ref = 'W-2'")
		return err //nolint:wrapcheck // test
	})
	if v := e.mustGetWeb("W-2", e.now()); v.Status != order.StatusNew || v.PendingReason != "discount_approval" ||
		!v.Cancellable {
		t.Fatalf("đơn new chờ duyệt giảm: %+v", v)
	}

	e.webOrder("W-3", true, ln(a, 3, 0), ln(b, 1, 0))
	e.mustRequest("W-3", exchange(a, 1, "FIT"), e.now())
	d := e.now()
	v = e.mustGetWeb("W-3", d.Add(time.Hour))
	if v.DeliveredAt == nil || !v.DeliveredAt.Equal(d) || v.ExchangeUntil == nil ||
		!v.ExchangeUntil.Equal(d.Add(7*24*time.Hour)) || v.Cancellable || v.Status != order.StatusDelivered {
		t.Fatalf("đơn đã giao: %+v", v)
	}
	if v.Lines[0].ExchangeableQty != 2 || v.Lines[1].ExchangeableQty != 1 {
		t.Fatalf("số còn đổi: %+v", v.Lines)
	}
	if v = e.mustGetWeb("W-3", d.Add(8*24*time.Hour)); v.Lines[0].ExchangeableQty != 0 || v.Lines[1].ExchangeableQty != 0 {
		t.Fatalf("quá hạn đổi vẫn còn số: %+v", v.Lines)
	}
}

// [WB03], QĐ25: đơn nhập tay có mã ngoài → ErrNotFound; mã không có → ErrNotFound; tenant khác có đơn web cùng mã ngoài
// không đọc được đơn của A (và ngược lại). JSON của bản xem không chứa SĐT, địa chỉ, tên người nhận.
func TestGetWebOrder_OnlyOwnWeb(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	in := e.m2Input(30000, ln(v, 1, 0))
	in.ExternalRef = "M-1"
	e.mustCreateM2(in)
	for _, ref := range []string{"M-1", "KHONG-CO"} {
		if _, err := e.getWeb(ref, e.now()); !errors.Is(err, order.ErrNotFound) {
			t.Fatalf("%s: lỗi %v, mong ErrNotFound", ref, err)
		}
	}
	e.webOrder("W-1", false, ln(v, 1, 0))
	b := e.staff.Tenant(t, "B")
	err := e.staff.TryExec(t.Context(), b, func(tx pgx.Tx) error {
		_, err := order.GetWebOrder(t.Context(), tx, e.seller, "W-1", e.now())
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("tenant B đọc đơn của A: %v", err)
	}
	raw, err := json.Marshal(e.mustGetWeb("W-1", e.now()))
	if err != nil {
		t.Fatal(err)
	}
	addr := fullAddress()
	for _, pii := range []string{string(addr.Phone), string(addr.Line), addr.Name, "0901", "Lê Lợi"} {
		if strings.Contains(string(raw), pii) {
			t.Fatalf("bản xem đơn web lộ %q: %s", pii, raw)
		}
	}
}

// Hủy đơn new chưa vận đơn → Cancelled, đơn cancelled lý do customer_changed_mind, ghi chú của khách, giữ hàng held về
// 0; gọi lại → ErrCannotCancel. Đơn confirmed chưa vận đơn → hủy được.
func TestCancelFromWeb_Direct(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	for _, c := range []struct{ ref, intent string }{{"W-1", order.IntentNew}, {"W-2", order.IntentConfirmed}} {
		o := e.webOrderIntent(c.ref, c.intent, ln(v, 2, 0))
		if e.heldOf(o.ID, "held") != 2 {
			t.Fatalf("%s: giữ hàng trước hủy %d", c.ref, e.heldOf(o.ID, "held"))
		}
		out, err := e.cancelWeb(c.ref, "  Đặt nhầm size  ")
		if err != nil || !out.Cancelled || out.RequestRef != "" || out.Request != nil ||
			out.Before.Status != c.intent || out.After.Status != order.StatusCancelled {
			t.Fatalf("%s: hủy %+v, %v", c.ref, out, err)
		}
		g := e.orderEnv.get(o.ID)
		if g.Status != order.StatusCancelled || g.CancelReason == nil || *g.CancelReason != order.CancelCustomerChangedMind ||
			g.CancelNote == nil || *g.CancelNote != "Đặt nhầm size" || e.heldOf(o.ID, "held") != 0 {
			t.Fatalf("%s: đơn sau hủy %+v", c.ref, g)
		}
		_, err = e.cancelWeb(c.ref, "")
		wantErr(t, c.ref+" hủy lại", err, order.ErrCannotCancel)
	}
	if b := e.balance(v); b.Held != 0 {
		t.Fatalf("số dư held sau hai lần hủy: %+v", b)
	}
}

// [WB04]: đơn có vận đơn awaiting_pickup → không hủy, tạo một yêu cầu hủy (received, lời nhắn của khách), đơn giữ
// nguyên confirmed; bấm lại → cùng requestRef, vẫn một yêu cầu. Đơn cancel_requested (nhân viên đã yêu cầu hãng hủy)
// → yêu cầu hủy. Đơn delivered → ErrCannotCancel; mã không có, đơn tay → ErrNotFound; lời nhắn 501 ký tự →
// ErrInvalidInput.
func TestCancelFromWeb_Request(t *testing.T) {
	t.Parallel()
	e := newReqEnv(t)
	v := e.variant(200000, 10)
	o := e.webOrderIntent("W-1", order.IntentConfirmed, ln(v, 1, 0))
	e.shipM3(o, m3Ship{carrier: "fake_web", status: "awaiting_pickup", packing: "packed"})
	out, err := e.cancelWeb("W-1", "Em không cần nữa")
	if err != nil || out.Cancelled || out.RequestRef == "" || out.Request == nil || out.Request.Ref != out.RequestRef {
		t.Fatalf("đơn có vận đơn: %+v, %v", out, err)
	}
	if g := e.orderEnv.get(o.ID); g.Status != order.StatusConfirmed {
		t.Fatalf("đơn có vận đơn bị đổi trạng thái: %s", g.Status)
	}
	d := e.get(out.Request.ID)
	if d.Kind != order.RequestCancel || d.Status != order.RequestReceived || d.CustomerNote != "Em không cần nữa" {
		t.Fatalf("yêu cầu hủy: %+v", d)
	}
	again, err := e.cancelWeb("W-1", "")
	if err != nil || again.Cancelled || again.RequestRef != out.RequestRef || again.Request != nil {
		t.Fatalf("bấm lại: %+v, %v", again, err)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_requests WHERE order_id = $1`, o.ID); n != 1 {
		t.Fatalf("số yêu cầu hủy: %d", n)
	}

	o2 := e.webOrderIntent("W-2", order.IntentConfirmed, ln(v, 1, 0))
	e.shipM3(o2, m3Ship{carrier: "fake_web", status: "awaiting_pickup", packing: "packed"})
	e.setStatusOwner(o2, order.StatusCancelRequested)
	if out, err := e.cancelWeb("W-2", ""); err != nil || out.Cancelled || out.RequestRef == "" {
		t.Fatalf("đơn cancel_requested: %+v, %v", out, err)
	}

	e.webOrder("W-3", true, ln(v, 1, 0))
	_, err = e.cancelWeb("W-3", "")
	wantErr(t, "đơn đã giao", err, order.ErrCannotCancel)
	in := e.m2Input(30000, ln(v, 1, 0))
	in.ExternalRef = "M-1"
	e.mustCreateM2(in)
	for _, ref := range []string{"M-1", "KHONG-CO"} {
		_, err = e.cancelWeb(ref, "")
		wantErr(t, ref, err, order.ErrNotFound)
	}
	_, err = e.cancelWeb("W-2", strings.Repeat("a", 501))
	wantErr(t, "lời nhắn dài", err, order.ErrInvalidInput)
}
