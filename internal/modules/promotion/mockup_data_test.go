package promotion_test

// Dữ liệu màn hình theo mockup M5 (Task 487): tên, mã, số mẫu của đích chiến dịch; SKU trong bảng giá mẫu; số đếm của
// nhiều đợt mã một lần; đơn đã dùng mã; số "Sắp gửi · 7 ngày tới" và "Mã đã dùng · năm" của quà sinh nhật.

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

func (e *env) targetsByIDs(scope string, ids []uuid.UUID) []promotion.CampaignTarget {
	e.t.Helper()
	var out []promotion.CampaignTarget
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = promotion.CampaignTargetsByIDs(e.t.Context(), tx, e.shop.Seller, scope, ids)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func modelCount(t promotion.CampaignTarget) int {
	if t.ModelCount == nil {
		return -1
	}
	return *t.ModelCount
}

// Đích theo mã giữ thứ tự mã nhập: danh mục kèm số mẫu đang bán chưa xóa của người bán (0 khi rỗng), mẫu không có số;
// mã không có (danh mục lạ, mẫu đã xóa) bị bỏ; all → rỗng. Danh sách chọn đích (category) cũng có số mẫu.
func TestCampaignTargetsByIDs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	full, empty := e.category("dam-cong-so"), e.category("ao-dai")
	p1, _ := e.webProduct(full, 100000)
	p2, _ := e.webProduct(full, 200000)
	gone, _ := e.webProduct(full, 300000)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE catalog.products SET deleted_at = now() WHERE id = $1`, gone)
		return err //nolint:wrapcheck // test
	})

	cats := e.targetsByIDs(promotion.ScopeCategories, []uuid.UUID{empty, uuid.New(), full})
	if len(cats) != 2 || cats[0].ID != empty || cats[0].Code != "ao-dai" || cats[0].Name != "ao-dai" ||
		modelCount(cats[0]) != 0 || cats[1].ID != full || modelCount(cats[1]) != 2 {
		t.Fatalf("đích danh mục: %+v", cats)
	}
	prods := e.targetsByIDs(promotion.ScopeProducts, []uuid.UUID{p2, gone, p1})
	if len(prods) != 2 || prods[0].ID != p2 || prods[1].ID != p1 || prods[0].Code == "" || prods[0].Name == "" ||
		prods[0].ModelCount != nil {
		t.Fatalf("đích mẫu: %+v", prods)
	}
	if all := e.targetsByIDs(promotion.ScopeAll, nil); len(all) != 0 {
		t.Fatalf("all: %+v", all)
	}

	var page []promotion.CampaignTarget
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		page, _, err = promotion.ListCampaignTargets(t.Context(), tx, e.shop.Seller, "category", "dam", nil, 50)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != full || modelCount(page[0]) != 2 {
		t.Fatalf("chọn đích danh mục: %+v", page)
	}
}

// Bảng giá mẫu của bản xem trước có SKU của biến thể.
func TestPreviewSamples_SKU(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.webProduct(uuid.Nil, 100000)
	c := e.mustCampaign(campaignInput("Có SKU"))
	p, err := e.preview(c.ID, c.Revision)
	if err != nil || len(p.Samples) != 1 || p.Samples[0].SKU == "" {
		t.Fatalf("xem trước: %+v %v", p, err)
	}
	var sku string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT sku FROM catalog.variants WHERE id = $1`, p.Samples[0].VariantID).Scan(&sku)
	})
	if p.Samples[0].SKU != sku {
		t.Fatalf("SKU %q, muốn %q", p.Samples[0].SKU, sku)
	}
}

