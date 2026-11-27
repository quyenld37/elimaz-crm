package order_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// quoteEnv là orderEnv có giờ báo giá cố định và bộ đếm mã mẫu, số biến thể riêng (không đụng mẫu "Áo thử" của Shop).
type quoteEnv struct {
	*orderEnv
	now time.Time
	seq atomic.Int64
}

func newQuoteEnv(t *testing.T) *quoteEnv {
	t.Helper()
	return &quoteEnv{orderEnv: newOrderEnv(t), now: time.Now().UTC().Truncate(time.Second)}
}

// product tạo một mẫu riêng của người bán ELIMAZ với một biến thể cho mỗi giá, nạp onHand mỗi biến thể; trả mã mẫu và
// mã biến thể.
func (e *quoteEnv) product(onHand int32, prices ...int64) (uuid.UUID, []uuid.UUID) {
	e.t.Helper()
	n := int(e.seq.Add(1))
	var product uuid.UUID
	ids := make([]uuid.UUID, len(prices))
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		product = catalogtest.Product(e.t, tx, e.tenant, e.shop.Seller, "WQ"+strconv.Itoa(n))
		for i := range prices {
			ids[i] = e.seedVariant(tx, product, 10000+100*n+i, prices[i], onHand)
		}
		return nil
	})
	return product, ids
}

// seedVariant tạo biến thể thứ n giá price của mẫu product (chủ bảng) và nạp onHand (0: không nạp).
func (e *quoteEnv) seedVariant(tx pgx.Tx, product uuid.UUID, n int, price int64, onHand int32) uuid.UUID {
	id := catalogtest.Variant(e.t, tx, e.tenant, e.shop.Seller, product, n, price)
	if onHand > 0 {
		invtest.SeedStock(e.t, tx, id, e.warehouse, onHand)
	}
	return id
}

func (e *quoteEnv) quote(in order.WebQuoteInput) (order.WebQuote, error) {
	var q order.WebQuote
	err := e.app(func(tx pgx.Tx) error {
		var err error
		q, err = order.QuoteWeb(e.t.Context(), tx, in, e.now)
		return err //nolint:wrapcheck // test
	})
	return q, err
}

func (e *quoteEnv) mustQuote(in order.WebQuoteInput) order.WebQuote {
	e.t.Helper()
	q, err := e.quote(in)
	if err != nil {
		e.t.Fatalf("báo giá: %v", err)
	}
	return q
}

// campaign tạo, xem trước rồi bật chiến dịch percent % (phạm vi cả shop khi products rỗng, không thì các mẫu đó), chạy
// từ 1 giờ trước e.now tới 7 ngày sau.
func (e *quoteEnv) campaign(percent int, allowCoupon bool, products ...uuid.UUID) promotion.WebCampaign {
	e.t.Helper()
	in := promotion.WebCampaignInput{
		Name: fmt.Sprintf("Giảm %d%%", percent), Percent: percent, Scope: promotion.ScopeAll, AllowCoupon: allowCoupon,
		StartsAt: e.now.Add(-time.Hour), EndsAt: e.now.Add(7 * 24 * time.Hour),
	}
	if len(products) > 0 {
		in.Scope, in.TargetIDs = promotion.ScopeProducts, products
	}
	w := promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	a := promotion.Actor{TenantID: e.tenant, UserID: e.actor.UserID}
	var out promotion.WebCampaign
	if err := e.app(func(tx pgx.Tx) error {
		c, err := w.CreateCampaign(e.t.Context(), tx, a, e.shop.Seller, in)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		p, err := w.PreviewCampaign(e.t.Context(), tx, e.shop.Seller, c.ID, c.Revision)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		ch, err := w.SetCampaignState(e.t.Context(), tx, a, e.shop.Seller, c.ID, c.Revision, promotion.StateEnabled,
			"thử báo giá", p.PreviewCounts)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out = *ch.After
		return nil
	}); err != nil {
		e.t.Fatalf("chiến dịch: %v", err)
	}
	return out
}

