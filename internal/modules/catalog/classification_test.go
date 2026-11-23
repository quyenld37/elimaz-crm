package catalog_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

func (e *catalogEnv) createCategory(in catalog.CategoryInput) (catalog.Category, error) {
	var c catalog.Category
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = e.writer().CreateCategory(e.t.Context(), tx, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *catalogEnv) mustCategory(name string, parent *uuid.UUID) catalog.Category {
	e.t.Helper()
	c, err := e.createCategory(catalog.CategoryInput{Name: name, ParentID: parent})
	if err != nil {
		e.t.Fatalf("CreateCategory %q: %v", name, err)
	}
	return c
}

func (e *catalogEnv) updateCategory(id uuid.UUID, version int64, patch catalog.CategoryPatch) (catalog.Category, error) {
	var c catalog.Category
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = e.writer().UpdateCategory(e.t.Context(), tx, id, version, patch, e.actor)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *catalogEnv) archiveCategory(id uuid.UUID, version int64) (catalog.Category, error) {
	var c catalog.Category
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = e.writer().ArchiveCategory(e.t.Context(), tx, id, version, e.actor)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *catalogEnv) listCategories() []catalog.Category {
	e.t.Helper()
	var out []catalog.Category
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = catalog.ListCategories(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("ListCategories: %v", err)
	}
	return out
}

func (e *catalogEnv) setClassification(id uuid.UUID, version int64, c catalog.Classification) (catalog.ProductClassification, error) {
	var out catalog.ProductClassification
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().SetClassification(e.t.Context(), tx, e.seller, id, version, c, e.actor)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func TestCategory_TreeDepthAndCycle(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	vay := e.mustCategory("Váy", nil)
	dai := e.mustCategory("Váy dài", &vay.ID)
	maxi := e.mustCategory("Váy maxi", &dai.ID)
	if vay.Depth != 1 || dai.Depth != 2 || maxi.Depth != 3 || vay.Slug != "vay" || maxi.Slug != "vay-maxi" ||
		maxi.Status != "active" || maxi.Version != 1 || *maxi.ParentID != dai.ID {
		t.Fatalf("cây sai: %+v %+v %+v", vay, dai, maxi)
	}
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Cấp 4", ParentID: &maxi.ID}); !errors.Is(err, catalog.ErrCategoryTooDeep) {
		t.Errorf("cấp 4: mong ErrCategoryTooDeep, có %v", err)
	}
	if _, err := e.updateCategory(vay.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &maxi.ID}); !errors.Is(err, catalog.ErrCategoryCycle) {
		t.Errorf("Váy làm con của Váy maxi: mong ErrCategoryCycle, có %v", err)
	}
	if _, err := e.updateCategory(vay.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &vay.ID}); !errors.Is(err, catalog.ErrCategoryCycle) {
		t.Errorf("Váy làm con của chính nó: mong ErrCategoryCycle, có %v", err)
	}
	// Chuyển cả cây con Váy dài (2 cấp) xuống dưới Đầm > Đầm dự tiệc → 4 cấp: từ chối.
	dam := e.mustCategory("Đầm", nil)
	tiec := e.mustCategory("Đầm dự tiệc", &dam.ID)
	if _, err := e.updateCategory(dai.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &tiec.ID}); !errors.Is(err, catalog.ErrCategoryTooDeep) {
		t.Errorf("cây con thành 4 cấp: mong ErrCategoryTooDeep, có %v", err)
	}
	// Chuyển Váy dài sang dưới Đầm (vẫn 3 cấp): được; độ sâu cây con giữ đúng.
	moved, err := e.updateCategory(dai.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &dam.ID})
	if err != nil || moved.Depth != 2 || moved.Version != 2 || *moved.ParentID != dam.ID {
		t.Fatalf("chuyển Váy dài sang Đầm: %+v, %v", moved, err)
	}
	// Đưa Váy dài lên gốc: độ sâu cây con giảm 1.
	root, err := e.updateCategory(dai.ID, 2, catalog.CategoryPatch{Move: true, Name: ptr("Váy dài gốc")})
	if err != nil || root.Depth != 1 || root.ParentID != nil || root.Name != "Váy dài gốc" {
		t.Fatalf("đưa lên gốc: %+v, %v", root, err)
	}
	depths := map[uuid.UUID]int{}
	for _, c := range e.listCategories() {
		depths[c.ID] = c.Depth
	}
	if depths[maxi.ID] != 2 || len(depths) != 5 {
		t.Errorf("Váy maxi phải ở cấp 2 sau khi cha lên gốc: %v", depths)
	}
	if _, err := e.updateCategory(dai.ID, 2, catalog.CategoryPatch{Name: ptr("x")}); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if _, err := e.updateCategory(uuid.New(), 1, catalog.CategoryPatch{Name: ptr("x")}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("danh mục không có: mong ErrNotFound, có %v", err)
	}
	missing := uuid.New()
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Mồ côi", ParentID: &missing}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("cha không có: mong ErrNotFound, có %v", err)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action LIKE 'catalog.category.%'"); n != 7 {
		t.Errorf("mong 7 dòng audit danh mục (5 tạo, 2 sửa), có %d", n)
	}
}

