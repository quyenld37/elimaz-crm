package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Giới hạn của một lần báo giá web (kế hoạch M5, Task 192): 1–50 dòng, mỗi dòng 1–20 đơn vị.
const (
	MaxWebLines = 50
	MaxWebQty   = 20
)

// Vấn đề của một dòng báo giá (QuotedLine.Problem; rỗng: không có). Một dòng chỉ báo một vấn đề, theo thứ tự ưu tiên
// unavailable → out_of_stock → price_changed; lệch giá vẫn thấy qua WebQuote.Changed và UnitPrice.
const (
	ProblemUnavailable  = "unavailable"
	ProblemOutOfStock   = "out_of_stock"
	ProblemPriceChanged = "price_changed"
)

// WebLine là một dòng giỏ website gửi lên: biến thể, số lượng và đơn giá website đang hiện (giá niêm yết, chưa trừ
// chiến dịch).
type WebLine struct {
	VariantID       uuid.UUID
	Qty             int
	QuotedUnitPrice int64
}

// WebQuoteInput là giỏ website cần báo giá. CouponCode rỗng: không dùng mã (cắt khoảng trắng hai đầu, không phân biệt
// hoa thường). CustomerPhone (nil: chưa có) chỉ để kiểm mã gắn SĐT. ShippingFee là phí giao website đã tính.
// Shipping (nil: website không gửi) là lựa chọn giao hàng khách chọn, đã được lớp HTTP đối chiếu (Task 490).
type WebQuoteInput struct {
	Lines         []WebLine
	CouponCode    string
	CustomerPhone *logging.Phone
	ShippingFee   int64
	Shipping      *WebShipChoice
}

// LineCampaign là chiến dịch web CRM áp cho dòng (promotion.ActiveForVariants).
type LineCampaign struct {
	ID          uuid.UUID
	Revision    int
	Percent     int
	AllowCoupon bool
}

// QuotedLine là một dòng đã báo giá theo số của CRM, cùng thứ tự với đầu vào. UnitPrice là giá niêm yết CRM (0 khi
// unavailable); CampaignDiscount = số lượng × giảm mỗi đơn vị của chiến dịch; CouponDiscount là phần mã chia về dòng;
// Net = số lượng × UnitPrice − hai khoản giảm.
type QuotedLine struct {
	VariantID                                   uuid.UUID
	Qty                                         int
	UnitPrice, CampaignDiscount, CouponDiscount int64
	Net                                         int64
	Campaign                                    *LineCampaign
	Problem                                     string
}

// CouponOutcome là kết quả mã: Applied, hoặc Reason (not_found hay lý do từ chối của promotion).
type CouponOutcome struct {
	Code    string
	Applied bool
	Reason  string
}

// WebQuote là báo giá: Merchandise = Σ số lượng × UnitPrice của dòng bán được; GrandTotal = Merchandise −
// CampaignDiscount − CouponDiscount + ShippingFee = Σ Net + ShippingFee. Coupon nil khi không gửi mã. Changed khi có
// dòng bán được mà giá website gửi khác giá CRM. Fingerprint là hex sha256 của JSON các dòng theo số CRM, các khoản
// giảm, phí, mã (chữ hoa).
type WebQuote struct {
	Lines                                                                  []QuotedLine
	Merchandise, CampaignDiscount, CouponDiscount, ShippingFee, GrandTotal int64
	Coupon                                                                 *CouponOutcome
	Fingerprint                                                            string
	Changed                                                                bool
}

