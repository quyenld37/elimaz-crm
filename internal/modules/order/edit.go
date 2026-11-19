package order

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/order/internal/state"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Tên trường của lần sửa (khớp tên trường của UpdateOrderRequest), ghi ở EditResult.Fields cho audit.
const (
	EditFieldLines         = "lines"
	EditFieldOrderDiscount = "orderDiscount"
	EditFieldCouponCode    = "couponCode"
	EditFieldShippingFee   = "shippingFee"
	EditFieldShipAddress   = "shipAddress"
	EditFieldCarrierNote   = "carrierNote"
	EditFieldCareBy        = "careBy"
	EditFieldTrafficSource = "trafficSourceId"
)

const (
	// releaseOrderEdited là lý do nhả giữ hàng của dòng bị bỏ khi sửa đơn.
	releaseOrderEdited = "order_edited"
	// shipmentAttached đặt vào orderview.Axes.ShipmentStatus khi đơn có vận đơn hiện hành: order không đọc trạng thái
	// vận đơn (của fulfillment), state.Editable chỉ cần biết có hay không.
	shipmentAttached = "attached"
)

// EditLine là một dòng mong muốn của đơn sau sửa. ItemID: dòng đang có (giữ đơn giá, ảnh chụp lúc đặt, KH11; VariantID
// phải đúng biến thể của dòng); nil: dòng mới (chụp giá hiện hành), trừ khi biến thể trùng một dòng đang có mà không
// dòng nào khác trỏ tới — khi đó là dòng đang có.
type EditLine struct {
	ItemID       *uuid.UUID
	VariantID    uuid.UUID
	Quantity     int32
	LineDiscount int64
}

// EditInput là một lần sửa đơn (kế hoạch M2, Task M2-20): Version là version người sửa đã đọc; trường nil là không
// sửa. Lines là danh sách dòng mong muốn, đủ cả dòng giữ nguyên (dòng không có trong danh sách bị bỏ). CouponCode ""
// là bỏ mã. CareBy, TrafficSourceID chỉ đặt được (hợp đồng không có cách bỏ).
type EditInput struct {
	Version         int64
	Lines           *[]EditLine
	OrderDiscount   *int64
	CouponCode      *string
	ShippingFee     *int64
	ShipAddress     *ShipAddress
	CarrierNote     *string
	CareBy          *uuid.UUID
	TrafficSourceID *uuid.UUID
}

// EditResult là đơn sau sửa (như Get) và tên các trường đã đổi (EditField*, theo thứ tự cố định; rỗng khi không trường
// nào khác giá trị hiện tại — khi đó không ghi gì, version giữ nguyên).
type EditResult struct {
	Order  Order
	Fields []string
}

