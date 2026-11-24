package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Cách website gửi đơn (WebOrderInput.SubmittedVia, QĐ15): thẳng lúc khách đang trên trang, hay từ hàng đợi cục bộ khi
// CRM từng ngừng (khách đã nhận trang kết quả).
const (
	SubmittedSync   = "sync"
	SubmittedQueued = "queued"
)

// ActorService là Actor.Type của khóa dịch vụ (website gửi đơn; UserID là mã khóa).
const ActorService = "service"

// WebCustomer là người đặt trên website: SĐT (thành danh tính SĐT của khách), tên, mã tài khoản website (rỗng: khách
// vãng lai; có thì thành danh tính web_account nếu chưa thuộc hồ sơ nào).
type WebCustomer struct {
	Phone        logging.Phone
	Name         string
	WebAccountID string
}

// WebQuoted là báo giá website đã hiện cho khách (QuoteWeb lúc trước): dấu vân tay và tổng.
type WebQuoted struct {
	Fingerprint string
	GrandTotal  int64
}

// WebOrderInput là một đơn website gửi (kế hoạch M5, Task 193). ExternalRef là mã đơn của website (khóa nghiệp vụ);
// Lines, CouponCode, ShippingFee như WebQuoteInput; Note là ghi chú của khách (thành ghi chú nội bộ của đơn);
// ReferralRef như CreateInput. Giá, giảm chiến dịch, giảm mã luôn do CRM tính lại; số của website chỉ để so.
type WebOrderInput struct {
	ExternalRef  string
	Customer     WebCustomer
	ShipAddress  ShipAddress
	Lines        []WebLine
	CouponCode   string
	ShippingFee  int64
	Quoted       WebQuoted
	SubmittedVia string
	Note         string
	ReferralRef  json.RawMessage
}

// WebOrderResult là đơn web đã tạo, hoặc đơn đã có của cùng ExternalRef (Replayed).
type WebOrderResult struct {
	OrderID     uuid.UUID
	Code        string
	Status      string
	GrandTotal  int64
	PriceReview bool
	Replayed    bool
}

// QuoteChangedError là lỗi khi đơn gửi thẳng mà giá hay tổng CRM khác số website đã báo; Quote là báo giá CRM hiện tại.
type QuoteChangedError struct {
	Quote WebQuote
}

func (e *QuoteChangedError) Error() string {
	return fmt.Sprintf("order: báo giá web đã đổi (tổng CRM %d)", e.Quote.GrandTotal)
}

// ItemUnavailableError liệt kê các biến thể (thứ tự gửi) không bán được trên web (ngừng bán, không có).
type ItemUnavailableError struct {
	VariantIDs []uuid.UUID
}

func (e *ItemUnavailableError) Error() string {
	return fmt.Sprintf("order: %d biến thể không bán được", len(e.VariantIDs))
}

// CouponInvalidError là lỗi khi đơn gửi thẳng mà mã không áp được; Reason là not_found hay lý do từ chối của promotion.
type CouponInvalidError struct {
	Reason string
}

func (e *CouponInvalidError) Error() string {
	return "order: mã giảm giá không dùng được (" + e.Reason + ")"
}

// errWebTotalMoved: tổng đơn vừa ghi khác báo giá trong cùng transaction (giá, chiến dịch đổi giữa hai câu đọc).
var errWebTotalMoved = errors.New("order: tổng đơn web khác báo giá")

// errWebRowsMoved: câu ghi cột web không gặp đơn hay dòng vừa chèn (không xảy ra trong cùng transaction).
var errWebRowsMoved = errors.New("order: không ghi được cột web của đơn vừa tạo")

const maxWebNoteRunes = 2000

