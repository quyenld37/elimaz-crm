package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// errFixtureDrift: bản ghi cùng id đã có ở CRM nhưng khác ở chỗ public API không sửa được (mã mẫu, mã giảm đã gỡ trần
// lượt…): dùng database mới cho bộ e2e.
var errFixtureDrift = errors.New("dữ liệu ở CRM khác tệp ở chỗ không sửa được; dùng database mới")

// errAdminUnitConflict: mã địa giới thử của tệp đã có ở CRM với cấp, tên hay tỉnh cha khác (không đè danh mục thật).
var errAdminUnitConflict = errors.New("mã đơn vị hành chính thử đã có ở CRM với nội dung khác")

// attrCodeSep: ký tự ngoài [a-z0-9] của tên thuộc tính đã bỏ dấu, thay bằng "-" để làm mã thuộc tính.
var attrCodeSep = regexp.MustCompile(`[^a-z0-9]+`)

// webFixtureSeeder ghi tệp fixture qua public API của catalog, promotion, inventory, customer (mỗi Writer tự ghi sự
// kiện, báo hiệu, audit như khi nhân viên sửa). Bus nil: không báo hiệu. Carrier.Name khác rỗng: bật hãng giả trên
// web qua Fulfillment (Task 511).
type webFixtureSeeder struct {
	Events      *events.Registry
	Queue       queue.JobQueue
	Bus         realtime.Bus
	Fulfillment *fulfillment.Service
	Carrier     webCarrierSeed
}

// fixtureApply là một lần chạy thân transaction (dựng mới mỗi lần RetryTx chạy lại).
type fixtureApply struct {
	tx     pgx.Tx
	actor  identity.AuthUser
	seller uuid.UUID
	cat    catalog.Writer
	rep    seedReport
	// values: mã giá trị thuộc tính theo "tên\x00giá trị".
	values map[string]uuid.UUID
}

// seed ghi fx trong một transaction tenant của actor trên pool chủ bảng owner, thứ tự khóa ADR-46: địa giới (không
// khóa), catalog (người bán, danh mục, thuộc tính, giá trị, mẫu theo id tăng dần: mẫu → phân loại → biến thể),
// fulfillment (hãng giả trên web khi có cờ: tài khoản → mã địa giới → cài đặt kênh web), promotion (mã), inventory (kho, một phiếu
// điều chỉnh mỗi 500 dòng). So rồi mới ghi: chạy lại cùng tệp → 0 thay đổi.
// Mẫu, biến thể thuộc người bán mặc định của tenant (người bán mà đường web dùng), không phải seller.id của tệp.
func (s webFixtureSeeder) seed(ctx context.Context, owner *db.DB, actor identity.AuthUser, fx webFixture) (seedReport, error) {
	var rep seedReport
	err := tenancy.WithTenant(ctx, owner, actor.TenantID, func(tx pgx.Tx) error {
		a := &fixtureApply{
			tx: tx, actor: actor, rep: newSeedReport(), values: map[string]uuid.UUID{},
			cat: catalog.Writer{Events: s.Events, Queue: s.Queue, Realtime: s.Bus, RequestID: seedFixtureCommand},
		}
		if err := a.run(ctx, fx, s); err != nil {
			return err
		}
		rep = a.rep
		return nil
	})
	if err != nil {
		return seedReport{}, fmt.Errorf("seed fixture: %w", err)
	}
	return rep, nil
}

func (a *fixtureApply) run(ctx context.Context, fx webFixture, s webFixtureSeeder) error {
	if err := a.adminUnits(ctx, fx.Locations); err != nil {
		return err
	}
	var err error
	if a.seller, err = catalog.EnsureDefaultSeller(ctx, a.tx, a.actor.TenantID); err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	if err := a.categories(ctx, fx.Categories); err != nil {
		return err
	}
	if err := a.attributes(ctx, fx.Products); err != nil {
		return err
	}
	products := slices.Clone(fx.Products)
	slices.SortFunc(products, func(x, y fixtureProduct) int { return strings.Compare(x.ID.String(), y.ID.String()) })
	for _, p := range products {
		if err := a.product(ctx, p); err != nil {
			return fmt.Errorf("mẫu %s: %w", p.Key, err)
		}
	}
	if err := a.webCarrier(ctx, s.Carrier, s.Fulfillment, fx.Locations); err != nil {
		return err
	}
	if err := a.coupons(ctx, fx.Coupons, promotion.Writer{Realtime: s.Bus, Events: s.Events, Queue: s.Queue}); err != nil {
		return err
	}
	return a.stock(ctx, fx.Products, inventory.Writer{Events: s.Events, Queue: s.Queue, Realtime: s.Bus})
}