// Edit sửa đơn id của người bán sellerID trong tx của bên gọi (một transaction: lỗi nào bên gọi cũng lùi cả lần sửa,
// đơn, dòng, mã, giữ hàng còn nguyên — DH10). Thứ tự (ADR-46):
//
//  1. Kiểm đầu vào không cần database (ErrInvalidInput). Khóa đơn FOR UPDATE (không có → ErrNotFound); version khác →
//     ErrVersionConflict; khóa mọi dòng đơn theo id (nhóm 7).
//  2. So với đơn hiện tại: chỉ trường khác giá trị hiện tại là sửa. state.Editable: trường không sửa được →
//     ErrLockedByShipment khi đơn có vận đơn hiện hành, ngược lại ErrInvalidTransition.
//  3. Dòng giữ nguyên giữ đơn giá cũ (KH11); dòng mới chụp giá hiện hành, cùng người bán, còn bán (ErrNotForSale,
//     ErrMixedSellers, ErrVariantNotFound). Đơn confirmed đổi địa chỉ phải đủ tỉnh, xã (ErrAddressIncomplete).
//  4. Tiền bằng pricing.Compute. Ngưỡng duyệt giảm giá (khi dòng hoặc giảm giá đổi, tỷ lệ giảm tay mới vượt ngưỡng và
//     lớn hơn tỷ lệ cũ): người có quyền duyệt → ghi người duyệt; đơn confirmed → ErrDiscountNeedsApproval; đơn new,
//     draft → chờ duyệt (applyTransition cùng trạng thái, lý do discount_approval).
//  5. Mã (nhóm 10): đổi mã → promotion.Detach rồi Redeem; "" → Detach; giữ mã mà tạm tính sau giảm đổi →
//     promotion.Reprice. Bị từ chối → *promotion.RejectError.
//  6. Ghi dòng (sửa dòng giữ nguyên, chèn dòng mới nối sau position lớn nhất) và đơn (version + 1).
//  7. Giữ hàng (nhóm 11) theo variant_id tăng dần trên mọi biến thể bị chạm: dòng bị bỏ → ReleaseHeldByItems
//     (order_edited), dòng giữ nguyên đổi số lượng → ResizeHeld (bước edit_v<version mới>), dòng mới → Reserve; thiếu →
//     *OutOfStockError. Rồi xóa dòng bị bỏ.
//  8. Sự kiện order.updated (seq = version mới), báo hiệu.
//
// Audit do bên gọi ghi bằng EditResult.Fields và Order.AuditFields. Không giữ trạng thái giữa các lần chạy lại.
func (w Writer) Edit(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, in EditInput, a Actor, now time.Time,
) (EditResult, error) {
	stock, err := w.stock()
	if err != nil {
		return EditResult{}, err
	}
	now = now.UTC()
	req, err := prepareEdit(in)
	if err != nil {
		return EditResult{}, err
	}
	q := store.New(tx)
	locked, err := lockOrder(ctx, q, sellerID, id)
	if err != nil {
		return EditResult{}, err
	}
	if locked.Version != in.Version {
		return EditResult{}, ErrVersionConflict
	}
	if _, err := q.LockOrderItems(ctx, store.LockOrderItemsParams{SellerID: sellerID, OrderID: id}); err != nil {
		return EditResult{}, fmt.Errorf("khóa dòng đơn: %w", err)
	}
	cur, err := Get(ctx, tx, sellerID, id)
	if err != nil {
		return EditResult{}, err
	}
	coupon, err := promotion.ActiveCouponCode(ctx, tx, id)
	if err != nil {
		return EditResult{}, fmt.Errorf("mã giảm giá của đơn: %w", err)
	}
	p, err := planEdit(cur, coupon, req)
	if err != nil || len(p.fields) == 0 {
		return EditResult{Order: cur}, err
	}
	if err := checkEditable(cur, p.fields); err != nil {
		return EditResult{}, err
	}
	e := editRun{w: w, stock: stock, tx: tx, q: q, cur: cur, tenant: locked.TenantID, p: p, a: a, now: now}
	if err := e.run(ctx); err != nil {
		return EditResult{}, err
	}
	o, err := Get(ctx, tx, sellerID, id)
	if err != nil {
		return EditResult{}, err
	}
	return EditResult{Order: o, Fields: p.fields}, nil
}

// editRequest là EditInput đã kiểm phần không cần database (mã cắt khoảng trắng, địa chỉ đã làm sạch).
type editRequest struct {
	in     EditInput
	coupon *string
	addr   *ShipAddress
}

// prepareEdit kiểm EditInput không cần database.
func prepareEdit(in EditInput) (editRequest, error) {
	r := editRequest{in: in}
	if (in.OrderDiscount != nil && !money(*in.OrderDiscount)) || (in.ShippingFee != nil && !money(*in.ShippingFee)) ||
		(in.CarrierNote != nil && !optText(*in.CarrierNote, maxCarrierNoteRunes)) ||
		(in.CareBy != nil && *in.CareBy == uuid.Nil) || (in.TrafficSourceID != nil && *in.TrafficSourceID == uuid.Nil) {
		return editRequest{}, ErrInvalidInput
	}
	if in.CouponCode != nil {
		c := strings.TrimSpace(*in.CouponCode)
		if c != "" && !validText(c, maxCouponCodeRunes) {
			return editRequest{}, fmt.Errorf("%w: mã giảm giá", ErrInvalidInput)
		}
		r.coupon = &c
	}
	if in.ShipAddress != nil {
		var err error
		if r.addr, err = cleanAddress(*in.ShipAddress); err != nil {
			return editRequest{}, err
		}
	}
	if in.Lines != nil {
		if err := validEditLines(*in.Lines); err != nil {
			return editRequest{}, err
		}
	}
	return r, nil
}

