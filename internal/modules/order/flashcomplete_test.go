package order_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Hoàn tất đơn nháp flash sale (kế hoạch M5, Task 202 / M5-19; QĐ3, QĐ7; ca FS15, FS17).

// newFlashWebEnv là flashEnv có danh mục hành chính mẫu (địa chỉ đủ tỉnh, xã cho lúc hoàn tất).
func newFlashWebEnv(t *testing.T) *flashEnv {
	t.Helper()
	f := newFlashEnv(t)
	f.staff.SeedAdminUnits(t)
	return f
}

// completeIn là lần hoàn tất của đơn res với SĐT phone: địa chỉ đủ, phí ship 30.000, hãng dự phòng khớp phí.
func (f *flashEnv) completeIn(res order.FlashBuyResult, phone string) order.CompleteFlashInput {
	return order.CompleteFlashInput{
		ExternalRef: res.ExternalRef, Phone: logging.Phone(phone), ShipAddress: *fullAddress(), ShippingFee: 30000,
		Shipping: &order.WebShipChoice{Fee: 30000}, Note: "Giao giờ hành chính",
	}
}

// complete chạy CompleteFlashDraft trong một transaction crm_app; ErrHoldExpired là kết quả cuối nên vẫn commit (như
// điểm cuối lưu câu trả lời).
func (f *flashEnv) complete(in order.CompleteFlashInput, now time.Time) (order.WebOrderResult, error) {
	var res order.WebOrderResult
	var final error
	err := f.app(func(tx pgx.Tx) error {
		var err error
		res, err = f.writer().CompleteFlashDraft(f.t.Context(), tx, in, f.service, now)
		final = nil
		if errors.Is(err, order.ErrHoldExpired) {
			final = err
			return nil
		}
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		return res, err
	}
	return res, final
}

// [FS15] Hoàn tất: SĐT khác → ErrNotFound, không đổi gì; đúng SĐT (dạng nhập khác) → đơn new, địa chỉ, phí ship, tổng
// = giá flash + phí, COD bằng tổng, hãng khách chọn, ghi chú; đơn vị sold; held của số dư không đổi; lịch sử draft → new;
// gọi lại → cùng đơn (Replayed), không ghi thêm; đối chiếu không lệch.
func TestFlashComplete_Phone(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	s := f.schedule(3, 1, 10)
	res := f.mustBuy(f.in(s, f.phoneCustomer("0912345001"), 1))
	heldBefore := f.balance(s.variant).Held
	at := f.start.Add(5 * time.Minute)
	if _, err := f.complete(f.completeIn(res, "0912345002"), at); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("SĐT khác: %v; mong ErrNotFound", err)
	}
	unknown := f.completeIn(res, "0912345001")
	unknown.ExternalRef = "khong-co"
	if _, err := f.complete(unknown, at); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("mã ngoài lạ: %v; mong ErrNotFound", err)
	}
	if o := f.getOrder(res.OrderID); o.Status != order.StatusDraft {
		t.Fatalf("sau SĐT sai: đơn %s", o.Status)
	}
	got, err := f.complete(f.completeIn(res, "+84 912 345 001"), at)
	if err != nil {
		t.Fatalf("hoàn tất: %v", err)
	}
	if got.OrderID != res.OrderID || got.Code != res.Code || got.Status != order.StatusNew || got.Replayed ||
		got.GrandTotal != 199000+30000 {
		t.Fatalf("kết quả %+v", got)
	}
	o := f.getOrder(res.OrderID)
	if o.Status != order.StatusNew || !o.AddressComplete || o.ShippingFee != 30000 || o.GrandTotal != 229000 ||
		o.CODAmount != 229000 || o.Channel != order.ChannelWeb {
		t.Errorf("đơn %+v", o)
	}
	if f.units(s.item, "sold") != 1 || f.units(s.item, "held") != 0 {
		t.Errorf("đơn vị sold %d, held %d", f.units(s.item, "sold"), f.units(s.item, "held"))
	}
	if st := f.reservationState(res.OrderID); st != "held" {
		t.Errorf("giữ hàng %s; mong held tới lúc hãng lấy hàng", st)
	}
	if b := f.balance(s.variant); b.Held != heldBefore {
		t.Errorf("held %d, mong %d", b.Held, heldBefore)
	}
	if n := f.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'draft'
		AND to_status = 'new'`, res.OrderID); n != 1 {
		t.Errorf("lịch sử draft → new: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND web_shipping IS NOT NULL
		AND submitted_via = 'sync'`, res.OrderID); n != 1 {
		t.Errorf("cột web của đơn chưa ghi")
	}
	if n := f.count(`SELECT count(*) FROM ord.order_notes WHERE order_id = $1`, res.OrderID); n != 1 {
		t.Errorf("ghi chú: %d", n)
	}
	again, err := f.complete(f.completeIn(res, "0912345001"), at.Add(time.Minute))
	if err != nil || again.OrderID != res.OrderID || !again.Replayed || again.Status != order.StatusNew {
		t.Fatalf("gọi lại: %+v, %v", again, err)
	}
	if v := f.getOrder(res.OrderID).Version; v != o.Version {
		t.Errorf("gọi lại đổi version %d → %d", o.Version, v)
	}
	f.mustReconcile()
}

