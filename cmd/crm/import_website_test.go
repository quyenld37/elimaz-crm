package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// importEnv là một tenant có đủ dữ liệu CRM mà thư mục mẫu testdata/admin-export trỏ tới: người bán mặc định, danh mục
// "Áo" > "Áo sơ mi", hai mẫu (P1 hai ảnh, P2 một ảnh), khách giữ SĐT 0912000111, tài khoản hãng vtp, best đang hoạt
// động (chưa hiện trên web); và một tenant khác có một chiến dịch, một góp ý (không được chạm).
type importEnv struct {
	t             *testing.T
	s             *testkit.Staff
	tenant, other uuid.UUID
	p1, p2        uuid.UUID
	p1Images      []uuid.UUID
	customer      uuid.UUID
	vtp           uuid.UUID
	store         *files.MemStore
	svc           *fulfillment.Service
	evs           *events.Registry
}

func newImportEnv(t *testing.T) *importEnv {
	t.Helper()
	content.RegisterFilePurposes()
	s := testkit.OpenStaff(t)
	e := &importEnv{
		t: t, s: s, tenant: s.Tenant(t, "ELIMAZ"), other: s.Tenant(t, "KHAC"), store: files.NewMemStore(),
		evs: events.NewRegistry(),
	}
	hc := &http.Client{Timeout: time.Second} // hãng giả không được gọi: lệnh nhập không gọi hãng
	reg, err := fulfillment.NewRegistry(
		fake.NewClientWithKey("vtp", "http://127.0.0.1:1", "0123456789abcdef0123456789abcdef", hc),
		fake.NewClientWithKey("best", "http://127.0.0.1:1", "0123456789abcdef0123456789abcdef", hc))
	if err != nil {
		t.Fatal(err)
	}
	e.svc = fulfillment.New(fulfillment.Deps{DB: s.App, Queue: queue.NewQueue(), Events: e.evs, Carriers: reg}).Service
	s.Exec(t, e.tenant, e.seedTenant)
	w := customer.Writer{Events: e.evs, Queue: queue.NewQueue()}
	e.app(e.tenant, func(tx pgx.Tx) error {
		c, err := w.CreateCustomer(t.Context(), tx, customer.CreateInput{FullName: "Chị Lan", Phone: "0912000111"},
			customer.Actor{Type: customer.ActorSystem}, time.Now())
		e.customer = c.ID
		return err //nolint:wrapcheck // test
	})
	// Tenant khác: một chiến dịch, một góp ý.
	s.Exec(t, e.other, func(tx pgx.Tx) error {
		catalogtest.Sellers(t, tx, e.other)
		return nil
	})
	e.app(e.other, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = promotion.Writer{Events: e.evs, Queue: queue.NewQueue()}.CreateCampaign(t.Context(), tx,
			promotion.Actor{TenantID: e.other, UserID: uuid.New()}, seller, promotion.WebCampaignInput{
				Name: "Khác", Percent: 10, Scope: promotion.ScopeAll, StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
			})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = w.SubmitFeedback(t.Context(), tx, customer.FeedbackInput{Topic: "other", Body: "khác"},
			customer.Actor{Type: customer.ActorService}, time.Now())
		return err //nolint:wrapcheck // test
	})
	return e
}

// seedTenant dựng dữ liệu CRM của tenant A (chủ bảng): danh mục, mẫu, ảnh, kho, tài khoản hãng.
func (e *importEnv) seedTenant(tx pgx.Tx) error {
	seller, err := e.seedCatalog(tx)
	if err != nil {
		return err
	}
	return e.seedCarriers(tx, seller)
}

