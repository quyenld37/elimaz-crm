package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// seedFixtureCommand là lệnh `crm seed-web-fixture --file <json> --staff-email <email>` (kế hoạch M5 Task 218, M5-35):
// dựng danh mục mẫu của bộ e2e website (người bán mặc định, danh mục, mẫu, biến thể, thuộc tính, mã giảm, tồn, địa giới
// thử) từ tệp do website xuất (`scripts/export-fixture-for-crm.ts` của apps/storefront-crm, định dạng version 1), đúng
// id trong tệp. Chỉ dev, test, staging; CRM_ROLE=migrate (chủ bảng: địa giới chỉ chủ bảng ghi được), như seed-dev.
const seedFixtureCommand = "seed-web-fixture"

// fixtureNamespaceText là không gian tên UUID v5 của tệp fixture (FIXTURE_NAMESPACE ở scripts/fixture-uuid.ts của
// website; cố định mãi: đổi là đổi mọi id CRM).
const fixtureNamespaceText = "6f1d3c2a-8b47-4e59-9a0c-2d5e7f81b3c4"

func fixtureNamespace() uuid.UUID { return uuid.MustParse(fixtureNamespaceText) }

const fixtureVersion = 1

// fixtureCreatedAt là ngày tạo của mẫu ở vị trí position của tệp: mốc cố định 2026-01-01 00:00 UTC + position giây
// (đặc tả website: sắp xếp «Mới nhất» ổn định theo tệp; position lớn hơn là mới hơn).
func fixtureCreatedAt(position int) time.Time {
	return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(position) * time.Second)
}

// defaultWebCarrierFee là phí mặc định của --web-carrier-fee: phí ship của bộ e2e website (30.000 đ nội thành thử).
const defaultWebCarrierFee = 30000

var (
	errSeedFixtureProduction = errors.New("lệnh seed-web-fixture chỉ chạy trên dev, test, staging")
	errSeedFixtureUsage      = errors.New("cách gọi: `CRM_ROLE=migrate crm seed-web-fixture --file <tệp json> " +
		"--staff-email <email nhân viên ghi audit> [--web-carrier <tên hãng trên web> [--web-carrier-fee <đồng>]]`")
	// errBadFixture: tệp không đúng định dạng version 1 (kiểm hết trước khi mở database, không ghi gì).
	errBadFixture = errors.New("tệp fixture sai")
)

// webFixture là tệp fixture version 1 (đặc tả: docs/tasks/task-218.md của apps/storefront-crm, mục «Định dạng tệp
// fixture cho CRM»).
type webFixture struct {
	Version    int               `json:"version"`
	Namespace  uuid.UUID         `json:"namespace"`
	Seller     fixtureSeller     `json:"seller"`
	Categories []fixtureCategory `json:"categories"`
	Products   []fixtureProduct  `json:"products"`
	Coupons    []fixtureCoupon   `json:"coupons"`
	Locations  []fixtureLocation `json:"locations"`
}

type fixtureSeller struct {
	ID   uuid.UUID `json:"id"`
	Key  string    `json:"key"`
	Name string    `json:"name"`
}

type fixtureCategory struct {
	ID          uuid.UUID  `json:"id"`
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	ParentID    *uuid.UUID `json:"parentId"`
	Position    int32      `json:"position"`
	HasProducts bool       `json:"hasProducts"`
}

type fixtureProduct struct {
	ID             uuid.UUID          `json:"id"`
	Key            string             `json:"key"`
	SellerID       uuid.UUID          `json:"sellerId"`
	Code           string             `json:"code"`
	Name           string             `json:"name"`
	Status         string             `json:"status"`
	Position       int                `json:"position"`
	CategoryIDs    []uuid.UUID        `json:"categoryIds"`
	FixtureSlug    string             `json:"fixtureSlug"`
	StorefrontSlug string             `json:"storefrontSlug"`
	Attributes     []fixtureAttribute `json:"attributes"`
	MeasureCm      *fixtureMeasure    `json:"measureCm,omitempty"`
	Images         []fixtureImage     `json:"images"`
	Variants       []fixtureVariant   `json:"variants"`
}

