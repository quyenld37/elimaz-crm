package order_test

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Helper riêng của Task M2-20 (tên có tiền tố edit; không sửa helper dùng chung).

// editOrder gọi Writer.Edit trên đơn id của e.seller với người sửa a, giờ e.now() + 1 giờ.
func (e *orderEnv) editOrder(id uuid.UUID, in order.EditInput, a order.Actor) (order.EditResult, error) {
	var res order.EditResult
	err := e.app(func(tx pgx.Tx) error {
		var err error
		res, err = e.writer().Edit(e.t.Context(), tx, e.seller, id, in, a, e.now().Add(time.Hour))
		return err //nolint:wrapcheck // test
	})
	return res, err
}

func (e *orderEnv) mustEdit(id uuid.UUID, in order.EditInput, a order.Actor) order.Order {
	e.t.Helper()
	res, err := e.editOrder(id, in, a)
	if err != nil {
		e.t.Fatalf("Edit: %v", err)
	}
	return res.Order
}

// editKeep giữ dòng it với số lượng q, giảm dòng d.
func editKeep(it order.Item, q int32, d int64) order.EditLine {
	id := it.ID
	return order.EditLine{ItemID: &id, VariantID: it.VariantID, Quantity: q, LineDiscount: d}
}

// editAdd là dòng mới q cái biến thể v, giảm dòng d.
func editAdd(v uuid.UUID, q int32, d int64) order.EditLine {
	return order.EditLine{VariantID: v, Quantity: q, LineDiscount: d}
}

func editLines(ls ...order.EditLine) *[]order.EditLine { return &ls }

func editPtr[T any](v T) *T { return &v }

// editItem trả dòng của biến thể v trong đơn o (không có → Fatal).
func editItem(t *testing.T, o order.Order, v uuid.UUID) order.Item {
	t.Helper()
	for _, it := range o.Items {
		if it.VariantID == v {
			return it
		}
	}
	t.Fatalf("đơn %s không có dòng biến thể %s: %+v", o.Code, v, o.Items)
	return order.Item{}
}

// editHeld là số lượng giữ held của dòng đơn item (0 khi không có).
func (e *orderEnv) editHeld(item uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(quantity), 0)::int FROM inventory.stock_reservations
		WHERE order_item_id = $1 AND state = 'held'`, item)
}

// sameOrder: hai lần đọc đơn giống nhau ở các trường sửa được (version, tiền, dòng).
func sameOrder(a, b order.Order) bool {
	if a.Version != b.Version || a.GrandTotal != b.GrandTotal || a.Subtotal != b.Subtotal ||
		a.CouponDiscount != b.CouponDiscount || a.OrderDiscount != b.OrderDiscount || len(a.Items) != len(b.Items) ||
		a.Status != b.Status || a.PendingReason != b.PendingReason || a.CarrierNote != b.CarrierNote {
		return false
	}
	for i := range a.Items {
		if a.Items[i] != b.Items[i] {
			return false
		}
	}
	return true
}

// DH10/AT07 (V1 tồn 5; đơn O có V1 ×2 confirmed → có thể bán 3): sửa V1 thành 6 → ErrOutOfStock (*OutOfStockError có
// thể bán 3), đơn, version, dòng, giữ hàng không đổi; thành 5 → có thể bán 0, version + 1, đơn giá giữ, order.updated
// seq = version, sổ bước edit_v<version>; thành 1 → có thể bán 4. Chạy với cả hai người bán (ADR-29).
func TestEdit_DH10_Quantity(t *testing.T) {
	eachM2Seller(t, func(t *testing.T, e *orderEnv) {
		v1 := e.variant(250000, 5)
		in := e.m2Input(30000, ln(v1, 2, 0))
		in.SellerID = e.seller
		o := e.mustCreateM2(in).Order
		it := o.Items[0]
		_, err := e.editOrder(o.ID, order.EditInput{Version: o.Version, Lines: editLines(editKeep(it, 6, 0))}, e.actor)
		oos, ok := errors.AsType[*order.OutOfStockError](err)
		if !ok || !errors.Is(err, order.ErrOutOfStock) || oos.VariantID != v1 || oos.Available != 3 {
			t.Fatalf("sửa thành 6: %v, mong OutOfStockError có thể bán 3", err)
		}
		if got := e.get(o.ID); !sameOrder(got, o) || e.balance(v1).Available != 3 || e.editHeld(it.ID) != 2 {
			t.Fatalf("thiếu hàng mà đơn hoặc giữ hàng đổi: %+v, có thể bán %d", got, e.balance(v1).Available)
		}
		got := e.mustEdit(o.ID, order.EditInput{Version: o.Version, Lines: editLines(editKeep(it, 5, 0))}, e.actor)
		if got.Version != o.Version+1 || got.Items[0].ID != it.ID || got.Items[0].Qty != 5 ||
			got.Items[0].UnitPrice != 250000 || got.Subtotal != 1250000 || got.GrandTotal != 1280000 ||
			got.CODAmount != 1280000 || got.Status != order.StatusConfirmed {
			t.Errorf("sau sửa thành 5: %+v", got)
		}
		if b := e.balance(v1); b.Available != 0 || b.Held != 5 || e.editHeld(it.ID) != 5 {
			t.Errorf("giữ sau sửa thành 5: %+v", b)
		}
		if n := e.events(o.ID, order.TopicOrderUpdated, got.Version); n != 1 {
			t.Errorf("order.updated seq %d: %d", got.Version, n)
		}
		if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1 AND bucket = 'held'
			AND delta = 3 AND step = $2`, v1, "edit_v"+itoa(got.Version)); n != 1 {
			t.Errorf("dòng sổ edit_v%d: %d", got.Version, n)
		}
		got = e.mustEdit(o.ID, order.EditInput{Version: got.Version, Lines: editLines(editKeep(it, 1, 0))}, e.actor)
		if b := e.balance(v1); b.Available != 4 || b.Held != 1 || got.Subtotal != 250000 {
			t.Errorf("sau sửa thành 1: %+v, %+v", b, got)
		}
	})
}