func TestCategory_Duplicates(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	vay := e.mustCategory("Váy", nil)
	e.mustCategory("Váy dài", &vay.ID)
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Váy dài", ParentID: &vay.ID, Slug: "vay-dai-2"}); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("cùng tên cùng cha: mong ErrAlreadyExists, có %v", err)
	}
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Váy", Slug: "vay-goc-2"}); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("cùng tên ở gốc (cha NULL): mong ErrAlreadyExists, có %v", err)
	}
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Váy khác", Slug: "vay"}); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("slug trùng: mong ErrAlreadyExists, có %v", err)
	}
	// Cùng tên khác cha: được (slug tự đặt khác).
	dam := e.mustCategory("Đầm", nil)
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Váy dài", ParentID: &dam.ID, Slug: "dam-vay-dai"}); err != nil {
		t.Errorf("cùng tên khác cha phải được: %v", err)
	}
	for name, in := range map[string]catalog.CategoryInput{
		"tên rỗng":      {Name: "  "},
		"slug sai mẫu":  {Name: "A", Slug: "Có Dấu"},
		"slug quá dài":  {Name: "B", Slug: string(make([]byte, 81))},
		"tên chỉ ký tự": {Name: "!!!"},
	} {
		if _, err := e.createCategory(in); !errors.Is(err, catalog.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	// Tenant khác dùng cùng slug, cùng tên: được.
	other := e.staff.Tenant(t, "B")
	if err := e.otherTenant(other, func(tx pgx.Tx) error {
		_, err := e.writer().CreateCategory(t.Context(), tx, catalog.CategoryInput{Name: "Váy"}, e.actorOf(other))
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Errorf("tenant khác cùng slug: %v", err)
	}
}

func TestCategory_Archive(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	vay := e.mustCategory("Váy", nil)
	dai := e.mustCategory("Váy dài", &vay.ID)
	in := dam01(e.seller)
	in.Status = catalog.StatusActive
	p, err := e.create(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.setClassification(p.ID, p.Version, catalog.Classification{CategoryID: &vay.ID}); err != nil {
		t.Fatalf("gán danh mục: %v", err)
	}
	if _, err := e.archiveCategory(vay.ID, 1); !errors.Is(err, catalog.ErrCategoryInUse) {
		t.Errorf("còn mẫu active: mong ErrCategoryInUse, có %v", err)
	}
	// Bỏ mẫu ra: vẫn còn danh mục con active → vẫn ErrCategoryInUse.
	if _, err := e.setClassification(p.ID, p.Version+1, catalog.Classification{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.archiveCategory(vay.ID, 1); !errors.Is(err, catalog.ErrCategoryInUse) {
		t.Errorf("còn danh mục con active: mong ErrCategoryInUse, có %v", err)
	}
	// Mẫu nháp trong danh mục không chặn lưu trữ.
	draft := dam01(e.seller)
	draft.Code = "DAM02"
	for i := range draft.Variants {
		draft.Variants[i].SKU = "X" + draft.Variants[i].SKU
	}
	d, err := e.create(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.setClassification(d.ID, d.Version, catalog.Classification{CategoryID: &dai.ID}); err != nil {
		t.Fatal(err)
	}
	if a, err := e.archiveCategory(dai.ID, 1); err != nil || a.Status != "archived" || a.Version != 2 {
		t.Fatalf("lưu trữ Váy dài (chỉ có mẫu nháp): %+v, %v", a, err)
	}
	if a, err := e.archiveCategory(vay.ID, 1); err != nil || a.Status != "archived" {
		t.Fatalf("lưu trữ Váy: %+v, %v", a, err)
	}
	// Không gán, không thêm con vào danh mục đã lưu trữ.
	if _, err := e.setClassification(p.ID, p.Version+2, catalog.Classification{CategoryID: &vay.ID}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("gán danh mục đã lưu trữ: mong ErrNotFound, có %v", err)
	}
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Con", ParentID: &vay.ID}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("thêm con vào danh mục đã lưu trữ: mong ErrNotFound, có %v", err)
	}
}

// nhanhAttributes tạo 8 thuộc tính và 30 giá trị (đúng số của Nhanh, 01 mục 3), giá trị i thuộc thuộc tính i % 8.
func (e *catalogEnv) nhanhAttributes() []catalog.Attribute {
	e.t.Helper()
	t := e.t
	var attrs []catalog.Attribute
	err := e.app(func(tx pgx.Tx) error {
		w := e.writer()
		for i := range int32(8) {
			a, err := w.CreateAttribute(t.Context(), tx, catalog.AttributeInput{
				Code: fmt.Sprintf("Thuoc-tinh-%d", i), Name: fmt.Sprintf("Thuộc tính %d", i), Position: i,
			}, e.actor)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			attrs = append(attrs, a)
		}
		for i := range int32(30) {
			if _, err := w.AddAttributeValue(t.Context(), tx, attrs[i%8].ID, catalog.AttributeValueInput{
				Value: fmt.Sprintf("Giá trị %d", i), Position: i,
			}, e.actor); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("8 thuộc tính, 30 giá trị: %v", err)
	}
	return attrs
}

func TestAttributes_NhanhCounts(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	attrs := e.nhanhAttributes()
	if attrs[0].Code != "thuoc-tinh-0" {
		t.Errorf("mã thuộc tính chuẩn hóa chữ thường: %q", attrs[0].Code)
	}
	var list []catalog.Attribute
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		list, err = catalog.ListAttributes(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, a := range list {
		total += len(a.Values)
	}
	if len(list) != 8 || total != 30 || list[0].Code != "thuoc-tinh-0" || len(list[0].Values) != 4 {
		t.Errorf("đọc lại: %d thuộc tính, %d giá trị (%+v)", len(list), total, list[0])
	}
	addValue := func(attr uuid.UUID, v string) error {
		return e.app(func(tx pgx.Tx) error {
			_, err := e.writer().AddAttributeValue(t.Context(), tx, attr, catalog.AttributeValueInput{Value: v}, e.actor)
			return err //nolint:wrapcheck // test
		})
	}
	if err := addValue(attrs[0].ID, "Giá trị 0"); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("giá trị trùng trong một thuộc tính: mong ErrAlreadyExists, có %v", err)
	}
	if err := addValue(attrs[1].ID, "Giá trị 0"); err != nil {
		t.Errorf("cùng giá trị ở thuộc tính khác phải được: %v", err)
	}
	if err := addValue(uuid.New(), "x"); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("thuộc tính không có: mong ErrNotFound, có %v", err)
	}
	if err := addValue(attrs[0].ID, " "); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Errorf("giá trị rỗng: mong ErrInvalidInput, có %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().CreateAttribute(t.Context(), tx, catalog.AttributeInput{Code: "THUOC-TINH-0", Name: "Trùng"}, e.actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("mã thuộc tính trùng: mong ErrAlreadyExists, có %v", err)
	}
}

func (e *catalogEnv) tags(codes ...string) []catalog.Tag {
	e.t.Helper()
	var out []catalog.Tag
	if err := e.app(func(tx pgx.Tx) error {
		for _, c := range codes {
			tg, err := e.writer().CreateTag(e.t.Context(), tx, catalog.TagInput{Code: c, Name: "Nhãn " + c}, e.actor)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			out = append(out, tg)
		}
		return nil
	}); err != nil {
		e.t.Fatalf("CreateTag: %v", err)
	}
	return out
}

func (e *catalogEnv) attributeValues(n int) []uuid.UUID {
	e.t.Helper()
	var ids []uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		a, err := e.writer().CreateAttribute(e.t.Context(), tx, catalog.AttributeInput{Code: "chat-lieu", Name: "Chất liệu"}, e.actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for i := range n {
			v, err := e.writer().AddAttributeValue(e.t.Context(), tx, a.ID, catalog.AttributeValueInput{Value: fmt.Sprint("Lụa ", i)}, e.actor)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			ids = append(ids, v.ID)
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func TestSetClassification_ReplaceAll(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	tags := e.tags("hang-moi", "sale", "ban-chay", "le-tet")
	vals := e.attributeValues(2)
	vay := e.mustCategory("Váy", nil)
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().CreateTag(t.Context(), tx, catalog.TagInput{Code: "Hang-Moi", Name: "Trùng"}, e.actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("mã nhãn trùng (khác hoa thường): mong ErrAlreadyExists, có %v", err)
	}
	sigsBefore := len(e.bus.Signals())
	want := catalog.Classification{
		CategoryID:        &vay.ID,
		AttributeValueIDs: vals,
		TagIDs:            []uuid.UUID{tags[0].ID, tags[1].ID, tags[2].ID, tags[1].ID}, // trùng một → gộp
	}
	got, err := e.setClassification(p.ID, 1, want)
	if err != nil {
		t.Fatalf("SetClassification: %v", err)
	}
	if got.Version != 2 || got.ProductID != p.ID || len(got.TagIDs) != 3 || len(got.AttributeValueIDs) != 2 || *got.CategoryID != vay.ID {
		t.Fatalf("kết quả sai: %+v", got)
	}
	var read catalog.ProductClassification
	if err := e.app(func(tx pgx.Tx) error {
		read, err = catalog.GetClassification(t.Context(), tx, e.seller, p.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	wantTags := []uuid.UUID{tags[0].ID, tags[1].ID, tags[2].ID}
	slices.SortFunc(wantTags, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	if !slices.Equal(read.TagIDs, wantTags) || !slices.Equal(read.AttributeValueIDs, got.AttributeValueIDs) || read.Version != 2 {
		t.Errorf("đọc lại sai: %+v, mong nhãn %v", read, wantTags)
	}
	if n := e.count("SELECT count(*) FROM catalog.product_tags_v WHERE product_id = $1", p.ID); n != 3 {
		t.Errorf("catalog.product_tags_v phải có 3 dòng, có %d", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.products_v WHERE product_id = $1 AND category_id = $2", p.ID, vay.ID); n != 1 {
		t.Errorf("products_v.category_id phải là Váy")
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1 AND seq = 2", p.ID.String()); n != 1 {
		t.Errorf("mong 1 product.updated seq 2, có %d", n)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.product.classify' AND entity_id = $1", p.ID.String()); n != 1 {
		t.Errorf("mong 1 dòng audit phân loại, có %d", n)
	}
	if sigs := e.bus.Signals()[sigsBefore:]; len(sigs) != 1 || sigs[0].Topic != "catalog.product" {
		t.Errorf("mong 1 báo hiệu catalog.product: %+v", sigs)
	}

	// Thay toàn bộ: chỉ còn 1 nhãn, bỏ danh mục và thuộc tính.
	got, err = e.setClassification(p.ID, 2, catalog.Classification{TagIDs: []uuid.UUID{tags[3].ID}})
	if err != nil || got.Version != 3 || got.CategoryID != nil || len(got.AttributeValueIDs) != 0 || len(got.TagIDs) != 1 {
		t.Fatalf("thay toàn bộ: %+v, %v", got, err)
	}
	if n := e.count("SELECT count(*) FROM catalog.product_tags_v WHERE product_id = $1", p.ID); n != 1 {
		t.Errorf("sau khi thay: product_tags_v phải có 1 dòng, có %d", n)
	}
}

func TestSetClassification_Errors(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.setClassification(p.ID, 1, catalog.Classification{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.setClassification(p.ID, 1, catalog.Classification{}); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if _, err := e.setClassification(uuid.New(), 1, catalog.Classification{}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("mẫu không có: mong ErrNotFound, có %v", err)
	}
	if _, err := e.setClassification(p.ID, 2, catalog.Classification{TagIDs: []uuid.UUID{uuid.New()}}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("nhãn không có: mong ErrNotFound, có %v", err)
	}
	many := make([]uuid.UUID, 101)
	for i := range many {
		many[i] = uuid.New()
	}
	if _, err := e.setClassification(p.ID, 2, catalog.Classification{TagIDs: many}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Errorf("101 nhãn: mong ErrInvalidInput, có %v", err)
	}
}

// otherTenant chạy fn bằng crm_app trong ngữ cảnh tenant khác.
func (e *catalogEnv) otherTenant(tenant uuid.UUID, fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, fn) //nolint:wrapcheck // test
}

// actorOf là nhân viên có catalog.manage của tenant khác.
func (e *catalogEnv) actorOf(tenant uuid.UUID) identity.AuthUser {
	e.t.Helper()
	uid := e.staff.User(e.t, tenant, uuid.NewString()[:8]+"@b.test", "active")
	return identity.AuthUser{
		UserID: uid, TenantID: tenant, Email: "b@b.test",
		Grants: []identity.Grant{{Permission: "catalog.manage", Scope: identity.Scope{Type: identity.ScopeAll}}},
	}
}

func TestSetClassification_OtherTenantValue(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	other := e.staff.Tenant(t, "B")
	var foreignValue, foreignTag uuid.UUID
	var foreignCat catalog.Category
	if err := e.otherTenant(other, func(tx pgx.Tx) error {
		w := e.writer()
		actor := e.actorOf(other)
		a, err := w.CreateAttribute(t.Context(), tx, catalog.AttributeInput{Code: "mau", Name: "Màu"}, actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		v, err := w.AddAttributeValue(t.Context(), tx, a.ID, catalog.AttributeValueInput{Value: "Đỏ"}, actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		foreignValue = v.ID
		tg, err := w.CreateTag(t.Context(), tx, catalog.TagInput{Code: "sale", Name: "Sale"}, actor)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		foreignTag = tg.ID
		foreignCat, err = w.CreateCategory(t.Context(), tx, catalog.CategoryInput{Name: "Váy"}, actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]catalog.Classification{
		"giá trị thuộc tính tenant khác": {AttributeValueIDs: []uuid.UUID{foreignValue}},
		"nhãn tenant khác":               {TagIDs: []uuid.UUID{foreignTag}},
		"danh mục tenant khác":           {CategoryID: &foreignCat.ID},
	} {
		if _, err := e.setClassification(p.ID, 1, c); !errors.Is(err, catalog.ErrNotFound) {
			t.Errorf("%s: mong ErrNotFound, có %v", name, err)
		}
	}
	if _, err := e.createCategory(catalog.CategoryInput{Name: "Con", ParentID: &foreignCat.ID}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("cha ở tenant khác: mong ErrNotFound, có %v", err)
	}
	if n := e.count("SELECT count(*) FROM catalog.product_attribute_values WHERE product_id = $1", p.ID); n != 0 {
		t.Errorf("không được ghi dòng nào, có %d", n)
	}
}

// Hai người đổi cha chéo nhau cùng lúc (A dưới B, B dưới A): đúng một người được, người kia ErrCategoryCycle; cây
// không bao giờ có vòng.
func TestCategory_ConcurrentCrossMove(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	for range 10 {
		a := e.mustCategory("A"+uuid.NewString()[:6], nil)
		b := e.mustCategory("B"+uuid.NewString()[:6], nil)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, errs[0] = e.updateCategory(a.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &b.ID}) })
		wg.Go(func() { _, errs[1] = e.updateCategory(b.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &a.ID}) })
		wg.Wait()
		ok, cycle := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, catalog.ErrCategoryCycle):
				cycle++
			default:
				t.Fatalf("lỗi lạ: %v", err)
			}
		}
		if ok != 1 || cycle != 1 {
			t.Fatalf("mong 1 được 1 vòng, có %d và %d", ok, cycle)
		}
	}
	// Thêm con và chuyển cha cùng lúc không làm cây quá 3 cấp.
	for range 10 {
		x := e.mustCategory("X"+uuid.NewString()[:6], nil)
		y := e.mustCategory("Y"+uuid.NewString()[:6], nil)
		y2 := e.mustCategory("Y2"+uuid.NewString()[:6], &y.ID)
		x2 := e.mustCategory("X2"+uuid.NewString()[:6], &x.ID)
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = e.updateCategory(x.ID, 1, catalog.CategoryPatch{Move: true, ParentID: &y2.ID}) })
		wg.Go(func() {
			_, _ = e.createCategory(catalog.CategoryInput{Name: "X3" + uuid.NewString()[:6], ParentID: &x2.ID})
		})
		wg.Wait()
	}
	if n := e.count(`SELECT count(*) FROM catalog.categories AS c
		LEFT JOIN catalog.categories AS p ON p.tenant_id = c.tenant_id AND p.id = c.parent_id
		WHERE c.depth <> coalesce(p.depth, 0) + 1`); n != 0 {
		t.Errorf("có %d danh mục sai độ sâu so với cha", n)
	}
}