// QuoteWeb báo giá giỏ in cho người bán catalog.DefaultSeller ở now, chỉ đọc trong tx (tenant), không khóa, không qua
// bộ đệm: giá từ catalog.VariantsForSale (thiếu, ngừng bán, của người bán khác → unavailable), còn hàng ở kho mặc định
// (inventory.Availability; có thể bán < số lượng → out_of_stock; không giữ hàng), chiến dịch đang chạy từ
// promotion.ActiveForVariants, mã từ promotion.Check (kênh web; nền = Σ (tiền dòng − giảm chiến dịch) của dòng bán được
// không thuộc chiến dịch cấm mã, QĐ29; mã chia về các dòng đó bằng pricing.Allocate). Lượt mỗi khách của mã không kiểm
// (báo giá chưa biết hồ sơ khách; tạo đơn kiểm). Đầu vào sai → ErrInvalidInput; tổng vượt 2^53 − 1 → ErrTotalOverflow.
// Lựa chọn giao hàng có vấn đề, hay ShippingFee lớn hơn phí của lựa chọn → ErrShipCarrierUnavailable,
// ErrShipFeeMismatch, ErrShipUnverified (Task 490); có lựa chọn thì dấu vân tay gồm cả lựa chọn.
// Kết quả có thể khác lúc tạo đơn; tạo đơn tính lại trong transaction của nó.
func QuoteWeb(ctx context.Context, tx pgx.Tx, in WebQuoteInput, now time.Time) (WebQuote, error) {
	code, err := validateWebQuote(in)
	if err != nil {
		return WebQuote{}, err
	}
	if err := shipError(shipProblem(in.Shipping, in.ShippingFee)); err != nil {
		return WebQuote{}, fmt.Errorf("báo giá web: %w", err)
	}
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return WebQuote{}, fmt.Errorf("báo giá web: %w", err)
	}
	ids := make([]uuid.UUID, len(in.Lines))
	for i, l := range in.Lines {
		ids[i] = l.VariantID
	}
	sale, err := catalog.VariantsForSale(ctx, tx, seller, ids)
	if err != nil {
		return WebQuote{}, fmt.Errorf("báo giá web: %w", err)
	}
	stock, err := webStock(ctx, tx, ids)
	if err != nil {
		return WebQuote{}, err
	}
	camps, err := webCampaigns(ctx, tx, seller, in.Lines, sale, now)
	if err != nil {
		return WebQuote{}, err
	}
	q, plines := quoteLines(in, sale, stock, camps)
	t, err := pricing.Compute(pricing.Input{Lines: plines, ShippingFee: in.ShippingFee})
	if err != nil {
		return WebQuote{}, pricingError(err)
	}
	if code != "" {
		if err := applyWebCoupon(ctx, tx, &q, t, code, in.CustomerPhone, seller, now); err != nil {
			return WebQuote{}, err
		}
		if t, err = pricing.Compute(pricing.Input{Lines: plines, CouponDiscount: q.CouponDiscount, ShippingFee: in.ShippingFee}); err != nil {
			return WebQuote{}, pricingError(err)
		}
	}
	for _, lt := range t.Lines {
		l := &q.Lines[lt.Key]
		l.Net = lt.Base - l.CouponDiscount
	}
	q.Merchandise, q.CampaignDiscount, q.GrandTotal = t.Subtotal, t.LineDiscountTotal, t.GrandTotal
	if q.Fingerprint, err = webQuoteFingerprint(q, code, in.Shipping); err != nil {
		return WebQuote{}, err
	}
	return q, nil
}

// quoteLines dựng các dòng báo giá theo số CRM (giá, chiến dịch, vấn đề, Changed) và các dòng tính tiền của dòng bán
// được (Key là chỉ số dòng trong in.Lines).
func quoteLines(in WebQuoteInput, sale map[uuid.UUID]catalog.SaleVariant, stock map[uuid.UUID]int32,
	camps map[uuid.UUID]promotion.Applied,
) (WebQuote, []pricing.Line) {
	q := WebQuote{Lines: make([]QuotedLine, len(in.Lines)), ShippingFee: in.ShippingFee}
	plines := make([]pricing.Line, 0, len(in.Lines))
	for i, l := range in.Lines {
		ql := QuotedLine{VariantID: l.VariantID, Qty: l.Qty}
		sv, ok := sale[l.VariantID]
		if !ok {
			ql.Problem = ProblemUnavailable
			q.Lines[i] = ql
			continue
		}
		ql.UnitPrice = sv.Price
		if a, ok := camps[l.VariantID]; ok {
			ql.Campaign = &LineCampaign{ID: a.CampaignID, Revision: a.Revision, Percent: a.Percent, AllowCoupon: a.AllowCoupon}
			ql.CampaignDiscount = int64(l.Qty) * a.Discount // a.Discount < giá ≤ MaxMoney, Qty ≤ 20: tràn bị Compute bắt
		}
		switch {
		case stock[l.VariantID] < int32(l.Qty): //nolint:gosec // G115: Qty trong [1, MaxWebQty]
			ql.Problem = ProblemOutOfStock
		case l.QuotedUnitPrice != sv.Price:
			ql.Problem = ProblemPriceChanged
		}
		q.Changed = q.Changed || l.QuotedUnitPrice != sv.Price
		q.Lines[i] = ql
		plines = append(plines, pricing.Line{Key: i, Qty: int64(l.Qty), UnitPrice: sv.Price, LineDiscount: ql.CampaignDiscount})
	}
	return q, plines
}

