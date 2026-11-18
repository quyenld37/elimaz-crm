package order

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Ý định lúc tạo đơn (05 mục 2). Rỗng là yêu cầu kiểu M1 (lớp bọc CreateManual, thân createOrder không intent): tạo
// thẳng confirmed với luật địa chỉ của M1.
const (
	IntentDraft     = "draft"
	IntentNew       = "new"
	IntentConfirmed = "confirmed"
)

// Loại đơn được tạo bằng Create (CHECK của ord.orders.order_type; customer_return do luồng trả hàng tạo).
const (
	OrderTypeShip     = "ship"
	OrderTypeExchange = "exchange"
)

// WarningPhoneBelongsToOtherCustomer là cảnh báo khi SĐT nhân viên đánh dấu là của khách đang thuộc nhóm khách khác (RefID là gốc
// nhóm đó, DH13); đơn vẫn tạo, không tự gộp.
const WarningPhoneBelongsToOtherCustomer = "phone_belongs_to_other_customer"

const (
	maxCarrierNoteRunes   = 500
	maxExternalRefRunes   = 120
	maxUnitCodeRunes      = 16
	maxReferralKeys       = 20
	maxReferralValueRunes = 200
	externalRefKey        = "orders_external_ref_key"
)

// LineInput là một dòng đơn nhập: biến thể, số lượng (> 0, int32 như inventory.ReserveLine), giảm giá dòng (đồng).
type LineInput struct {
	VariantID    uuid.UUID
	Quantity     int32
	LineDiscount int64
}

// CreateInput là một lần tạo đơn (kế hoạch M2, Task M2-16). SellerID rỗng → catalog.DefaultSeller; WarehouseID rỗng →
// inventory.DefaultWarehouse; Channel rỗng → manual; OrderType rỗng → ship. ShipAddress nil chỉ được với ý định
// draft, new. CouponCode có từ Task M2-17 (trước đó khác rỗng → ErrInvalidInput).
type CreateInput struct {
	IdempotencyKey                   uuid.UUID
	SellerID, WarehouseID            uuid.UUID
	Channel, ExternalRef             string
	TrafficSourceID                  *uuid.UUID
	Customer                         customer.OrderCustomerInput
	ShipAddress                      *ShipAddress
	Lines                            []LineInput
	OrderDiscount                    int64
	CouponCode                       string
	ShippingFee                      int64
	Intent                           string
	CarrierNote                      string
	CareBy                           *uuid.UUID
	ConversationID, ChannelAccountID *uuid.UUID
	ReferralRef                      json.RawMessage
	OrderType                        string
	ReplacedReturnID                 *uuid.UUID
}

// Warning là cảnh báo kèm đơn vừa tạo (đơn vẫn tạo): Code (WarningPhoneBelongsToOtherCustomer, possible_duplicate) và
// mã liên quan.
type Warning struct {
	Code  string
	RefID uuid.UUID
}

// CreateResult là kết quả Create: đơn đủ dòng như Get, cảnh báo, Replayed khi khóa nghiệp vụ đã có đơn cùng nội dung
// (trả đơn đó, không ghi gì).
type CreateResult struct {
	Order    Order
	Warnings []Warning
	Replayed bool
}

