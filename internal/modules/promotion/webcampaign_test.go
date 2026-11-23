package promotion_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// campaignWriter là Writer có sổ sự kiện, hàng đợi (chiến dịch phát promotion.web_campaign.changed).
func campaignWriter() promotion.Writer {
	return promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
}

// webProduct tạo một mẫu (danh mục cat, uuid.Nil: không) của người bán ELIMAZ với một biến thể cho mỗi giá; trả mã mẫu
// và mã biến thể.
func (e *env) webProduct(cat uuid.UUID, prices ...int64) (uuid.UUID, []uuid.UUID) {
	e.t.Helper()
	var p uuid.UUID
	var vs []uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		n := int(e.seq.Add(1))
		p = catalogtest.Product(e.t, tx, e.tenant, e.shop.Seller, fmt.Sprintf("WC%04d", n))
		for i, price := range prices {
			vs = append(vs, catalogtest.Variant(e.t, tx, e.tenant, e.shop.Seller, p, 1000*n+i, price))
		}
		if cat == uuid.Nil {
			return nil
		}
		_, err := tx.Exec(e.t.Context(), `UPDATE catalog.products SET category_id = $1 WHERE id = $2`, cat, p)
		return err //nolint:wrapcheck // test
	})
	return p, vs
}

// category tạo danh mục cấp 1 (chủ bảng); trả mã.
func (e *env) category(slug string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO catalog.categories (tenant_id, name, slug, depth)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $1, 1) RETURNING id`, slug).Scan(&id)
	})
	return id
}

// campaignInput là chiến dịch 20 % cả shop, chạy từ 1 giờ trước tới 7 ngày sau.
func campaignInput(name string) promotion.WebCampaignInput {
	now := time.Now()
	return promotion.WebCampaignInput{
		Name: name, Percent: 20, Scope: promotion.ScopeAll, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(7 * 24 * time.Hour),
	}
}

func (e *env) createCampaign(in promotion.WebCampaignInput) (promotion.WebCampaign, error) {
	var c promotion.WebCampaign
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = campaignWriter().CreateCampaign(e.t.Context(), tx, e.actor(), e.shop.Seller, in)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *env) mustCampaign(in promotion.WebCampaignInput) promotion.WebCampaign {
	e.t.Helper()
	c, err := e.createCampaign(in)
	if err != nil {
		e.t.Fatalf("tạo chiến dịch: %v", err)
	}
	return c
}

func (e *env) preview(id uuid.UUID, rev int) (promotion.Preview, error) {
	var p promotion.Preview
	err := e.app(func(tx pgx.Tx) error {
		var err error
		p, err = campaignWriter().PreviewCampaign(e.t.Context(), tx, e.shop.Seller, id, rev)
		return err //nolint:wrapcheck // test
	})
	return p, err
}

func (e *env) setState(id uuid.UUID, rev int, state string, seen promotion.PreviewCounts) (promotion.CampaignChange, error) {
	var ch promotion.CampaignChange
	err := e.app(func(tx pgx.Tx) error {
		var err error
		ch, err = campaignWriter().SetCampaignState(e.t.Context(), tx, e.actor(), e.shop.Seller, id, rev, state,
			"duyệt chương trình", seen)
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

func (e *env) save(id uuid.UUID, rev int, in promotion.WebCampaignInput) (promotion.CampaignChange, error) {
	var ch promotion.CampaignChange
	err := e.app(func(tx pgx.Tx) error {
		var err error
		ch, err = campaignWriter().SaveCampaign(e.t.Context(), tx, e.actor(), e.shop.Seller, id, rev, in)
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

func (e *env) deleteCampaign(id uuid.UUID, rev int, reason string) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := campaignWriter().DeleteCampaign(e.t.Context(), tx, e.actor(), e.shop.Seller, id, rev, reason)
		return err //nolint:wrapcheck // test
	})
}

// enable xem trước rồi bật c; trả bản sau khi bật.
func (e *env) enable(c promotion.WebCampaign) promotion.WebCampaign {
	e.t.Helper()
	p, err := e.preview(c.ID, c.Revision)
	if err != nil {
		e.t.Fatalf("xem trước: %v", err)
	}
	ch, err := e.setState(c.ID, c.Revision, promotion.StateEnabled, p.PreviewCounts)
	if err != nil {
		e.t.Fatalf("bật: %v", err)
	}
	return *ch.After
}

func (e *env) active(vs []promotion.VariantRef, now time.Time) map[uuid.UUID]promotion.Applied {
	e.t.Helper()
	var out map[uuid.UUID]promotion.Applied
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = promotion.ActiveForVariants(e.t.Context(), tx, e.shop.Seller, vs, now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func wantErr(t *testing.T, name string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: lỗi %v, cần %v", name, err, want)
	}
}

// Vòng đời [KM04]: tạo nháp → bật khi chưa xem trước 422 → xem trước, đổi tên (revision tăng) → bật bằng bản xem cũ
// bị từ chối → xem lại, bật → sửa, xóa khi đang bật bị từ chối → revision cũ 409 → tạm dừng, xóa có lý do. Mỗi lần
// ghi một sự kiện promotion.web_campaign.changed, Seq = revision; xem trước không phát.
func TestWebCampaign_Lifecycle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.webProduct(uuid.Nil, 100000, 250000)
	c := e.mustCampaign(campaignInput("  Sale cuối năm  "))
	if c.Name != "Sale cuối năm" || c.State != promotion.StateDraft || c.Revision != 1 || len(c.LinkKey) != 8 ||
		!strings.HasPrefix(strings.ReplaceAll(c.ID.String(), "-", ""), c.LinkKey) || c.ID.Version() != 4 {
		t.Fatalf("chiến dịch mới: %+v", c)
	}
	_, err := e.setState(c.ID, 1, promotion.StateEnabled, promotion.PreviewCounts{Revision: 1, Models: 1, Variants: 2})
	wantErr(t, "bật khi chưa xem trước", err, promotion.ErrPreviewRequired)

	p, err := e.preview(c.ID, 1)
	if err != nil || p.Models != 1 || p.Variants != 2 || p.Revision != 1 || len(p.Samples) != 2 ||
		p.Samples[0].SalePrice != 80000 {
		t.Fatalf("xem trước: %+v %v", p, err)
	}
	in := campaignInput("Sale cuối năm 2")
	ch, err := e.save(c.ID, 1, in)
	if err != nil || ch.After.Revision != 2 || ch.Before.Name != "Sale cuối năm" || ch.After.State != promotion.StateDraft {
		t.Fatalf("lưu nháp: %+v %v", ch, err)
	}
	_, err = e.setState(c.ID, 2, promotion.StateEnabled, p.PreviewCounts)
	wantErr(t, "bật bằng bản xem của revision cũ", err, promotion.ErrPreviewRequired)
	_, err = e.setState(c.ID, 2, promotion.StateEnabled, promotion.PreviewCounts{Revision: 2, Models: 1, Variants: 2})
	wantErr(t, "bật khi chưa xem trước revision mới", err, promotion.ErrPreviewRequired)

	on := e.enable(*ch.After)
	if on.State != promotion.StateEnabled || on.Revision != 3 {
		t.Fatalf("sau khi bật: %+v", on)
	}
	_, err = e.save(c.ID, 3, in)
	wantErr(t, "sửa khi đang bật", err, promotion.ErrCampaignRunning)
	wantErr(t, "xóa khi đang bật", e.deleteCampaign(c.ID, 3, "hết hàng"), promotion.ErrCampaignRunning)
	_, err = e.setState(c.ID, 2, promotion.StatePaused, promotion.PreviewCounts{})
	wantErr(t, "revision cũ", err, promotion.ErrVersionConflict)
	wantErr(t, "xóa không lý do", e.deleteCampaign(c.ID, 3, "   "), promotion.ErrInvalidInput)

	off, err := e.setState(c.ID, 3, promotion.StatePaused, promotion.PreviewCounts{})
	if err != nil || off.After.State != promotion.StatePaused || off.After.Revision != 4 {
		t.Fatalf("tạm dừng: %+v %v", off, err)
	}
	if err := e.deleteCampaign(c.ID, 4, "hết đợt"); err != nil {
		t.Fatalf("xóa: %v", err)
	}
	err = e.app(func(tx pgx.Tx) error {
		_, err := promotion.GetWebCampaign(t.Context(), tx, e.shop.Seller, c.ID)
		return err //nolint:wrapcheck // test
	})
	wantErr(t, "đọc sau khi xóa", err, promotion.ErrNotFound)
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events
		WHERE topic = 'promotion.web_campaign.changed' AND aggregate_type = 'web_campaign' AND aggregate_id = $1
		AND seq BETWEEN 1 AND 5`, c.ID.String()); n != 5 {
		t.Fatalf("sự kiện: %d, cần 5 (tạo, lưu, bật, tạm dừng, xóa)", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1
		AND payload->>'deleted' = 'true' AND seq = 5`, c.ID.String()); n != 1 {
		t.Fatalf("sự kiện xóa: %d", n)
	}
}

// Trần phần trăm (PERCENT_TOO_HIGH): 60 % với trần mặc định 50 bị từ chối khi tạo; trần cài 70 thì tạo được; hạ trần về
// 50 sau đó thì bật bị từ chối (dù đã xem trước).
func TestWebCampaign_MaxPercent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.webProduct(uuid.Nil, 100000)
	in := campaignInput("Xả kho")
	in.Percent = 60
	_, err := e.createCampaign(in)
	wantErr(t, "60 % trần 50", err, promotion.ErrPercentTooHigh)
	setMax := func(v int) {
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO platform.settings (tenant_id, key, value)
				VALUES (current_setting('app.tenant_id')::uuid, 'promotion.web_campaigns', jsonb_build_object('maxPercent', $1::int))
				ON CONFLICT (tenant_id, key) DO UPDATE SET value = excluded.value`, v)
			return err //nolint:wrapcheck // test
		})
	}
	setMax(70)
	c := e.mustCampaign(in)
	p, err := e.preview(c.ID, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	setMax(50)
	_, err = e.setState(c.ID, c.Revision, promotion.StateEnabled, p.PreviewCounts)
	wantErr(t, "bật 60 % khi trần về 50", err, promotion.ErrPercentTooHigh)
	setMax(500) // ngoài 1–99 → mặc định 50
	_, err = e.setState(c.ID, c.Revision, promotion.StateEnabled, p.PreviewCounts)
	wantErr(t, "trần cài sai", err, promotion.ErrPercentTooHigh)
}