// seedCatalog: danh mục "Áo" > " Áo sơ mi ", mẫu P1 (hai ảnh), P2 (một ảnh) thuộc "Áo sơ mi"; trả người bán mặc định.
func (e *importEnv) seedCatalog(tx pgx.Tx) (uuid.UUID, error) {
	ctx, t := e.t.Context(), e.t
	seller, _ := catalogtest.Sellers(t, tx, e.tenant)
	var ao, somi uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO catalog.categories (tenant_id, name, slug, depth)
		VALUES (current_setting('app.tenant_id')::uuid, 'Áo', 'ao', 1) RETURNING id`).Scan(&ao); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // test
	}
	if err := tx.QueryRow(ctx, `INSERT INTO catalog.categories (tenant_id, parent_id, name, slug, depth)
		VALUES (current_setting('app.tenant_id')::uuid, $1, ' Áo sơ mi ', 'ao-so-mi', 2) RETURNING id`, ao).Scan(&somi); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // test
	}
	e.p1 = catalogtest.Product(t, tx, e.tenant, seller, "P0001")
	e.p2 = catalogtest.Product(t, tx, e.tenant, seller, "P0002")
	for i, p := range []uuid.UUID{e.p1, e.p2} {
		catalogtest.Variant(t, tx, e.tenant, seller, p, 100+i, 350000)
		if _, err := tx.Exec(ctx, `UPDATE catalog.products SET category_id = $1 WHERE id = $2`, somi, p); err != nil {
			return uuid.Nil, err //nolint:wrapcheck // test
		}
	}
	for i, p := range []uuid.UUID{e.p1, e.p1, e.p2} {
		f := filestest.Image(t, tx, e.tenant, "product_image")
		if p == e.p1 {
			e.p1Images = append(e.p1Images, f)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3)`, p, f, i+1); err != nil {
			return uuid.Nil, err //nolint:wrapcheck // test
		}
	}
	return seller, nil
}

// seedCarriers: kho mặc định, tài khoản hãng vtp, best đang hoạt động (chưa hiện trên web).
func (e *importEnv) seedCarriers(tx pgx.Tx, seller uuid.UUID) error {
	ctx := e.t.Context()
	wh, err := inventory.EnsureDefaultWarehouse(ctx, tx, e.tenant, seller)
	if err != nil {
		return err //nolint:wrapcheck // test
	}
	for _, key := range []string{"vtp", "best"} {
		id, err := fulfillment.EnsureCarrierAccount(ctx, tx, fulfillment.SeedAccountInput{
			SellerID: seller, WarehouseID: wh, CarrierKey: key, Name: strings.ToUpper(key), ExternalAccountRef: key + "-1",
		})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if key == "vtp" {
			e.vtp = id
		}
	}
	return nil
}

func (e *importEnv) app(tenant uuid.UUID, fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.s.App, tenant, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *importEnv) count(tenant uuid.UUID, q string, args ...any) int {
	e.t.Helper()
	return e.s.CountIn(e.t, tenant, q, args...)
}

// exportDir chép thư mục mẫu vào thư mục tạm (thay {{P1}}, {{P2}} bằng mã mẫu CRM) và ghi hai ảnh banner (đầu tệp
// WebP); broken: ảnh desktop là tệp không phải ảnh.
func (e *importEnv) exportDir(broken bool) string {
	e.t.Helper()
	dir := e.t.TempDir()
	raw, err := os.ReadFile("testdata/admin-export/data.json.tmpl")
	if err != nil {
		e.t.Fatal(err)
	}
	s := strings.NewReplacer("{{P1}}", e.p1.String(), "{{P2}}", e.p2.String()).Replace(string(raw))
	must(e.t, os.WriteFile(filepath.Join(dir, "data.json"), []byte(s), 0o600)) //nolint:gosec // G703: thư mục tạm của test
	u, err := os.ReadFile("testdata/admin-export/unmatched.json")
	must(e.t, err)
	must(e.t, os.WriteFile(filepath.Join(dir, "unmatched.json"), u, 0o600)) //nolint:gosec // G703: như trên
	must(e.t, os.Mkdir(filepath.Join(dir, "banner-images"), 0o700))
	webp := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)
	desktop := webp
	if broken {
		desktop = []byte("<html>không phải ảnh</html>")
	}
	must(e.t, os.WriteFile(filepath.Join(dir, "banner-images", "0123456789abcdef0123456789abcdef.webp"), desktop, 0o600))
	must(e.t, os.WriteFile(filepath.Join(dir, "banner-images", "fedcba9876543210fedcba9876543210.webp"), webp, 0o600))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// importer dựng lệnh nhập cho tenant A; WaitImage giả worker của CRM: đặt tệp ảnh banner vừa tải lên thành ready