// CreateFromWeb tạo đơn website in trong tx của bên gọi (một transaction; lỗi nào bên gọi cũng lùi cả). Người bán là
// catalog.DefaultSeller, nguồn là nguồn có key web (SourceByKey), kênh web, ý định new, actor a là khóa dịch vụ. Thứ tự:
//
//  1. Đã có đơn web của ExternalRef → trả đơn đó (Replayed), không ghi gì. Chưa có mà shop đang tạm dừng nhận đơn web
//     (GetWebIntake, đọc không khóa) và đơn gửi thẳng (sync) → ErrWebIntakePaused; queued vẫn nhận (Task 456).
//  2. QuoteWeb trong tx: dòng không bán được → *ItemUnavailableError (của người bán khác → ErrMixedSellers).
//  3. Mã không áp được: sync → *CouponInvalidError; queued → bỏ mã, price_review. Giá dòng hay tổng CRM khác số website
//     đã báo: sync → *QuoteChangedError; queued → tạo theo số CRM, price_review, lưu báo giá web vào web_quote (QĐ15).
//  4. Tạo đơn bằng đường của Create (savepoint) với giảm dòng = giảm chiến dịch của báo giá, dòng chiến dịch cấm mã
//     ngoài nền mã (webPricing; cùng couponBases với QuoteWeb), không xét ngưỡng duyệt giảm tay. Khách theo SĐT
//     (PhoneIsCustomers, nguồn web). Hết hàng → ErrOutOfStock (*OutOfStockError); mã bị Redeem từ chối (lượt mỗi khách)
//     → như bước 3; cùng ExternalRef vừa được tx khác tạo (unique, hay Create trả Replayed) → trả đơn đó, không ghi gì.
//  5. Tổng đơn khác báo giá (đổi giữa hai câu đọc) → như lệch giá ở bước 3. Ghi cột web lên đơn và dòng (chiến dịch:
//     mã, revision, phần trăm, số giảm; QĐ33), ghi chú của khách thành ghi chú nội bộ, WebAccountID thành danh tính
//     web_account của khách (đã thuộc hồ sơ khác → bỏ qua, không gộp).
//
// Thứ tự khóa như Create (ADR-46): khách (5) → đơn mới (7) → coupons (10) → stock_balances (11); danh tính web chỉ
// khóa lại dòng khách tx đã giữ. Không giữ trạng thái giữa các lần chạy lại.
func (w Writer) CreateFromWeb(ctx context.Context, tx pgx.Tx, in WebOrderInput, a Actor, now time.Time,
) (WebOrderResult, error) {
	now = now.UTC()
	in.ExternalRef = strings.TrimSpace(in.ExternalRef)
	if err := validateWebOrder(in); err != nil {
		return WebOrderResult{}, err
	}
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return WebOrderResult{}, fmt.Errorf("người bán của đơn web: %w", err)
	}
	if res, found, err := webOrderByRef(ctx, tx, seller, in.ExternalRef); found || err != nil {
		return res, err
	}
	if err := checkWebIntake(ctx, tx, in.SubmittedVia); err != nil {
		return WebOrderResult{}, err
	}
	src, err := SourceByKey(ctx, tx, SourceKeyWeb)
	if err != nil {
		return WebOrderResult{}, err
	}
	o := webOrder{w: w, in: in, seller: seller, source: src, a: a, now: now, code: strings.TrimSpace(in.CouponCode)}
	for {
		res, retry, err := o.attempt(ctx, tx)
		if !retry {
			return res, err
		}
	}
}

// validateWebOrder kiểm phần riêng của đơn web (dòng, phí, mã: QuoteWeb; địa chỉ, tham chiếu: Create).
func validateWebOrder(in WebOrderInput) error {
	if in.ExternalRef == "" || !optText(in.ExternalRef, maxExternalRefRunes) ||
		(in.SubmittedVia != SubmittedSync && in.SubmittedVia != SubmittedQueued) ||
		!money(in.Quoted.GrandTotal) || utf8.RuneCountInString(in.Note) > maxWebNoteRunes {
		return fmt.Errorf("%w: đơn web", ErrInvalidInput)
	}
	return nil
}