func (a *fixtureApply) adminUnits(ctx context.Context, ls []fixtureLocation) error {
	units := make([]customer.AdminUnit, 0, len(ls))
	for _, l := range ls { // tỉnh trước xã (khóa ngoại parent_code)
		units = append(units, customer.AdminUnit{Code: l.Code, Level: customer.AdminProvince, Name: l.Name})
	}
	for _, l := range ls {
		for _, w := range l.Wards {
			units = append(units, customer.AdminUnit{Code: w.Code, Level: customer.AdminWard, ParentCode: l.Code, Name: w.Name})
		}
	}
	n, conflicts, err := customer.EnsureFixtureAdminUnits(ctx, a.tx, units)
	if err != nil {
		return err //nolint:wrapcheck // customer đã bọc
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("mã %s: %w", strings.Join(conflicts, ", "), errAdminUnitConflict)
	}
	if n > 0 {
		a.rep.Created["admin_unit"] += n
	}
	return nil
}

func (a *fixtureApply) categories(ctx context.Context, cs []fixtureCategory) error {
	have, err := catalog.ListCategories(ctx, a.tx)
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	for _, c := range cs {
		i := slices.IndexFunc(have, func(h catalog.Category) bool { return h.ID == c.ID })
		if i < 0 {
			if _, err := a.cat.CreateCategory(ctx, a.tx, catalog.CategoryInput{
				ID: c.ID, Name: c.Name, Slug: c.Slug, Position: c.Position,
			}, a.actor); err != nil {
				return fmt.Errorf("tạo danh mục %s: %w", c.Key, err)
			}
			a.rep.Created["category"]++
			continue
		}
		h := have[i]
		var patch catalog.CategoryPatch
		if h.Name != c.Name {
			patch.Name = &c.Name
		}
		if h.Slug != c.Slug {
			patch.Slug = &c.Slug
		}
		if h.Position != c.Position {
			patch.Position = &c.Position
		}
		if patch == (catalog.CategoryPatch{}) {
			continue
		}
		if _, err := a.cat.UpdateCategory(ctx, a.tx, h.ID, h.Version, patch, a.actor); err != nil {
			return fmt.Errorf("sửa danh mục %s: %w", c.Key, err)
		}
		a.rep.Updated["category"]++
	}
	return nil
}

func attrCode(name string) string {
	return strings.Trim(attrCodeSep.ReplaceAllString(textnorm.Fold(name), "-"), "-")
}

func valueKey(name, value string) string { return name + "\x00" + value }

// attributes tạo thuộc tính (mã = tên bỏ dấu, nối "-") và giá trị còn thiếu theo thứ tự gặp trong tệp, ghi mã giá trị
// vào a.values.
func (a *fixtureApply) attributes(ctx context.Context, ps []fixtureProduct) error {
	have, err := catalog.ListAttributes(ctx, a.tx)
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	byCode := make(map[string]catalog.Attribute, len(have))
	for _, at := range have {
		byCode[at.Code] = at
	}
	for _, p := range ps {
		for _, fa := range p.Attributes {
			code := attrCode(fa.Name)
			at, ok := byCode[code]
			if !ok {
				at, err = a.cat.CreateAttribute(ctx, a.tx, catalog.AttributeInput{
					Code: code, Name: fa.Name, Position: clampInt32(int64(len(byCode) + 1)),
				}, a.actor)
				if err != nil {
					return fmt.Errorf("tạo thuộc tính %q: %w", fa.Name, err)
				}
				a.rep.Created["attribute"]++
			}
			i := slices.IndexFunc(at.Values, func(v catalog.AttributeValue) bool { return v.Value == fa.Value })
			if i < 0 {
				v, err := a.cat.AddAttributeValue(ctx, a.tx, at.ID, catalog.AttributeValueInput{
					Value: fa.Value, Position: clampInt32(int64(len(at.Values) + 1)),
				}, a.actor)
				if err != nil {
					return fmt.Errorf("thêm giá trị cho thuộc tính %q: %w", fa.Name, err)
				}
				at.Values = append(at.Values, v)
				i = len(at.Values) - 1
				a.rep.Created["attribute_value"]++
			}
			byCode[code] = at
			a.values[valueKey(fa.Name, fa.Value)] = at.Values[i].ID
		}
	}
	return nil
}