// Create tạo đơn trong tx của bên gọi (04 mục 3 quy tắc 3: một transaction; mọi lỗi → bên gọi lùi cả transaction, không
// còn khách mới, đơn, giữ hàng nào). Thứ tự (ADR-46: khách nhóm 5 → đơn nhóm 7 → tồn nhóm 11; đọc không khóa đi trước):
//
//  1. Kiểm đầu vào không cần database (ErrInvalidInput), gộp dòng cùng biến thể, chọn người bán, kho mặc định.
//  2. Khóa nghiệp vụ đã có đơn: cùng nội dung (mã băm, fingerprint) → trả đơn đó, Replayed; khác → ErrDuplicateOrder.
//  3. Nguồn đơn đang bật (ErrTrafficSourceInactive); biến thể bán được của người bán (ErrNotForSale, ErrMixedSellers,
//     ErrVariantNotFound), đơn giá hiện hành; tiền bằng pricing.Compute (ErrLineDiscountExceeds,
//     ErrOrderDiscountExceeds, ErrTotalOverflow); địa chỉ (customer.ValidateUnits khi đủ tỉnh, xã); ý định confirmed cần
//     địa chỉ đủ (ErrAddressIncomplete).
//  4. customer.Writer.FindOrCreateForOrder; SĐT thuộc nhóm khác → cảnh báo WarningPhoneBelongsToOtherCustomer.
//  5. Chèn đơn (mã từ nextCode, cod_amount = grand_total; nháp hạn now + DraftTTL), dòng theo thứ tự nhập.
//  6. Giữ hàng (inventory.Writer.Reserve, theo variant_id tăng dần; thiếu → *OutOfStockError).
//  7. recordCreated: lịch sử, sự kiện order.created (+ order.confirmed), báo hiệu.
//
// Audit do bên gọi ghi bằng Order.AuditFields (order không nhập identity). Không giữ trạng thái giữa các lần chạy lại.
func (w Writer) Create(ctx context.Context, tx pgx.Tx, in CreateInput, a Actor, now time.Time) (CreateResult, error) {
	stock, err := w.stock()
	if err != nil {
		return CreateResult{}, err
	}
	now = now.UTC()
	p, err := prepare(in)
	if err != nil {
		return CreateResult{}, err
	}
	if err := p.resolveDefaults(ctx, tx); err != nil {
		return CreateResult{}, err
	}
	if res, found, err := replay(ctx, tx, p); found || err != nil {
		return res, err
	}
	d, err := p.draft(ctx, tx, now)
	if err != nil {
		return CreateResult{}, err
	}
	cust, err := customer.Writer{Events: w.Events, Queue: w.Queue, Realtime: w.Realtime}.FindOrCreateForOrder(
		ctx, tx, p.customerInput(), customer.Actor{Type: a.Type, ID: a.UserID}, now)
	if err != nil {
		return CreateResult{}, fmt.Errorf("khách của đơn: %w", err)
	}
	var warnings []Warning
	if cust.PhoneConflictWith != nil {
		warnings = append(warnings, Warning{Code: WarningPhoneBelongsToOtherCustomer, RefID: *cust.PhoneConflictWith})
	}
	id, reserve, err := insertOrder(ctx, tx, p, d, cust.CustomerID, a, now)
	if err != nil {
		return CreateResult{}, err
	}
	if _, err := stock.Reserve(ctx, tx, reserve, inventory.Actor{Type: a.kind(), ID: a.UserID}); err != nil {
		if ie, ok := errors.AsType[*inventory.InsufficientError](err); ok {
			return CreateResult{}, &OutOfStockError{VariantID: ie.VariantID, Available: ie.Available}
		}
		return CreateResult{}, fmt.Errorf("giữ hàng: %w", err)
	}
	o, err := Get(ctx, tx, p.seller, id)
	if err != nil {
		return CreateResult{}, err
	}
	if err := w.recordCreated(ctx, tx, o, a, now); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Order: o, Warnings: warnings}, nil
}

// prepared là đầu vào đã kiểm, chuẩn hóa (dòng đã gộp, địa chỉ đã cắt khoảng trắng, mặc định đã chọn).
type prepared struct {
	in             CreateInput
	seller, wh     uuid.UUID
	lines          []LineInput
	addr           *ShipAddress
	status         string
	channel, oType string
	unitsComplete  bool
}

// prepare kiểm phần đầu vào không cần database.
func prepare(in CreateInput) (prepared, error) {
	p := prepared{in: in, wh: in.WarehouseID, seller: in.SellerID, channel: in.Channel, oType: in.OrderType}
	if p.channel == "" {
		p.channel = ChannelManual
	}
	if p.oType == "" {
		p.oType = OrderTypeShip
	}
	if in.IdempotencyKey == uuid.Nil || len(in.Lines) == 0 || len(in.Lines) > maxItems ||
		!money(in.ShippingFee) || !money(in.OrderDiscount) ||
		!slices.Contains([]string{ChannelManual, ChannelFacebook, ChannelWeb}, p.channel) ||
		!slices.Contains([]string{OrderTypeShip, OrderTypeExchange}, p.oType) ||
		!optText(in.CarrierNote, maxCarrierNoteRunes) || !optText(in.ExternalRef, maxExternalRefRunes) ||
		!validReferral(in.ReferralRef) {
		return prepared{}, ErrInvalidInput
	}
	if in.CouponCode != "" {
		return prepared{}, fmt.Errorf("%w: mã giảm giá chưa có (Task M2-17)", ErrInvalidInput)
	}
	for _, l := range in.Lines {
		if l.VariantID == uuid.Nil || l.Quantity <= 0 || !money(l.LineDiscount) {
			return prepared{}, ErrInvalidInput
		}
	}
	var err error
	if p.lines, err = mergeLines(in.Lines); err != nil {
		return prepared{}, err
	}
	switch in.Intent {
	case IntentDraft, IntentNew:
		p.status = in.Intent
	case IntentConfirmed, "":
		p.status = StatusConfirmed
	default:
		return prepared{}, ErrInvalidInput
	}
	if in.ShipAddress == nil {
		switch in.Intent {
		case "":
			return prepared{}, fmt.Errorf("%w: đơn kiểu M1 cần địa chỉ giao", ErrInvalidInput)
		case IntentConfirmed:
			return prepared{}, ErrAddressIncomplete
		}
		return p, nil
	}
	if p.addr, err = cleanAddress(*in.ShipAddress); err != nil {
		return prepared{}, err
	}
	p.unitsComplete = p.addr.ProvinceCode != "" && p.addr.WardCode != ""
	if in.Intent == IntentConfirmed && !p.unitsComplete {
		return prepared{}, ErrAddressIncomplete
	}
	return p, nil
}