// [FS15] Hoàn tất sau hạn giữ (lịch chưa kịp hủy) → ErrHoldExpired, đơn cancelled draft_expired trong cùng
// transaction, đơn vị về available (đợt còn mở), claims 0; gọi lại → vẫn ErrHoldExpired.
func TestFlashComplete_Expired(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	s := f.schedule(3, 1, 10)
	res := f.mustBuy(f.in(s, f.phoneCustomer("0912345011"), 1))
	late := f.start.Add(11 * time.Minute) // hạn = start + 1 phút + 10 phút: đúng hạn là hết
	if _, err := f.complete(f.completeIn(res, "0912345011"), late); !errors.Is(err, order.ErrHoldExpired) {
		t.Fatalf("sau hạn: %v; mong ErrHoldExpired", err)
	}
	o := f.getOrder(res.OrderID)
	if o.Status != order.StatusCancelled || o.CancelReason == nil || *o.CancelReason != order.CancelDraftExpired {
		t.Fatalf("đơn %s %v", o.Status, o.CancelReason)
	}
	if f.units(s.item, "available") != 3 || f.claimed(s.item) != 0 {
		t.Errorf("đơn vị trống %d, claims %d", f.units(s.item, "available"), f.claimed(s.item))
	}
	if _, err := f.complete(f.completeIn(res, "0912345011"), late); !errors.Is(err, order.ErrHoldExpired) {
		t.Fatalf("gọi lại: %v; mong ErrHoldExpired", err)
	}
	f.mustReconcile()
}

// [FS17] Đợt đóng khi khách đang giữ, khách hoàn tất trong hạn → đơn new, đơn vị sold; held của số dư không đổi tới
// lúc hãng lấy hàng; đối chiếu không lệch. Hủy sau đó (đợt đã đóng): đơn vị giữ sold, held − 1.
func TestFlashComplete_AfterClose(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	s := f.schedule(3, 1, 10)
	res := f.mustBuy(f.in(s, f.phoneCustomer("0912345021"), 1))
	f.mustClose(s)
	before := f.balance(s.variant)
	got, err := f.complete(f.completeIn(res, "0912345021"), f.start.Add(5*time.Minute))
	if err != nil || got.Status != order.StatusNew {
		t.Fatalf("hoàn tất: %+v, %v", got, err)
	}
	if f.units(s.item, "sold") != 1 || f.balance(s.variant).Held != before.Held {
		t.Errorf("sold %d, held %d → %d", f.units(s.item, "sold"), before.Held, f.balance(s.variant).Held)
	}
	f.mustReconcile()
	if err := f.cancelOrder(res.OrderID); err != nil {
		t.Fatalf("hủy: %v", err)
	}
	if f.units(s.item, "sold") != 1 || f.balance(s.variant).Held != before.Held-1 {
		t.Errorf("sau hủy: sold %d, held %d; mong 1, %d", f.units(s.item, "sold"), f.balance(s.variant).Held, before.Held-1)
	}
	f.mustReconcile()
}

// Đầu vào sai: thiếu mã ngoài, SĐT sai dạng, ghi chú quá dài → ErrInvalidInput; địa chỉ thiếu xã →
// ErrAddressIncomplete; khách trả phí hơn phí hãng → ErrShipFeeMismatch; đơn web thường (không flash) → ErrNotFound.
// Không ca nào đổi đơn.
func TestFlashComplete_Rejects(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	s := f.schedule(3, 1, 10)
	res := f.mustBuy(f.in(s, f.phoneCustomer("0912345031"), 1))
	at := f.start.Add(5 * time.Minute)
	base := f.completeIn(res, "0912345031")
	noRef, badPhone, longNote, noWard, fee := base, base, base, base, base
	noRef.ExternalRef = " "
	badPhone.Phone = "12ab"
	longNote.Note = string(make([]rune, 2001))
	noWard.ShipAddress.WardCode = ""
	fee.Shipping = &order.WebShipChoice{Fee: 20000}
	for name, c := range map[string]struct {
		in   order.CompleteFlashInput
		want error
	}{
		"thiếu mã ngoài": {noRef, order.ErrInvalidInput}, "SĐT sai": {badPhone, order.ErrInvalidInput},
		"ghi chú dài": {longNote, order.ErrInvalidInput}, "thiếu xã": {noWard, order.ErrAddressIncomplete},
		"phí lệch": {fee, order.ErrShipFeeMismatch},
	} {
		if _, err := f.complete(c.in, at); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; mong %v", name, err, c.want)
		}
	}
	if o := f.getOrder(res.OrderID); o.Status != order.StatusDraft || o.Version != 1 {
		t.Errorf("đơn %s v%d sau các lần bị từ chối", o.Status, o.Version)
	}
}

// Đơn không phải đơn nháp flash của website → ErrNotFound (không lộ đơn): đơn web có dòng thường (bỏ món flash bằng
// owner), đơn kênh khác web (đổi kênh bằng owner). Không đổi đơn.
func TestFlashComplete_NotFlashOrder(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	s := f.schedule(3, 1, 10)
	at := f.start.Add(5 * time.Minute)
	plain := f.mustBuy(f.in(s, f.phoneCustomer("0912345041"), 1))
	f.ownerExec([]any{plain.OrderID}, `UPDATE ord.order_items SET flash_sale_item_id = NULL WHERE order_id = $1`)
	manual := f.mustBuy(f.in(s, f.phoneCustomer("0912345042"), 1))
	f.ownerExec([]any{manual.OrderID}, `UPDATE ord.orders SET channel = 'manual' WHERE id = $1`)
	for name, c := range map[string]struct {
		res   order.FlashBuyResult
		phone string
	}{"đơn web dòng thường": {plain, "0912345041"}, "đơn kênh manual": {manual, "0912345042"}} {
		if _, err := f.complete(f.completeIn(c.res, c.phone), at); !errors.Is(err, order.ErrNotFound) {
			t.Errorf("%s: %v; mong ErrNotFound", name, err)
		}
		if o := f.getOrder(c.res.OrderID); o.Status != order.StatusDraft || o.Version != 1 {
			t.Errorf("%s: đơn %s v%d", name, o.Status, o.Version)
		}
	}
}