// Bản xem trước lệch số tính lại lúc bật (thêm biến thể sau khi xem) → 422 preview_required.
func TestWebCampaign_PreviewStaleCounts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.webProduct(uuid.Nil, 100000)
	c := e.mustCampaign(campaignInput("Thu"))
	p, err := e.preview(c.ID, c.Revision)
	if err != nil || p.Variants != 1 {
		t.Fatalf("xem trước: %+v %v", p, err)
	}
	e.webProduct(uuid.Nil, 50000)
	_, err = e.setState(c.ID, c.Revision, promotion.StateEnabled, p.PreviewCounts)
	wantErr(t, "biến thể mới sau khi xem", err, promotion.ErrPreviewRequired)
	p2, err := e.preview(c.ID, c.Revision)
	if err != nil || p2.Models != 2 || p2.Variants != 2 {
		t.Fatalf("xem lại: %+v %v", p2, err)
	}
	if _, err := e.setState(c.ID, c.Revision, promotion.StateEnabled, p2.PreviewCounts); err != nil {
		t.Fatalf("bật sau khi xem lại: %v", err)
	}
}

// Đầu vào sai → ErrInvalidInput (luật campaignInputSchema): tên rỗng, 121 ký tự; phần trăm 0, 100; kết thúc không sau
// bắt đầu; all có đích; products không đích, 31 đích, đích trùng, đích không có; lý do 301 ký tự.
func TestWebCampaign_InvalidInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	p, _ := e.webProduct(uuid.Nil, 100000)
	many := make([]uuid.UUID, 31)
	for i := range many {
		many[i] = uuid.New()
	}
	for name, mod := range map[string]func(*promotion.WebCampaignInput){
		"tên rỗng":         func(in *promotion.WebCampaignInput) { in.Name = "   " },
		"tên 121":          func(in *promotion.WebCampaignInput) { in.Name = strings.Repeat("á", 121) },
		"0 %":              func(in *promotion.WebCampaignInput) { in.Percent = 0 },
		"100 %":            func(in *promotion.WebCampaignInput) { in.Percent = 100 },
		"kết thúc = đầu":   func(in *promotion.WebCampaignInput) { in.EndsAt = in.StartsAt },
		"all có đích":      func(in *promotion.WebCampaignInput) { in.TargetIDs = []uuid.UUID{p} },
		"products 0 đích":  func(in *promotion.WebCampaignInput) { in.Scope = promotion.ScopeProducts },
		"products 31 đích": func(in *promotion.WebCampaignInput) { in.Scope, in.TargetIDs = promotion.ScopeProducts, many },
		"đích trùng": func(in *promotion.WebCampaignInput) {
			in.Scope, in.TargetIDs = promotion.ScopeProducts, []uuid.UUID{p, p}
		},
		"mẫu không có": func(in *promotion.WebCampaignInput) {
			in.Scope, in.TargetIDs = promotion.ScopeProducts, []uuid.UUID{uuid.New()}
		},
		"danh mục không có": func(in *promotion.WebCampaignInput) {
			in.Scope, in.TargetIDs = promotion.ScopeCategories, []uuid.UUID{p}
		},
		"phạm vi lạ": func(in *promotion.WebCampaignInput) { in.Scope = "brand" },
	} {
		in := campaignInput("Thử")
		mod(&in)
		_, err := e.createCampaign(in)
		wantErr(t, name, err, promotion.ErrInvalidInput)
	}
	c := e.mustCampaign(campaignInput("Thử"))
	_, err := e.app2(func(tx pgx.Tx) (any, error) {
		return campaignWriter().SetCampaignState(t.Context(), tx, e.actor(), e.shop.Seller, c.ID, 1,
			promotion.StatePaused, strings.Repeat("x", 301), promotion.PreviewCounts{})
	})
	wantErr(t, "lý do 301", err, promotion.ErrInvalidInput)
}