// (đường xử lý ảnh thật có test riêng ở gói files).
func (e *importEnv) importer(dry bool) *websiteImporter {
	return &websiteImporter{
		DB: e.s.App, Tenant: e.tenant, DryRun: dry, Events: e.evs, Queue: queue.NewQueue(), Fulfillment: e.svc,
		Files: e.store, Now: time.Now, ImageView: catalog.ImageView{BaseURL: "https://img.test", Preset: 1},
		WaitImage: func(ctx context.Context, id uuid.UUID) error {
			sum := make([]byte, 32)
			_, _ = rand.Read(sum)
			return tenancy.WithTenant(ctx, e.s.Owner, e.tenant, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE platform.files SET status = 'ready', detected_type = 'image/webp',
					width = 2400, height = 1000, content_sha256 = $2, preset_version = 1001 WHERE id = $1`, id, sum)
				return err //nolint:wrapcheck // test
			})
		},
	}
}

func (e *importEnv) run(dir string, dry bool) (*importReport, string) {
	e.t.Helper()
	x, unmatched, err := readWebsiteExport(dir)
	if err != nil {
		e.t.Fatal(err)
	}
	rep, err := e.importer(dry).run(e.t.Context(), dir, x, unmatched)
	if err != nil {
		e.t.Fatal(err)
	}
	var out bytes.Buffer
	rep.print(&out)
	return rep, out.String()
}

// importTables là các bảng lệnh nhập có thể ghi (đếm trước/sau chạy khô, chạy lại, và ở tenant khác).
func importTables() []string {
	return []string{
		"customer.feedback", "ord.web_intake", "fulfillment.channel_carrier_settings", "promotion.web_campaigns",
		"promotion.web_campaign_targets", "promotion.birthday_settings", "platform.files", "content.home_banners",
		"content.home_banner_sets", "content.looks", "content.look_items", "content.look_sets", "identity.audit_logs",
		"platform.domain_events",
		"fulfillment.carrier_accounts WHERE show_on_web",
	}
}

func (e *importEnv) snapshot(tenant uuid.UUID) map[string]int {
	e.t.Helper()
	out := map[string]int{}
	for _, tb := range importTables() {
		out[tb] = e.count(tenant, "SELECT count(*) FROM "+tb)
	}
	return out
}

func sameCounts(t *testing.T, name string, before, after map[string]int) {
	t.Helper()
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s: %s %d → %d", name, k, v, after[k])
		}
	}
}

func wantCounts(t *testing.T, name string, rep *importReport, added, skipped, failed map[string]int) {
	t.Helper()
	for _, k := range importKinds() {
		c := rep.Kinds[k]
		if c.Added != added[k] || c.Skipped != skipped[k] || c.Failed != failed[k] {
			t.Errorf("%s: %s thêm %d bỏ qua %d lỗi %d, mong %d/%d/%d (%v)", name, k, c.Added, c.Skipped, c.Failed,
				added[k], skipped[k], failed[k], c.Notes)
		}
	}
}

// Thư mục mẫu [KM04]: chạy khô → đúng số từng loại mà database không đổi (đếm trước/sau); --apply → cùng số, dữ liệu
// đúng (chiến dịch giữ id, enabled → paused, ngày tạo; danh mục theo đường tên; banner 4 kèm ảnh đã tải; bộ phối ảnh
// thứ 2 của P1; góp ý giữ id, trạng thái, ghi chú, gắn khách; cài đặt quà, tạm dừng, hãng); chạy lại → 0 thay đổi,
// không thêm audit, sự kiện; tenant khác không bị chạm; bảng in ra và log không có SĐT, nội dung góp ý, ghi chú.
func TestImportWebsite_SampleDryRunApplyAgain(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	dir := e.exportDir(false)
	otherBefore := e.snapshot(e.other)
	added := map[string]int{
		kindFeedback: 3, kindPause: 1, kindCarriers: 3, kindCampaign: 3, kindGift: 1, kindBanner: 4, kindLook: 2,
	}
	none := map[string]int{}

	before := e.snapshot(e.tenant)
	dry, dryOut := e.run(dir, true)
	wantCounts(t, "chạy khô", dry, added, none, none)
	sameCounts(t, "chạy khô", before, e.snapshot(e.tenant))
	if n := len(e.store.Keys(files.BucketTmp)); n != 0 {
		t.Errorf("chạy khô ghi %d đối tượng lên kho tệp", n)
	}

	rep, out := e.run(dir, false)
	wantCounts(t, "apply", rep, added, none, none)
	e.checkImported()

	after := e.snapshot(e.tenant)
	again, againOut := e.run(dir, false)
	wantCounts(t, "chạy lại", again, none, map[string]int{
		kindFeedback: 3, kindPause: 1, kindCarriers: 3, kindCampaign: 3, kindGift: 1, kindBanner: 4, kindLook: 2,
	}, none)
	sameCounts(t, "chạy lại", after, e.snapshot(e.tenant))
	sameCounts(t, "tenant khác", otherBefore, e.snapshot(e.other))

	var logs bytes.Buffer
	reportImport(again, &bytes.Buffer{}, slog.New(slog.NewJSONHandler(&logs, nil)), false)
	all := dryOut + out + againOut + logs.String()
	for _, want := range []string{"CHẠY KHÔ", "ĐÃ GHI", "sinh nhật: 2 hồ sơ", "1 từ chối", "campaign 1, look 1"} {
		if !strings.Contains(all, want) {
			t.Errorf("bảng thiếu %q:\n%s", want, all)
		}
	}
	for _, bad := range []string{
		"0912", "912000111", "0987654321", "0900000002", "NOIDUNG-KIN", "GHICHU-KIN", "Chị Lan",
		"SDT-0911222333", "staff@elimaz",
	} {
		if strings.Contains(all, bad) {
			t.Errorf("bảng hay log chứa %q", bad)
		}
	}
}

func (e *importEnv) checkImported() {
	t := e.t
	t.Helper()
	e.app(e.tenant, func(tx pgx.Tx) error {
		ctx := t.Context()
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		c1, err := promotion.GetWebCampaign(ctx, tx, seller, uuid.MustParse("a1000000-0000-4000-8000-000000000001"))
		if err != nil || c1.State != promotion.StatePaused || !c1.CreatedAt.Equal(time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)) {
			t.Errorf("chiến dịch đang bật nhập = %+v %v", c1, err)
		}
		c3, err := promotion.GetWebCampaign(ctx, tx, seller, uuid.MustParse("a3000000-0000-4000-8000-000000000003"))
		if err != nil || c3.State != promotion.StateDraft || len(c3.TargetIDs) != 1 {
			t.Errorf("chiến dịch theo danh mục = %+v %v", c3, err)
		}
		bs, err := content.GetBanners(ctx, tx)
		if err != nil || len(bs.Banners) != 4 || bs.Banners[3].Kind != content.KindImage ||
			bs.Banners[3].ImageDesktop == uuid.Nil || bs.Banners[3].ImageMobile == uuid.Nil || bs.Banners[0].Theme != "red" {
			t.Errorf("banner = %+v %v", bs, err)
		}
		ls, err := content.GetLooks(ctx, tx)
		if err != nil || len(ls.Looks) != 2 || ls.Looks[0].PhotoFileID != e.p1Images[1] || !ls.Looks[0].OnHome ||
			ls.Looks[0].ModelHeightCM != 165 || len(ls.Looks[1].CategoryIDs) != 1 {
			t.Errorf("bộ phối = %+v %v", ls, err)
		}
		return e.checkImportedSettings(ctx, tx)
	})
	if n := e.count(e.tenant, "SELECT count(*) FROM fulfillment.carrier_accounts WHERE show_on_web"); n != 2 {
		t.Errorf("tài khoản hãng hiện trên web = %d", n)
	}
	if n := e.count(e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE actor_type = 'system' AND actor_id = $1`,
		importSystemID().String()); n == 0 {
		t.Error("không có audit của tài khoản hệ thống")
	}
}

// checkImportedSettings: góp ý (giữ id, trạng thái, giờ sửa, ghi chú, gắn khách), cài đặt quà, tạm dừng, hãng ưu tiên.
func (e *importEnv) checkImportedSettings(ctx context.Context, tx pgx.Tx) error {
	t := e.t
	fs, _, err := customer.ListFeedback(ctx, tx, customer.FeedbackFilter{})
	if err != nil || len(fs) != 3 {
		t.Fatalf("góp ý = %d %v", len(fs), err)
	}
	for _, f := range fs {
		switch f.ID.String() {
		case "d1000000-0000-4000-8000-000000000001":
			if f.CustomerID == nil || *f.CustomerID != e.customer {
				t.Errorf("góp ý có SĐT khách không gắn khách: %v", f.CustomerID)
			}
		case "d2000000-0000-4000-8000-000000000002":
			if f.State != customer.FeedbackAnswered || f.CustomerID != nil ||
				!f.UpdatedAt.Equal(time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)) || f.Note == "" {
				t.Errorf("góp ý đã trả lời = %+v", f)
			}
		}
	}
	g, err := promotion.GetBirthdaySettings(ctx, tx)
	if err != nil || !g.Enabled || g.Amount != 50000 || g.DaysBefore != 7 || g.ValidDays != 14 || g.MinLeadDays != 30 {
		t.Errorf("cài đặt quà = %+v %v", g, err)
	}
	wi, err := order.GetWebIntake(ctx, tx)
	if err != nil || !wi.Paused || wi.Reason != "Bảo trì" {
		t.Errorf("tạm dừng = %+v %v", wi, err)
	}
	set, err := fulfillment.GetWebCarrierSettings(ctx, tx)
	if err != nil || set.PreferredAccountID == nil || *set.PreferredAccountID != e.vtp {
		t.Errorf("hãng ưu tiên = %+v %v", set, err)
	}
	return nil
}

// Một ảnh banner hỏng: banner đó lỗi (lệnh thoát khác 0), ba banner kia và mọi loại khác vẫn nhập; chạy lại sau khi
// sửa ảnh → chỉ thêm banner đó.
func TestImportWebsite_BrokenBannerImage(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	rep, out := e.run(e.exportDir(true), false)
	if c := rep.Kinds[kindBanner]; c.Added != 3 || c.Failed != 1 || !strings.Contains(out, "b4000000-0000-4000-8000-000000000004") {
		t.Fatalf("banner: %+v\n%s", c, out)
	}
	if rep.Kinds[kindLook].Added != 2 || rep.Kinds[kindCampaign].Added != 3 || rep.Failed() != 1 {
		t.Fatalf("loại khác: lỗi %d\n%s", rep.Failed(), out)
	}
	if code := importWebsiteExit(errors.Join(errImportIncomplete), &bytes.Buffer{}); code == exitOK {
		t.Fatal("có bản ghi lỗi mà thoát 0")
	}
	again, _ := e.run(e.exportDir(false), false)
	if c := again.Kinds[kindBanner]; c.Added != 1 || c.Skipped != 3 || c.Failed != 0 {
		t.Fatalf("chạy lại sau khi sửa ảnh: %+v", c)
	}
	if n := e.count(e.tenant, "SELECT count(*) FROM content.home_banners"); n != 4 {
		t.Fatalf("banner = %d", n)
	}
}

// version lạ → thoát khác 0 (exitConfig) trước khi mở database; JSON có trường lạ, thiếu data.json → exitConfig; cờ
// sai (không --dry-run/--apply, cả hai, tenant không phải UUID) → exitConfig.
func TestImportWebsite_VersionAndUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"version": 2, "campaigns": []}`), 0o600))
	_, _, err := readWebsiteExport(dir)
	if !errors.Is(err, errImportVersion) || importWebsiteExit(err, &bytes.Buffer{}) != exitConfig {
		t.Fatalf("version 2: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"version": 1, "lạ": 1}`), 0o600))
	if _, _, err := readWebsiteExport(dir); !errors.Is(err, errBadExport) {
		t.Fatalf("trường lạ: %v", err)
	}
	if _, _, err := readWebsiteExport(t.TempDir()); !errors.Is(err, errBadExport) {
		t.Fatalf("thiếu data.json: %v", err)
	}
	tenant := uuid.New().String()
	for _, args := range [][]string{
		{"--dir", dir, "--tenant", tenant},
		{"--dir", dir, "--tenant", tenant, "--dry-run", "--apply"},
		{"--dir", dir, "--tenant", "ELIMAZ", "--dry-run"},
		{"--tenant", tenant, "--apply"},
	} {
		_, err := parseImportWebsiteFlags(args)
		if importWebsiteExit(err, &bytes.Buffer{}) != exitConfig {
			t.Errorf("cờ %v: %v", args, err)
		}
	}
	fl, err := parseImportWebsiteFlags([]string{"--dir", dir, "--tenant", tenant, "--dry-run"})
	if err != nil || !fl.DryRun || fl.ImageWait != defaultImageWait {
		t.Fatalf("cờ đúng: %+v %v", fl, err)
	}
}