// cleanAddress kiểm địa chỉ giao theo luật M1 (tên, SĐT, số nhà) và độ dài mã tỉnh, xã; trả bản đã cắt khoảng trắng.
func cleanAddress(in ShipAddress) (*ShipAddress, error) {
	a := ShipAddress{
		Name:         strings.TrimSpace(in.Name),
		Phone:        logging.Phone(strings.TrimSpace(string(in.Phone))),
		Line:         logging.Address(strings.TrimSpace(string(in.Line))),
		ProvinceCode: strings.TrimSpace(in.ProvinceCode),
		WardCode:     strings.TrimSpace(in.WardCode),
	}
	if !validText(a.Name, maxNameRunes) || !validText(string(a.Line), maxLineRunes) ||
		!phoneRe.MatchString(string(a.Phone)) || !optText(a.ProvinceCode, maxUnitCodeRunes) ||
		!optText(a.WardCode, maxUnitCodeRunes) {
		return nil, ErrInvalidInput
	}
	return &a, nil
}

func money(v int64) bool { return v >= 0 && v <= maxMoney }

// optText: rỗng, hoặc chữ hợp lệ không quá limit ký tự.
func optText(s string, limit int) bool { return s == "" || validText(s, limit) }

// validReferral: rỗng, hoặc đối tượng JSON tối đa 20 khóa, mỗi giá trị là chuỗi tối đa 200 ký tự (hợp đồng).
func validReferral(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || m == nil || len(m) > maxReferralKeys {
		return false
	}
	for _, v := range m {
		if utf8.RuneCountInString(v) > maxReferralValueRunes || !utf8.ValidString(v) {
			return false
		}
	}
	return true
}

// resolveDefaults chọn người bán mặc định và kho chính khi đầu vào bỏ trống (chỉ đọc).
func (p *prepared) resolveDefaults(ctx context.Context, tx pgx.Tx) error {
	var err error
	if p.seller == uuid.Nil {
		if p.seller, err = catalog.DefaultSeller(ctx, tx); err != nil {
			return fmt.Errorf("người bán của đơn: %w", err)
		}
	}
	if p.wh == uuid.Nil {
		if p.wh, err = inventory.DefaultWarehouse(ctx, tx); err != nil {
			return fmt.Errorf("chọn kho giữ hàng: %w", err)
		}
	}
	return nil
}

// customerInput là đầu vào tìm-hoặc-tạo khách: SĐT khách vắng mà nhân viên đánh dấu SĐT người nhận là của khách thì
// dùng SĐT người nhận (DH12: không đánh dấu thì SĐT người nhận không chạm hồ sơ khách).
func (p prepared) customerInput() customer.OrderCustomerInput {
	c := p.in.Customer
	if c.Phone == "" && c.PhoneIsCustomers && p.addr != nil {
		c.Phone = string(p.addr.Phone)
	}
	return c
}

// fingerprint là mã băm nội dung của một lần tạo đơn (QĐ1 của Task 73): phần đầu vào còn đọc lại được từ đơn đã lưu.
// Khách chỉ tính khi đầu vào chọn khách có sẵn (khách mới được tạo trong chính lần đó).
type fingerprint struct {
	Seller, Warehouse                       uuid.UUID
	Channel, ExternalRef, CarrierNote, Type string
	TrafficSource, Customer, CareBy         *uuid.UUID
	Conversation, ChannelAccount, Replaced  *uuid.UUID
	Lines                                   []LineInput
	ShippingFee, OrderDiscount              int64
	Address                                 ShipAddress
}