// webOrderByRef: đơn web đã có của mã ngoài ref → (kết quả Replayed, true).
func webOrderByRef(ctx context.Context, tx pgx.Tx, seller uuid.UUID, ref string) (WebOrderResult, bool, error) {
	r, err := store.New(tx).WebOrderByExternalRef(ctx, store.WebOrderByExternalRefParams{
		SellerID: seller, ExternalRef: pgtype.Text{String: ref, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WebOrderResult{}, false, nil
	}
	if err != nil {
		return WebOrderResult{}, false, fmt.Errorf("đọc đơn web theo mã ngoài: %w", err)
	}
	return WebOrderResult{
		OrderID: r.ID, Code: r.Code, Status: r.Status, GrandTotal: r.GrandTotal, PriceReview: r.PriceReview, Replayed: true,
	}, true, nil
}

// webOrder là một lần CreateFromWeb: code là mã đang dùng (bỏ khi queued mà mã không áp được), review là cờ
// price_review đã biết.
type webOrder struct {
	w      Writer
	in     WebOrderInput
	seller uuid.UUID
	source uuid.UUID
	a      Actor
	now    time.Time
	code   string
	review bool
}

// attempt báo giá lại, kiểm lệch và tạo đơn trong một savepoint; retry khi mã bị Redeem từ chối ở đường queued (lần
// sau không mã).
func (o *webOrder) attempt(ctx context.Context, tx pgx.Tx) (WebOrderResult, bool, error) {
	q, err := o.quote(ctx, tx)
	if err != nil {
		return WebOrderResult{}, false, err
	}
	if q.Coupon != nil && !q.Coupon.Applied {
		if o.in.SubmittedVia == SubmittedSync {
			return WebOrderResult{}, false, &CouponInvalidError{Reason: q.Coupon.Reason}
		}
		o.code, o.review = "", true
		return WebOrderResult{}, true, nil
	}
	if q.Changed || q.GrandTotal != o.in.Quoted.GrandTotal {
		if o.in.SubmittedVia == SubmittedSync {
			return WebOrderResult{}, false, &QuoteChangedError{Quote: q}
		}
		o.review = true
	}
	var res WebOrderResult
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var err error
		res, err = o.insert(ctx, sp, q)
		return err
	})
	if err == nil {
		return res, false, nil
	}
	return o.afterFailure(ctx, tx, err)
}

// afterFailure xử lý lỗi của lần tạo đã lùi savepoint.
func (o *webOrder) afterFailure(ctx context.Context, tx pgx.Tx, err error) (WebOrderResult, bool, error) {
	if re, ok := errors.AsType[*promotion.RejectError](err); ok {
		if o.in.SubmittedVia == SubmittedSync {
			return WebOrderResult{}, false, &CouponInvalidError{Reason: string(re.Reason)}
		}
		o.code, o.review = "", true
		return WebOrderResult{}, true, nil
	}
	switch {
	case errors.Is(err, ErrDuplicateOrder), errors.Is(err, ErrInvalidInput):
		// Cùng ExternalRef vừa được tx khác tạo (unique khóa nghiệp vụ hay mã ngoài): trả đơn đó.
		if res, found, lookErr := webOrderByRef(ctx, tx, o.seller, o.in.ExternalRef); found || lookErr != nil {
			return res, false, lookErr
		}
	case errors.Is(err, errWebTotalMoved), errors.Is(err, ErrNotForSale), errors.Is(err, ErrVariantNotFound):
		// Giá, chiến dịch, trạng thái bán đổi giữa báo giá và tạo đơn: báo giá lại (món ngừng bán → ItemUnavailableError);
		// gửi thẳng → báo giá mới cho website. Từ hàng đợi chỉ tới đây khi món vừa ngừng bán rồi bán lại: trả nguyên lỗi.
		q, qErr := o.quote(ctx, tx)
		if qErr != nil {
			return WebOrderResult{}, false, qErr
		}
		if o.in.SubmittedVia == SubmittedSync {
			return WebOrderResult{}, false, &QuoteChangedError{Quote: q}
		}
	}
	return WebOrderResult{}, false, err
}

