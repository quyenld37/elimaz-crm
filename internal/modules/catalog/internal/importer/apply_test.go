package importer_test

import (
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
)

// recheckPlan: mẫu mới AO01 (dòng 2, 3: tạo hai biến thể) và mẫu có sẵn DAM01 (dòng 4 sửa, dòng 5 giữ nguyên, dòng 6
// tạo), kèm dữ liệu "lúc kiểm" khớp plan.
func recheckPlan() (importer.Plan, importer.Refs) {
	dam, v4, v5 := uuid.New(), uuid.New(), uuid.New()
	plan := importer.Plan{Products: []importer.PlannedProduct{
		{Code: "AO01", Name: "Áo", Variants: []importer.PlannedVariant{
			{Row: 2, Action: importer.ActionCreate, SKU: "AO01-D-S", Color: "Đỏ", Size: "S", Price: 1},
			{Row: 3, Action: importer.ActionCreate, SKU: "AO01-D-M", Color: "Đỏ", Size: "M", Price: 1},
		}},
		{Code: "DAM01", Name: "Đầm", ExistingProductID: &dam, Variants: []importer.PlannedVariant{
			{Row: 4, Action: importer.ActionUpdate, ExistingID: &v4, ExistingVersion: 3, SKU: "DAM01-S", Color: "Đỏ", Size: "S", Price: 2},
			{Row: 5, Action: importer.ActionUnchanged, ExistingID: &v5, ExistingVersion: 1, SKU: "DAM01-M", Color: "Đỏ", Size: "M", Price: 1},
			{Row: 6, Action: importer.ActionCreate, SKU: "DAM01-L", Color: "Đỏ", Size: "L", Price: 1},
		}},
	}}
	now := importer.Refs{
		ExistingSKUs: map[string]importer.ExistingVariant{
			"DAM01-S": {ID: v4, ProductID: dam, ProductCode: "DAM01", Version: 3},
			"DAM01-M": {ID: v5, ProductID: dam, ProductCode: "DAM01", Version: 1},
		},
		ExistingProducts: map[string]importer.ExistingProduct{"DAM01": {ID: dam}},
	}
	return plan, now
}

// Recheck so plan đã kiểm với dữ liệu lúc áp: không đổi gì → không lỗi; mỗi kiểu thay đổi giữa lúc kiểm và lúc áp →
// đúng một lỗi đúng dòng, cột, mã.
func TestRecheck(t *testing.T) {
	t.Parallel()
	other := uuid.New()
	for name, c := range map[string]struct {
		change       func(now *importer.Refs)
		row          int
		column, code string
	}{
		"không đổi": {change: func(*importer.Refs) {}},
		"SKU mới bị mẫu khác lấy": {
			change: func(now *importer.Refs) {
				now.ExistingSKUs["AO01-D-M"] = importer.ExistingVariant{ID: uuid.New(), ProductID: other, ProductCode: "VAY09"}
			},
			row: 3, column: importer.ColSKU, code: importer.CodeSKUBelongsToOtherProduct,
		},
		"mã mẫu mới vừa được tạo": {
			change: func(now *importer.Refs) { now.ExistingProducts["AO01"] = importer.ExistingProduct{ID: other} },
			row:    2, column: importer.ColCode, code: importer.CodeCodeTaken,
		},
		"mẫu có sẵn vừa bị xóa": {
			change: func(now *importer.Refs) { delete(now.ExistingProducts, "DAM01") },
			row:    4, column: importer.ColCode, code: importer.CodeChangedSinceValidation,
		},
		"biến thể sửa vừa đổi version": {
			change: func(now *importer.Refs) {
				v := now.ExistingSKUs["DAM01-S"]
				v.Version++
				now.ExistingSKUs["DAM01-S"] = v
			},
			row: 4, column: importer.ColSKU, code: importer.CodeChangedSinceValidation,
		},
		"biến thể giữ nguyên vừa đổi version": {
			change: func(now *importer.Refs) {
				v := now.ExistingSKUs["DAM01-M"]
				v.Version++
				now.ExistingSKUs["DAM01-M"] = v
			},
			row: 5, column: importer.ColSKU, code: importer.CodeChangedSinceValidation,
		},
		"SKU có sẵn vừa chuyển sang mẫu khác": {
			change: func(now *importer.Refs) {
				v := now.ExistingSKUs["DAM01-M"]
				v.ProductID, v.ProductCode = other, "VAY09"
				now.ExistingSKUs["DAM01-M"] = v
			},
			row: 5, column: importer.ColSKU, code: importer.CodeSKUBelongsToOtherProduct,
		},
		"biến thể có sẵn vừa bị xóa": {
			change: func(now *importer.Refs) { delete(now.ExistingSKUs, "DAM01-S") },
			row:    4, column: importer.ColSKU, code: importer.CodeChangedSinceValidation,
		},
		"SKU mới vừa được tạo ở chính mẫu này": {
			change: func(now *importer.Refs) {
				now.ExistingSKUs["DAM01-L"] = importer.ExistingVariant{ID: uuid.New(), ProductCode: "DAM01"}
			},
			row: 6, column: importer.ColSKU, code: importer.CodeChangedSinceValidation,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRecheck(t, c.change, c.row, c.column, c.code)
		})
	}
}