func (f fingerprint) sum() []byte {
	b, _ := json.Marshal(f) //nolint:errchkjson // chỉ chuỗi, số, uuid
	s := sha256.Sum256(b)
	return s[:]
}

// fingerprintOf: mã băm của đầu vào đã chuẩn hóa.
func (p prepared) fingerprint() []byte {
	f := fingerprint{
		Seller: p.seller, Warehouse: p.wh, Channel: p.channel, ExternalRef: p.in.ExternalRef,
		CarrierNote: p.in.CarrierNote, Type: p.oType, TrafficSource: p.in.TrafficSourceID, Customer: p.in.Customer.CustomerID,
		CareBy: p.in.CareBy, Conversation: p.in.ConversationID, ChannelAccount: p.in.ChannelAccountID,
		Replaced: p.in.ReplacedReturnID, Lines: p.lines, ShippingFee: p.in.ShippingFee, OrderDiscount: p.in.OrderDiscount,
	}
	if p.addr != nil {
		f.Address = *p.addr
	}
	return f.sum()
}

// storedFingerprint: mã băm của đơn đã lưu o, so được với prepared.fingerprint của đầu vào withCustomer.
func storedFingerprint(o Order, withCustomer bool) []byte {
	f := fingerprint{
		Seller: o.SellerID, Warehouse: o.WarehouseID, Channel: o.Channel, CarrierNote: o.CarrierNote, Type: o.OrderType,
		TrafficSource: o.TrafficSourceID, CareBy: o.CareBy, Conversation: o.ConversationID,
		ChannelAccount: o.ChannelAccountID, Replaced: o.ReplacedReturnID, ShippingFee: o.ShippingFee,
		OrderDiscount: o.OrderDiscount, Address: o.ShipAddress,
	}
	if o.ExternalRef != nil {
		f.ExternalRef = *o.ExternalRef
	}
	if withCustomer {
		f.Customer = &o.CustomerID
	}
	for _, it := range o.Items {
		f.Lines = append(f.Lines, LineInput{VariantID: it.VariantID, Quantity: it.Qty, LineDiscount: it.LineDiscount})
	}
	return f.sum()
}