// validEditLines: 1–100 dòng, biến thể có, số lượng > 0, giảm dòng hợp lệ, không hai dòng cùng biến thể hay cùng itemId.
func validEditLines(ls []EditLine) error {
	if len(ls) == 0 || len(ls) > maxItems {
		return fmt.Errorf("%w: số dòng %d", ErrInvalidInput, len(ls))
	}
	variants := make(map[uuid.UUID]bool, len(ls))
	items := make(map[uuid.UUID]bool, len(ls))
	for _, l := range ls {
		if l.VariantID == uuid.Nil || l.Quantity <= 0 || !money(l.LineDiscount) || variants[l.VariantID] ||
			(l.ItemID != nil && items[*l.ItemID]) {
			return fmt.Errorf("%w: dòng sửa", ErrInvalidInput)
		}
		variants[l.VariantID] = true
		if l.ItemID != nil {
			items[*l.ItemID] = true
		}
	}
	return nil
}

// editedLine là một dòng của đơn sau sửa: item khác nil là dòng giữ nguyên (đơn giá, ảnh chụp từ item).
type editedLine struct {
	item     *Item
	variant  uuid.UUID
	qty      int32
	discount int64
}

// editPlan là phần so sánh của lần sửa với đơn hiện tại (chưa đọc database thêm).
type editPlan struct {
	req     editRequest
	fields  []string
	lines   []editedLine // giữ nguyên theo position, rồi dòng mới theo thứ tự nhập
	removed []Item
	coupon  string // mã đang dùng của đơn ("" khi không)

	orderDiscount, shippingFee int64
	addr                       ShipAddress
	carrierNote                string
	careBy, trafficSource      *uuid.UUID
}

// has: trường field có trong các trường đổi.
func (p editPlan) has(field string) bool { return slices.Contains(p.fields, field) }

// planEdit so req với đơn cur (mã đang dùng coupon), trả các trường thật sự đổi và giá trị sau sửa.
func planEdit(cur Order, coupon string, req editRequest) (editPlan, error) {
	p := editPlan{
		req: req, coupon: coupon, orderDiscount: cur.OrderDiscount, shippingFee: cur.ShippingFee, addr: cur.ShipAddress,
		carrierNote: cur.CarrierNote, careBy: cur.CareBy, trafficSource: cur.TrafficSourceID,
	}
	in := req.in
	if in.Lines != nil {
		changed, err := p.planLines(cur, *in.Lines)
		if err != nil {
			return editPlan{}, err
		}
		if changed {
			p.fields = append(p.fields, EditFieldLines)
		}
	} else {
		for i := range cur.Items {
			it := cur.Items[i]
			p.lines = append(p.lines, editedLine{item: &it, variant: it.VariantID, qty: it.Qty, discount: it.LineDiscount})
		}
	}
	p.planFields(cur)
	return p, nil
}

