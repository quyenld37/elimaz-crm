package content_test

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

type env struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	user   uuid.UUID
	seller uuid.UUID
	seq    atomic.Int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	content.RegisterFilePurposes()
	staff := testkit.OpenStaff(t)
	tenant := staff.Tenant(t, "A")
	e := &env{t: t, staff: staff, tenant: tenant, user: staff.User(t, tenant, "nv@elimaz.test", "active")}
	staff.Exec(t, tenant, func(tx pgx.Tx) (err error) {
		e.seller = catalogtest.Seller(t, tx, tenant, "ELIMAZ", "own")
		return err
	})
	return e
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của e (đường đọc, ghi của tiến trình api).
func (e *env) app(fn func(tx pgx.Tx) error) error {
	ctx := e.t.Context()
	return tenancy.WithTenant(ctx, e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func writer() content.Writer {
	return content.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
}

func (e *env) save(list []content.Banner, rev int) (content.BannerChange, error) {
	var ch content.BannerChange
	err := e.app(func(tx pgx.Tx) error {
		var err error
		ch, err = writer().SaveBanners(e.t.Context(), tx, list, rev, content.Actor{TenantID: e.tenant, UserID: e.user}, time.Now())
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

func (e *env) get() content.BannerSet {
	e.t.Helper()
	var s content.BannerSet
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = content.GetBanners(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// category tạo danh mục (cha parent, uuid.Nil: cấp 1) bằng chủ bảng.
func (e *env) category(slug string, parent uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	depth := 1
	var p *uuid.UUID
	if parent != uuid.Nil {
		depth, p = 2, &parent
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO catalog.categories (tenant_id, parent_id, name, slug, depth)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $2, $3) RETURNING id`, p, slug, depth).Scan(&id)
	})
	return id
}

// product tạo mẫu đang bán ở danh mục cat; photo: có ảnh đã xử lý; price: giá biến thể (0 = chưa có giá).
func (e *env) product(cat uuid.UUID, photo bool, price int64) uuid.UUID {
	e.t.Helper()
	var p uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		n := int(e.seq.Add(1))
		p = catalogtest.Product(e.t, tx, e.tenant, e.seller, fmt.Sprintf("BN%04d", n))
		catalogtest.Variant(e.t, tx, e.tenant, e.seller, p, n, price)
		if _, err := tx.Exec(e.t.Context(), `UPDATE catalog.products SET category_id = $1 WHERE id = $2`, cat, p); err != nil {
			return err //nolint:wrapcheck // test
		}
		if !photo {
			return nil
		}
		f := filestest.Image(e.t, tx, e.tenant, "product_image")
		_, err := tx.Exec(e.t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
			VALUES ($1, $2, $3, 1)`, e.tenant, p, f)
		return err //nolint:wrapcheck // test
	})
	return p
}

// campaign tạo chiến dịch web (chủ bảng) theo danh mục targets, trạng thái state, chạy từ 1 giờ trước tới 1 ngày sau.
func (e *env) campaign(state string, targets ...uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		scope := "all"
		if len(targets) > 0 {
			scope = "categories"
		}
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO promotion.web_campaigns (tenant_id, seller_id, name, percent, scope,
			starts_at, ends_at, state, created_by) VALUES ($1, $2, 'Sale', 20, $3, now() - interval '1 hour',
			now() + interval '1 day', $4, $5) RETURNING id`, e.tenant, e.seller, scope, state, e.user).Scan(&id); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, c := range targets {
			if _, err := tx.Exec(e.t.Context(), `INSERT INTO promotion.web_campaign_targets (tenant_id, campaign_id,
				target_kind, target_id) VALUES ($1, $2, 'category', $3)`, e.tenant, id, c); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return id
}

func (e *env) bannerImage() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = filestest.Image(e.t, tx, e.tenant, content.BannerPurposeKey)
		return nil
	})
	return id
}

// Lưu thay cả danh sách theo revision: chưa có → rỗng revision 0; lưu tăng revision, giữ thứ tự, chuẩn hóa; revision cũ
// → ErrRevisionConflict, không đổi gì; mỗi lần lưu một sự kiện content.banners.changed (Seq = revision mới).
func TestSaveBanners_ReplaceByRevision(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if s := e.get(); s.Revision != 0 || len(s.Banners) != 0 {
		t.Fatalf("chưa lưu: %+v", s)
	}
	img := e.bannerImage()
	camp := e.campaign("enabled")
	first := []content.Banner{
		{
			ID: uuid.New(), Enabled: true, Kind: content.KindImage, ImageDesktop: img, Alt: " Flash sale ", LinkKind: content.LinkOffers,
			Title: "bỏ",
		},
		{ID: uuid.New(), Kind: content.KindCampaign, CampaignID: camp, Kicker: "Ưu đãi", Theme: content.ThemeRed},
	}
	ch, err := e.save(first, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Before.Revision != 0 || ch.After.Revision != 1 || ch.After.UpdatedBy == nil || *ch.After.UpdatedBy != e.user {
		t.Fatalf("lần lưu: %+v", ch)
	}
	s := e.get()
	if s.Revision != 1 || len(s.Banners) != 2 || s.Banners[0].ID != first[0].ID || s.Banners[1].ID != first[1].ID {
		t.Fatalf("sau lưu: %+v", s)
	}
	checkSaved(t, s.Banners, img, camp)
	if _, err := e.save(nil, 0); !errors.Is(err, content.ErrRevisionConflict) {
		t.Fatalf("revision cũ: %v", err)
	}
	if _, err := e.save([]content.Banner{first[1], first[0]}, 1); err != nil {
		t.Fatal(err)
	}
	if s := e.get(); s.Revision != 2 || s.Banners[0].ID != first[1].ID {
		t.Fatalf("đổi thứ tự: %+v", s)
	}
	if _, err := e.save(nil, 2); err != nil {
		t.Fatal(err)
	}
	if s := e.get(); s.Revision != 3 || len(s.Banners) != 0 {
		t.Fatalf("xóa hết: %+v", s)
	}
	e.checkEvents(3)
}

// checkSaved: banner ảnh đã chuẩn hóa (mô tả cắt khoảng trắng, bỏ chữ, màu auto), banner chiến dịch giữ nguyên.
func checkSaved(t *testing.T, bs []content.Banner, img, camp uuid.UUID) {
	t.Helper()
	if b := bs[0]; b.Alt != "Flash sale" || b.Title != "" || b.ImageDesktop != img || b.Theme != content.ThemeAuto {
		t.Fatalf("banner ảnh lưu: %+v", b)
	}
	if b := bs[1]; b.Enabled || b.CampaignID != camp || b.Kicker != "Ưu đãi" || b.Theme != content.ThemeRed {
		t.Fatalf("banner chiến dịch lưu: %+v", b)
	}
}

// checkEvents: đúng một sự kiện content.banners.changed cho mỗi revision 1..n.
func (e *env) checkEvents(n int) {
	e.t.Helper()
	for seq := 1; seq <= n; seq++ {
		if n := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = $1
			AND aggregate_type = 'home_banners' AND aggregate_id = $2 AND seq = $3`, content.TopicBannersChanged,
			e.tenant.String(), seq); n != 1 {
			e.t.Fatalf("sự kiện seq %d: %d", seq, n)
		}
	}
}

// Kiểm khi lưu dùng dữ liệu thật: ảnh bị từ chối → ErrFileRejected; ảnh của tenant khác → ErrImageInvalid; ghim mẫu
// ngoài danh mục → ErrPinNotInSource; 9 banner → ErrTooManyBanners; lần lưu bị từ chối không đổi revision.
func TestSaveBanners_ChecksReferences(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var rejected uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		rejected = filestest.Rejected(t, tx, e.tenant, content.BannerPurposeKey)
		return nil
	})
	other := e.staff.Tenant(t, "B")
	var foreign uuid.UUID
	e.staff.Exec(t, other, func(tx pgx.Tx) error {
		foreign = filestest.Image(t, tx, other, content.BannerPurposeKey)
		return nil
	})
	catA, catB := e.category("ao", uuid.Nil), e.category("vay", uuid.Nil)
	inB := e.product(catB, true, 100000)
	imageBanner := func(img uuid.UUID) content.Banner {
		return content.Banner{
			ID: uuid.New(), Enabled: true, Kind: content.KindImage, ImageDesktop: img, Alt: "a",
			LinkKind: content.LinkOffers,
		}
	}
	nine := make([]content.Banner, 9)
	for i := range nine {
		nine[i] = content.Banner{ID: uuid.New(), Kind: content.KindNew}
	}
	for name, c := range map[string]struct {
		list []content.Banner
		want error
	}{
		"ảnh bị từ chối":  {[]content.Banner{imageBanner(rejected)}, content.ErrFileRejected},
		"ảnh tenant khác": {[]content.Banner{imageBanner(foreign)}, content.ErrImageInvalid},
		"ghim ngoài danh mục": {[]content.Banner{{
			ID: uuid.New(), Kind: content.KindCategory, CategoryID: catA,
			ProductIDs: []uuid.UUID{inB},
		}}, content.ErrPinNotInSource},
		"9 banner": {nine, content.ErrTooManyBanners},
	} {
		if _, err := e.save(c.list, 0); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, muốn %v", name, err, c.want)
		}
	}
	if s := e.get(); s.Revision != 0 || len(s.Banners) != 0 {
		t.Fatalf("lần lưu hỏng đã ghi: %+v", s)
	}
}