// quote là QuoteWeb của đơn với mã o.code; dòng không bán được → *ItemUnavailableError (người bán khác →
// ErrMixedSellers).
func (o *webOrder) quote(ctx context.Context, tx pgx.Tx) (WebQuote, error) {
	phone := o.in.Customer.Phone
	q, err := QuoteWeb(ctx, tx, WebQuoteInput{
		Lines: o.in.Lines, CouponCode: o.code, CustomerPhone: &phone, ShippingFee: o.in.ShippingFee,
	}, o.now)
	if err != nil {
		return WebQuote{}, err
	}
	var gone []uuid.UUID
	for _, l := range q.Lines {
		if l.Problem != ProblemUnavailable {
			continue
		}
		switch err := missingVariant(ctx, tx, o.seller, l.VariantID); {
		case errors.Is(err, ErrMixedSellers):
			return WebQuote{}, fmt.Errorf("đơn web: %w", err)
		case errors.Is(err, ErrNotForSale), errors.Is(err, ErrVariantNotFound):
			gone = append(gone, l.VariantID)
		default:
			return WebQuote{}, err
		}
	}
	if len(gone) > 0 {
		return WebQuote{}, &ItemUnavailableError{VariantIDs: gone}
	}
	return q, nil
}

// insert tạo đơn theo báo giá q trong savepoint sp và ghi phần web.
func (o *webOrder) insert(ctx context.Context, sp pgx.Tx, q WebQuote) (WebOrderResult, error) {
	web := &webPricing{noCoupon: map[uuid.UUID]bool{}}
	lines := make([]LineInput, len(q.Lines))
	for i, l := range q.Lines {
		lines[i] = LineInput{VariantID: l.VariantID, Quantity: int32(l.Qty), LineDiscount: l.CampaignDiscount} //nolint:gosec // G115: Qty ≤ MaxWebQty
		if l.Campaign != nil && !l.Campaign.AllowCoupon {
			web.noCoupon[l.VariantID] = true
		}
	}
	addr := o.in.ShipAddress
	cr, err := o.w.create(ctx, sp, CreateInput{
		IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceURL, []byte("web:"+o.in.ExternalRef)),
		SellerID:       o.seller, Channel: ChannelWeb, ExternalRef: o.in.ExternalRef, TrafficSourceID: &o.source,
		Customer: customer.OrderCustomerInput{
			FullName: o.in.Customer.Name, Phone: string(o.in.Customer.Phone), PhoneIsCustomers: true, Source: customer.SourceWeb,
		},
		ShipAddress: &addr, Lines: lines, CouponCode: o.code, ShippingFee: o.in.ShippingFee, Intent: IntentNew,
		ReferralRef: o.in.ReferralRef,
	}, web, o.a, o.now)
	if err != nil {
		return WebOrderResult{}, err
	}
	if cr.Replayed {
		// Tx khác cùng ExternalRef (cùng khóa nghiệp vụ) vừa commit sau bước 1: trả đơn đó, không ghi gì lên nó.
		res, _, err := webOrderByRef(ctx, sp, o.seller, o.in.ExternalRef)
		return res, err
	}
	ord := cr.Order
	if ord.GrandTotal != q.GrandTotal {
		if o.in.SubmittedVia == SubmittedSync {
			return WebOrderResult{}, errWebTotalMoved
		}
		o.review = true
	}
	if err := o.writeWebFields(ctx, sp, ord.ID, q); err != nil {
		return WebOrderResult{}, err
	}
	if strings.TrimSpace(o.in.Note) != "" {
		if _, err := AddNote(ctx, sp, o.seller, ord.ID, o.in.Note, o.a, o.now); err != nil {
			return WebOrderResult{}, err
		}
	}
	if acc := strings.TrimSpace(o.in.Customer.WebAccountID); acc != "" {
		_, err := customer.Writer{Events: o.w.Events, Queue: o.w.Queue, Realtime: o.w.Realtime}.AddIdentity(ctx, sp,
			ord.CustomerID, customer.IdentityInput{Kind: customer.IdentityWebAccount, Value: acc, Source: customer.SourceWeb},
			customer.Actor{Type: o.a.Type, ID: o.a.UserID})
		if _, taken := errors.AsType[*customer.IdentityTakenError](err); err != nil && !taken {
			return WebOrderResult{}, fmt.Errorf("danh tính web của khách: %w", err)
		}
	}
	return WebOrderResult{
		OrderID: ord.ID, Code: ord.Code, Status: ord.Status, GrandTotal: ord.GrandTotal, PriceReview: o.review,
	}, nil
}