// planFields ghi các trường ngoài dòng đơn thật sự khác giá trị hiện tại của cur và giá trị sau sửa.
func (p *editPlan) planFields(cur Order) {
	in, req, coupon := p.req.in, p.req, p.coupon
	if in.OrderDiscount != nil && *in.OrderDiscount != cur.OrderDiscount {
		p.orderDiscount = *in.OrderDiscount
		p.fields = append(p.fields, EditFieldOrderDiscount)
	}
	if req.coupon != nil && !strings.EqualFold(*req.coupon, coupon) {
		p.fields = append(p.fields, EditFieldCouponCode)
	}
	if in.ShippingFee != nil && *in.ShippingFee != cur.ShippingFee {
		p.shippingFee = *in.ShippingFee
		p.fields = append(p.fields, EditFieldShippingFee)
	}
	if req.addr != nil && *req.addr != cur.ShipAddress {
		p.addr = *req.addr
		p.fields = append(p.fields, EditFieldShipAddress)
	}
	if in.CarrierNote != nil && *in.CarrierNote != cur.CarrierNote {
		p.carrierNote = *in.CarrierNote
		p.fields = append(p.fields, EditFieldCarrierNote)
	}
	if in.CareBy != nil && (cur.CareBy == nil || *cur.CareBy != *in.CareBy) {
		p.careBy = in.CareBy
		p.fields = append(p.fields, EditFieldCareBy)
	}
	if in.TrafficSourceID != nil && (cur.TrafficSourceID == nil || *cur.TrafficSourceID != *in.TrafficSourceID) {
		p.trafficSource = in.TrafficSourceID
		p.fields = append(p.fields, EditFieldTrafficSource)
	}
}

// planLines dựng p.lines, p.removed từ danh sách dòng mong muốn; trả true khi khác dòng hiện tại.
func (p *editPlan) planLines(cur Order, want []EditLine) (bool, error) {
	byID := make(map[uuid.UUID]*Item, len(cur.Items))
	byVariant := make(map[uuid.UUID]*Item, len(cur.Items))
	for i := range cur.Items {
		byID[cur.Items[i].ID], byVariant[cur.Items[i].VariantID] = &cur.Items[i], &cur.Items[i]
	}
	kept := make(map[uuid.UUID]EditLine, len(want))
	for _, l := range want {
		if l.ItemID == nil {
			continue
		}
		it, ok := byID[*l.ItemID]
		if !ok || it.VariantID != l.VariantID {
			return false, fmt.Errorf("%w: dòng đơn không có hoặc khác biến thể", ErrInvalidInput)
		}
		kept[it.ID] = l
	}
	var added []editedLine
	for _, l := range want {
		if l.ItemID != nil {
			continue
		}
		if it, ok := byVariant[l.VariantID]; ok {
			if _, taken := kept[it.ID]; taken {
				return false, fmt.Errorf("%w: hai dòng cùng biến thể", ErrInvalidInput)
			}
			kept[it.ID] = l // cùng biến thể với dòng đang có: vẫn là dòng đó (giữ đơn giá)
			continue
		}
		added = append(added, editedLine{variant: l.VariantID, qty: l.Quantity, discount: l.LineDiscount})
	}
	changed := len(added) > 0
	for i := range cur.Items {
		it := &cur.Items[i]
		l, ok := kept[it.ID]
		if !ok {
			p.removed = append(p.removed, *it)
			changed = true
			continue
		}
		changed = changed || l.Quantity != it.Qty || l.LineDiscount != it.LineDiscount
		p.lines = append(p.lines, editedLine{item: it, variant: it.VariantID, qty: l.Quantity, discount: l.LineDiscount})
	}
	p.lines = append(p.lines, added...)
	return changed, nil
}

// checkEditable hỏi state.Editable cho mọi trường đổi của đơn cur.
func checkEditable(cur Order, fields []string) error {
	axes := orderview.Axes{Status: cur.Status, PendingReason: cur.PendingReason, PackingStatus: cur.PackingStatus}
	if cur.CurrentShipmentID != nil {
		axes.ShipmentStatus = shipmentAttached
	}
	s := state.Editable(axes)
	allowed := map[string]bool{
		EditFieldLines: s.Lines, EditFieldOrderDiscount: s.Discounts, EditFieldCouponCode: s.Coupon,
		EditFieldShippingFee: s.ShippingFee, EditFieldShipAddress: s.Address, EditFieldCarrierNote: s.CarrierNote,
		EditFieldCareBy: s.CareBy, EditFieldTrafficSource: s.TrafficSource,
	}
	for _, f := range fields {
		switch {
		case allowed[f]:
		case cur.CurrentShipmentID != nil:
			return fmt.Errorf("%w: %s", ErrLockedByShipment, f)
		default:
			return fmt.Errorf("%w: sửa %s của đơn %s", ErrInvalidTransition, f, cur.Status)
		}
	}
	return nil
}