func (e *env) app2(fn func(tx pgx.Tx) (any, error)) (any, error) {
	var out any
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = fn(tx)
		return err
	})
	return out, err
}

// [WB06] Phạm vi và giá thắng: products phủ đúng mẫu; categories phủ đúng danh mục của mẫu; all phủ mọi mẫu; biến thể
// có hai chiến dịch lấy giá thấp hơn; ngoài giờ, tạm dừng, nháp không áp; allowCoupon, revision theo chiến dịch thắng.
func TestActiveForVariants(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cat := e.category("dam")
	p1, v1 := e.webProduct(uuid.Nil, 100000)
	p2, v2 := e.webProduct(cat, 333333)
	p3, v3 := e.webProduct(uuid.Nil, 3)
	refs := []promotion.VariantRef{
		{VariantID: v1[0], ProductID: p1, ListPrice: 100000},
		{VariantID: v2[0], ProductID: p2, CategoryID: cat, ListPrice: 333333},
		{VariantID: v3[0], ProductID: p3, ListPrice: 3},
	}
	now := time.Now()

	byProduct := campaignInput("Theo mẫu")
	byProduct.Scope, byProduct.TargetIDs, byProduct.Percent, byProduct.AllowCoupon = promotion.ScopeProducts, []uuid.UUID{p1}, 30, true
	cp := e.enable(e.mustCampaign(byProduct))
	byCat := campaignInput("Theo danh mục")
	byCat.Scope, byCat.TargetIDs, byCat.Percent = promotion.ScopeCategories, []uuid.UUID{cat}, 15
	cc := e.enable(e.mustCampaign(byCat))

	got := e.active(refs, now)
	if a := got[v1[0]]; a.CampaignID != cp.ID || a.UnitPrice != 70000 || a.Discount != 30000 || !a.AllowCoupon ||
		a.Revision != cp.Revision || a.Percent != 30 {
		t.Fatalf("products: %+v", a)
	}
	if a := got[v2[0]]; a.CampaignID != cc.ID || a.UnitPrice != 283333 || a.AllowCoupon {
		t.Fatalf("categories: %+v", a)
	}
	if _, ok := got[v3[0]]; ok || len(got) != 2 {
		t.Fatalf("biến thể ngoài phạm vi: %+v", got)
	}

	all := campaignInput("Cả shop")
	all.Percent = 40
	ca := e.enable(e.mustCampaign(all))
	got = e.active(refs, now)
	if got[v1[0]].CampaignID != ca.ID || got[v1[0]].UnitPrice != 60000 || got[v2[0]].CampaignID != ca.ID {
		t.Fatalf("giá thấp hơn thắng: %+v", got)
	}
	if a, ok := got[v3[0]]; !ok || a.UnitPrice != 2 { // round(3 × 0,6) = 2
		t.Fatalf("giá 3 đồng giảm 40 %%: %+v %v", a, ok)
	}
	if got := e.active(refs, all.StartsAt.Add(-time.Minute)); len(got) != 0 {
		t.Fatalf("trước giờ bắt đầu: %+v", got)
	}
	if got := e.active(refs, all.EndsAt); len(got) != 0 {
		t.Fatalf("đúng giờ kết thúc: %+v", got)
	}
	if _, err := e.setState(ca.ID, ca.Revision, promotion.StatePaused, promotion.PreviewCounts{}); err != nil {
		t.Fatal(err)
	}
	e.mustCampaign(all) // nháp, không áp
	if got := e.active(refs, now); got[v1[0]].CampaignID != cp.ID || len(got) != 2 {
		t.Fatalf("sau khi tạm dừng cả shop: %+v", got)
	}

	e.checkLive(now, 2, cc.ID, cat)
}

