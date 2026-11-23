package catalog_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

// Cân nặng của biến thể (Task 190, QĐ30): chưa nhập → nil; đặt 450 → 450, đọc lại thấy 450, sửa phần khác giữ nguyên;
// 0, -1, 50.001 → ErrInvalidWeight (không đổi gì); 1 và 50.000 nhận; ClearWeight bỏ cân nặng.
func TestUpdateVariant_Weight(t *testing.T) {
	e := newCatalogEnv(t)
	in := dam01(e.seller)
	p, err := e.create(in)
	if err != nil {
		t.Fatalf("tạo mẫu: %v", err)
	}
	v := p.Variants[len(p.Variants)-1]
	if v.WeightGrams != nil {
		t.Fatalf("mới tạo: mong chưa có cân nặng, có %d", *v.WeightGrams)
	}
	for _, bad := range []int{0, -1, catalog.MaxWeightGrams + 1} {
		if _, err := e.updateVariant(v.ID, v.Version, catalog.VariantPatch{WeightGrams: ptr(bad)}); !errors.Is(err, catalog.ErrInvalidWeight) {
			t.Errorf("cân nặng %d: mong ErrInvalidWeight, có %v", bad, err)
		}
	}
	v, err = e.updateVariant(v.ID, v.Version, catalog.VariantPatch{WeightGrams: ptr(450)})
	if err != nil || v.WeightGrams == nil || *v.WeightGrams != 450 {
		t.Fatalf("đặt 450: %+v, %v", v.WeightGrams, err)
	}
	v, err = e.updateVariant(v.ID, v.Version, catalog.VariantPatch{Price: ptr(int64(360000))})
	if err != nil || v.WeightGrams == nil || *v.WeightGrams != 450 {
		t.Fatalf("sửa giá phải giữ cân nặng: %+v, %v", v.WeightGrams, err)
	}
	var got catalog.Product
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = catalog.GetProduct(t.Context(), tx, e.seller, p.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if w := got.Variants[len(got.Variants)-1].WeightGrams; w == nil || *w != 450 {
		t.Fatalf("đọc lại: mong 450, có %v", w)
	}
	for _, edge := range []int{catalog.MinWeightGrams, catalog.MaxWeightGrams} {
		if v, err = e.updateVariant(v.ID, v.Version, catalog.VariantPatch{WeightGrams: ptr(edge)}); err != nil || *v.WeightGrams != edge {
			t.Fatalf("biên %d: %+v, %v", edge, v.WeightGrams, err)
		}
	}
	v, err = e.updateVariant(v.ID, v.Version, catalog.VariantPatch{WeightGrams: ptr(10), ClearWeight: true})
	if err != nil || v.WeightGrams != nil {
		t.Fatalf("bỏ cân nặng: %+v, %v", v.WeightGrams, err)
	}
}