// editRun là phần đọc thêm và ghi của một lần sửa đã qua kiểm (Edit bước 3–8).
type editRun struct {
	w      Writer
	stock  inventory.Writer
	tx     pgx.Tx
	q      *store.Queries
	cur    Order
	tenant uuid.UUID
	p      editPlan
	a      Actor
	now    time.Time

	// Kết quả tính (gán lại mỗi lần run).
	sale            map[uuid.UUID]line // dòng mới theo biến thể
	addrComplete    bool
	totals          pricing.Totals
	pending, record bool
	version         int64
	added           map[uuid.UUID]uuid.UUID // biến thể → mã dòng mới vừa chèn
}

func (e *editRun) run(ctx context.Context) error {
	e.sale, e.addrComplete, e.pending, e.record = nil, e.cur.AddressComplete, false, false
	e.added = make(map[uuid.UUID]uuid.UUID)
	if err := e.readMore(ctx); err != nil {
		return err
	}
	in := e.pricingInput()
	t, err := pricing.Compute(in)
	if err != nil {
		return pricingError(err)
	}
	if err := e.decideApproval(ctx, t); err != nil {
		return err
	}
	e.version = e.cur.Version + 1
	if in.CouponDiscount, err = e.applyCoupon(ctx, t); err != nil {
		return err
	}
	if e.totals, err = pricing.Compute(in); err != nil {
		return pricingError(err)
	}
	if err := e.writeLines(ctx); err != nil {
		return err
	}
	if err := e.writeOrder(ctx); err != nil {
		return err
	}
	if err := e.applyStock(ctx); err != nil {
		return err
	}
	o := e.cur
	o.Version, o.AddressComplete = e.version, e.addrComplete
	if err := e.w.emit(ctx, e.tx, e.tenant, o, TopicOrderUpdated, e.version); err != nil {
		return err
	}
	if err := e.w.signal(ctx, e.tx, e.tenant, o); err != nil {
		return err
	}
	if !e.pending {
		return nil
	}
	return e.w.applyTransition(ctx, e.tx, &o, state.Status(o.Status), PendingDiscountApproval,
		reasonDiscountOverThreshold, e.a, e.now)
}

// readMore đọc (không khóa) phần cần cho các trường đổi: biến thể của dòng mới, đơn vị hành chính, nguồn đơn.
func (e *editRun) readMore(ctx context.Context) error {
	var fresh []LineInput
	for _, l := range e.p.lines {
		if l.item == nil {
			fresh = append(fresh, LineInput{VariantID: l.variant, Quantity: l.qty, LineDiscount: l.discount})
		}
	}
	if len(fresh) > 0 {
		ls, err := saleLines(ctx, e.tx, e.cur.SellerID, fresh)
		if err != nil {
			return err
		}
		e.sale = make(map[uuid.UUID]line, len(ls))
		for _, l := range ls {
			e.sale[l.variant.ID] = l
		}
	}
	if e.p.has(EditFieldShipAddress) {
		a := e.p.addr
		e.addrComplete = a.ProvinceCode != "" && a.WardCode != ""
		if e.addrComplete {
			if _, err := customer.ValidateUnits(ctx, e.tx, a.ProvinceCode, a.WardCode); err != nil {
				return fmt.Errorf("kiểm địa chỉ giao: %w", err)
			}
		} else if e.cur.Status == StatusConfirmed {
			return ErrAddressIncomplete
		}
	}
	if e.p.has(EditFieldTrafficSource) {
		return ActiveSource(ctx, e.tx, *e.p.trafficSource)
	}
	return nil
}