func (a *fixtureApply) product(ctx context.Context, fp fixtureProduct) error {
	p, err := catalog.GetProduct(ctx, a.tx, a.seller, fp.ID)
	if errors.Is(err, catalog.ErrNotFound) {
		return a.createProduct(ctx, fp)
	}
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	if p.Code != strings.ToUpper(strings.TrimSpace(fp.Code)) {
		return fmt.Errorf("mã mẫu %s ở CRM khác %s trong tệp: %w", p.Code, fp.Code, errFixtureDrift)
	}
	var patch catalog.ProductPatch
	if p.Name != fp.Name {
		patch.Name = &fp.Name
	}
	if want := catalog.StatusActive; p.Status != want {
		patch.Status = &want
	}
	if want, _ := fp.MeasureCm.cm(); !sameMeasure(p.Measure, want) { // đã kiểm ở validate
		patch.Measure = &want
	}
	if patch != (catalog.ProductPatch{}) {
		if _, err := a.cat.UpdateProduct(ctx, a.tx, a.seller, p.ID, p.Version, patch, a.actor); err != nil {
			return err //nolint:wrapcheck // catalog đã bọc
		}
		a.rep.Updated["product"]++
	}
	changed, err := a.classify(ctx, fp)
	if err != nil {
		return err
	}
	if changed {
		a.rep.Updated["classification"]++
	}
	return a.variants(ctx, fp)
}

func (a *fixtureApply) createProduct(ctx context.Context, fp fixtureProduct) error {
	in := catalog.ProductInput{
		ID: fp.ID, CreatedAt: fixtureCreatedAt(fp.Position), SellerID: a.seller, Code: fp.Code, Name: fp.Name,
		Status: catalog.StatusActive,
	}
	in.Measure, _ = fp.MeasureCm.cm() // đã kiểm ở validate
	for _, v := range fp.Variants {
		in.Variants = append(in.Variants, variantInput(v))
	}
	if _, err := a.cat.CreateProduct(ctx, a.tx, in, a.actor); err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	a.rep.Created["product"]++
	a.rep.Created["variant"] += len(fp.Variants)
	_, err := a.classify(ctx, fp)
	return err
}

// sameMeasure: hai bộ số đo bằng nhau (nil chỉ bằng nil).
func sameMeasure(a, b catalog.MeasureCm) bool {
	eq := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return eq(a.Length, b.Length) && eq(a.Width, b.Width) && eq(a.Height, b.Height)
}

func variantInput(v fixtureVariant) catalog.VariantInput {
	return catalog.VariantInput{ID: v.ID, SKU: v.SKU, Barcode: v.Barcode, Color: v.Color, Size: v.Size, Price: v.Price}
}