// replay: khóa nghiệp vụ của p đã có đơn → (đơn, true, nil) khi cùng nội dung, ErrDuplicateOrder khi khác.
func replay(ctx context.Context, tx pgx.Tx, p prepared) (CreateResult, bool, error) {
	id, err := store.New(tx).OrderByIdempotencyKey(ctx, store.OrderByIdempotencyKeyParams{
		SellerID: p.seller, IdempotencyKey: p.in.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateResult{}, false, nil
	}
	if err != nil {
		return CreateResult{}, false, fmt.Errorf("đọc đơn theo khóa nghiệp vụ: %w", err)
	}
	o, err := Get(ctx, tx, p.seller, id)
	if err != nil {
		return CreateResult{}, false, err
	}
	if !bytes.Equal(storedFingerprint(o, p.in.Customer.CustomerID != nil), p.fingerprint()) {
		return CreateResult{}, false, ErrDuplicateOrder
	}
	return CreateResult{Order: o, Replayed: true}, true, nil
}

// drafted là phần đã kiểm với database và tính của đơn, trước khi ghi.
type drafted struct {
	lines          []line
	totals         pricing.Totals
	addrComplete   bool
	draftExpiresAt pgtype.Timestamptz
}

// draft đọc (không khóa) nguồn đơn, biến thể, đơn vị hành chính, cài đặt và tính tiền.
func (p prepared) draft(ctx context.Context, tx pgx.Tx, now time.Time) (drafted, error) {
	if p.in.TrafficSourceID != nil {
		if err := ActiveSource(ctx, tx, *p.in.TrafficSourceID); err != nil {
			return drafted{}, err
		}
	}
	lines, err := saleLines(ctx, tx, p.seller, p.lines)
	if err != nil {
		return drafted{}, err
	}
	in := pricing.Input{OrderDiscount: p.in.OrderDiscount, ShippingFee: p.in.ShippingFee}
	for i, l := range lines {
		in.Lines = append(in.Lines, pricing.Line{
			Key: i, Qty: int64(l.qty), UnitPrice: l.variant.Price, LineDiscount: l.discount,
		})
	}
	totals, err := pricing.Compute(in)
	if err != nil {
		return drafted{}, pricingError(err)
	}
	d := drafted{lines: lines, totals: totals}
	if p.unitsComplete {
		if _, err := customer.ValidateUnits(ctx, tx, p.addr.ProvinceCode, p.addr.WardCode); err != nil {
			return drafted{}, fmt.Errorf("kiểm địa chỉ giao: %w", err)
		}
		d.addrComplete = true
	}
	if p.status == StatusDraft {
		s, err := LoadSettings(ctx, tx)
		if err != nil {
			return drafted{}, err
		}
		d.draftExpiresAt = pgtype.Timestamptz{Time: now.Add(s.DraftTTL), Valid: true}
	}
	return d, nil
}

// pricingError đổi lỗi của pricing.Compute sang sentinel của order (ErrMoneyOverflow → ErrTotalOverflow của M1, Q30).
func pricingError(err error) error {
	for _, m := range [...][2]error{
		{pricing.ErrLineDiscountExceeds, ErrLineDiscountExceeds},
		{pricing.ErrOrderDiscountExceeds, ErrOrderDiscountExceeds},
		{pricing.ErrMoneyOverflow, ErrTotalOverflow},
	} {
		if errors.Is(err, m[0]) {
			return m[1]
		}
	}
	return fmt.Errorf("%w: %w", ErrInvalidInput, err) // pricing.ErrNegative: đã chặn ở prepare
}

// insertOrder lấy mã đơn, chèn đơn và dòng đơn (position theo thứ tự nhập); trả mã đơn và các dòng giữ hàng theo
// variant_id tăng dần.
func insertOrder(ctx context.Context, tx pgx.Tx, p prepared, d drafted, customerID uuid.UUID, a Actor, now time.Time,
) (uuid.UUID, []inventory.ReserveLine, error) {
	code, err := nextCode(ctx, tx)
	if err != nil {
		return uuid.Nil, nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("sinh mã đơn: %w", err)
	}
	addrJSON, shipPhone, reversed := []byte("{}"), logging.Phone(""), ""
	if p.addr != nil {
		if addrJSON, err = json.Marshal(*p.addr); err != nil {
			return uuid.Nil, nil, fmt.Errorf("ghi địa chỉ giao: %w", err)
		}
		shipPhone, reversed = normalizedShipPhone(p.addr.Phone)
	}
	// version = số sự kiện của lần tạo (created seq 1, confirmed seq 2): version luôn bằng seq của sự kiện cuối.
	version, confirmedAt := int64(1), pgtype.Timestamptz{}
	if p.status == StatusConfirmed {
		version, confirmedAt = 2, pgtype.Timestamptz{Time: now, Valid: true}
	}
	t := d.totals
	err = store.New(tx).InsertOrder(ctx, store.InsertOrderParams{
		ID: id, SellerID: p.seller, Code: code, CustomerID: customerID, Channel: p.channel,
		IdempotencyKey: p.in.IdempotencyKey, Status: p.status, ShipAddress: addrJSON, ShipPhone: string(shipPhone),
		ShipPhoneReversed: reversed, AddressComplete: d.addrComplete, Subtotal: t.Subtotal,
		LineDiscountTotal: t.LineDiscountTotal, OrderDiscount: t.OrderDiscount, CouponDiscount: t.CouponDiscount,
		DiscountTotal: t.DiscountTotal, ShippingFeeCharged: t.ShippingFee, CodAmount: t.GrandTotal,
		GrandTotal: t.GrandTotal, CreatedBy: a.id(), Version: version, WarehouseID: p.wh,
		TrafficSourceID: nullUUID(p.in.TrafficSourceID), ExternalRef: pgtype.Text{String: p.in.ExternalRef, Valid: p.in.ExternalRef != ""},
		ReferralRef: p.in.ReferralRef, DraftExpiresAt: d.draftExpiresAt, CarrierNote: p.in.CarrierNote,
		CareBy: nullUUID(p.in.CareBy), ConversationID: nullUUID(p.in.ConversationID),
		ChannelAccountID: nullUUID(p.in.ChannelAccountID), OrderType: p.oType, ReplacedReturnID: nullUUID(p.in.ReplacedReturnID),
		ConfirmedAt: confirmedAt, ConfirmedBy: confirmedBy(p.status, a), Now: now,
	})
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == pgUniqueViolate {
		switch pe.ConstraintName {
		case idempotencyIdx:
			return uuid.Nil, nil, ErrDuplicateOrder
		case externalRefKey:
			return uuid.Nil, nil, fmt.Errorf("%w: mã ngoài đã có đơn", ErrInvalidInput)
		}
	}
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("ghi đơn: %w", err)
	}
	reserve, err := insertItems(ctx, tx, id, p, d)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return id, reserve, nil
}