// quoteCoupon tạo mã in (hạn mặc định: 1 ngày trước e.now tới 30 ngày sau).
func (e *quoteEnv) quoteCoupon(in promotion.CouponInput) {
	e.t.Helper()
	in.Name, in.ReleaseOnCancel = "Mã thử "+in.Code, true
	if in.ValidFrom.IsZero() {
		in.ValidFrom, in.ValidTo = e.now.Add(-24*time.Hour), e.now.Add(30*24*time.Hour)
	}
	e.createCoupon(in)
}

func wl(v uuid.UUID, qty int, quoted int64) order.WebLine {
	return order.WebLine{VariantID: v, Qty: qty, QuotedUnitPrice: quoted}
}

func ptr[T any](v T) *T { return &v }

// [WB13]: giá CRM 350.000; gửi đúng giá → không đổi; gửi 300.000 → Changed, đơn giá CRM, price_changed. Cùng đầu vào
// cho cùng dấu vân tay; giá gửi khác cho dấu khác.
func TestQuoteWeb_PriceChanged(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, vs := e.product(5, 350000)
	in := order.WebQuoteInput{Lines: []order.WebLine{wl(vs[0], 2, 350000)}, ShippingFee: 30000}
	q := e.mustQuote(in)
	if q.Changed || q.Lines[0].Problem != "" || q.Lines[0].UnitPrice != 350000 || q.Merchandise != 700000 ||
		q.GrandTotal != 730000 || q.Lines[0].Net != 700000 {
		t.Fatalf("giá đúng: %+v", q)
	}
	if again := e.mustQuote(in); again.Fingerprint != q.Fingerprint || len(q.Fingerprint) != 64 {
		t.Errorf("dấu vân tay không ổn định: %q, %q", q.Fingerprint, again.Fingerprint)
	}
	in.Lines[0].QuotedUnitPrice = 300000
	c := e.mustQuote(in)
	if !c.Changed || c.Lines[0].Problem != order.ProblemPriceChanged || c.Lines[0].UnitPrice != 350000 || c.GrandTotal != 730000 {
		t.Fatalf("giá cũ: %+v", c)
	}
	if c.Fingerprint == q.Fingerprint {
		t.Error("dòng đổi giá mà dấu vân tay không đổi")
	}
}

// [WB09]: biến thể ngừng bán, mã lạ, biến thể của người bán khác → unavailable, không đổi sang biến thể khác, không
// tính vào tổng; dòng còn lại tính bình thường.
func TestQuoteWeb_Unavailable(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, vs := e.product(5, 200000, 250000)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { catalogtest.Discontinue(t, tx, e.shop.Seller, vs[1]); return nil })
	partner := e.variantOf(e.shop.Partner, 100000, 5)
	unknown := uuid.New()
	q := e.mustQuote(order.WebQuoteInput{Lines: []order.WebLine{
		wl(vs[0], 1, 200000), wl(vs[1], 1, 250000), wl(unknown, 1, 1000), wl(partner, 1, 100000),
	}})
	if len(q.Lines) != 4 {
		t.Fatalf("mong 4 dòng, có %+v", q.Lines)
	}
	for i, want := range []uuid.UUID{vs[0], vs[1], unknown, partner} {
		l := q.Lines[i]
		if l.VariantID != want {
			t.Fatalf("dòng %d đổi biến thể: %s, mong %s", i, l.VariantID, want)
		}
		if i == 0 {
			continue
		}
		if l.Problem != order.ProblemUnavailable || l.UnitPrice != 0 || l.Net != 0 || l.Campaign != nil {
			t.Errorf("dòng %d: %+v", i, l)
		}
	}
	if q.Merchandise != 200000 || q.GrandTotal != 200000 || q.Changed {
		t.Fatalf("tổng: %+v", q)
	}
}