// classify đặt danh mục và giá trị thuộc tính của mẫu theo tệp (giữ nhãn nhân viên đã gắn); trả true khi có đổi.
func (a *fixtureApply) classify(ctx context.Context, fp fixtureProduct) (bool, error) {
	cur, err := catalog.GetClassification(ctx, a.tx, a.seller, fp.ID)
	if err != nil {
		return false, err //nolint:wrapcheck // catalog đã bọc
	}
	want := catalog.Classification{CategoryID: &fp.CategoryIDs[0], TagIDs: cur.TagIDs}
	for _, fa := range fp.Attributes {
		want.AttributeValueIDs = append(want.AttributeValueIDs, a.values[valueKey(fa.Name, fa.Value)])
	}
	slices.SortFunc(want.AttributeValueIDs, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	want.AttributeValueIDs = slices.Compact(want.AttributeValueIDs)
	got := slices.Clone(cur.AttributeValueIDs)
	slices.SortFunc(got, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	if cur.CategoryID != nil && *cur.CategoryID == *want.CategoryID && slices.Equal(got, want.AttributeValueIDs) {
		return false, nil
	}
	if _, err := a.cat.SetClassification(ctx, a.tx, a.seller, fp.ID, cur.Version, want, a.actor); err != nil {
		return false, err //nolint:wrapcheck // catalog đã bọc
	}
	return true, nil
}

// variants sửa biến thể đã có (sku, màu, cỡ, giá, đang bán) và thêm biến thể còn thiếu; biến thể ở CRM không có trong
// tệp giữ nguyên.
func (a *fixtureApply) variants(ctx context.Context, fp fixtureProduct) error {
	p, err := catalog.GetProduct(ctx, a.tx, a.seller, fp.ID)
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	version := p.Version
	for _, fv := range fp.Variants {
		i := slices.IndexFunc(p.Variants, func(v catalog.Variant) bool { return v.ID == fv.ID })
		if i < 0 {
			if _, err := a.cat.AddVariant(ctx, a.tx, a.seller, p.ID, version, variantInput(fv), a.actor); err != nil {
				return fmt.Errorf("thêm biến thể %s: %w", fv.Key, err)
			}
			version++
			a.rep.Created["variant"]++
			continue
		}
		patch := variantPatch(p.Variants[i], fv)
		if patch == (catalog.VariantPatch{}) {
			continue
		}
		if _, err := a.cat.UpdateVariant(ctx, a.tx, a.seller, fv.ID, p.Variants[i].Version, patch, a.actor); err != nil {
			return fmt.Errorf("sửa biến thể %s: %w", fv.Key, err)
		}
		version++
		a.rep.Updated["variant"]++
	}
	return a.variantOrder(ctx, fp)
}

// variantOrder đặt thứ tự biến thể của mẫu theo tệp (Task 513): biến thể trong tệp theo thứ tự tệp, rồi biến thể chỉ có ở
// CRM theo thứ tự hiện tại; đã đúng thì không ghi gì.
func (a *fixtureApply) variantOrder(ctx context.Context, fp fixtureProduct) error {
	p, err := catalog.GetProduct(ctx, a.tx, a.seller, fp.ID)
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	want := make([]uuid.UUID, 0, len(p.Variants))
	inFile := make(map[uuid.UUID]bool, len(fp.Variants))
	for _, fv := range fp.Variants {
		want = append(want, fv.ID)
		inFile[fv.ID] = true
	}
	have := make([]uuid.UUID, 0, len(p.Variants))
	for _, v := range p.Variants {
		have = append(have, v.ID)
		if !inFile[v.ID] {
			want = append(want, v.ID)
		}
	}
	if slices.Equal(have, want) {
		return nil
	}
	if err := a.cat.ReorderVariants(ctx, a.tx, a.seller, p.ID, p.Version, want, a.actor); err != nil {
		return fmt.Errorf("sắp thứ tự biến thể của %s: %w", fp.Key, err)
	}
	a.rep.Updated["variant_order"]++
	return nil
}

func variantPatch(cur catalog.Variant, fv fixtureVariant) catalog.VariantPatch {
	var p catalog.VariantPatch
	if sku := strings.ToUpper(strings.TrimSpace(fv.SKU)); cur.SKU != sku {
		p.SKU = &sku
	}
	if c := strings.TrimSpace(fv.Color); cur.Color != c {
		p.Color = &c
	}
	if s := strings.TrimSpace(fv.Size); cur.Size != s {
		p.Size = &s
	}
	if b := strings.TrimSpace(fv.Barcode); b != "" && cur.Barcode != b {
		p.Barcode = &b
	}
	if cur.Price != fv.Price {
		p.Price = &fv.Price
	}
	if active := catalog.VariantActive; cur.Status != active {
		p.Status = &active
	}
	return p
}

// coupons tạo hay sửa mã theo code. Số lượt tối đa = usageLimit + số đã dùng: mỗi lần seed lại, mã còn đúng usageLimit
// lượt (không xóa lượt đã dùng của đơn có thật trong database).
func (a *fixtureApply) coupons(ctx context.Context, cs []fixtureCoupon, w promotion.Writer) error {
	actor := promotion.Actor{TenantID: a.actor.TenantID, UserID: a.actor.UserID}
	for _, fc := range cs {
		code := strings.ToUpper(strings.TrimSpace(fc.Code))
		list, _, err := promotion.ListCoupons(ctx, a.tx, promotion.CouponFilter{Q: code, Limit: 100})
		if err != nil {
			return err //nolint:wrapcheck // promotion đã bọc
		}
		i := slices.IndexFunc(list, func(c promotion.Coupon) bool { return strings.EqualFold(c.Code, code) })
		kind, value := promotion.KindFixed, fc.Value
		if fc.Kind == "percent" {
			kind, value = promotion.KindPercent, fc.Value*100 // phần vạn
		}
		var minSub int64
		if fc.MinSubtotal != nil {
			minSub = *fc.MinSubtotal
		}
		if i < 0 {
			if _, err := w.CreateCoupon(ctx, a.tx, actor, promotion.CouponInput{
				Code: code, Name: code, DiscountKind: kind, Value: value, MaxDiscount: fc.MaxDiscount, MinSubtotal: minSub,
				Channels: []string{fc.Channel}, MaxUses: fc.UsageLimit, ValidFrom: fc.StartsAt.UTC(), ValidTo: fc.EndsAt.UTC(),
			}); err != nil {
				return fmt.Errorf("tạo mã %s: %w", code, err)
			}
			a.rep.Created["coupon"]++
			continue
		}
		patch, err := couponPatch(list[i], fc, kind, value, minSub)
		if err != nil {
			return err
		}
		if patch == nil {
			continue
		}
		if _, err := w.UpdateCoupon(ctx, a.tx, actor, list[i].ID, list[i].Version, *patch); err != nil {
			return fmt.Errorf("sửa mã %s: %w", code, err)
		}
		a.rep.Updated["coupon"]++
	}
	return nil
}

// couponPatch: phần cần sửa để mã cur khớp tệp; nil khi đã khớp.
func couponPatch(cur promotion.Coupon, fc fixtureCoupon, kind string, value, minSub int64) (*promotion.CouponPatch, error) {
	var p promotion.CouponPatch
	changed := false
	set := func(differ bool, apply func()) {
		if differ {
			apply()
			changed = true
		}
	}
	set(cur.DiscountKind != kind, func() { p.DiscountKind = &kind })
	set(cur.Value != value, func() { p.Value = &value })
	set(cur.MinSubtotal != minSub, func() { p.MinSubtotal = &minSub })
	switch {
	case fc.MaxDiscount == nil && cur.MaxDiscount != nil, fc.UsageLimit == nil && cur.MaxUses != nil:
		return nil, fmt.Errorf("mã %s: không gỡ được trần giảm hay trần lượt: %w", cur.Code, errFixtureDrift)
	}
	set(fc.MaxDiscount != nil && (cur.MaxDiscount == nil || *cur.MaxDiscount != *fc.MaxDiscount), func() { p.MaxDiscount = fc.MaxDiscount })
	if fc.UsageLimit != nil {
		want := clampInt32(int64(*fc.UsageLimit) + int64(cur.UsedCount))
		set(cur.MaxUses == nil || *cur.MaxUses != want, func() { p.MaxUses = &want })
	}
	set(!slices.Equal(cur.Channels, []string{fc.Channel}), func() { p.Channels = []string{fc.Channel} })
	from, to := fc.StartsAt.UTC(), fc.EndsAt.UTC()
	set(!cur.ValidFrom.Equal(from), func() { p.ValidFrom = &from })
	set(!cur.ValidTo.Equal(to), func() { p.ValidTo = &to })
	active := promotion.StatusActive
	set(cur.Status != active, func() { p.Status = &active })
	if !changed {
		return nil, nil //nolint:nilnil // nil: không cần sửa
	}
	return &p, nil
}

// stock đặt số có thể bán ở kho mặc định = stock của tệp bằng phiếu điều chỉnh (delta = stock − có thể bán; giữ hàng
// cũ không bị nhả, phần giữ được bù vào tồn thực). Mỗi biến thể đổi tồn đếm một "stock" ở Updated.
func (a *fixtureApply) stock(ctx context.Context, ps []fixtureProduct, w inventory.Writer) error {
	wh, err := inventory.EnsureDefaultWarehouse(ctx, a.tx, a.actor.TenantID, a.seller)
	if err != nil {
		return err //nolint:wrapcheck // inventory đã bọc
	}
	want := map[uuid.UUID]int32{}
	ids := []uuid.UUID{}
	for _, p := range ps {
		for _, v := range p.Variants {
			want[v.ID] = v.Stock
			ids = append(ids, v.ID)
		}
	}
	bs, err := inventory.Availability(ctx, a.tx, wh, ids)
	if err != nil {
		return err //nolint:wrapcheck // inventory đã bọc
	}
	have := make(map[uuid.UUID]int32, len(bs)) // không có dòng số dư → 0
	for _, b := range bs {
		have[b.VariantID] = b.Available
	}
	slices.SortFunc(ids, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	var lines []inventory.DocumentLine
	for _, id := range ids {
		if d := want[id] - have[id]; d != 0 {
			lines = append(lines, inventory.DocumentLine{VariantID: id, Quantity: d})
			a.rep.Updated["stock"]++
		}
	}
	actor := inventory.Actor{Type: inventory.ActorStaff, ID: a.actor.UserID}
	for chunk := range slices.Chunk(lines, inventory.MaxDocumentLines) {
		doc, err := inventory.CreateDocument(ctx, a.tx, inventory.DocumentInput{
			WarehouseID: wh, Kind: inventory.KindAdjust, Note: "seed-web-fixture (e2e website)",
		}, actor)
		if err != nil {
			return err //nolint:wrapcheck // inventory đã bọc
		}
		if doc, err = inventory.ReplaceLines(ctx, a.tx, doc.ID, doc.Version, chunk); err != nil {
			return err //nolint:wrapcheck // như trên
		}
		if _, err := w.PostDocument(ctx, a.tx, doc.ID, doc.Version, actor); err != nil {
			return err //nolint:wrapcheck // như trên
		}
	}
	return nil
}