func confirmedBy(status string, a Actor) pgtype.UUID {
	if status != StatusConfirmed {
		return pgtype.UUID{}
	}
	return a.id()
}

// nullUUID là cột uuid có thể rỗng từ con trỏ (nil → NULL).
func nullUUID(u *uuid.UUID) (out pgtype.UUID) {
	if u != nil {
		out = pgtype.UUID{Bytes: *u, Valid: true}
	}
	return out
}

// insertItems chèn dòng đơn theo thứ tự nhập (position 1, 2, …), ảnh chụp mã, tên, đơn giá; trả dòng giữ hàng theo
// variant_id tăng dần (ADR-46 nhóm 11).
func insertItems(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, p prepared, d drafted) ([]inventory.ReserveLine, error) {
	q := store.New(tx)
	reserve := make([]inventory.ReserveLine, len(d.lines))
	for i, l := range d.lines {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("sinh mã dòng đơn: %w", err)
		}
		lt := d.totals.Lines[i]
		if err := q.InsertOrderItem(ctx, store.InsertOrderItemParams{
			ID: id, SellerID: p.seller, OrderID: orderID, Position: int32(i + 1), VariantID: l.variant.ID,
			SkuSnapshot: l.variant.SKU, NameSnapshot: l.variant.Name, Quantity: l.qty, UnitPrice: l.variant.Price,
			LineDiscount: l.discount, AllocatedOrderDiscount: lt.Allocated, NetAmount: lt.Net,
		}); err != nil {
			return nil, fmt.Errorf("ghi dòng đơn: %w", err)
		}
		reserve[i] = inventory.ReserveLine{OrderItemID: id, VariantID: l.variant.ID, WarehouseID: p.wh, Qty: l.qty}
	}
	slices.SortFunc(reserve, func(x, y inventory.ReserveLine) int { return bytes.Compare(x.VariantID[:], y.VariantID[:]) })
	return reserve, nil
}

// line là một dòng đơn đã kiểm: biến thể bán được (đơn giá hiện hành, Q23), số lượng, giảm dòng.
type line struct {
	variant  catalog.SaleVariant
	qty      int32
	discount int64
}

// saleLines đọc các biến thể của đơn trong một câu (catalog.VariantsForSale); biến thể thiếu trong kết quả được hỏi
// lại từng cái theo thứ tự nhập để biết lý do: ngừng bán → ErrNotForSale; có ở người bán khác của shop →
// ErrMixedSellers (KH14); không có trong shop (kể cả của tenant khác) → ErrVariantNotFound.
func saleLines(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, in []LineInput) ([]line, error) {
	ids := make([]uuid.UUID, len(in))
	for i, l := range in {
		ids[i] = l.VariantID
	}
	sale, err := catalog.VariantsForSale(ctx, tx, sellerID, ids)
	if err != nil {
		return nil, fmt.Errorf("kiểm biến thể: %w", err)
	}
	out := make([]line, len(in))
	for i, l := range in {
		v, ok := sale[l.VariantID]
		if !ok {
			return nil, missingVariant(ctx, tx, sellerID, l.VariantID)
		}
		out[i] = line{variant: v, qty: l.Quantity, discount: l.LineDiscount}
	}
	return out, nil
}

// missingVariant trả lý do biến thể id không bán được cho người bán sellerID.
func missingVariant(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) error {
	_, err := catalog.VariantForSale(ctx, tx, sellerID, id)
	switch {
	case errors.Is(err, catalog.ErrNotForSale):
		return ErrNotForSale
	case errors.Is(err, catalog.ErrNotFound):
		found, err := store.New(tx).VariantExists(ctx, id)
		if err != nil {
			return fmt.Errorf("kiểm biến thể: %w", err)
		}
		if found {
			return ErrMixedSellers
		}
		return ErrVariantNotFound
	case err != nil:
		return fmt.Errorf("kiểm biến thể: %w", err)
	}
	// Vừa đổi sang bán được giữa hai câu: coi như ngừng bán lúc kiểm; nhân viên gửi lại.
	return ErrNotForSale
}