// Một bộ phối trùng link key (8 hex đầu của mã) với bộ khác trong cùng tệp: chỉ bộ đó lỗi, hai bộ kia vẫn nhập (lần
// lưu cả loại không bị lùi). Lỗi gặp khi chạy khô bản xuất thật của website (mẫu của test 466: mã bộ 00000000-…).
func TestImportWebsite_LookLinkKeyClash(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	dir := e.exportDir(false)
	editExport(t, dir, func(doc map[string]any) {
		looks := doc["looks"].(map[string]any)
		items := looks["items"].([]any)
		clash := maps.Clone(items[0].(map[string]any))
		clash["id"], clash["position"], clash["enabled"] = "c1000000-0000-4000-8000-0000000000ff", 9, false
		looks["items"] = append(items, clash)
	})

	rep, out := e.run(dir, false)
	if c := rep.Kinds[kindLook]; c.Added != 2 || c.Failed != 1 || !strings.Contains(out, "c1000000-0000-4000-8000-0000000000ff") {
		t.Fatalf("bộ phối: %+v\n%s", c, out)
	}
	if n := e.count(e.tenant, "SELECT count(*) FROM content.looks"); n != 2 {
		t.Fatalf("bộ phối đã lưu = %d", n)
	}
}

// editExport sửa data.json của thư mục xuất dir bằng fn (trên JSON đã giải).
func editExport(t *testing.T, dir string, fn func(doc map[string]any)) {
	t.Helper()
	file := filepath.Join(dir, "data.json")
	raw, err := os.ReadFile(file)
	must(t, err)
	var doc map[string]any
	must(t, json.Unmarshal(raw, &doc))
	fn(doc)
	raw, err = json.Marshal(doc)
	must(t, err)
	must(t, os.WriteFile(file, raw, 0o600))
}