// pricingInput là đầu vào tính tiền của đơn sau sửa (chưa có mã): Key là chỉ số trong e.p.lines.
func (e *editRun) pricingInput() pricing.Input {
	in := pricing.Input{OrderDiscount: e.p.orderDiscount, ShippingFee: e.p.shippingFee}
	for i, l := range e.p.lines {
		price := e.sale[l.variant].variant.Price
		if l.item != nil {
			price = l.item.UnitPrice
		}
		in.Lines = append(in.Lines, pricing.Line{Key: i, Qty: int64(l.qty), UnitPrice: price, LineDiscount: l.discount})
	}
	return in
}

// decideApproval xét ngưỡng duyệt khi dòng hoặc giảm cấp đơn đổi (t là tiền sau sửa, chưa có mã; Quyết định 9 của
// Task 77): tỷ lệ giảm tay mới vượt ngưỡng và lớn hơn tỷ lệ cũ → người có quyền duyệt: ghi người duyệt; đơn confirmed:
// ErrDiscountNeedsApproval; đơn new, draft chưa chờ duyệt: chờ duyệt.
func (e *editRun) decideApproval(ctx context.Context, t pricing.Totals) error {
	if !e.p.has(EditFieldLines) && !e.p.has(EditFieldOrderDiscount) {
		return nil
	}
	bp := pricing.ManualDiscountBP(t)
	old := pricing.ManualDiscountBP(pricing.Totals{
		Subtotal: e.cur.Subtotal, LineDiscountTotal: e.cur.LineDiscountTotal, OrderDiscount: e.cur.OrderDiscount,
	})
	if bp == 0 || bp <= old {
		return nil
	}
	s, err := LoadSettings(ctx, e.tx)
	if err != nil {
		return err
	}
	switch {
	case !overThreshold(t, s):
	case e.a.CanApproveDiscount:
		e.record = e.a.UserID != uuid.Nil && e.cur.PendingReason != PendingDiscountApproval
	case e.cur.Status == StatusConfirmed:
		return ErrDiscountNeedsApproval
	case e.cur.PendingReason != PendingDiscountApproval:
		e.pending = true
	}
	return nil
}

// applyCoupon đổi lượt mã theo lần sửa (nhóm 10) và trả phần giảm của mã sau sửa.
func (e *editRun) applyCoupon(ctx context.Context, t pricing.Totals) (int64, error) {
	base := t.Subtotal - t.LineDiscountTotal - t.OrderDiscount
	oldBase := e.cur.Subtotal - e.cur.LineDiscountTotal - e.cur.OrderDiscount
	switch {
	case e.p.has(EditFieldCouponCode):
		if e.p.coupon != "" {
			if err := promotion.Detach(ctx, e.tx, e.cur.ID); err != nil {
				return 0, fmt.Errorf("bỏ mã giảm giá: %w", err)
			}
		}
		code := *e.p.req.coupon
		if code == "" {
			return 0, nil
		}
		eval, err := e.evalInput(ctx, base)
		if err != nil {
			return 0, err
		}
		r, err := promotion.Writer{Events: e.w.Events, Queue: e.w.Queue}.Redeem(ctx, e.tx, code,
			promotion.OrderRef{TenantID: e.tenant, ID: e.cur.ID, Version: e.version}, eval)
		if err != nil {
			return 0, fmt.Errorf("dùng mã giảm giá: %w", err)
		}
		return r.Discount, nil
	case e.p.coupon != "" && base != oldBase:
		eval, err := e.evalInput(ctx, base)
		if err != nil {
			return 0, err
		}
		d, err := promotion.Reprice(ctx, e.tx, e.cur.ID, eval)
		if err != nil {
			return 0, fmt.Errorf("tính lại mã giảm giá: %w", err)
		}
		return d, nil
	}
	return e.cur.CouponDiscount, nil
}

// evalInput là phần của đơn sau sửa mà luật mã cần (Quyết định 10: SĐT người nhận, khách gốc hiện tại).
func (e *editRun) evalInput(ctx context.Context, base int64) (promotion.EvalInput, error) {
	root, err := customer.RootOf(ctx, e.tx, e.cur.CustomerID)
	if err != nil {
		return promotion.EvalInput{}, fmt.Errorf("khách gốc của đơn: %w", err)
	}
	return promotion.EvalInput{
		Base: base, Channel: e.cur.Channel, CustomerRootID: root, Phone: e.p.addr.Phone, SellerID: e.cur.SellerID,
		Now: e.now,
	}, nil
}