// validateWebQuote kiểm giới hạn của in và trả mã đã cắt khoảng trắng.
func validateWebQuote(in WebQuoteInput) (string, error) {
	if len(in.Lines) == 0 || len(in.Lines) > MaxWebLines {
		return "", fmt.Errorf("%w: báo giá cần 1–%d dòng", ErrInvalidInput, MaxWebLines)
	}
	seen := make(map[uuid.UUID]bool, len(in.Lines))
	for _, l := range in.Lines {
		if l.VariantID == uuid.Nil || seen[l.VariantID] {
			return "", fmt.Errorf("%w: dòng báo giá thiếu mã hay trùng biến thể", ErrInvalidInput)
		}
		seen[l.VariantID] = true
		if l.Qty < 1 || l.Qty > MaxWebQty || l.QuotedUnitPrice < 0 || l.QuotedUnitPrice > pricing.MaxMoney {
			return "", fmt.Errorf("%w: số lượng hay đơn giá ngoài giới hạn", ErrInvalidInput)
		}
	}
	if in.ShippingFee < 0 || in.ShippingFee > pricing.MaxMoney || !validShipChoice(in.Shipping) {
		return "", fmt.Errorf("%w: phí giao ngoài giới hạn", ErrInvalidInput)
	}
	code := strings.TrimSpace(in.CouponCode)
	if utf8.RuneCountInString(code) > maxCouponCodeRunes {
		return "", fmt.Errorf("%w: mã giảm giá quá dài", ErrInvalidInput)
	}
	return code, nil
}

// webStock trả số có thể bán ở kho mặc định của từng biến thể trong ids.
func webStock(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]int32, error) {
	wh, err := inventory.DefaultWarehouse(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("báo giá web: %w", err)
	}
	bs, err := inventory.Availability(ctx, tx, wh, ids)
	if err != nil {
		return nil, fmt.Errorf("báo giá web: %w", err)
	}
	out := make(map[uuid.UUID]int32, len(bs))
	for _, b := range bs {
		out[b.VariantID] = b.Available
	}
	return out, nil
}

// webCampaigns trả chiến dịch thắng của các dòng bán được (danh mục của mẫu đọc từ catalog.products_v).
func webCampaigns(ctx context.Context, tx pgx.Tx, seller uuid.UUID, lines []WebLine, sale map[uuid.UUID]catalog.SaleVariant,
	now time.Time,
) (map[uuid.UUID]promotion.Applied, error) {
	var products []uuid.UUID
	for _, l := range lines {
		if sv, ok := sale[l.VariantID]; ok && !slices.Contains(products, sv.ProductID) {
			products = append(products, sv.ProductID)
		}
	}
	if len(products) == 0 {
		return map[uuid.UUID]promotion.Applied{}, nil
	}
	rows, err := store.New(tx).ProductCategories(ctx, store.ProductCategoriesParams{SellerID: seller, ProductIds: products})
	if err != nil {
		return nil, fmt.Errorf("đọc danh mục của mẫu: %w", err)
	}
	cats := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, r := range rows {
		if r.CategoryID.Valid {
			cats[r.ProductID] = uuid.UUID(r.CategoryID.Bytes)
		}
	}
	refs := make([]promotion.VariantRef, 0, len(lines))
	for _, l := range lines {
		if sv, ok := sale[l.VariantID]; ok {
			refs = append(refs, promotion.VariantRef{
				VariantID: sv.ID, ProductID: sv.ProductID, CategoryID: cats[sv.ProductID], ListPrice: sv.Price,
			})
		}
	}
	out, err := promotion.ActiveForVariants(ctx, tx, seller, refs, now)
	if err != nil {
		return nil, fmt.Errorf("báo giá web: %w", err)
	}
	return out, nil
}