// Số đếm nhiều đợt một câu bằng số của từng đợt (GetBatchStats); đợt không có mã nào trao/dùng/hết hạn → 0; mã đợt lạ
// không có trong kết quả. UsedOrders trả đơn của lượt dùng còn hiệu lực của mã đã dùng; mã chưa dùng không có.
func TestListBatchStats_UsedOrders(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	b, err := e.ensure(birthdayInput("Đếm", 6))
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := e.ensure(birthdayInput("Yên", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.markGiven(b.codes[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	cust := e.customer()
	in := e.input(cust, 600000)
	in.Now = template().ValidFrom.Add(time.Hour)
	ord := e.order(cust)
	if _, err := e.redeem(b.codes[1], ord, in); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons SET valid_from = now() - interval '2 day',
			valid_to = now() - interval '1 day' WHERE code = ANY($1)`, b.codes[4:6])
		return err //nolint:wrapcheck // test
	})
	var stats map[uuid.UUID]promotion.BatchStats
	var one promotion.BatchStats
	var used map[uuid.UUID]uuid.UUID
	ids := map[string]uuid.UUID{}
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		if stats, err = promotion.ListBatchStats(t.Context(), tx, []uuid.UUID{b.batch.ID, quiet.batch.ID, uuid.New()},
			time.Now()); err != nil {
			return err //nolint:wrapcheck // test
		}
		if one, err = promotion.GetBatchStats(t.Context(), tx, b.batch.ID, time.Now()); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, code := range b.codes[:3] {
			var id uuid.UUID
			if err := tx.QueryRow(t.Context(), `SELECT id FROM promotion.coupons WHERE code = $1`, code).Scan(&id); err != nil {
				return err //nolint:wrapcheck // test
			}
			ids[code] = id
		}
		used, err = promotion.UsedOrders(t.Context(), tx, []uuid.UUID{ids[b.codes[0]], ids[b.codes[1]], ids[b.codes[2]]})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	want := promotion.BatchStats{Given: 1, Used: 1, Expired: 2}
	if len(stats) != 2 || stats[b.batch.ID] != want || one != want || stats[quiet.batch.ID] != (promotion.BatchStats{}) {
		t.Fatalf("số đếm: %+v một đợt %+v", stats, one)
	}
	if len(used) != 1 || used[ids[b.codes[1]]] != ord {
		t.Fatalf("đơn đã dùng: %+v, muốn %v", used, ord)
	}
}

// "Sắp gửi · 7 ngày tới": khách chờ ngày gửi đã có đơn giao thành công, ngày gửi trong (hôm nay, hôm nay + 7]; khách
// chưa có đơn, ngày gửi hôm nay + 8 không tính. "Mã đã dùng · năm": quà của năm sinh nhật hiện tại (giờ VN), không theo
// bộ lọc tháng; tập rỗng → 0/0. Khách có quà mang mã của mã quà (CouponID).
func TestListBirthdayCustomers_WeekAndYear(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	if p := e.customers(promotion.BirthdayCustomerQuery{Limit: 50}); p.UpcomingWeek != 0 || p.Year != 2026 ||
		p.YearIssued != 0 || p.YearUsed != 0 {
		t.Fatalf("chưa có khách: %+v", p)
	}
	d60 := 60 * 24 * time.Hour
	given := &bdCustomer{phone: "0912000011", day: 28, month: 11, savedAgo: d60, delivered: 1}
	used := &bdCustomer{phone: "0912000012", day: 27, month: 11, savedAgo: d60, delivered: 1}
	for _, c := range []*bdCustomer{given, used} {
		e.seedBirthday(c)
	}
	if r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow()); err != nil || r.Given != 2 {
		t.Fatalf("lượt phát: %+v %v", r, err)
	}
	var usedCoupon uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `UPDATE promotion.coupons k SET used_count = 1 FROM promotion.birthday_gifts g
			WHERE g.coupon_id = k.id AND g.customer_id = $1 RETURNING k.id`, used.customerID).Scan(&usedCoupon)
	})
	soon := &bdCustomer{phone: "0912000013", day: 2, month: 12, savedAgo: d60, delivered: 1}    // gửi 25/11
	edge := &bdCustomer{phone: "0912000014", day: 8, month: 12, savedAgo: d60, delivered: 1}    // gửi 1/12 = +7
	late := &bdCustomer{phone: "0912000015", day: 9, month: 12, savedAgo: d60, delivered: 1}    // gửi 2/12 = +8
	noOrder := &bdCustomer{phone: "0912000016", day: 3, month: 12, savedAgo: d60, delivered: 0} // chưa có đơn
	for _, c := range []*bdCustomer{soon, edge, late, noOrder} {
		e.seedBirthday(c)
	}
	p := e.customers(promotion.BirthdayCustomerQuery{Limit: 50})
	if p.UpcomingWeek != 2 || p.Year != 2026 || p.YearIssued != 2 || p.YearUsed != 1 {
		t.Fatalf("số đầu trang: tuần %d, năm %d %d/%d", p.UpcomingWeek, p.Year, p.YearUsed, p.YearIssued)
	}
	for _, c := range p.Items {
		switch {
		case c.CustomerID == used.customerID && c.CouponID != usedCoupon:
			t.Errorf("mã quà của khách đã dùng: %v, muốn %v", c.CouponID, usedCoupon)
		case c.Code == "" && c.CouponID != uuid.Nil:
			t.Errorf("khách chưa có quà mà có mã quà: %+v", c)
		}
	}
	if m := e.customers(promotion.BirthdayCustomerQuery{Month: 3, Limit: 50}); m.YearIssued != 2 || m.YearUsed != 1 ||
		m.UpcomingWeek != 0 {
		t.Fatalf("lọc tháng 3: tuần %d (muốn 0, theo bộ lọc), năm %d/%d (không theo bộ lọc)", m.UpcomingWeek, m.YearUsed,
			m.YearIssued)
	}
}

// SĐT gắn mã hiện dạng trong nước che giữa, như maskPhone của màn hình; không gắn → rỗng.
func TestCoupon_MaskedBoundPhone(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"+84905123781", "0905 *** 781"}, {"0912000678", "0912 *** 678"}, {"", ""}, {"+84123", "***"},
	} {
		if got := (promotion.Coupon{BoundPhone: logging.Phone(c.in)}).MaskedBoundPhone(); got != c.want {
			t.Errorf("%q → %q, muốn %q", c.in, got, c.want)
		}
	}
}