// Website chưa chọn hãng (configured false: mọi hãng hiện) mà có hãng ưu tiên (script 466 vẫn xuất preferred): không
// tắt hãng nào; bật hiện trên web đúng tài khoản vtp (CRM đòi hãng ưu tiên hiện trên web) rồi đặt ưu tiên vtp; không có
// ưu tiên → bỏ qua cả loại.
func TestImportWebsite_CarriersNotConfiguredPreferred(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	dir := e.exportDir(false)
	editExport(t, dir, func(doc map[string]any) {
		doc["carriers"] = map[string]any{"configured": false, "revision": 1, "preferred": map[string]any{"nhanhId": 2, "code": "vtp"}}
	})
	rep, out := e.run(dir, false)
	if c := rep.Kinds[kindCarriers]; c.Added != 2 || c.Failed != 0 {
		t.Fatalf("hãng: %+v\n%s", c, out)
	}
	if n := e.count(e.tenant, "SELECT count(*) FROM fulfillment.carrier_accounts WHERE show_on_web AND id = $1", e.vtp); n != 1 {
		t.Fatalf("vtp hiện trên web = %d, mong 1", n)
	}
	if n := e.count(e.tenant, "SELECT count(*) FROM fulfillment.carrier_accounts WHERE show_on_web"); n != 1 {
		t.Fatalf("hãng hiện trên web = %d, mong 1", n)
	}
	e.app(e.tenant, func(tx pgx.Tx) error {
		set, err := fulfillment.GetWebCarrierSettings(t.Context(), tx)
		if err != nil || set.PreferredAccountID == nil || *set.PreferredAccountID != e.vtp {
			t.Errorf("hãng ưu tiên = %+v %v", set, err)
		}
		return nil
	})
	editExport(t, dir, func(doc map[string]any) { doc["carriers"] = map[string]any{"configured": false} })
	rep, _ = e.run(dir, false)
	if c := rep.Kinds[kindCarriers]; c.Added != 0 || c.Skipped != 1 || c.Failed != 0 {
		t.Fatalf("không ưu tiên: %+v", c)
	}
}

