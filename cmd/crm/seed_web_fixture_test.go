package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// fixturePath là tệp mẫu do website sinh (`npx jiti scripts/export-fixture-for-crm.ts`, apps/storefront-crm nhánh
// crm/m5): 6 danh mục, 9 mẫu, 39 biến thể, 1 mã, 2 tỉnh.
const fixturePath = "testdata/web-fixture.json"

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mutate đọc tệp mẫu thành cây JSON, cho fn sửa, rồi mã hóa lại.
func mutate(t *testing.T, fn func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readFixture(t), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func at(m map[string]any, path ...any) map[string]any {
	var cur any = m
	for _, p := range path {
		switch k := p.(type) {
		case string:
			cur = cur.(map[string]any)[k]
		case int:
			cur = cur.([]any)[k]
		}
	}
	return cur.(map[string]any)
}

func TestParseWebFixture_Sample(t *testing.T) {
	t.Parallel()
	fx, err := parseWebFixture(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	variants := 0
	for _, p := range fx.Products {
		variants += len(p.Variants)
	}
	if len(fx.Categories) != 6 || len(fx.Products) != 9 || variants != 39 || len(fx.Coupons) != 1 || len(fx.Locations) != 2 {
		t.Fatalf("đếm sai: %d danh mục, %d mẫu, %d biến thể, %d mã, %d tỉnh", len(fx.Categories), len(fx.Products), variants,
			len(fx.Coupons), len(fx.Locations))
	}
}

// Tệp hỏng từng kiểu → errBadFixture (kiểm trước khi mở database; tệp đúng mà không qua là không ghi gì).
func TestParseWebFixture_Rejects(t *testing.T) {
	t.Parallel()
	cases := map[string]func(m map[string]any){
		"version 2":            func(m map[string]any) { m["version"] = 2 },
		"thiếu version":        func(m map[string]any) { delete(m, "version") },
		"namespace khác":       func(m map[string]any) { m["namespace"] = uuid.NewString() },
		"id mẫu không phải v5": func(m map[string]any) { at(m, "products", 0)["id"] = uuid.NewString() },
		"id biến thể sai khóa": func(m map[string]any) { at(m, "products", 0, "variants", 0)["key"] = "khac" },
		"id danh mục sai":      func(m map[string]any) { at(m, "categories", 0)["id"] = uuid.NewString() },
		"người bán sai id":     func(m map[string]any) { at(m, "seller")["id"] = uuid.NewString() },
		"sellerId lạ":          func(m map[string]any) { at(m, "products", 0)["sellerId"] = uuid.NewString() },
		"danh mục lạ":          func(m map[string]any) { at(m, "products", 0)["categoryIds"] = []any{uuid.NewString()} },
		"hai danh mục": func(m map[string]any) {
			p := at(m, "products", 0)
			p["categoryIds"] = append(p["categoryIds"].([]any), p["categoryIds"].([]any)[0])
		},
		"danh mục có cha": func(m map[string]any) { at(m, "categories", 0)["parentId"] = at(m, "categories", 1)["id"] },
		"giá 0":           func(m map[string]any) { at(m, "products", 0, "variants", 0)["price"] = 0 },
		"giá lẻ":          func(m map[string]any) { at(m, "products", 0, "variants", 0)["price"] = 1.5 },
		"tồn âm":          func(m map[string]any) { at(m, "products", 0, "variants", 0)["stock"] = -1 },
		"sku trùng": func(m map[string]any) {
			at(m, "products", 0, "variants", 1)["sku"] = at(m, "products", 0, "variants", 0)["sku"]
		},
		"mã mẫu trùng":          func(m map[string]any) { at(m, "products", 1)["code"] = at(m, "products", 0)["code"] },
		"trạng thái lạ":         func(m map[string]any) { at(m, "products", 0)["status"] = "draft" },
		"trường lạ":             func(m map[string]any) { m["flashSales"] = []any{} },
		"mã giảm kiểu lạ":       func(m map[string]any) { at(m, "coupons", 0)["kind"] = "free" },
		"mã giảm % > 100":       func(m map[string]any) { at(m, "coupons", 0)["value"] = 101 },
		"mã giảm kênh lạ":       func(m map[string]any) { at(m, "coupons", 0)["channel"] = "pos" },
		"mã giảm theo sản phẩm": func(m map[string]any) { at(m, "coupons", 0)["productIds"] = []any{at(m, "products", 0)["id"]} },
		"mã giảm hạn ngược": func(m map[string]any) {
			c := at(m, "coupons", 0)
			c["startsAt"], c["endsAt"] = c["endsAt"], c["startsAt"]
		},
		"mã giảm trùng":    func(m map[string]any) { m["coupons"] = append(m["coupons"].([]any), m["coupons"].([]any)[0]) },
		"tỉnh trùng mã":    func(m map[string]any) { at(m, "locations", 1)["code"] = at(m, "locations", 0)["code"] },
		"tỉnh tên rỗng":    func(m map[string]any) { at(m, "locations", 0)["name"] = " " },
		"xã trùng mã tỉnh": func(m map[string]any) { at(m, "locations", 0, "wards", 0)["code"] = at(m, "locations", 0)["code"] },
		"số đo 0":          func(m map[string]any) { at(m, "products", 0)["measureCm"] = map[string]any{"length": 0} },
		"số đo 1001":       func(m map[string]any) { at(m, "products", 0)["measureCm"] = map[string]any{"width": 1001} },
		"số đo 2 số lẻ":    func(m map[string]any) { at(m, "products", 0)["measureCm"] = map[string]any{"height": 2.55} },
		"số đo 1000,1":     func(m map[string]any) { at(m, "products", 0)["measureCm"] = map[string]any{"height": 1000.1} },
		"số đo âm":         func(m map[string]any) { at(m, "products", 0)["measureCm"] = map[string]any{"length": -3} },
		"số đo trường lạ":  func(m map[string]any) { at(m, "products", 0)["measureCm"] = map[string]any{"depth": 3} },
		"hai giá trị cùng vị trí": func(m map[string]any) {
			a := at(m, "products", 0)["attributes"].([]any)
			at(m, "products", 0)["attributes"] = append(a, map[string]any{"name": "Xuất xứ", "value": "Hàn", "position": 1})
		},
	}
	for name, fn := range cases {
		if _, err := parseWebFixture(mutate(t, fn)); !errors.Is(err, errBadFixture) {
			t.Errorf("%s: mong errBadFixture, có %v", name, err)
		}
	}
	if _, err := parseWebFixture([]byte("{")); !errors.Is(err, errBadFixture) {
		t.Errorf("JSON hỏng: %v", err)
	}
}

// Lệnh: production → từ chối trước khi đọc tệp, mở database; sai cách gọi; vai trò không phải migrate; tệp hỏng.
func TestRunSeedWebFixture_Refusals(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.DiscardHandler)
	var out bytes.Buffer
	dev := config.Config{Env: "dev", Role: config.RoleMigrate, Migrate: &config.MigrateConfig{OwnerURL: "postgres://khong-co:1/x"}}
	args := []string{"--file", fixturePath, "--staff-email", "chu@elimaz.test"}
	if err := runSeedWebFixture(t.Context(), config.Config{Env: "production", Role: config.RoleMigrate}, args, &out, log); !errors.Is(err, errSeedFixtureProduction) {
		t.Fatalf("production: %v", err)
	}
	usage := [][]string{{}, {"--file", fixturePath}, {"--staff-email", "a@b.c"}, append(slices.Clone(args), "thừa"), {"--la", "x"}}
	// Cờ hãng trên web sai (Task 511): phí ngoài 0–trần, phí không số, phí không kèm tên, tên rỗng.
	for _, extra := range [][]string{
		{"--web-carrier", "Hãng giao hàng A", "--web-carrier-fee", "-1"},
		{"--web-carrier", "Hãng giao hàng A", "--web-carrier-fee", "500001"},
		{"--web-carrier", "Hãng giao hàng A", "--web-carrier-fee", "x"},
		{"--web-carrier-fee", "30000"},
		{"--web-carrier", "  "},
	} {
		usage = append(usage, append(slices.Clone(args), extra...))
	}
	for _, a := range usage {
		if err := runSeedWebFixture(t.Context(), dev, a, &out, log); !errors.Is(err, errSeedFixtureUsage) {
			t.Errorf("%v: mong errSeedFixtureUsage, có %v", a, err)
		}
	}
	if err := runSeedWebFixture(t.Context(), config.Config{Env: "dev", Role: config.RoleAPI}, args, &out, log); !errors.Is(err, errSeedRole) {
		t.Errorf("vai trò api: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "v2.json")
	if err := os.WriteFile(bad, mutate(t, func(m map[string]any) { m["version"] = 2 }), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSeedWebFixture(t.Context(), dev, []string{"--file", bad, "--staff-email", "chu@elimaz.test"}, &out, log); !errors.Is(err, errBadFixture) {
		t.Errorf("version 2: %v", err)
	}
	if code := seedFixtureExit(errSeedFixtureProduction, &out); code != exitConfig {
		t.Errorf("production phải thoát exitConfig, có %d", code)
	}
	if code := seedFixtureExit(errBadFixture, &out); code != exitConfig {
		t.Errorf("tệp hỏng phải thoát exitConfig, có %d", code)
	}
	if strings.Contains(out.String(), "chu@elimaz.test") {
		t.Errorf("không được in email nhân viên: %q", out.String())
	}
}

// Sổ sự kiện có đủ consumer như tiến trình chạy thật dựng được ở vai trò migrate (lệnh seed dùng newModules).
func TestSeedWebFixture_ModulesForMigrate(t *testing.T) {
	t.Parallel()
	if _, err := newModules(config.Config{Env: "dev", Role: config.RoleMigrate}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

type seedEnv struct {
	t      *testing.T
	s      *testkit.Staff
	actor  identity.AuthUser
	seeder webFixtureSeeder
}

func newSeedEnv(t *testing.T) *seedEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "ELIMAZ")
	uid := s.User(t, tenant, "chu@elimaz.test", "active")
	return &seedEnv{
		t: t, s: s, actor: identity.AuthUser{UserID: uid, TenantID: tenant, Email: "chu@elimaz.test"},
		seeder: webFixtureSeeder{Events: events.NewRegistry(), Queue: queue.NewQueue()},
	}
}

func (e *seedEnv) seed(raw []byte) seedReport {
	e.t.Helper()
	fx, err := parseWebFixture(raw)
	if err != nil {
		e.t.Fatal(err)
	}
	rep, err := e.seeder.seed(e.t.Context(), e.s.Owner, e.actor, fx)
	if err != nil {
		e.t.Fatalf("seed: %v", err)
	}
	return rep
}

// seedSample seed tệp mẫu và trả lỗi (ca lỗi: lệnh phải lùi cả transaction).
func (e *seedEnv) seedSample() error {
	e.t.Helper()
	fx, err := parseWebFixture(readFixture(e.t))
	if err != nil {
		e.t.Fatal(err)
	}
	_, err = e.seeder.seed(e.t.Context(), e.s.Owner, e.actor, fx)
	return err
}

func (e *seedEnv) app(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.s.App, e.actor.TenantID, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *seedEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.s.CountIn(e.t, e.actor.TenantID, q, args...)
}

// availability: số có thể bán ở kho mặc định của các biến thể.
func (e *seedEnv) availability(ids []uuid.UUID) map[uuid.UUID]int32 {
	e.t.Helper()
	out := map[uuid.UUID]int32{}
	e.app(func(tx pgx.Tx) error {
		wh, err := inventory.DefaultWarehouse(e.t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		bs, err := inventory.Availability(e.t.Context(), tx, wh, ids)
		for _, b := range bs {
			out[b.VariantID] = b.Available
		}
		return err //nolint:wrapcheck // test
	})
	return out
}

// variantIDs: mọi biến thể của tệp và số biến thể có tồn > 0.
func variantIDs(fx webFixture) (ids []uuid.UUID, stocked int) {
	for _, p := range fx.Products {
		for _, v := range p.Variants {
			ids = append(ids, v.ID)
			if v.Stock > 0 {
				stocked++
			}
		}
	}
	return ids, stocked
}

// checkFirstReport: lần seed đầu tạo đủ từng loại, chỉ "sửa" tồn của biến thể có hàng.
func checkFirstReport(t *testing.T, fx webFixture, rep seedReport) {
	t.Helper()
	_, stocked := variantIDs(fx)
	want := map[string]int{
		"category": 6, "product": 9, "variant": 39, "coupon": 1, "admin_unit": 4, "attribute": 7, "attribute_value": 11,
	}
	if !maps.Equal(rep.Created, want) {
		t.Errorf("tạo: mong %v, có %v", want, rep.Created)
	}
	if !maps.Equal(rep.Updated, map[string]int{"stock": stocked}) {
		t.Errorf("lần đầu chỉ đặt tồn %d biến thể có hàng: %+v", stocked, rep.Updated)
	}
}

// checkSeededProduct: mẫu đầu tiên của tệp đọc lại bằng crm_app đúng mã, tên, biến thể, phân loại.
func checkSeededProduct(t *testing.T, tx pgx.Tx, fp fixtureProduct) {
	t.Helper()
	seller, err := catalog.DefaultSeller(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := catalog.GetProduct(t.Context(), tx, seller, fp.ID)
	if err != nil || p.Code != "ECV26.002" || p.Name != fp.Name || p.Status != catalog.StatusActive || len(p.Variants) != 3 {
		t.Fatalf("mẫu sai: %+v %v", p, err)
	}
	for _, v := range p.Variants {
		i := slices.IndexFunc(fp.Variants, func(f fixtureVariant) bool { return f.ID == v.ID })
		if i < 0 || v.SKU != strings.ToUpper(fp.Variants[i].SKU) || v.Price != fp.Variants[i].Price ||
			v.Color != fp.Variants[i].Color || v.Size != fp.Variants[i].Size || v.Status != catalog.VariantActive {
			t.Errorf("biến thể sai: %+v", v)
		}
	}
	c, err := catalog.GetClassification(t.Context(), tx, seller, fp.ID)
	if err != nil || c.CategoryID == nil || *c.CategoryID != fp.CategoryIDs[0] || len(c.AttributeValueIDs) != 1 {
		t.Errorf("phân loại sai: %+v %v", c, err)
	}
}

// checkSeededRest: danh mục, mã giảm, địa giới đọc lại bằng crm_app.
func checkSeededRest(t *testing.T, tx pgx.Tx, fx webFixture) {
	t.Helper()
	cats, err := catalog.ListCategories(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fc := range fx.Categories {
		i := slices.IndexFunc(cats, func(c catalog.Category) bool { return c.ID == fc.ID })
		if i < 0 || cats[i].Slug != fc.Slug || cats[i].Name != fc.Name || cats[i].Position != fc.Position {
			t.Errorf("danh mục %s sai", fc.Key)
		}
	}
	cs, _, err := promotion.ListCoupons(t.Context(), tx, promotion.CouponFilter{Q: "ELIMAZTHU10"})
	if err != nil || len(cs) != 1 {
		t.Fatalf("mã: %v %v", cs, err)
	}
	k := cs[0]
	if k.DiscountKind != promotion.KindPercent || k.Value != 1000 || k.MaxDiscount == nil || *k.MaxDiscount != 50000 ||
		k.MinSubtotal != 300000 || k.MaxUses == nil || *k.MaxUses != 100 || !slices.Equal(k.Channels, []string{"web"}) ||
		k.ValidFrom.Year() != 2026 || k.ValidTo.Year() != 2027 || k.Status != promotion.StatusActive {
		t.Errorf("mã sai: %+v", k)
	}
	pair, err := customer.ValidateUnits(t.Context(), tx, "101", "10101")
	if err != nil || pair.Ward.Name != "Phường thử nghiệm A" {
		t.Errorf("địa giới: %+v %v", pair, err)
	}
}

// Seed tệp mẫu: tạo đúng id, đúng dữ liệu; chạy lại cùng tệp → 0 thay đổi (không sự kiện, không audit mới); tệp đổi
// giá và tồn (WB02) → chỉ đổi đúng phần đó, có price.changed, tồn đặt tuyệt đối.
func TestSeedWebFixture_CreateRerunUpdate(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	fx, err := parseWebFixture(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	checkFirstReport(t, fx, e.seed(readFixture(t)))
	e.app(func(tx pgx.Tx) error {
		checkSeededProduct(t, tx, fx.Products[0])
		checkSeededRest(t, tx, fx)
		return nil
	})
	ids, _ := variantIDs(fx)
	avail := e.availability(ids)
	for _, p := range fx.Products {
		for _, v := range p.Variants {
			if avail[v.ID] != v.Stock {
				t.Errorf("tồn %s: mong %d, có %d", v.Key, v.Stock, avail[v.ID])
			}
		}
	}

	// Chạy lại cùng tệp: 0 thay đổi, không thêm sự kiện, audit, phiếu kho.
	events0 := e.count("SELECT count(*) FROM platform.domain_events")
	audits0 := e.count("SELECT count(*) FROM identity.audit_logs")
	if again := e.seed(readFixture(t)); again.Total() != 0 {
		t.Fatalf("chạy lại cùng tệp phải 0 thay đổi: %+v", again)
	}
	if e.count("SELECT count(*) FROM platform.domain_events") != events0 || e.count("SELECT count(*) FROM identity.audit_logs") != audits0 {
		t.Fatal("chạy lại cùng tệp không được ghi sự kiện hay audit")
	}

	// WB02: đổi giá một biến thể, tồn một biến thể khác.
	v0, v1 := fx.Products[0].Variants[0], fx.Products[0].Variants[2]
	changed := mutate(t, func(m map[string]any) {
		at(m, "products", 0, "variants", 0)["price"] = 459000
		at(m, "products", 0, "variants", 2)["stock"] = 1
	})
	upd := e.seed(changed)
	if upd.Updated["variant"] != 1 || upd.Updated["stock"] != 1 || upd.Total() != 2 {
		t.Fatalf("tệp đổi giá, tồn: mong 1 biến thể + 1 tồn, có %+v", upd)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'price.changed' AND aggregate_id = $1", v0.ID.String()); n != 1 {
		t.Errorf("mong 1 price.changed của biến thể đổi giá, có %d", n)
	}
	if a := e.availability([]uuid.UUID{v1.ID}); a[v1.ID] != 1 {
		t.Errorf("tồn phải đặt tuyệt đối về 1, có %d", a[v1.ID])
	}
	if again := e.seed(changed); again.Total() != 0 {
		t.Fatalf("chạy lại tệp đã đổi phải 0 thay đổi: %+v", again)
	}
}

// Task 516, 517: số đo của tệp (measureCm cm của website) vào mẫu CRM (mm): test-pants Dài 88 Rộng 20, test-nightwear
// 30/25/3, mẫu không có measureCm thì không có số đo. Tệp đổi số đo (72,5 cm) → sửa đúng mẫu đó (1 mẫu sửa); bỏ measureCm → bỏ số đo; chạy
// lại cùng tệp → 0 thay đổi.
func TestSeedWebFixture_Measure(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	fx, err := parseWebFixture(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	e.seed(readFixture(t))
	pants := slices.IndexFunc(fx.Products, func(p fixtureProduct) bool { return p.Key == "test-pants" })
	if pants < 0 {
		t.Fatal("tệp mẫu thiếu test-pants")
	}
	check := func(step string, want map[string][3]int) {
		t.Helper()
		e.app(func(tx pgx.Tx) error {
			seller, err := catalog.DefaultSeller(t.Context(), tx)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			for _, fp := range fx.Products {
				p, err := catalog.GetProduct(t.Context(), tx, seller, fp.ID)
				if err != nil {
					return err //nolint:wrapcheck // test
				}
				got := [3]int{deref(p.Measure.Length), deref(p.Measure.Width), deref(p.Measure.Height)}
				if got != want[fp.Key] {
					t.Errorf("%s: %s số đo %v, mong %v", step, fp.Key, got, want[fp.Key])
				}
			}
			return nil
		})
	}
	check("lần đầu", map[string][3]int{"test-pants": {880, 200, 0}, "test-nightwear": {300, 250, 30}})
	longer := mutate(t, func(m map[string]any) {
		at(m, "products", pants)["measureCm"] = map[string]any{"length": 72.5, "height": 0.1}
	})
	if upd := e.seed(longer); upd.Updated["product"] != 1 || upd.Total() != 1 {
		t.Fatalf("đổi số đo: mong 1 mẫu sửa, có %+v", upd)
	}
	check("đổi", map[string][3]int{"test-pants": {725, 0, 1}, "test-nightwear": {300, 250, 30}})
	if again := e.seed(longer); again.Total() != 0 {
		t.Fatalf("chạy lại: %+v", again)
	}
	none := mutate(t, func(m map[string]any) { delete(at(m, "products", pants), "measureCm") })
	if upd := e.seed(none); upd.Updated["product"] != 1 {
		t.Fatalf("bỏ số đo: %+v", upd)
	}
	check("bỏ", map[string][3]int{"test-nightwear": {300, 250, 30}})
}

func deref(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// Địa giới thử trùng mã với đơn vị đã có khác tên → errAdminUnitConflict, không ghi gì.
func TestSeedWebFixture_AdminUnitConflict(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	if _, err := e.s.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source)
		VALUES ('101', 'province', NULL, 'Tỉnh thật', 'tinh that', DATE '2025-07-01', 'test')`); err != nil {
		t.Fatal(err)
	}
	if err := e.seedSample(); !errors.Is(err, errAdminUnitConflict) {
		t.Fatalf("mong errAdminUnitConflict, có %v", err)
	}
	if n := e.count("SELECT count(*) FROM catalog.products"); n != 0 {
		t.Fatalf("lần lỗi không được ghi mẫu nào, có %d", n)
	}
}

// Lỗi ở bước gần cuối (mã giảm, sau địa giới, danh mục, thuộc tính, mẫu đã ghi) → cả transaction lùi: không còn địa
// giới, danh mục, thuộc tính, mẫu, biến thể, sự kiện nào của lần seed.
func TestSeedWebFixture_FailureLateRollsBackAll(t *testing.T) {
	t.Parallel()
	e := newSeedEnv(t)
	// Mã ELIMAZTHU10 có sẵn với trần 5 lượt; tệp bỏ trần (usageLimit null) → không gỡ được trần → errFixtureDrift.
	e.app(func(tx pgx.Tx) error {
		_, err := promotion.Writer{}.CreateCoupon(t.Context(), tx, promotion.Actor{TenantID: e.actor.TenantID, UserID: e.actor.UserID},
			promotion.CouponInput{
				Code: "ELIMAZTHU10", Name: "ELIMAZTHU10", DiscountKind: promotion.KindPercent, Value: 1000,
				Channels: []string{"web"}, MaxUses: new(int32(5)),
				ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			})
		return err //nolint:wrapcheck // test
	})
	events0 := e.count("SELECT count(*) FROM platform.domain_events")
	fx, err := parseWebFixture(mutate(t, func(m map[string]any) { at(m, "coupons", 0)["usageLimit"] = nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.seeder.seed(t.Context(), e.s.Owner, e.actor, fx); !errors.Is(err, errFixtureDrift) {
		t.Fatalf("mong errFixtureDrift ở bước mã giảm, có %v", err)
	}
	for q, want := range map[string]int{
		"SELECT count(*) FROM catalog.categories": 0, "SELECT count(*) FROM catalog.attributes": 0,
		"SELECT count(*) FROM catalog.products": 0, "SELECT count(*) FROM catalog.variants": 0,
		"SELECT count(*) FROM platform.domain_events": events0,
	} {
		if n := e.count(q); n != want {
			t.Errorf("%s: mong %d sau khi lùi, có %d", q, want, n)
		}
	}
	var units int
	if err := e.s.Owner.Pool.QueryRow(t.Context(), "SELECT count(*) FROM customer.admin_units WHERE code IN ('101', '10101')").Scan(&units); err != nil || units != 0 {
		t.Fatalf("địa giới thử phải lùi theo: %d %v", units, err)
	}
}

// Đường lệnh thật (CRM_ROLE=migrate trên database thử): nạp nhân viên theo email, sổ sự kiện của newModules (consumer
// webcache nhận việc → bộ đệm web được làm mới), in số đếm không có email; chạy lại in 0 thay đổi.
func TestRunSeedWebFixture_Command(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	cfg := config.Config{
		Env: "dev", Role: config.RoleMigrate, DatabaseURL: appURL, DBMaxConns: 4,
		Migrate: &config.MigrateConfig{OwnerURL: ownerURL},
	}
	owner, err := openOwner(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Pool.Close)
	s := &testkit.Staff{Owner: owner}
	tenant := s.Tenant(t, "ELIMAZ")
	s.User(t, tenant, "chu@elimaz.test", "active")
	args := []string{"--file", fixturePath, "--staff-email", "chu@elimaz.test"}
	log := slog.New(slog.DiscardHandler)
	var out bytes.Buffer
	if err := runSeedWebFixture(t.Context(), cfg, args, &out, log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "product 9") || !strings.Contains(out.String(), "variant 39") ||
		strings.Contains(out.String(), "chu@elimaz.test") {
		t.Fatalf("kết quả in ra: %q", out.String())
	}
	if n := s.CountIn(t, tenant, "SELECT count(*) FROM platform.jobs WHERE kind = 'event.webcache'"); n == 0 {
		t.Fatal("sự kiện của seed phải tới consumer webcache (làm mới bộ đệm web)")
	}
	out.Reset()
	if err := runSeedWebFixture(t.Context(), cfg, args, &out, log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tạo 0; sửa 0;") {
		t.Fatalf("chạy lại phải 0 thay đổi: %q", out.String())
	}
	out.Reset()
	if err := runSeedWebFixture(t.Context(), cfg, append(slices.Clone(args), "--web-carrier", "Hãng giao hàng A"), &out, log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sửa carrier_account 1, carrier_location 4, web_carrier_settings 1;") {
		t.Fatalf("--web-carrier phải bật hãng giả: %q", out.String())
	}
	if n := s.CountIn(t, tenant, "SELECT count(*) FROM fulfillment.channel_carrier_settings WHERE fallback_fee = 30000 AND preferred_account_id IS NOT NULL"); n != 1 {
		t.Fatalf("cài đặt hãng trên web sau lệnh: %d dòng", n)
	}
}

// Lệnh qua đúng đường của main (run: config.Load từ biến môi trường, chọn lệnh, mã thoát) trên database thử: chạy lần
// đầu, chạy lại 0 thay đổi, production thoát 2 kể cả khi tệp không có (chưa đọc tệp), lệnh thiếu cờ thoát 2. Không đâu
// in email nhân viên.
func TestRun_SeedWebFixtureCommandLine(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	owner, err := openOwner(t.Context(), config.Config{Migrate: &config.MigrateConfig{OwnerURL: ownerURL}, DBMaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Pool.Close)
	s := &testkit.Staff{Owner: owner}
	s.User(t, s.Tenant(t, "ELIMAZ"), "chu@elimaz.test", "active")
	env := map[string]string{"CRM_ROLE": "migrate", "CRM_ENV": "dev", "DATABASE_URL": appURL, "DATABASE_OWNER_URL": ownerURL}
	cmd := func(env map[string]string, args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(t.Context(), args, func(k string) string { return env[k] }, &out, &errOut)
		if strings.Contains(out.String()+errOut.String(), "chu@elimaz.test") {
			t.Errorf("không được in email nhân viên: %q %q", out.String(), errOut.String())
		}
		return code, out.String(), errOut.String()
	}
	args := []string{seedFixtureCommand, "--file", fixturePath, "--staff-email", "chu@elimaz.test"}
	code, out, errOut := cmd(env, args...)
	if code != exitOK || !strings.Contains(out, "seed-web-fixture: tenant ") || !strings.Contains(out, "product 9") {
		t.Fatalf("lần đầu: thoát %d\n%s\n%s", code, out, errOut)
	}
	t.Logf("lần đầu: %s", out)
	if code, out, errOut = cmd(env, args...); code != exitOK || !strings.Contains(out, "tạo 0; sửa 0;") {
		t.Fatalf("chạy lại: thoát %d\n%s\n%s", code, out, errOut)
	}
	prod := maps.Clone(env)
	prod["CRM_ENV"] = "production"
	if code, _, errOut = cmd(prod, seedFixtureCommand, "--file", "/khong/co/tep.json", "--staff-email", "chu@elimaz.test"); code != exitConfig ||
		!strings.Contains(errOut, errSeedFixtureProduction.Error()) {
		t.Fatalf("production: thoát %d, %q", code, errOut)
	}
	if code, _, errOut = cmd(env, seedFixtureCommand); code != exitConfig || !strings.Contains(errOut, "cách gọi") {
		t.Fatalf("thiếu cờ: thoát %d, %q", code, errOut)
	}
}