// checkLive: WebCampaigns ở now có đúng n chiến dịch (không nháp, tạm dừng) và chiến dịch withTarget có đúng một đích
// target.
func (e *env) checkLive(now time.Time, n int, withTarget, target uuid.UUID) {
	e.t.Helper()
	var live []promotion.WebCampaign
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		live, err = promotion.WebCampaigns(e.t.Context(), tx, now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	if len(live) != n {
		e.t.Fatalf("WebCampaigns (không nháp, tạm dừng): %d, cần %d", len(live), n)
	}
	for _, c := range live {
		if c.ID == withTarget && (len(c.TargetIDs) != 1 || c.TargetIDs[0] != target) {
			e.t.Fatalf("đích của chiến dịch danh mục: %+v", c.TargetIDs)
		}
	}
}

// Trang /uu-dai: tìm theo link key (không phân biệt hoa thường); nháp, đã xóa → ErrNotFound; tạm dừng vẫn đọc được.
func TestWebCampaignByLinkKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.webProduct(uuid.Nil, 100000)
	c := e.mustCampaign(campaignInput("Trang ưu đãi"))
	get := func(key string) (promotion.WebCampaign, error) {
		var out promotion.WebCampaign
		err := e.app(func(tx pgx.Tx) error {
			var err error
			out, err = promotion.WebCampaignByLinkKey(t.Context(), tx, key)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	_, err := get(c.LinkKey)
	wantErr(t, "nháp", err, promotion.ErrNotFound)
	on := e.enable(c)
	off, err := e.setState(on.ID, on.Revision, promotion.StatePaused, promotion.PreviewCounts{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := get(strings.ToUpper(c.LinkKey)); err != nil || got.ID != c.ID {
		t.Fatalf("tạm dừng: %+v %v", got, err)
	}
	if err := e.deleteCampaign(c.ID, off.After.Revision, "bỏ"); err != nil {
		t.Fatal(err)
	}
	_, err = get(c.LinkKey)
	wantErr(t, "đã xóa", err, promotion.ErrNotFound)
}
