package catalog_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

func sameMeasure(a, b catalog.MeasureMm) bool {
	return equalPtr(a.Length, b.Length) && equalPtr(a.Width, b.Width) && equalPtr(a.Height, b.Height)
}

func (e *catalogEnv) updateProduct(p catalog.Product, patch catalog.ProductPatch) (catalog.Product, error) {
	var out catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().UpdateProduct(e.t.Context(), tx, e.seller, p.ID, p.Version, patch, e.actor)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

// Số đo của mẫu (Task 516, 517: mm, mỗi số tùy chọn 1–10000 = 0,1–1000 cm): tạo không số đo → rỗng; tạo kèm Dài 72,5
// cm, Rộng 20 cm → đọc lại đúng 725, 200, Cao rỗng; 0, -1, 10001 ở bất kỳ số nào → ErrInvalidMeasure, không tạo mẫu.
func TestProductMeasure_Create(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	if p.Measure != (catalog.MeasureMm{}) {
		t.Fatalf("mới tạo không số đo: %+v", p.Measure)
	}
	in := dam01(e.seller)
	in.Code, in.Variants = "QUAN01", in.Variants[:1]
	in.Variants[0].SKU = "QUAN01-S"
	in.Measure = catalog.MeasureMm{Length: ptr(725), Width: ptr(200)}
	q := e.mustCreate(in)
	got, err := e.get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMeasure(got.Measure, in.Measure) || !sameMeasure(q.Measure, in.Measure) {
		t.Fatalf("đọc lại: %+v / %+v, mong Dài 725 Rộng 200", got.Measure, q.Measure)
	}
	for i, bad := range []catalog.MeasureMm{
		{Length: ptr(0)},
		{Width: ptr(-1)},
		{Height: ptr(catalog.MaxMeasureMm + 1)},
		{Length: ptr(100), Width: ptr(100), Height: ptr(0)},
	} {
		in := dam01(e.seller)
		in.Code, in.Variants = fmt.Sprintf("BAD0%d", i+1), nil
		in.Measure = bad
		if _, err := e.create(in); !errors.Is(err, catalog.ErrInvalidMeasure) {
			t.Errorf("%+v: mong ErrInvalidMeasure, có %v", bad, err)
		}
	}
	if n := e.count("SELECT count(*) FROM catalog.products WHERE code LIKE 'BAD0%'"); n != 0 {
		t.Fatalf("số đo sai vẫn tạo %d mẫu", n)
	}
}

// Sửa số đo: Measure nil giữ nguyên (sửa tên vẫn giữ số đo); đặt cả ba số; đặt đối tượng chỉ có Cao → bỏ Dài, Rộng;
// đối tượng rỗng bỏ cả ba; biên 1 và 10000 mm nhận, 0 và 10001 → ErrInvalidMeasure (version, số đo không đổi); version cũ
// → ErrVersionConflict; mỗi lần đổi số đo version tăng 1, ghi audit catalog.product.update có số đo.
func TestProductMeasure_Update(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	in := dam01(e.seller)
	in.Measure = catalog.MeasureMm{Length: ptr(300), Width: ptr(255), Height: ptr(30)}
	p := e.mustCreate(in)
	renamed, err := e.updateProduct(p, catalog.ProductPatch{Name: ptr("Đầm mới")})
	if err != nil || !sameMeasure(renamed.Measure, in.Measure) {
		t.Fatalf("sửa tên phải giữ số đo: %+v, %v", renamed.Measure, err)
	}
	steps := []catalog.MeasureMm{
		{Length: ptr(catalog.MinMeasureMm), Width: ptr(catalog.MaxMeasureMm), Height: ptr(7)},
		{Height: ptr(5)},
		{},
	}
	cur := renamed
	for _, want := range steps {
		next, err := e.updateProduct(cur, catalog.ProductPatch{Measure: &want})
		if err != nil {
			t.Fatalf("đặt %+v: %v", want, err)
		}
		if next.Version != cur.Version+1 || !sameMeasure(next.Measure, want) {
			t.Fatalf("đặt %+v: version %d→%d, số đo %+v", want, cur.Version, next.Version, next.Measure)
		}
		got, err := e.get(p.ID)
		if err != nil || !sameMeasure(got.Measure, want) {
			t.Fatalf("đọc lại %+v: %+v, %v", want, got.Measure, err)
		}
		cur = next
	}
	for _, bad := range []catalog.MeasureMm{{Length: ptr(0)}, {Width: ptr(catalog.MaxMeasureMm + 1)}} {
		if _, err := e.updateProduct(cur, catalog.ProductPatch{Measure: &bad}); !errors.Is(err, catalog.ErrInvalidMeasure) {
			t.Errorf("%+v: mong ErrInvalidMeasure, có %v", bad, err)
		}
	}
	if got, _ := e.get(p.ID); got.Version != cur.Version || got.Measure != (catalog.MeasureMm{}) {
		t.Fatalf("số đo sai không được đổi gì: version %d (mong %d), %+v", got.Version, cur.Version, got.Measure)
	}
	if _, err := e.updateProduct(renamed, catalog.ProductPatch{Measure: &steps[0]}); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Fatalf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.product.update'
		AND entity_id = $1 AND after->>'lengthMm' = '1' AND after->>'widthMm' = '10000'`, p.ID.String()); n != 1 {
		t.Fatalf("audit có số đo: %d dòng, mong 1", n)
	}
}

// Database chặn số đo ngoài 1–10000 mm dù không đi qua Writer (CHECK của cột).
func TestProductMeasure_DatabaseCheck(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	for _, col := range []string{"length_mm", "width_mm", "height_mm"} {
		for _, v := range []int{0, 10001} {
			err := e.app(func(tx pgx.Tx) error {
				_, err := tx.Exec(e.t.Context(), "UPDATE catalog.products SET "+col+" = $1 WHERE id = $2", v, p.ID)
				return err //nolint:wrapcheck // test
			})
			if err == nil {
				t.Errorf("%s = %d: mong lỗi CHECK", col, v)
			}
		}
	}
}

// measureHeader là tiêu đề cơ bản kèm ba cột số đo.
func measureHeader() []string {
	return append(basicHeader(), "Dài (cm)", "Rộng (cm)", "Cao (cm)")
}

func measureRow(n int, code, sku, size string, extra map[string]string) xlsx.Row {
	cells := map[string]string{
		"Mã mẫu*": code, "Tên mẫu*": "Quần " + code, "Màu*": "Đen", "Size*": size, "SKU*": sku, "Giá bán*": "1",
	}
	for k, v := range extra {
		cells[k] = v
	}
	return xlsx.Row{Number: n, Cells: cells}
}

// Nhập Excel có cột số đo (Task 516, 517): mẫu mới nhận số đo của ô (72,5 và 72.5, 20 và 20,0 là cùng số giữa các dòng
// cùng mẫu); mẫu có sẵn: ô có số đặt số đó, ô trống giữ số cũ;
// tệp không có cột số đo (mẫu cũ) vẫn nhập được và giữ số đo.
func TestImportMeasure_Apply(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	old := e.mustCreate(catalog.ProductInput{
		SellerID: e.seller, Code: "QUAN02", Name: "Quần QUAN02",
		Measure:  catalog.MeasureMm{Length: ptr(900), Width: ptr(220), Height: ptr(20)},
		Variants: []catalog.VariantInput{{SKU: "QUAN02-S", Color: "Đen", Size: "S", Price: 1}},
	})
	body := writeRows(t, measureHeader(), []xlsx.Row{
		measureRow(2, "QUAN01", "QUAN01-S", "S", map[string]string{"Dài (cm)": "72,5", "Rộng (cm)": "20"}),
		measureRow(3, "QUAN01", "QUAN01-M", "M", map[string]string{"Dài (cm)": "72.5", "Rộng (cm)": "20,0"}),
		measureRow(4, "QUAN02", "QUAN02-S", "S", map[string]string{"Dài (cm)": "100,5"}),
	})
	imp := e.validate(body)
	e.mustReady(imp)
	e.applyExpect(imp, catalog.ImportApplied)
	var created catalog.Product
	if err := e.app(func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(e.t.Context(), "SELECT id FROM catalog.products WHERE code = 'QUAN01'").Scan(&id); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		created, err = catalog.GetProduct(e.t.Context(), tx, e.seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if want := (catalog.MeasureMm{Length: ptr(725), Width: ptr(200)}); !sameMeasure(created.Measure, want) {
		t.Fatalf("mẫu mới: %+v", created.Measure)
	}
	got, err := e.get(old.ID)
	if want := (catalog.MeasureMm{Length: ptr(1005), Width: ptr(220), Height: ptr(20)}); err != nil || !sameMeasure(got.Measure, want) {
		t.Fatalf("mẫu có sẵn: %+v, %v; mong Dài 1005, giữ Rộng 220, Cao 20", got.Measure, err)
	}
	plain := writeRows(t, basicHeader(), []xlsx.Row{measureRow(2, "QUAN02", "QUAN02-S", "S", nil)})
	imp = e.validate(plain)
	e.mustReady(imp)
	e.applyNow(imp)
	if again, _ := e.get(old.ID); !sameMeasure(again.Measure, got.Measure) {
		t.Fatalf("tệp không cột số đo phải giữ số đo: %+v", again.Measure)
	}
}

// Ô số đo sai (0, 1001, hai chữ số thập phân, chữ) → lỗi invalid_measure đúng ô; hai dòng cùng mẫu khác số đo →
// inconsistent_product_fields.
func TestImportMeasure_Errors(t *testing.T) {
	t.Parallel()
	e := newImportEnv(t)
	body := writeRows(t, measureHeader(), []xlsx.Row{
		measureRow(2, "A1", "A1-S", "S", map[string]string{"Dài (cm)": "0"}),
		measureRow(3, "A2", "A2-S", "S", map[string]string{"Rộng (cm)": "1001"}),
		measureRow(4, "A3", "A3-S", "S", map[string]string{"Cao (cm)": "2.55"}),
		measureRow(5, "A4", "A4-S", "S", map[string]string{"Dài (cm)": "abc"}),
		measureRow(6, "A5", "A5-S", "S", map[string]string{"Dài (cm)": "50"}),
		measureRow(7, "A5", "A5-M", "M", map[string]string{"Dài (cm)": "51"}),
	})
	imp := e.validate(body)
	if imp.Status != catalog.ImportInvalid {
		t.Fatalf("%s, mong invalid", imp.Status)
	}
	want := []string{
		"2 Dài (cm) invalid_measure", "3 Rộng (cm) invalid_measure", "4 Cao (cm) invalid_measure",
		"5 Dài (cm) invalid_measure", "7 Dài (cm) inconsistent_product_fields",
	}
	if len(imp.Errors) != len(want) {
		t.Fatalf("lỗi %+v, mong %v", imp.Errors, want)
	}
	for i, er := range imp.Errors {
		if got := fmt.Sprintf("%d %s %s", er.Row, er.Column, er.Code); got != want[i] {
			t.Errorf("lỗi %d: %q, mong %q", i, got, want[i])
		}
	}
}