type fixtureAttribute struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Position int    `json:"position"`
}

type fixtureMeasure struct {
	Length *float64 `json:"length,omitempty"`
	Width  *float64 `json:"width,omitempty"`
	Height *float64 `json:"height,omitempty"`
}

// mm đổi số đo cm của tệp (số thực của JSON website) sang catalog.MeasureMm; ok false khi một số có hơn một chữ số
// thập phân hay ngoài 0,1–1000 cm (catalog.MeasureMmFromCm). m nil (tệp không có measureCm) → không số đo.
func (m *fixtureMeasure) mm() (catalog.MeasureMm, bool) {
	if m == nil {
		return catalog.MeasureMm{}, true
	}
	ok := true
	conv := func(v *float64) *int {
		if v == nil {
			return nil
		}
		n, good := catalog.MeasureMmFromCm(*v)
		if !good {
			ok = false
			return nil
		}
		return &n
	}
	out := catalog.MeasureMm{Length: conv(m.Length), Width: conv(m.Width), Height: conv(m.Height)}
	return out, ok
}

// fixtureImage: chưa seed (xem «Việc phát hiện thêm» của tệp task 218); đọc để tệp hợp lệ.
type fixtureImage struct {
	Position int    `json:"position"`
	Color    string `json:"color"`
	File     string `json:"file"`
	SHA256   string `json:"sha256"`
}

type fixtureVariant struct {
	ID      uuid.UUID `json:"id"`
	Key     string    `json:"key"`
	SKU     string    `json:"sku"`
	Barcode string    `json:"barcode"`
	Color   string    `json:"color"`
	Size    string    `json:"size"`
	Price   int64     `json:"price"`
	Stock   int32     `json:"stock"`
}

type fixtureCoupon struct {
	Code               string      `json:"code"`
	Kind               string      `json:"kind"`
	Value              int64       `json:"value"`
	MaxDiscount        *int64      `json:"maxDiscount"`
	MinSubtotal        *int64      `json:"minSubtotal"`
	StartsAt           time.Time   `json:"startsAt"`
	EndsAt             time.Time   `json:"endsAt"`
	UsageLimit         *int32      `json:"usageLimit"`
	CombinableWithSale bool        `json:"combinableWithSale"`
	Channel            string      `json:"channel"`
	ProductIDs         []uuid.UUID `json:"productIds"`
}

type fixtureLocation struct {
	Code  string        `json:"code"`
	Name  string        `json:"name"`
	Wards []fixtureWard `json:"wards"`
}

type fixtureWard struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// fixtureUUID là id CRM của bản ghi fixture: UUID v5 của "<kind>:<key>" dưới fixtureNamespace (fixtureUuid của website).
func fixtureUUID(kind, key string) uuid.UUID {
	return uuid.NewSHA1(fixtureNamespace(), []byte(kind+":"+key))
}

// parseWebFixture đọc và kiểm tệp (JSON chặt: trường lạ là lỗi). Mọi lỗi bọc errBadFixture.
func parseWebFixture(raw []byte) (webFixture, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var fx webFixture
	if err := dec.Decode(&fx); err != nil {
		return webFixture{}, fmt.Errorf("%w: đọc JSON: %w", errBadFixture, err)
	}
	if dec.More() {
		return webFixture{}, fmt.Errorf("%w: thừa dữ liệu sau đối tượng gốc", errBadFixture)
	}
	if err := fx.validate(); err != nil {
		return webFixture{}, fmt.Errorf("%w: %w", errBadFixture, err)
	}
	return fx, nil
}

// fixtureProblem là một chỗ sai của tệp (bọc errBadFixture ở parseWebFixture).
type fixtureProblem string