// webQuoteRecord là báo giá website lưu ở ord.orders.web_quote khi price_review: chỉ số tiền, mã biến thể, mã giảm.
type webQuoteRecord struct {
	Fingerprint string               `json:"fingerprint"`
	GrandTotal  int64                `json:"grandTotal"`
	ShippingFee int64                `json:"shippingFee"`
	CouponCode  string               `json:"couponCode,omitempty"`
	Lines       []webQuoteRecordLine `json:"lines"`
}

type webQuoteRecordLine struct {
	VariantID       uuid.UUID `json:"variantId"`
	Qty             int       `json:"qty"`
	QuotedUnitPrice int64     `json:"quotedUnitPrice"`
}

// writeWebFields ghi price_review, web_quote, submitted_via lên đơn id và chiến dịch lên các dòng của q.
func (o *webOrder) writeWebFields(ctx context.Context, tx pgx.Tx, id uuid.UUID, q WebQuote) error {
	var raw []byte
	if o.review {
		rec := webQuoteRecord{
			Fingerprint: o.in.Quoted.Fingerprint, GrandTotal: o.in.Quoted.GrandTotal, ShippingFee: o.in.ShippingFee,
			CouponCode: strings.ToUpper(strings.TrimSpace(o.in.CouponCode)), Lines: make([]webQuoteRecordLine, len(o.in.Lines)),
		}
		for i, l := range o.in.Lines {
			rec.Lines[i] = webQuoteRecordLine(l)
		}
		var err error
		if raw, err = json.Marshal(rec); err != nil {
			return fmt.Errorf("ghi báo giá web: %w", err)
		}
	}
	st := store.New(tx)
	n, err := st.SetOrderWebFields(ctx, store.SetOrderWebFieldsParams{
		PriceReview: o.review, WebQuote: raw, SubmittedVia: pgtype.Text{String: o.in.SubmittedVia, Valid: true},
		SellerID: o.seller, ID: id,
	})
	if err != nil {
		return fmt.Errorf("ghi cột web của đơn: %w", err)
	}
	if n != 1 {
		return errWebRowsMoved
	}
	for _, l := range q.Lines {
		if l.Campaign == nil {
			continue
		}
		n, err := st.SetOrderItemWebCampaign(ctx, store.SetOrderItemWebCampaignParams{
			CampaignID: pgtype.UUID{Bytes: l.Campaign.ID, Valid: true}, CampaignRevision: pgtype.Int4{Int32: int32(l.Campaign.Revision), Valid: true}, //nolint:gosec // G115: revision của promotion là int4
			CampaignPercent:  pgtype.Int4{Int32: int32(l.Campaign.Percent), Valid: true}, //nolint:gosec // G115: 1–99
			CampaignDiscount: pgtype.Int8{Int64: l.CampaignDiscount, Valid: true},
			SellerID:         o.seller, OrderID: id, VariantID: l.VariantID,
		})
		if err != nil {
			return fmt.Errorf("ghi chiến dịch lên dòng đơn: %w", err)
		}
		if n != 1 {
			return errWebRowsMoved
		}
	}
	return nil
}