// Danh mục theo đường tên: đúng một danh mục CRM cùng đường (cắt khoảng trắng từng cấp) → mã đó; không có hay hai danh
// mục cùng đường → lỗi, không đoán; chỉ khớp đủ đường (một cấp "Áo sơ mi" không khớp "Áo > Áo sơ mi").
func TestImportWebsite_CategoryPathResolve(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	cats := categoryPaths{
		pathKey([]string{"Áo", "Áo sơ mi"}): {a},
		pathKey([]string{"Váy"}):            {b, c},
	}
	if id, err := cats.resolve(wxCategory{NhanhID: 1, Path: []string{" Áo ", "Áo sơ mi "}}); err != nil || id != a {
		t.Fatalf("khớp đúng một: %v %v", id, err)
	}
	for name, path := range map[string][]string{
		"hai danh mục": {"Váy"}, "không có": {"Quần"}, "thiếu cấp cha": {"Áo sơ mi"},
	} {
		if _, err := cats.resolve(wxCategory{Path: path}); err == nil {
			t.Errorf("%s: không lỗi", name)
		}
	}
}

// Lệnh qua đúng đường của main (run: config từ biến môi trường, cờ, mở pool crm_app, kiểm tenant, dựng module, mã
// thoát) trên database thử chỉ có một tenant: chạy khô một tệp nhỏ (góp ý có SĐT, cài đặt quà, tạm dừng) → thoát 0, in
// bảng, database không đổi; --apply ở vai trò không có kho tệp (migrate) → thoát 2, không ghi; --apply có cấu hình kho
// tệp (như container worker) → ghi; tenant lạ, version lạ, thiếu cờ → thoát 2. Không đâu in SĐT, nội dung góp ý.
func TestRun_ImportWebsiteCommandLine(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	owner, err := db.Open(t.Context(), ownerURL, db.PoolOptions{MaxConns: 2})
	must(t, err)
	t.Cleanup(owner.Pool.Close)
	s := &testkit.Staff{Owner: owner}
	tenant, dir := s.Tenant(t, "ELIMAZ"), t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"version": 1, "campaigns": [], "banners": null,
		"looks": null, "feedback": [{"id": "d1000000-0000-4000-8000-000000000001", "topic": "product", "mood": 5,
		"body": "NOIDUNG-KIN", "contact": "0912 000 111", "state": "new", "createdAt": "2026-10-01T02:00:00.000Z",
		"updatedAt": "2026-10-01T02:00:00.000Z"}], "birthdays": {"declinedLeftOut": 0, "profiles": []},
		"giftSettings": {"saved": true, "enabled": true, "amount": 50000, "daysBefore": 7, "validDays": 14,
		"minLeadDays": 30, "minOrder": 0}, "checkoutPause": {"paused": false, "reason": ""},
		"carriers": {"configured": false}}`), 0o600))
	env := map[string]string{"CRM_ROLE": "migrate", "CRM_ENV": "dev", "DATABASE_URL": appURL, "DATABASE_OWNER_URL": ownerURL}
	noLeak := func(all string) {
		for _, bad := range []string{"0912", "912000111", "NOIDUNG-KIN"} {
			if strings.Contains(all, bad) {
				t.Errorf("lệnh in %q: %s", bad, all)
			}
		}
	}
	cmd := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := run(t.Context(), append([]string{importWebsiteCommand}, args...), func(k string) string { return env[k] },
			&out, &errOut)
		noLeak(out.String() + errOut.String())
		return code, out.String() + errOut.String()
	}
	count := func() int {
		return s.CountIn(t, tenant, "SELECT count(*) FROM customer.feedback") +
			s.CountIn(t, tenant, "SELECT count(*) FROM promotion.birthday_settings")
	}
	if code, out := cmd("--dir", dir, "--tenant", tenant.String(), "--dry-run"); code != exitOK ||
		!strings.Contains(out, "CHẠY KHÔ") || count() != 0 {
		t.Fatalf("chạy khô: thoát %d, %d dòng\n%s", code, count(), out)
	}
	if code, out := cmd("--dir", dir, "--tenant", tenant.String(), "--apply"); code != exitConfig ||
		!strings.Contains(out, "CRM_ROLE=worker") || count() != 0 {
		t.Fatalf("apply không kho tệp: thoát %d, %d dòng\n%s", code, count(), out)
	}
	// Cấu hình như container worker (kho tệp có cấu hình; tệp không có banner ảnh nên kho không được gọi).
	cfg := config.Config{
		Env: "dev", Role: config.RoleMigrate, DatabaseURL: appURL, DBMaxConns: 4, S3Endpoint: "http://127.0.0.1:1",
		S3Region: "us-east-1", S3AccessKeyID: "k", S3SecretAccessKey: "s", S3BucketPrefix: "crm-",
		FilesPublicBaseURL: "https://img.test", ImagePresetVersion: 1,
	}
	var out, logs bytes.Buffer
	err = runImportWebsite(t.Context(), cfg, []string{"--dir", dir, "--tenant", tenant.String(), "--apply"}, &out,
		slog.New(slog.NewJSONHandler(&logs, nil)))
	noLeak(out.String() + logs.String())
	if err != nil || !strings.Contains(out.String(), "ĐÃ GHI") || count() != 2 {
		t.Fatalf("apply: %v, %d dòng\n%s", err, count(), out.String())
	}
	if code, out := cmd("--dir", dir, "--tenant", uuid.NewString(), "--dry-run"); code != exitConfig ||
		!strings.Contains(out, errImportTenant.Error()) {
		t.Fatalf("tenant lạ: thoát %d\n%s", code, out)
	}
	if code, _ := cmd("--dir", dir, "--tenant", tenant.String()); code != exitConfig {
		t.Fatalf("thiếu --dry-run/--apply: thoát %d", code)
	}
	must(t, os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"version": 2}`), 0o600))
	if code, out := cmd("--dir", dir, "--tenant", tenant.String(), "--dry-run"); code != exitConfig ||
		!strings.Contains(out, errImportVersion.Error()) {
		t.Fatalf("version 2: thoát %d\n%s", code, out)
	}
}