func (p fixtureProblem) Error() string { return string(p) }

func problemf(format string, a ...any) error { return fixtureProblem(fmt.Sprintf(format, a...)) }

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func (fx webFixture) validate() error {
	switch {
	case fx.Version != fixtureVersion:
		return problemf("version %d (chỉ nhận %d)", fx.Version, fixtureVersion)
	case fx.Namespace != fixtureNamespace():
		return problemf("namespace %s khác %s", fx.Namespace, fixtureNamespaceText)
	case fx.Seller.ID != fixtureUUID("seller", fx.Seller.Key) || blank(fx.Seller.Name):
		return problemf("người bán %q: id không phải UUID v5 của khóa, hay tên rỗng", fx.Seller.Key)
	}
	cats := map[uuid.UUID]bool{}
	for _, c := range fx.Categories {
		switch {
		case c.ID != fixtureUUID("category", c.Key) || cats[c.ID]:
			return problemf("danh mục %q: id sai hay trùng", c.Key)
		case blank(c.Name) || blank(c.Slug) || c.Position < 0:
			return problemf("danh mục %q: tên, slug rỗng hay vị trí âm", c.Key)
		case c.ParentID != nil:
			return problemf("danh mục %q: bản 1 chỉ có danh mục gốc", c.Key)
		}
		cats[c.ID] = true
	}
	ids, codes, skus := map[uuid.UUID]bool{}, map[string]bool{}, map[string]bool{}
	for _, p := range fx.Products {
		if err := p.validate(fx.Seller.ID, cats, ids, codes, skus); err != nil {
			return err
		}
	}
	if err := validateCoupons(fx.Coupons); err != nil {
		return err
	}
	return validateLocations(fx.Locations)
}

func (p fixtureProduct) validate(seller uuid.UUID, cats, ids map[uuid.UUID]bool, codes, skus map[string]bool) error {
	code := strings.ToUpper(strings.TrimSpace(p.Code))
	switch {
	case p.ID != fixtureUUID("product", p.Key) || ids[p.ID]:
		return problemf("mẫu %q: id sai hay trùng", p.Key)
	case p.SellerID != seller:
		return problemf("mẫu %q: sellerId không phải người bán của tệp", p.Key)
	case code == "" || codes[code]:
		return problemf("mẫu %q: mã rỗng hay trùng", p.Key)
	case blank(p.Name) || p.Status != string(catalog.StatusActive):
		return problemf("mẫu %q: tên rỗng hay trạng thái khác active", p.Key)
	case len(p.CategoryIDs) != 1 || !cats[p.CategoryIDs[0]]:
		return problemf("mẫu %q: cần đúng một danh mục có trong tệp", p.Key)
	}
	if _, ok := p.MeasureCm.mm(); !ok {
		return problemf("mẫu %q: số đo phải là cm 0,1–1000, tối đa một chữ số thập phân", p.Key)
	}
	ids[p.ID], codes[code] = true, true
	positions := map[int]bool{}
	for _, a := range p.Attributes {
		if blank(a.Name) || blank(a.Value) || a.Position < 1 || positions[a.Position] {
			return problemf("mẫu %q: thuộc tính rỗng hay vị trí trùng", p.Key)
		}
		positions[a.Position] = true
	}
	for _, v := range p.Variants {
		sku := strings.ToUpper(strings.TrimSpace(v.SKU))
		switch {
		case v.ID != fixtureUUID("variant", v.Key) || ids[v.ID]:
			return problemf("biến thể %q: id sai hay trùng", v.Key)
		case sku == "" || skus[sku]:
			return problemf("biến thể %q: sku rỗng hay trùng", v.Key)
		case v.Price <= 0 || v.Price > catalog.MaxMoney:
			return problemf("biến thể %q: giá phải > 0", v.Key)
		case v.Stock < 0:
			return problemf("biến thể %q: tồn âm", v.Key)
		}
		ids[v.ID], skus[sku] = true, true
	}
	return nil
}