// applyWebCoupon kiểm mã code (promotion.Check, chỉ đọc) trên nền là tiền sau giảm chiến dịch của các dòng được áp mã
// (t: tổng chưa có mã) và ghi kết quả, phần giảm chia về dòng vào q.
func applyWebCoupon(ctx context.Context, tx pgx.Tx, q *WebQuote, t pricing.Totals, code string, phone *logging.Phone,
	seller uuid.UUID, now time.Time,
) error {
	bases, base := couponBases(t.Lines, func(i int) bool {
		c := q.Lines[t.Lines[i].Key].Campaign
		return c == nil || c.AllowCoupon
	})
	in := promotion.EvalInput{Base: base, Channel: ChannelWeb, SellerID: seller, Now: now}
	if phone != nil {
		in.Phone = *phone
	}
	r, err := promotion.Check(ctx, tx, code, in)
	if err != nil {
		return fmt.Errorf("báo giá web: %w", err)
	}
	q.Coupon = &CouponOutcome{Code: code, Applied: r.OK, Reason: string(r.Reason)}
	if !r.OK || r.Discount == 0 {
		return nil
	}
	q.CouponDiscount = r.Discount // ≤ base (Evaluate)
	for i, a := range pricing.Allocate(bases, r.Discount) {
		q.Lines[t.Lines[i].Key].CouponDiscount = a
	}
	return nil
}

// couponBases là nền mã của từng dòng tính tiền lines (Base khi eligible(i), không thì 0: dòng thuộc chiến dịch web cấm
// mã, QĐ29) và tổng nền. Một chỗ cho báo giá web và tạo đơn web: mã tính trên tổng nền, chia về dòng bằng
// pricing.Allocate(bases, giảm mã).
func couponBases(lines []pricing.LineTotals, eligible func(i int) bool) ([]int64, int64) {
	bases := make([]int64, len(lines))
	var base int64
	for i, lt := range lines {
		if eligible(i) {
			bases[i] = lt.Base
			base += lt.Base // Σ Base ≤ Subtotal ≤ MaxMoney (Compute đã kiểm)
		}
	}
	return bases, base
}

// fingerprintLine là một dòng trong JSON của dấu vân tay (tên trường ngắn, thứ tự cố định).
type fingerprintLine struct {
	V uuid.UUID  `json:"v"`
	Q int        `json:"q"`
	U int64      `json:"u"`
	C int64      `json:"c"`
	K int64      `json:"k"`
	P string     `json:"p"`
	G *uuid.UUID `json:"g"`
	R int        `json:"r"`
}

// fingerprintShip là lựa chọn giao hàng trong dấu vân tay (tài khoản, dịch vụ, phí).
type fingerprintShip struct {
	A *uuid.UUID `json:"a"`
	S string     `json:"s"`
	F int64      `json:"f"`
}

// webQuoteFingerprint là hex sha256 của JSON chuẩn (struct, thứ tự trường cố định) gồm các dòng theo số CRM, phí,
// tổng, mã (chữ hoa), kết quả mã và lựa chọn giao hàng ship (nil: không có khóa "ship", dấu như trước Task 490).
func webQuoteFingerprint(q WebQuote, code string, ship *WebShipChoice) (string, error) {
	lines := make([]fingerprintLine, len(q.Lines))
	for i, l := range q.Lines {
		lines[i] = fingerprintLine{V: l.VariantID, Q: l.Qty, U: l.UnitPrice, C: l.CampaignDiscount, K: l.CouponDiscount, P: l.Problem}
		if l.Campaign != nil {
			lines[i].G, lines[i].R = &l.Campaign.ID, l.Campaign.Revision
		}
	}
	applied := q.Coupon != nil && q.Coupon.Applied
	var fs *fingerprintShip
	if ship != nil {
		fs = &fingerprintShip{A: ship.AccountID, S: ship.Service, F: ship.Fee}
	}
	b, err := json.Marshal(struct {
		Lines   []fingerprintLine `json:"lines"`
		Fee     int64             `json:"fee"`
		Total   int64             `json:"total"`
		Code    string            `json:"code"`
		Applied bool              `json:"applied"`
		Ship    *fingerprintShip  `json:"ship,omitempty"`
	}{lines, q.ShippingFee, q.GrandTotal, strings.ToUpper(code), applied, fs})
	if err != nil {
		return "", fmt.Errorf("mã hóa dấu vân tay báo giá: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