// Nguồn của banner (Pool) và lý do ẩn tính trên dữ liệu thật: danh mục cha gồm mẫu của danh mục con; mẫu không ảnh hay
// chưa giá không tính; chiến dịch theo danh mục con; bán chạy theo danh sách bán 30 ngày; chiến dịch đã xóa →
// campaign-missing; tạm dừng → campaign-not-running.
func TestLoadRefData_PoolAndVisibility(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	parent := e.category("ao", uuid.Nil)
	child := e.category("ao-so-mi", parent)
	empty := e.category("vay", uuid.Nil)
	shown := e.product(child, true, 250000)
	noPhoto := e.product(empty, false, 250000)
	e.product(empty, true, 0) // chưa có giá: không lên banner
	running := e.campaign("enabled", child)
	onEmpty := e.campaign("enabled", empty)
	paused := e.campaign("paused")
	deleted := e.campaign("enabled")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.web_campaigns SET deleted_at = now() WHERE id = $1`, deleted)
		return err //nolint:wrapcheck // test
	})
	b := func(kind string, mod func(*content.Banner)) content.Banner {
		x := content.Banner{ID: uuid.New(), Enabled: true, Kind: kind}
		mod(&x)
		return x
	}
	cases := []struct {
		name   string
		b      content.Banner
		sold   []uuid.UUID
		reason string
	}{
		{"danh mục cha", b(content.KindCategory, func(x *content.Banner) { x.CategoryID = parent }), nil, ""},
		{
			"danh mục không mẫu hiện được", b(content.KindCategory, func(x *content.Banner) { x.CategoryID = empty }), nil,
			content.HiddenNoProducts,
		},
		{"hàng mới", b(content.KindNew, func(*content.Banner) {}), nil, ""},
		{"chiến dịch chạy", b(content.KindCampaign, func(x *content.Banner) { x.CampaignID = running }), nil, ""},
		{
			"chiến dịch không mẫu", b(content.KindCampaign, func(x *content.Banner) { x.CampaignID = onEmpty }), nil,
			content.HiddenNoProducts,
		},
		{
			"chiến dịch tạm dừng", b(content.KindCampaign, func(x *content.Banner) { x.CampaignID = paused }), nil,
			content.HiddenCampaignNotRunning,
		},
		{
			"chiến dịch đã xóa", b(content.KindCampaign, func(x *content.Banner) { x.CampaignID = deleted }), nil,
			content.HiddenCampaignMissing,
		},
		{"bán chạy có bán", b(content.KindTrending, func(*content.Banner) {}), []uuid.UUID{noPhoto, shown}, ""},
		{
			"bán chạy chỉ mẫu không ảnh", b(content.KindTrending, func(*content.Banner) {}),
			[]uuid.UUID{noPhoto},
			content.HiddenNoProducts,
		},
		{"bán chạy không dữ liệu", b(content.KindTrending, func(*content.Banner) {}), nil, content.HiddenNoProducts},
	}
	for _, c := range cases {
		var ref content.RefData
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			ref, err = content.LoadRefData(t.Context(), tx, []content.Banner{c.b}, c.sold)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		if _, reason := content.Visibility(c.b, ref, time.Now()); reason != c.reason {
			t.Errorf("%s: lý do %q, muốn %q", c.name, reason, c.reason)
		}
	}
}

// Tenant khác không thấy, không đụng danh sách của nhau.
func TestBanners_OtherTenant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.save([]content.Banner{{ID: uuid.New(), Kind: content.KindNew}}, 0); err != nil {
		t.Fatal(err)
	}
	b := &env{t: t, staff: e.staff, tenant: e.staff.Tenant(t, "B")}
	b.user = e.staff.User(t, b.tenant, "nv@b.test", "active")
	if s := b.get(); s.Revision != 0 || len(s.Banners) != 0 {
		t.Fatalf("tenant B thấy: %+v", s)
	}
	if _, err := b.save(nil, 0); err != nil {
		t.Fatal(err)
	}
	if s := e.get(); s.Revision != 1 || len(s.Banners) != 1 {
		t.Fatalf("tenant A bị đổi: %+v", s)
	}
}
