package catalog_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

// legacyBook sinh tệp từ hai mẫu giả: một có danh mục CRM ("Áo > Áo sơ mi"), một có danh mục lạ.
func (e *importEnv) legacyBook() ([]byte, []string) {
	e.t.Helper()
	var data []byte
	var missing []string
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		data, missing, err = catalog.LegacyWorkbook(e.t.Context(), tx, []catalog.LegacyProduct{
			{
				Code: "NH-AO1", Name: "Áo chuyển từ hệ cũ", Description: "Dòng một\nDòng hai", CategoryPath: []string{"Áo", "Áo sơ mi"},
				Variants: []catalog.LegacyVariant{
					{SKU: "NH-AO1-D-M", Barcode: "8938500000011", Color: "Đỏ", Size: "M", Price: 330000, CostPrice: new(int64(150000))},
					{SKU: "NH-AO1-D-L", Color: "Đỏ", Size: "L", Price: 330000},
				},
			},
			{
				Code: "NH-VAY1", Name: "Váy lạ", CategoryPath: []string{"Hàng lạ", "Không có"},
				Variants: []catalog.LegacyVariant{{SKU: "NH-VAY1-X-S", Color: "Xanh", Size: "S", Price: 500000}},
			},
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("LegacyWorkbook: %v", err)
	}
	return data, missing
}

// Tệp sinh ra qua đúng đường nhập Excel: kiểm xong là ready, nhân viên áp thì mẫu mới là draft, SKU, giá, màu, size,
// giá vốn đúng; danh mục lạ được báo và để trống; chạy lại cùng tệp không tạo thêm.
func TestLegacyWorkbook_GoesThroughImportAsDraft(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	data, missing := e.legacyBook()
	if !slices.Equal(missing, []string{"Hàng lạ > Không có"}) {
		t.Errorf("danh mục thiếu = %v", missing)
	}
	imp := e.validate(data)
	e.mustReady(imp)
	if imp.Summary.Models != 2 || imp.Summary.Create != 3 {
		t.Fatalf("tóm tắt = %+v, muốn 2 mẫu 3 biến thể", imp.Summary)
	}
	e.applyExpect(imp, catalog.ImportApplied)
	if n := e.count("SELECT count(*) FROM catalog.products WHERE code IN ('NH-AO1','NH-VAY1') AND status = 'draft'"); n != 2 {
		t.Errorf("mẫu draft = %d, muốn 2", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.variants WHERE sku = 'NH-AO1-D-M' AND price = 330000 AND cost_price = 150000 AND color = 'Đỏ' AND size = 'M' AND barcode = '8938500000011'"); n != 1 {
		t.Errorf("biến thể NH-AO1-D-M sai (%d)", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.variants WHERE sku = 'NH-AO1-D-L' AND cost_price IS NULL AND length(barcode) > 0"); n != 1 {
		t.Errorf("biến thể NH-AO1-D-L: thiếu giá vốn phải là NULL, barcode CRM tự sinh (%d)", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.products WHERE code = 'NH-AO1' AND category_id IS NOT NULL"); n != 1 {
		t.Errorf("mẫu NH-AO1 phải có danh mục (%d)", n)
	}
	if n := e.count("SELECT count(*) FROM catalog.products WHERE code = 'NH-VAY1' AND category_id IS NOT NULL"); n != 0 {
		t.Errorf("mẫu NH-VAY1 (danh mục lạ) phải để trống danh mục (%d)", n)
	}
	// Chạy lại cùng đợt (tệp giống hệt): bản nhập thứ hai không tạo thêm mẫu.
	data2, _ := e.legacyBook()
	imp2 := e.validate(data2)
	if imp2.Status == catalog.ImportReady {
		e.applyNow(imp2)
	}
	if n := e.products(); n != 2 {
		t.Errorf("sau lần chạy lại có %d mẫu, muốn 2", n)
	}
}

// Mã cũ: ghi hai lần không trùng (lần hai 0 dòng mới), đọc lại đúng thực thể, tenant khác không thấy; tra theo SKU
// không phân biệt hoa thường và không thấy SKU của người bán khác.
func TestLegacyRefs_IdempotentAndTenantScoped(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	data, _ := e.legacyBook()
	e.applyExpect(e.validate(data), catalog.ImportApplied)
	var vars map[string]catalog.SKUVariant
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		vars, err = catalog.VariantsBySKU(e.t.Context(), tx, e.seller, []string{"nh-ao1-d-m", "NH-VAY1-X-S", "KHONG-CO"})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(vars) != 2 || vars["NH-AO1-D-M"].Color != "Đỏ" || vars["NH-VAY1-X-S"].ProductVersion < 1 || vars["NH-AO1-D-M"].ProductID == uuid.Nil {
		t.Fatalf("VariantsBySKU = %+v", vars)
	}
	refs := []catalog.LegacyRef{
		{Kind: catalog.LegacyKindProduct, LegacyID: "100", EntityID: vars["NH-AO1-D-M"].ProductID},
		{Kind: catalog.LegacyKindVariant, LegacyID: "101", EntityID: vars["NH-AO1-D-M"].ID},
	}
	for i, want := range []int{2, 0} {
		var n int
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			n, err = catalog.RecordLegacyRefs(e.t.Context(), tx, e.tenant, refs)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("lần %d: ghi %d mã mới, muốn %d", i+1, n, want)
		}
	}
	// Cùng mã cũ, thực thể khác: giữ thực thể đã ghi (không ghi đè).
	if err := e.app(func(tx pgx.Tx) error {
		n, err := catalog.RecordLegacyRefs(e.t.Context(), tx, e.tenant, []catalog.LegacyRef{
			{Kind: catalog.LegacyKindProduct, LegacyID: "100", EntityID: uuid.New()},
		})
		if n != 0 {
			t.Errorf("ghi đè mã cũ: %d dòng", n)
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	var got map[string]uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = catalog.LegacyRefs(e.t.Context(), tx, catalog.LegacyKindProduct, []string{"100", "999"})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["100"] != vars["NH-AO1-D-M"].ProductID {
		t.Errorf("LegacyRefs = %v", got)
	}
	other := e.staff.Tenant(t, "B")
	if err := e.otherTenant(other, func(tx pgx.Tx) error {
		g, err := catalog.LegacyRefs(e.t.Context(), tx, catalog.LegacyKindProduct, []string{"100"})
		if len(g) != 0 {
			t.Errorf("tenant khác thấy mã cũ: %v", g)
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
}