// Mã 10% (phần vạn 1000) trần 50.000, tối thiểu 300.000, giỏ 2 × 350.000 → giảm 50.000; mã hết hạn → expired; mã gắn
// SĐT khác → bound_other (đúng SĐT → áp); mã lạ → not_found; dưới tối thiểu → min_subtotal; hoa thường, khoảng trắng.
func TestQuoteWeb_Coupon(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, vs := e.product(5, 350000)
	_, cheap := e.product(5, 290000)
	pct := promotion.CouponInput{DiscountKind: promotion.KindPercent, Value: 1000, MaxDiscount: ptr[int64](50000), MinSubtotal: 300000}
	ok, expired, bound := pct, pct, pct
	ok.Code = "WEB10"
	expired.Code, expired.ValidFrom, expired.ValidTo = "CU10", e.now.Add(-48*time.Hour), e.now.Add(-time.Hour)
	bound.Code, bound.BoundPhone = "LAN10", "0901234567"
	for _, c := range []promotion.CouponInput{ok, expired, bound} {
		e.quoteCoupon(c)
	}
	two := []order.WebLine{wl(vs[0], 2, 350000)}
	other, lan := logging.Phone("0987654321"), logging.Phone("+84901234567")
	for _, tc := range []struct {
		name, code string
		phone      *logging.Phone
		lines      []order.WebLine
		applied    bool
		reason     string
		discount   int64
	}{
		{"đạt", " web10 ", nil, two, true, "", 50000},
		{"hết hạn", "CU10", nil, two, false, string(promotion.ReasonExpired), 0},
		{"gắn SĐT khác", "LAN10", &other, two, false, string(promotion.ReasonBoundOther), 0},
		{"gắn SĐT, không gửi SĐT", "LAN10", nil, two, false, string(promotion.ReasonBoundOther), 0},
		{"gắn đúng SĐT", "LAN10", &lan, two, true, "", 50000},
		{"mã lạ", "KHONGCO", nil, two, false, string(promotion.ReasonNotFound), 0},
		{"dưới tối thiểu", "WEB10", nil, []order.WebLine{wl(cheap[0], 1, 290000)}, false, string(promotion.ReasonMinSubtotal), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := e.mustQuote(order.WebQuoteInput{Lines: tc.lines, CouponCode: tc.code, CustomerPhone: tc.phone, ShippingFee: 30000})
			if q.Coupon == nil || q.Coupon.Applied != tc.applied || q.Coupon.Reason != tc.reason ||
				q.Coupon.Code != strings.TrimSpace(tc.code) || q.CouponDiscount != tc.discount {
				t.Fatalf("mã: %+v, giảm %d", q.Coupon, q.CouponDiscount)
			}
			var sum int64
			for _, l := range q.Lines {
				sum += l.CouponDiscount
			}
			if sum != tc.discount || q.GrandTotal != q.Merchandise-tc.discount+30000 {
				t.Fatalf("chia mã %d, tổng %d", sum, q.GrandTotal)
			}
		})
	}
	if q := e.mustQuote(order.WebQuoteInput{Lines: two}); q.Coupon != nil {
		t.Errorf("không gửi mã mà có kết quả mã: %+v", q.Coupon)
	}
}

// [WB06]: dòng A thuộc chiến dịch 20% không cho dùng mã 1 × 400.000, dòng B không chiến dịch 1 × 200.000, mã 10% →
// giảm chiến dịch 80.000 ở A, mã chỉ trên B = 20.000. Chiến dịch cho dùng mã → mã tính trên cả hai dòng sau giảm.
func TestQuoteWeb_CampaignAndCoupon(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	pa, va := e.product(5, 400000)
	pc, vc := e.product(5, 400000)
	_, vb := e.product(5, 200000)
	camp := e.campaign(20, false, pa)
	open := e.campaign(20, true, pc)
	e.quoteCoupon(promotion.CouponInput{Code: "TEN", DiscountKind: promotion.KindPercent, Value: 1000})

	q := e.mustQuote(order.WebQuoteInput{Lines: []order.WebLine{wl(va[0], 1, 400000), wl(vb[0], 1, 200000)}, CouponCode: "TEN"})
	a, b := q.Lines[0], q.Lines[1]
	if a.UnitPrice != 400000 || a.CampaignDiscount != 80000 || a.CouponDiscount != 0 || a.Net != 320000 ||
		a.Campaign == nil || a.Campaign.ID != camp.ID || a.Campaign.Percent != 20 || a.Campaign.Revision != camp.Revision ||
		a.Campaign.AllowCoupon {
		t.Fatalf("dòng A: %+v %+v", a, a.Campaign)
	}
	if b.CampaignDiscount != 0 || b.Campaign != nil || b.CouponDiscount != 20000 || b.Net != 180000 {
		t.Fatalf("dòng B: %+v", b)
	}
	if q.Merchandise != 600000 || q.CampaignDiscount != 80000 || q.CouponDiscount != 20000 || q.GrandTotal != 500000 ||
		!q.Coupon.Applied || q.Changed {
		t.Fatalf("tổng: %+v", q)
	}

	q = e.mustQuote(order.WebQuoteInput{Lines: []order.WebLine{wl(vc[0], 1, 400000), wl(vb[0], 1, 200000)}, CouponCode: "TEN"})
	if q.Lines[0].Campaign == nil || q.Lines[0].Campaign.ID != open.ID || q.CampaignDiscount != 80000 ||
		q.CouponDiscount != 52000 || q.Lines[0].CouponDiscount+q.Lines[1].CouponDiscount != 52000 || q.GrandTotal != 468000 {
		t.Fatalf("chiến dịch cho dùng mã: %+v", q)
	}

	// Chỉ dòng chiến dịch không cho dùng mã → nền mã 0, mã không áp (zero_discount, Task 433).
	q = e.mustQuote(order.WebQuoteInput{Lines: []order.WebLine{wl(va[0], 1, 400000)}, CouponCode: "TEN"})
	if q.CouponDiscount != 0 || q.Lines[0].CouponDiscount != 0 || q.GrandTotal != 320000 || q.Coupon == nil ||
		q.Coupon.Applied || q.Coupon.Reason != string(promotion.ReasonZeroDiscount) {
		t.Fatalf("chỉ dòng chiến dịch: %+v %+v", q, q.Coupon)
	}
}