// writeLines sửa dòng giữ nguyên và chèn dòng mới (nối sau position lớn nhất hiện có, kể cả dòng sắp xóa) khi tiền
// dòng có thể đổi.
func (e *editRun) writeLines(ctx context.Context) error {
	if !e.p.has(EditFieldLines) && !e.p.has(EditFieldOrderDiscount) && !e.p.has(EditFieldCouponCode) &&
		e.totals.CouponDiscount == e.cur.CouponDiscount {
		return nil
	}
	upd := store.UpdateEditedItemsParams{Now: e.now, SellerID: e.cur.SellerID, OrderID: e.cur.ID}
	var pos int32
	for _, it := range e.cur.Items {
		pos = max(pos, it.Position)
	}
	for i, l := range e.p.lines {
		lt := e.totals.Lines[i]
		if l.item != nil {
			upd.Ids = append(upd.Ids, l.item.ID)
			upd.Quantities = append(upd.Quantities, l.qty)
			upd.LineDiscounts = append(upd.LineDiscounts, l.discount)
			upd.Allocated, upd.Net = append(upd.Allocated, lt.Allocated), append(upd.Net, lt.Net)
			continue
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("sinh mã dòng đơn: %w", err)
		}
		pos++
		v := e.sale[l.variant].variant
		if err := e.q.InsertOrderItem(ctx, store.InsertOrderItemParams{
			ID: id, SellerID: e.cur.SellerID, OrderID: e.cur.ID, Position: pos, VariantID: v.ID, SkuSnapshot: v.SKU,
			NameSnapshot: v.Name, Quantity: l.qty, UnitPrice: v.Price, LineDiscount: l.discount,
			AllocatedOrderDiscount: lt.Allocated, NetAmount: lt.Net,
		}); err != nil {
			return fmt.Errorf("ghi dòng đơn mới: %w", err)
		}
		e.added[l.variant] = id
	}
	if len(upd.Ids) == 0 {
		return nil
	}
	n, err := e.q.UpdateEditedItems(ctx, upd)
	if err != nil {
		return fmt.Errorf("sửa dòng đơn: %w", err)
	}
	if n != int64(len(upd.Ids)) {
		return fmt.Errorf("%w: sửa %d/%d dòng đơn", ErrVersionConflict, n, len(upd.Ids)) // đã khóa: không xảy ra
	}
	return nil
}

// writeOrder ghi đơn sau sửa khi version còn khớp (đơn đã khóa: không khớp là lỗi lập trình, vẫn trả
// ErrVersionConflict để lùi).
func (e *editRun) writeOrder(ctx context.Context) error {
	t := e.totals
	prm := store.UpdateOrderEditParams{
		SetAddress: e.p.has(EditFieldShipAddress), AddressComplete: e.addrComplete, Subtotal: t.Subtotal,
		LineDiscountTotal: t.LineDiscountTotal, OrderDiscount: t.OrderDiscount, CouponDiscount: t.CouponDiscount,
		DiscountTotal: t.DiscountTotal, ShippingFeeCharged: t.ShippingFee, GrandTotal: t.GrandTotal,
		CarrierNote: e.p.carrierNote, CareBy: db.NullUUID(e.p.careBy), TrafficSourceID: db.NullUUID(e.p.trafficSource),
		RecordApproval: e.record, ApprovedBy: e.a.id(), Now: e.now, SellerID: e.cur.SellerID, ID: e.cur.ID,
		Version: e.cur.Version, PaymentStatus: editedPaymentStatus(e.cur, t.GrandTotal),
	}
	if prm.SetAddress {
		var err error
		if prm.NewAddress, err = json.Marshal(e.p.addr); err != nil {
			return fmt.Errorf("ghi địa chỉ giao: %w", err)
		}
		phone, reversed := normalizedShipPhone(e.p.addr.Phone)
		prm.ShipPhone, prm.ShipPhoneReversed = string(phone), reversed
	}
	v, err := e.q.UpdateOrderEdit(ctx, prm)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("ghi đơn sau sửa: %w", err)
	}
	e.version = v
	return nil
}