func validateCoupons(cs []fixtureCoupon) error {
	seen := map[string]bool{}
	for _, c := range cs {
		code := strings.ToUpper(strings.TrimSpace(c.Code))
		switch {
		case code == "" || seen[code]:
			return problemf("mã giảm %q rỗng hay trùng", c.Code)
		case c.Kind != "percent" && c.Kind != "amount":
			return problemf("mã giảm %s: kind %q", code, c.Kind)
		case c.Kind == "percent" && (c.Value < 1 || c.Value > 100), c.Kind == "amount" && (c.Value < 1 || c.Value > catalog.MaxMoney):
			return problemf("mã giảm %s: giá trị %d ngoài khoảng", code, c.Value)
		case c.Channel != "web":
			return problemf("mã giảm %s: kênh %q (chỉ web)", code, c.Channel)
		case len(c.ProductIDs) != 0:
			return problemf("mã giảm %s: CRM chưa có mã theo sản phẩm (productIds phải rỗng)", code)
		case !c.StartsAt.Before(c.EndsAt):
			return problemf("mã giảm %s: hạn đầu phải trước hạn cuối", code)
		case c.MaxDiscount != nil && *c.MaxDiscount <= 0, c.MinSubtotal != nil && *c.MinSubtotal < 0,
			c.UsageLimit != nil && *c.UsageLimit < 1:
			return problemf("mã giảm %s: trần giảm, đơn tối thiểu hay số lượt sai", code)
		}
		seen[code] = true
	}
	return nil
}