type roundingRow struct {
	ListPrice int64 `json:"listPrice"`
	Percent   int   `json:"percent"`
	SalePrice int64 `json:"salePrice"`
	Applies   bool  `json:"applies"`
}

// [WB06]: chiến dịch 33% đang chạy trên CRM giảm đúng số theo bảng mẫu làm tròn của website (mọi dòng 33% của
// web-campaign-rounding.json): dòng áp được → đơn giá − giảm mỗi đơn vị = salePrice, số lượng 3; không áp → không
// chiến dịch.
func TestQuoteWeb_CampaignRoundingTable(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "contracts", "web-campaign-rounding.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all, rows []roundingRow
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Percent == 33 {
			rows = append(rows, r)
		}
	}
	if len(rows) < 10 || len(rows) > order.MaxWebLines {
		t.Fatalf("bảng mẫu có %d dòng 33%%", len(rows))
	}
	e := newQuoteEnv(t)
	prices := make([]int64, len(rows))
	for i, r := range rows {
		prices[i] = r.ListPrice
	}
	_, vs := e.product(5, prices...)
	e.campaign(33, false)
	lines := make([]order.WebLine, len(rows))
	for i, v := range vs {
		lines[i] = wl(v, 3, rows[i].ListPrice)
	}
	q := e.mustQuote(order.WebQuoteInput{Lines: lines})
	for i, r := range rows {
		l := q.Lines[i]
		if !r.Applies {
			if l.Campaign != nil || l.CampaignDiscount != 0 {
				t.Errorf("%+v: không áp mà có %+v", r, l)
			}
			continue
		}
		if l.Campaign == nil || l.CampaignDiscount != 3*(r.ListPrice-r.SalePrice) || l.Net != 3*r.SalePrice {
			t.Errorf("%+v: có %+v", r, l)
		}
	}
}