// editedPaymentStatus là trạng thái thanh toán của đơn cur sau khi tổng đơn thành grand: tổng không đổi hoặc đã hoàn
// tiền (refunded, M3) → giữ; còn lại theo luật của ApplyPaymentTotals (unpaid khi chưa trả, paid khi đã trả đủ, partial).
func editedPaymentStatus(cur Order, grand int64) string {
	switch {
	case grand == cur.GrandTotal || cur.PaymentStatus == "refunded":
		return cur.PaymentStatus
	case cur.PrepaidTotal == 0:
		return PaymentUnpaid
	case cur.PrepaidTotal >= grand:
		return PaymentPaid
	}
	return PaymentPartial
}

// stockChange là việc giữ hàng của một biến thể bị chạm: nhả dòng bị bỏ, đổi số lượng dòng giữ nguyên, giữ dòng mới.
type stockChange struct {
	variant, item uuid.UUID
	qty           int32
	kind          int
}

const (
	stockRelease = iota
	stockResize
	stockReserve
)

// applyStock đổi giữ hàng (nhóm 11) theo variant_id tăng dần trên mọi biến thể bị chạm (mỗi lần gọi inventory khóa
// đúng một số dư), rồi xóa dòng bị bỏ (giữ hàng của chúng đã nhả).
func (e *editRun) applyStock(ctx context.Context) error {
	var cs []stockChange
	for _, it := range e.p.removed {
		cs = append(cs, stockChange{variant: it.VariantID, item: it.ID, kind: stockRelease})
	}
	for _, l := range e.p.lines {
		switch {
		case l.item == nil:
			cs = append(cs, stockChange{variant: l.variant, item: e.added[l.variant], qty: l.qty, kind: stockReserve})
		case l.qty != l.item.Qty:
			cs = append(cs, stockChange{variant: l.variant, item: l.item.ID, qty: l.qty, kind: stockResize})
		}
	}
	slices.SortFunc(cs, func(x, y stockChange) int { return bytes.Compare(x.variant[:], y.variant[:]) })
	a := inventory.Actor{Type: e.a.kind(), ID: e.a.UserID}
	step := "edit_v" + strconv.FormatInt(e.version, 10)
	for _, c := range cs {
		var err error
		switch c.kind {
		case stockRelease:
			err = e.stock.ReleaseHeldByItems(ctx, e.tx, []uuid.UUID{c.item}, releaseOrderEdited, a)
		case stockResize:
			err = e.stock.ResizeHeld(ctx, e.tx, c.item, c.qty, step, a)
		default:
			_, err = e.stock.Reserve(ctx, e.tx, []inventory.ReserveLine{{
				OrderItemID: c.item, VariantID: c.variant, WarehouseID: e.cur.WarehouseID, Qty: c.qty,
			}}, a)
		}
		if ie, ok := errors.AsType[*inventory.InsufficientError](err); ok {
			return &OutOfStockError{VariantID: ie.VariantID, Available: ie.Available}
		}
		if err != nil {
			return fmt.Errorf("đổi giữ hàng khi sửa đơn: %w", err)
		}
	}
	if len(e.p.removed) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(e.p.removed))
	for i, it := range e.p.removed {
		ids[i] = it.ID
	}
	n, err := e.q.DeleteOrderItems(ctx, store.DeleteOrderItemsParams{SellerID: e.cur.SellerID, OrderID: e.cur.ID, Ids: ids})
	if err != nil {
		return fmt.Errorf("xóa dòng đơn bị bỏ: %w", err)
	}
	if n != int64(len(ids)) {
		return fmt.Errorf("%w: xóa %d/%d dòng đơn", ErrVersionConflict, n, len(ids)) // đã khóa: không xảy ra
	}
	return nil
}