// assertRecheck áp change lên dữ liệu lúc áp rồi Recheck: code rỗng → không lỗi; khác → đúng một lỗi đúng dòng, cột, mã.
func assertRecheck(t *testing.T, change func(now *importer.Refs), row int, column, code string) {
	t.Helper()
	plan, now := recheckPlan()
	change(&now)
	errs := importer.Recheck(plan, now)
	if code == "" {
		if len(errs) != 0 {
			t.Fatalf("lỗi %+v", errs)
		}
		return
	}
	if len(errs) != 1 || errs[0].Row != row || errs[0].Column != column || errs[0].Code != code || errs[0].Message == "" {
		t.Fatalf("lỗi %+v, mong dòng %d cột %s mã %s", errs, row, column, code)
	}
}

// Mutation testing (Task 403): mã mẫu hay SKU còn nhưng là bản ghi khác (xóa rồi tạo lại cùng mã, cùng SKU) → lỗi
// "đổi từ lúc kiểm", không chỉ khi mất mã.
func TestRecheck_RecreatedSameKey(t *testing.T) {
	t.Parallel()
	t.Run("mẫu có sẵn vừa bị xóa rồi tạo lại cùng mã", func(t *testing.T) {
		t.Parallel()
		assertRecheck(t, func(now *importer.Refs) { now.ExistingProducts["DAM01"] = importer.ExistingProduct{ID: uuid.New()} },
			4, importer.ColCode, importer.CodeChangedSinceValidation)
	})
	t.Run("biến thể sửa vừa bị xóa rồi tạo lại cùng SKU", func(t *testing.T) {
		t.Parallel()
		assertRecheck(t, func(now *importer.Refs) {
			v := now.ExistingSKUs["DAM01-S"]
			v.ID = uuid.New()
			now.ExistingSKUs["DAM01-S"] = v
		}, 4, importer.ColSKU, importer.CodeChangedSinceValidation)
	})
}

// Nhiều lỗi xếp theo dòng.
func TestRecheck_SortedByRow(t *testing.T) {
	t.Parallel()
	plan, now := recheckPlan()
	delete(now.ExistingSKUs, "DAM01-M")
	now.ExistingSKUs["AO01-D-S"] = importer.ExistingVariant{ID: uuid.New(), ProductCode: "VAY09"}
	errs := importer.Recheck(plan, now)
	if len(errs) != 2 || errs[0].Row != 2 || errs[1].Row != 5 {
		t.Fatalf("lỗi %+v", errs)
	}
}

// Mutation testing (Task 403): mẫu không có dòng biến thể nào được bỏ qua mà không dừng vòng lặp — mẫu sau nó vẫn được
// kiểm lại.
func TestRecheck_ProductWithoutVariantsSkipped(t *testing.T) {
	t.Parallel()
	plan, now := recheckPlan()
	plan.Products = append([]importer.PlannedProduct{{Code: "RONG01", Name: "Rỗng"}}, plan.Products...)
	now.ExistingProducts["AO01"] = importer.ExistingProduct{ID: uuid.New()}
	errs := importer.Recheck(plan, now)
	if len(errs) != 1 || errs[0].Row != 2 || errs[0].Code != importer.CodeCodeTaken {
		t.Fatalf("lỗi %+v", errs)
	}
}

// Mutation testing (Task 403): biến thể sửa hay giữ nguyên mà plan thiếu ExistingID (không phải tạo mới) là "đã đổi",
// kể cả khi SKU vẫn còn và version khớp.
func TestRecheck_UpdateWithoutExistingID(t *testing.T) {
	t.Parallel()
	plan, now := recheckPlan()
	plan.Products[1].Variants[0].ExistingID = nil
	errs := importer.Recheck(plan, now)
	if len(errs) != 1 || errs[0].Row != 4 || errs[0].Column != importer.ColSKU ||
		errs[0].Code != importer.CodeChangedSinceValidation {
		t.Fatalf("lỗi %+v", errs)
	}
}