// Hết hàng → out_of_stock (tồn 0; tồn 1 mà đặt 2); đủ hàng thì không; báo giá không giữ hàng.
func TestQuoteWeb_OutOfStock(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, none := e.product(0, 100000)
	_, one := e.product(1, 100000)
	q := e.mustQuote(order.WebQuoteInput{Lines: []order.WebLine{wl(none[0], 1, 100000), wl(one[0], 2, 90000)}})
	if q.Lines[0].Problem != order.ProblemOutOfStock || q.Lines[1].Problem != order.ProblemOutOfStock {
		t.Fatalf("dòng: %+v", q.Lines)
	}
	if !q.Changed || q.Merchandise != 300000 {
		t.Fatalf("hết hàng vẫn tính giá, Changed theo giá: %+v", q)
	}
	if q = e.mustQuote(order.WebQuoteInput{Lines: []order.WebLine{wl(one[0], 1, 100000)}}); q.Lines[0].Problem != "" {
		t.Fatalf("đủ hàng: %+v", q.Lines[0])
	}
	if b := e.balance(one[0]); b.Held != 0 || b.Available != 1 {
		t.Fatalf("báo giá giữ hàng: %+v", b)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_reservations`); n != 0 {
		t.Fatalf("báo giá tạo %d dòng giữ hàng", n)
	}
}

// 20 × 9.000.000.000.000.000 → ErrTotalOverflow (tổng vượt 2^53 − 1).
func TestQuoteWeb_Overflow(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, vs := e.product(50, 9_000_000_000_000_000)
	_, err := e.quote(order.WebQuoteInput{Lines: []order.WebLine{wl(vs[0], 20, 9_000_000_000_000_000)}})
	if !errors.Is(err, order.ErrTotalOverflow) {
		t.Fatalf("mong ErrTotalOverflow, có %v", err)
	}
	if _, err := e.quote(order.WebQuoteInput{Lines: []order.WebLine{wl(vs[0], 1, 1)}, ShippingFee: 1 << 53}); !errors.Is(err, order.ErrInvalidInput) {
		t.Fatalf("phí ship vượt trần: %v", err)
	}
}

// Đầu vào sai → ErrInvalidInput: 0 dòng, 51 dòng, số lượng 0/21, giá gửi âm, phí âm, trùng biến thể, mã dài quá 64.
func TestQuoteWeb_InvalidInput(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, vs := e.product(5, 100000)
	v := vs[0]
	many := make([]order.WebLine, order.MaxWebLines+1)
	for i := range many {
		many[i] = wl(uuid.New(), 1, 1)
	}
	for name, in := range map[string]order.WebQuoteInput{
		"không dòng":     {},
		"51 dòng":        {Lines: many},
		"số lượng 0":     {Lines: []order.WebLine{wl(v, 0, 100000)}},
		"số lượng 21":    {Lines: []order.WebLine{wl(v, order.MaxWebQty+1, 100000)}},
		"giá gửi âm":     {Lines: []order.WebLine{wl(v, 1, -1)}},
		"phí âm":         {Lines: []order.WebLine{wl(v, 1, 100000)}, ShippingFee: -1},
		"trùng biến thể": {Lines: []order.WebLine{wl(v, 1, 100000), wl(v, 2, 100000)}},
		"mã 65 ký tự":    {Lines: []order.WebLine{wl(v, 1, 100000)}, CouponCode: strings.Repeat("Ạ", 65)},
		"không mã":       {Lines: []order.WebLine{wl(uuid.Nil, 1, 100000)}},
	} {
		if _, err := e.quote(in); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	if _, err := e.quote(order.WebQuoteInput{Lines: many[:order.MaxWebLines]}); err != nil {
		t.Errorf("50 dòng: %v", err)
	}
}

// Quy luật (rapid): giỏ ngẫu nhiên trên 4 biến thể (một thuộc chiến dịch không cho mã, một chiến dịch cho mã, một ngừng
// bán, một tồn ít), giá gửi đúng hay lệch, mã hay không → Merchandise − giảm chiến dịch − giảm mã + phí = GrandTotal =
// Σ Net + phí; mỗi dòng Net = số lượng × đơn giá − hai khoản giảm ≥ 0; dòng chiến dịch không cho mã không có giảm mã;
// Σ giảm mã dòng = giảm mã; Changed ⇔ có dòng bán được lệch giá; dòng ngừng bán không vào tổng.
func TestQuoteWeb_Invariants(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	pNo, vNo := e.product(20, 410000)
	pYes, vYes := e.product(20, 333333)
	_, vPlain := e.product(3, 199999)
	_, vGone := e.product(20, 50000)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { catalogtest.Discontinue(t, tx, e.shop.Seller, vGone[0]); return nil })
	e.campaign(20, false, pNo)
	e.campaign(15, true, pYes)
	e.quoteCoupon(promotion.CouponInput{Code: "R10", DiscountKind: promotion.KindPercent, Value: 1000, MaxDiscount: ptr[int64](70000)})
	e.quoteCoupon(promotion.CouponInput{Code: "R50K", DiscountKind: promotion.KindFixed, Value: 50000, MinSubtotal: 100000})
	cart := quoteCart{
		vars:     []uuid.UUID{vNo[0], vYes[0], vPlain[0], vGone[0]},
		prices:   map[uuid.UUID]int64{vNo[0]: 410000, vYes[0]: 333333, vPlain[0]: 199999, vGone[0]: 50000},
		noCoupon: vNo[0], gone: vGone[0],
	}
	rapid.Check(t, func(rt *rapid.T) {
		in, wantChanged := cart.draw(rt)
		q, err := e.quote(in)
		if err != nil {
			rt.Fatalf("báo giá: %v", err)
		}
		cart.check(rt, in, q)
		if q.Changed != wantChanged {
			rt.Fatalf("Changed %v, mong %v", q.Changed, wantChanged)
		}
		if (in.CouponCode == "") != (q.Coupon == nil) || (q.Coupon != nil && !q.Coupon.Applied && q.CouponDiscount != 0) {
			rt.Fatalf("mã: %+v, giảm %d", q.Coupon, q.CouponDiscount)
		}
	})
}

// quoteCart là các biến thể của TestQuoteWeb_Invariants: giá CRM, biến thể thuộc chiến dịch không cho mã, biến thể
// ngừng bán.
type quoteCart struct {
	vars           []uuid.UUID
	prices         map[uuid.UUID]int64
	noCoupon, gone uuid.UUID
}

// draw sinh giỏ ngẫu nhiên và trả kèm Changed mong đợi (có dòng bán được mà giá gửi khác giá CRM).
func (c quoteCart) draw(rt *rapid.T) (order.WebQuoteInput, bool) {
	idx := rapid.SliceOfNDistinct(rapid.IntRange(0, len(c.vars)-1), 1, len(c.vars), rapid.ID[int]).Draw(rt, "dòng")
	in := order.WebQuoteInput{
		CouponCode:  rapid.SampledFrom([]string{"", "R10", "R50K", "KHONG"}).Draw(rt, "mã"),
		ShippingFee: rapid.Int64Range(0, 100000).Draw(rt, "phí"),
	}
	changed := false
	for _, i := range idx {
		v := c.vars[i]
		quoted := c.prices[v]
		if rapid.Bool().Draw(rt, "lệch") {
			quoted = max(quoted+rapid.Int64Range(-1000, 1000).Draw(rt, "chênh"), 0)
		}
		changed = changed || (v != c.gone && quoted != c.prices[v])
		in.Lines = append(in.Lines, wl(v, rapid.IntRange(1, order.MaxWebQty).Draw(rt, "sl"), quoted))
	}
	return in, changed
}

// check kiểm từng dòng và các tổng của q.
func (c quoteCart) check(rt *rapid.T, in order.WebQuoteInput, q order.WebQuote) {
	var net, camp, coup, merch int64
	for i, l := range q.Lines {
		if l.VariantID == c.gone {
			if l.Problem != order.ProblemUnavailable || l.Net != 0 {
				rt.Fatalf("dòng ngừng bán: %+v", l)
			}
			continue
		}
		gross := int64(in.Lines[i].Qty) * l.UnitPrice
		if l.UnitPrice != c.prices[l.VariantID] || l.Net != gross-l.CampaignDiscount-l.CouponDiscount || l.Net < 0 {
			rt.Fatalf("dòng %d: %+v", i, l)
		}
		if l.VariantID == c.noCoupon && (l.CouponDiscount != 0 || l.Campaign == nil || l.Campaign.AllowCoupon) {
			rt.Fatalf("dòng chiến dịch không cho mã: %+v", l)
		}
		merch, net, camp, coup = merch+gross, net+l.Net, camp+l.CampaignDiscount, coup+l.CouponDiscount
	}
	if merch != q.Merchandise || camp != q.CampaignDiscount || coup != q.CouponDiscount ||
		q.GrandTotal != net+in.ShippingFee || q.GrandTotal != q.Merchandise-q.CampaignDiscount-q.CouponDiscount+q.ShippingFee {
		rt.Fatalf("tổng: %+v", q)
	}
}