// eachM2Seller chạy body với từng người bán làm người bán của đơn trên cùng một newM2Env.
func eachM2Seller(t *testing.T, body func(t *testing.T, e *orderEnv)) {
	t.Helper()
	base := newM2Env(t)
	for _, s := range []uuid.UUID{base.shop.Seller, base.shop.Partner} {
		t.Run(s.String(), func(t *testing.T) {
			e := *base
			e.t, e.seller = t, s
			body(t, &e)
		})
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// Bỏ dòng V2 (đã giữ hàng; việc từ Task 62), thêm V3 ×1: V2 được nhả (lý do order_edited, dòng giữ hàng còn, mất liên
// kết dòng đơn), dòng V2 bị xóa; V3 giữ 1, dòng mới giá hiện hành, nối sau dòng cuối; tổng tiền đúng (trigger qua lúc
// commit).
func TestEdit_RemoveAndAddLines(t *testing.T) {
	e := newM2Env(t)
	v1, v2, v3 := e.variant(250000, 5), e.variant(180000, 5), e.variant(99000, 5)
	o := e.mustCreateM2(e.m2Input(30000, ln(v1, 2, 20000), ln(v2, 1, 0))).Order
	i1, i2 := editItem(t, o, v1), editItem(t, o, v2)
	got := e.mustEdit(o.ID, order.EditInput{
		Version: o.Version, Lines: editLines(editKeep(i1, 2, 20000), editAdd(v3, 1, 0)),
	}, e.actor)
	if len(got.Items) != 2 || got.Items[0].ID != i1.ID || got.Items[1].VariantID != v3 ||
		got.Items[1].UnitPrice != 99000 || got.Items[1].Position != 3 || got.Subtotal != 599000 ||
		got.GrandTotal != 609000 {
		t.Fatalf("sau sửa: %+v", got)
	}
	if b := e.balance(v2); b.Held != 0 || b.Available != 5 {
		t.Errorf("V2 chưa nhả: %+v", b)
	}
	if b := e.balance(v3); b.Held != 1 || e.editHeld(got.Items[1].ID) != 1 {
		t.Errorf("V3 chưa giữ: %+v", b)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_items WHERE id = $1`, i2.ID); n != 0 {
		t.Errorf("dòng V2 còn: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1 AND state = 'released'
		AND release_reason = 'order_edited' AND order_item_id IS NULL`, v2); n != 1 {
		t.Errorf("giữ hàng V2 đã nhả, mất liên kết: %d", n)
	}
	if b := e.balance(v1); b.Held != 2 {
		t.Errorf("V1 đổi giữ: %+v", b)
	}
}

// DH11/AT28: hai người cùng đọc một version; đúng một người lưu được (version + 1), người kia ErrVersionConflict; dữ
// liệu là của người lưu được.
func TestEdit_DH11_VersionConflict(t *testing.T) {
	e := newM2Env(t)
	v1 := e.variant(250000, 10)
	o := e.mustCreateM2(e.m2Input(0, ln(v1, 2, 0))).Order
	o = e.mustEdit(o.ID, order.EditInput{Version: o.Version, CarrierNote: editPtr("gọi trước")}, e.actor)
	notes, qtys := []string{"người A", "người B"}, []int32{3, 4}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, note := range notes {
		wg.Go(func() {
			<-start
			_, errs[i] = e.editOrder(o.ID, order.EditInput{
				Version: o.Version, CarrierNote: editPtr(note), Lines: editLines(editKeep(o.Items[0], qtys[i], 0)),
			}, e.actor)
		})
	}
	close(start)
	wg.Wait()
	saved, wantNote, wantQty := 0, "", int32(0)
	for i, err := range errs {
		switch {
		case err == nil:
			saved, wantNote, wantQty = saved+1, notes[i], qtys[i]
		case !errors.Is(err, order.ErrVersionConflict):
			t.Fatalf("người %d: %v", i, err)
		}
	}
	if saved != 1 {
		t.Fatalf("mong đúng một người lưu được: %v", errs)
	}
	got := e.get(o.ID)
	if got.Version != o.Version+1 || got.CarrierNote != wantNote || got.Items[0].Qty != wantQty ||
		e.balance(v1).Held != wantQty {
		t.Errorf("dữ liệu không phải của người lưu được (%s): %+v", wantNote, got)
	}
}

// KH11: đổi giá V1 thành 270.000 sau khi tạo; sửa số lượng dòng V1 → đơn giá vẫn 250.000; thêm V3 → giá hiện hành;
// gửi V1 không itemId (Quyết định 7) → vẫn là dòng cũ, giá cũ.
func TestEdit_KH11_PriceSnapshot(t *testing.T) {
	e := newM2Env(t)
	v1, v3 := e.variant(250000, 10), e.variant(100000, 10)
	o := e.mustCreateM2(e.m2Input(0, ln(v1, 1, 0))).Order
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET price = 270000 WHERE id IN ($1, $2)`, v1, v3)
		return err //nolint:wrapcheck // test
	})
	got := e.mustEdit(o.ID, order.EditInput{Version: o.Version, Lines: editLines(editKeep(o.Items[0], 3, 0))}, e.actor)
	if got.Items[0].UnitPrice != 250000 || got.Subtotal != 750000 {
		t.Errorf("dòng giữ nguyên đổi giá: %+v", got.Items)
	}
	got = e.mustEdit(o.ID, order.EditInput{
		Version: got.Version, Lines: editLines(editAdd(v1, 2, 0), editAdd(v3, 1, 0)),
	}, e.actor)
	if len(got.Items) != 2 || got.Items[0].ID != o.Items[0].ID || got.Items[0].UnitPrice != 250000 ||
		got.Items[0].Qty != 2 || got.Items[1].UnitPrice != 270000 || got.Subtotal != 770000 {
		t.Errorf("sau thêm V3: %+v", got.Items)
	}
}

// Mã min_subtotal 600.000 (10%, không trần) trên đơn 2 × 250.000 + 1 × 180.000: tăng V1 thành 3 → Reprice (phần giảm
// 93.000); bỏ V2 làm base còn 500.000 → coupon_rejected (min_subtotal), không đổi gì; đổi sang mã khác → lượt cũ trả,
// lượt mới dùng; couponCode "" → bỏ mã, used_count − 1; mã trùng mã đang dùng (khác hoa thường) → không đổi.
func TestEdit_Coupon(t *testing.T) {
	e := newM2Env(t)
	min600 := e.editCoupon("MIN600", 600000)
	other := e.editCoupon("KHAC", 0)
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	in := e.m2Input(0, ln(v1, 2, 0), ln(v2, 1, 0))
	in.CouponCode = "MIN600"
	o := e.mustCreateM2(in).Order
	if o.CouponDiscount != 68000 {
		t.Fatalf("đơn có mã: %+v", o)
	}
	i1, i2 := editItem(t, o, v1), editItem(t, o, v2)
	got := e.mustEdit(o.ID, order.EditInput{Version: o.Version, Lines: editLines(editKeep(i1, 3, 0), editKeep(i2, 1, 0))},
		e.actor)
	if got.CouponDiscount != 93000 || got.GrandTotal != 837000 ||
		e.count(`SELECT discount::int FROM promotion.coupon_redemptions WHERE order_id = $1 AND status = 'active'`,
			o.ID) != 93000 {
		t.Fatalf("Reprice: %+v", got)
	}
	_, err := e.editOrder(o.ID, order.EditInput{Version: got.Version, Lines: editLines(editKeep(i1, 2, 0))}, e.actor)
	if rejectReason(err) != promotion.ReasonMinSubtotal {
		t.Fatalf("bỏ V2: %v, mong coupon_rejected min_subtotal", err)
	}
	if after := e.get(o.ID); !sameOrder(after, got) || e.balance(v2).Held != 1 {
		t.Fatalf("mã bị từ chối mà đơn đổi: %+v", after)
	}
	same := e.mustEdit(o.ID, order.EditInput{Version: got.Version, CouponCode: editPtr(" min600 ")}, e.actor)
	if same.Version != got.Version {
		t.Errorf("mã trùng mã đang dùng mà version đổi: %d → %d", got.Version, same.Version)
	}
	got = e.mustEdit(o.ID, order.EditInput{Version: got.Version, CouponCode: editPtr("khac")}, e.actor)
	if got.CouponDiscount != 93000 || e.usedCount(min600) != 0 || e.usedCount(other) != 1 {
		t.Errorf("đổi mã: %+v, used MIN600 %d, KHAC %d", got, e.usedCount(min600), e.usedCount(other))
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'coupon.used'
		AND aggregate_id = $1 AND seq = $2`, o.ID.String(), got.Version); n != 1 {
		t.Errorf("coupon.used seq %d: %d", got.Version, n)
	}
	got = e.mustEdit(o.ID, order.EditInput{Version: got.Version, CouponCode: editPtr("")}, e.actor)
	if got.CouponDiscount != 0 || got.GrandTotal != 930000 || e.usedCount(other) != 0 ||
		e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE order_id = $1 AND status = 'active'`, o.ID) != 0 {
		t.Errorf("bỏ mã: %+v, used KHAC %d", got, e.usedCount(other))
	}
	var alloc int64
	for _, it := range got.Items {
		alloc += it.AllocatedOrderDiscount
	}
	if alloc != 0 {
		t.Errorf("bỏ mã mà dòng còn phần giảm: %+v", got.Items)
	}
}

// editCoupon tạo mã 10% không trần, điều kiện tạm tính tối thiểu minSubtotal, còn hạn ở giờ thử.
func (e *orderEnv) editCoupon(code string, minSubtotal int64) uuid.UUID {
	e.t.Helper()
	var out promotion.Coupon
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = promotion.Writer{}.CreateCoupon(e.t.Context(), tx, promotion.Actor{TenantID: e.tenant, UserID: e.actor.UserID},
			promotion.CouponInput{
				Code: code, Name: "Mã thử " + code, DiscountKind: promotion.KindPercent, Value: 1000,
				MinSubtotal: minSubtotal, ReleaseOnCancel: true,
				ValidFrom: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo mã %s: %v", code, err)
	}
	return out.ID
}

func (e *orderEnv) usedCount(coupon uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, coupon)
}

// Q25/DH15 (phần CRM): đơn có vận đơn hiện hành → sửa dòng, phí, địa chỉ ErrLockedByShipment, không đổi gì; sửa người
// phụ trách → được (gửi kèm dòng không đổi vẫn được, Quyết định 5); đơn delivered → sửa dòng ErrLockedByShipment, người
// phụ trách được.
func TestEdit_LockedByShipment(t *testing.T) {
	e := newM2Env(t)
	o, v1, _ := e.confirmedDH01()
	if err := e.attachBy(o.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	o = e.get(o.ID)
	i1 := editItem(t, o, v1)
	for name, in := range map[string]order.EditInput{
		"dòng":         {Version: o.Version, Lines: editLines(editKeep(i1, 1, 0))},
		"phí ship":     {Version: o.Version, ShippingFee: editPtr(int64(0))},
		"ghi chú hãng": {Version: o.Version, CarrierNote: editPtr("đổi")},
		"địa chỉ": {Version: o.Version, ShipAddress: &order.ShipAddress{
			Name: "Anh Ba", Phone: logging.Phone("0909000111"), Line: "1 Hai Bà Trưng", ProvinceCode: "T01", WardCode: "T01001",
		}},
	} {
		if _, err := e.editOrder(o.ID, in, e.actor); !errors.Is(err, order.ErrLockedByShipment) {
			t.Errorf("%s: %v, mong ErrLockedByShipment", name, err)
		}
	}
	if got := e.get(o.ID); !sameOrder(got, o) {
		t.Fatalf("bị chặn mà đơn đổi: %+v", got)
	}
	care := e.manager().UserID
	keepAll := make([]order.EditLine, 0, len(o.Items))
	for _, it := range o.Items {
		keepAll = append(keepAll, editKeep(it, it.Qty, it.LineDiscount))
	}
	res, err := e.editOrder(o.ID, order.EditInput{Version: o.Version, CareBy: &care, Lines: &keepAll}, e.actor)
	if err != nil || res.Order.CareBy == nil || *res.Order.CareBy != care || res.Order.Version != o.Version+1 ||
		len(res.Fields) != 1 || res.Fields[0] != "careBy" {
		t.Fatalf("sửa người phụ trách: %v %+v", err, res)
	}

	d, dv, ds := e.attachedOrder(1)
	e.mustProgress(d.ID, ds, order.StepPickedUp, true)
	e.mustProgress(d.ID, ds, order.StepDelivered, true)
	d = e.get(d.ID)
	if d.Status != order.StatusDelivered {
		t.Fatalf("đơn chưa giao: %+v", d)
	}
	if _, err := e.editOrder(d.ID, order.EditInput{
		Version: d.Version, Lines: editLines(editKeep(editItem(t, d, dv), 2, 0)),
	}, e.actor); !errors.Is(err, order.ErrLockedByShipment) {
		t.Errorf("đơn delivered sửa dòng: %v, mong ErrLockedByShipment", err)
	}
	if _, err := e.editOrder(d.ID, order.EditInput{Version: d.Version, CareBy: &care}, e.actor); err != nil {
		t.Errorf("đơn delivered sửa người phụ trách: %v", err)
	}
}

// Ngưỡng duyệt (mặc định 10%): đơn confirmed, nhân viên không có quyền duyệt tăng giảm tay vượt ngưỡng →
// ErrDiscountNeedsApproval, không đổi gì; người có quyền → được, ghi người duyệt; đơn new → chờ duyệt (lý do
// discount_approval, lịch sử, version + 2); giảm tay vượt ngưỡng không tăng thêm (tăng số lượng) → không đòi duyệt lại.
func TestEdit_DiscountApproval(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(1000000, 10)
	o := e.mustCreateM2(e.m2Input(0, ln(v, 1, 0))).Order
	if _, err := e.editOrder(o.ID, order.EditInput{Version: o.Version, OrderDiscount: editPtr(int64(100001))},
		e.actor); !errors.Is(err, order.ErrDiscountNeedsApproval) {
		t.Fatalf("nhân viên vượt ngưỡng: %v, mong ErrDiscountNeedsApproval", err)
	}
	if got := e.get(o.ID); !sameOrder(got, o) {
		t.Fatalf("bị chặn mà đơn đổi: %+v", got)
	}
	m := e.manager()
	got := e.mustEdit(o.ID, order.EditInput{Version: o.Version, OrderDiscount: editPtr(int64(200000))}, m)
	if got.OrderDiscount != 200000 || got.GrandTotal != 800000 || got.DiscountApprovedBy == nil ||
		*got.DiscountApprovedBy != m.UserID || got.Status != order.StatusConfirmed {
		t.Errorf("người duyệt sửa: %+v", got)
	}
	got = e.mustEdit(o.ID, order.EditInput{Version: got.Version, Lines: editLines(editKeep(got.Items[0], 2, 0))}, e.actor)
	if got.Subtotal != 2000000 {
		t.Errorf("tăng số lượng (giảm tay nhẹ đi): %+v", got)
	}

	n := e.createIntent(order.IntentNew, fullAddress(), 0)
	got = e.mustEdit(n.ID, order.EditInput{Version: n.Version, OrderDiscount: editPtr(int64(100001))}, e.actor)
	if got.Status != order.StatusNew || got.PendingReason != order.PendingDiscountApproval || got.Version != n.Version+2 {
		t.Errorf("đơn new vượt ngưỡng: %+v", got)
	}
	if c := e.history(n.ID, `from_status = 'new' AND to_status = 'new' AND to_pending = 'discount_approval'
		AND reason = 'manual_discount_over_threshold'`); c != 1 {
		t.Errorf("lịch sử chờ duyệt: %d", c)
	}
	if c := e.events(n.ID, order.TopicOrderUpdated, n.Version+1); c != 1 {
		t.Errorf("order.updated seq %d: %d", n.Version+1, c)
	}
}

// Đầu vào sai → ErrInvalidInput, không đổi gì: không dòng, số lượng 0, hai dòng cùng biến thể, hai dòng cùng itemId,
// itemId lạ, itemId với biến thể khác, phí âm, mã quá dài; đơn không có, đơn của người bán khác → ErrNotFound; dòng mới
// của người bán khác → ErrMixedSellers; version cũ → ErrVersionConflict.
func TestEdit_Errors(t *testing.T) {
	e := newM2Env(t)
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	o := e.mustCreateM2(e.m2Input(0, ln(v1, 1, 0), ln(v2, 1, 0))).Order
	i1, i2 := editItem(t, o, v1), editItem(t, o, v2)
	wrongVariant := editKeep(i1, 1, 0)
	wrongVariant.VariantID = v2
	stranger := editKeep(i1, 1, 0)
	stranger.ItemID = editPtr(uuid.New())
	for name, in := range map[string]order.EditInput{
		"không dòng":           {Version: o.Version, Lines: editLines()},
		"số lượng 0":           {Version: o.Version, Lines: editLines(editKeep(i1, 0, 0))},
		"cùng biến thể":        {Version: o.Version, Lines: editLines(editKeep(i1, 1, 0), editAdd(v1, 1, 0))},
		"cùng itemId":          {Version: o.Version, Lines: editLines(editKeep(i1, 1, 0), editKeep(i1, 2, 0))},
		"itemId lạ":            {Version: o.Version, Lines: editLines(stranger)},
		"itemId biến thể khác": {Version: o.Version, Lines: editLines(wrongVariant, editKeep(i2, 1, 0))},
		"phí âm":               {Version: o.Version, ShippingFee: editPtr(int64(-1))},
		"giảm dòng âm":         {Version: o.Version, Lines: editLines(editKeep(i1, 1, -1))},
	} {
		if _, err := e.editOrder(o.ID, in, e.actor); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("%s: %v, mong ErrInvalidInput", name, err)
		}
	}
	if _, err := e.editOrder(uuid.New(), order.EditInput{Version: 1}, e.actor); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v", err)
	}
	other := *e
	other.seller = e.partner
	if _, err := other.editOrder(o.ID, order.EditInput{Version: o.Version, CarrierNote: editPtr("x")},
		e.actor); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v, mong ErrNotFound", err)
	}
	vp := e.variantOf(e.partner, 1000, 5)
	if _, err := e.editOrder(o.ID, order.EditInput{
		Version: o.Version, Lines: editLines(editKeep(i1, 1, 0), editAdd(vp, 1, 0)),
	}, e.actor); !errors.Is(err, order.ErrMixedSellers) {
		t.Errorf("dòng của người bán khác: %v, mong ErrMixedSellers", err)
	}
	if _, err := e.editOrder(o.ID, order.EditInput{Version: o.Version - 1, CarrierNote: editPtr("x")},
		e.actor); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if got := e.get(o.ID); !sameOrder(got, o) {
		t.Errorf("lỗi mà đơn đổi: %+v", got)
	}
}

// Không trường nào khác giá trị hiện tại → trả đơn, không ghi gì (version, sự kiện giữ nguyên); đổi địa chỉ đơn
// confirmed thành thiếu xã → ErrAddressIncomplete; địa chỉ đủ mới → ShipPhone chuẩn hóa mới, Fields có shipAddress.
func TestEdit_NoChangeAndAddress(t *testing.T) {
	e := newM2Env(t)
	v1 := e.variant(250000, 10)
	o := e.mustCreateM2(e.m2Input(30000, ln(v1, 1, 0))).Order
	res, err := e.editOrder(o.ID, order.EditInput{
		Version: o.Version, Lines: editLines(editKeep(o.Items[0], 1, 0)), ShippingFee: editPtr(int64(30000)),
		ShipAddress: fullAddress(), CouponCode: editPtr(""),
	}, e.actor)
	if err != nil || res.Order.Version != o.Version || len(res.Fields) != 0 ||
		e.events(o.ID, order.TopicOrderUpdated, o.Version+1) != 0 {
		t.Fatalf("không đổi gì: %v %+v", err, res)
	}
	noWard := fullAddress()
	noWard.WardCode = ""
	if _, err := e.editOrder(o.ID, order.EditInput{Version: o.Version, ShipAddress: noWard},
		e.actor); !errors.Is(err, order.ErrAddressIncomplete) {
		t.Errorf("confirmed thiếu xã: %v, mong ErrAddressIncomplete", err)
	}
	addr := &order.ShipAddress{
		Name: "Anh Ba", Phone: logging.Phone("0909000111"), Line: "1 Hai Bà Trưng", ProvinceCode: "T01", WardCode: "T01001",
	}
	res, err = e.editOrder(o.ID, order.EditInput{Version: o.Version, ShipAddress: addr, ShippingFee: editPtr(int64(0))},
		e.actor)
	if err != nil || res.Order.ShipPhone != "+84909000111" || res.Order.ShipAddress.Name != "Anh Ba" ||
		res.Order.GrandTotal != 250000 || len(res.Fields) != 2 {
		t.Fatalf("đổi địa chỉ và phí: %v %+v", err, res)
	}
}

// Đơn đã trả trước đủ 250.000 (paid, COD 0): sửa V1 thành 2 → partial, COD 250.000; về lại 1 → paid, COD 0 (luật của
// ApplyPaymentTotals).
func TestEdit_PrepaidOrder(t *testing.T) {
	e := newM2Env(t)
	v1 := e.variant(250000, 10)
	o := e.mustCreateM2(e.m2Input(0, ln(v1, 1, 0))).Order
	if err := e.pay(o.ID, 250000); err != nil {
		t.Fatal(err)
	}
	o = e.get(o.ID)
	if o.PaymentStatus != order.PaymentPaid || o.CODAmount != 0 {
		t.Fatalf("đơn đã trả: %+v", o)
	}
	got := e.mustEdit(o.ID, order.EditInput{Version: o.Version, Lines: editLines(editKeep(o.Items[0], 2, 0))}, e.actor)
	if got.PaymentStatus != order.PaymentPartial || got.CODAmount != 250000 || got.PrepaidTotal != 250000 {
		t.Errorf("sau tăng: %+v", got)
	}
	got = e.mustEdit(o.ID, order.EditInput{Version: got.Version, Lines: editLines(editKeep(o.Items[0], 1, 0))}, e.actor)
	if got.PaymentStatus != order.PaymentPaid || got.CODAmount != 0 {
		t.Errorf("sau giảm: %+v", got)
	}
}