func validateLocations(ls []fixtureLocation) error {
	seen := map[string]bool{}
	check := func(code, name string) error {
		if blank(code) || blank(name) || seen[code] {
			return problemf("đơn vị hành chính %q: mã, tên rỗng hay mã trùng", code)
		}
		seen[code] = true
		return nil
	}
	for _, l := range ls {
		if err := check(l.Code, l.Name); err != nil {
			return err
		}
		for _, w := range l.Wards {
			if err := check(w.Code, w.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// runDevCommand chạy hai lệnh chỉ dành cho dev, test, staging (service-key, seed-web-fixture) và trả mã thoát.
func runDevCommand(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer, logger *slog.Logger) int {
	if isSeedFixtureCommand(args) {
		return seedFixtureExit(runSeedWebFixture(ctx, cfg, args[1:], stdout, logger), stderr)
	}
	return serviceKeyExit(runServiceKey(ctx, cfg, args[1:], stdout, logger), stderr)
}

// isSeedFixtureCommand: args là lệnh `crm seed-web-fixture …`.
func isSeedFixtureCommand(args []string) bool { return len(args) >= 1 && args[0] == seedFixtureCommand }

// seedFixtureExit in lỗi ra stderr và trả mã thoát: production, sai cách gọi, sai vai trò, tệp hỏng → exitConfig.
func seedFixtureExit(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, seedFixtureCommand+":", err)
	if errors.Is(err, errSeedFixtureProduction) || errors.Is(err, errSeedFixtureUsage) || errors.Is(err, errSeedRole) ||
		errors.Is(err, errBadFixture) {
		return exitConfig
	}
	return exitFail
}

// runSeedWebFixture chạy lệnh: chặn production trước mọi việc; đọc cờ; đòi vai trò migrate; đọc, kiểm tệp; rồi mới mở
// database bằng chủ bảng, nạp nhân viên --staff-email (người làm của audit, cho biết tenant), dựng sổ sự kiện đủ
// consumer (newModules, như tiến trình thật: bộ đệm web được làm mới) và ghi một transaction. In số đếm (không email).
func runSeedWebFixture(ctx context.Context, cfg config.Config, args []string, stdout io.Writer, logger *slog.Logger) error {
	if cfg.Env == "production" {
		return errSeedFixtureProduction
	}
	fs := flag.NewFlagSet(seedFixtureCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "tệp JSON do website xuất")
	email := fs.String("staff-email", "", "email nhân viên ghi audit")
	carrierName := fs.String("web-carrier", "", "tên hãng giả hiện trên website (rỗng: không bật)")
	carrierFee := fs.Int64("web-carrier-fee", defaultWebCarrierFee, "phí ship (đồng) của hãng giả, làm phí dự phòng")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *file == "" || *email == "" {
		return errSeedFixtureUsage
	}
	carrier, err := parseWebCarrierFlags(fs, *carrierName, *carrierFee)
	if err != nil {
		return err
	}
	if cfg.Role != config.RoleMigrate || cfg.Migrate == nil {
		return errSeedRole
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("đọc tệp fixture: %w", err)
	}
	fx, err := parseWebFixture(raw)
	if err != nil {
		return err
	}
	owner, err := openOwner(ctx, cfg)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	actor, err := identity.LoadUser(ctx, owner, *email)
	if err != nil {
		return fmt.Errorf("nạp nhân viên ghi audit: %w", err)
	}
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: cfg.DatabaseURL, Logger: logger})
	mods, err := newModules(cfg, owner, hub, metrics.NewRegistry(), nil)
	if err != nil {
		return err
	}
	seeder := webFixtureSeeder{Events: mods.Events, Queue: queue.NewQueue(), Bus: hub, Carrier: carrier}
	if mods.M3.Fulfillment != nil {
		seeder.Fulfillment = mods.M3.Fulfillment.Service
	}
	rep, err := seeder.seed(ctx, owner, actor, fx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "seed-web-fixture: tenant %s; tạo %s; sửa %s; ảnh chưa seed: %d\n",
		actor.TenantID, rep.line(rep.Created), rep.line(rep.Updated), fx.imageCount())
	logger.Info("seed-web-fixture xong", slog.String("tenant_id", actor.TenantID.String()), slog.Int("changes", rep.Total()))
	return nil
}

// parseWebCarrierFlags kiểm cờ hãng: tên không rỗng sau khi bỏ khoảng trắng (khi có cờ), phí 0–trần phí dự phòng của
// fulfillment, --web-carrier-fee chỉ đi kèm --web-carrier.
func parseWebCarrierFlags(fs *flag.FlagSet, name string, fee int64) (webCarrierSeed, error) {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	switch {
	case !set["web-carrier"] && set["web-carrier-fee"],
		set["web-carrier"] && blank(name),
		fee < 0 || fee > fulfillment.MaxWebFallbackFee:
		return webCarrierSeed{}, errSeedFixtureUsage
	case !set["web-carrier"]:
		return webCarrierSeed{}, nil
	}
	return webCarrierSeed{Name: strings.TrimSpace(name), Fee: fee}, nil
}

func (fx webFixture) imageCount() int {
	n := 0
	for _, p := range fx.Products {
		n += len(p.Images)
	}
	return n
}

// seedReport đếm bản ghi đã tạo, đã sửa theo loại (category, attribute, attribute_value, product, variant,
// classification, coupon, stock, admin_unit).
type seedReport struct {
	Created, Updated map[string]int
}

func newSeedReport() seedReport {
	return seedReport{Created: map[string]int{}, Updated: map[string]int{}}
}

// Total là tổng số thay đổi; 0 khi chạy lại cùng tệp.
func (r seedReport) Total() int {
	n := 0
	for _, m := range []map[string]int{r.Created, r.Updated} {
		for _, v := range m {
			n += v
		}
	}
	return n
}

func (seedReport) line(m map[string]int) string {
	if len(m) == 0 {
		return "0"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// clampInt32 chặn v (số lượt usageLimit + đã dùng, vị trí) trong [0, MaxInt32].
func clampInt32(v int64) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < 0:
		return 0
	}
	return int32(v)
}
