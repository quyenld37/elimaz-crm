package catalog_test

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// propertyChecks đặt số lượt rapid của một test về n (TestMain của gói đặt 500 cho bộ dựng HTML), trừ khi người chạy
// đã đặt RAPID_CHECKS; trả lại giá trị cũ khi test xong. Test gọi hàm này không chạy song song.
func propertyChecks(t *testing.T, n int) {
	t.Helper()
	if os.Getenv("RAPID_CHECKS") != "" {
		return
	}
	old := flag.Lookup("rapid.checks").Value.String()
	if err := flag.Set("rapid.checks", fmt.Sprint(n)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flag.Set("rapid.checks", old) })
}

// Quy luật tất cả hoặc không (KH01, 100 lượt): plan ngẫu nhiên 1–30 mẫu (1–3 biến thể mỗi mẫu); có khi một dòng ngẫu
// nhiên bị cài lỗi lúc áp (SKU của dòng đó vừa bị mẫu khác lấy) → số mẫu, biến thể trong database hoặc đúng bằng
// trước (failed), hoặc đúng bằng trước + plan (applied); không có trạng thái nửa chừng.
func TestApply_AllOrNothingProperty(t *testing.T) {
	propertyChecks(t, 100)
	e := newImportEnv(t)
	run := 0
	rapid.Check(t, func(rt *rapid.T) {
		run++
		models := rapid.IntRange(1, 30).Draw(rt, "models")
		var rows []xlsx.Row
		for m := range models {
			for v := range rapid.IntRange(1, 3).Draw(rt, fmt.Sprintf("variants%d", m)) {
				rows = append(rows, xlsx.Row{Number: len(rows) + 2, Cells: map[string]string{
					"Mã mẫu*": fmt.Sprintf("R%d-M%d", run, m), "Tên mẫu*": "Mẫu thử", "Màu*": "Đỏ",
					"Size*": []string{"S", "M", "L"}[v], "SKU*": fmt.Sprintf("R%d-M%d-%d", run, m, v),
					"Giá bán*": fmt.Sprint(rapid.IntRange(0, 2_000_000).Draw(rt, "price")),
				}})
			}
		}
		imp := e.validate(writeRows(t, basicHeader(), rows))
		if imp.Status != catalog.ImportReady {
			rt.Fatalf("%s %+v", imp.Status, imp.Errors)
		}
		broken := rapid.Bool().Draw(rt, "broken")
		if broken {
			r := rows[rapid.IntRange(0, len(rows)-1).Draw(rt, "row")]
			if _, err := e.create(catalog.ProductInput{
				SellerID: e.seller, Code: fmt.Sprintf("X%d", run), Name: "Chen ngang",
				Variants: []catalog.VariantInput{{SKU: r.Cells["SKU*"], Color: "Xanh", Size: "S", Price: 1}},
			}); err != nil {
				rt.Fatal(err)
			}
		}
		products, variants := e.products(), e.count("SELECT count(*) FROM catalog.variants")
		got := e.applyNow(imp)
		dp, dv := e.products()-products, e.count("SELECT count(*) FROM catalog.variants")-variants
		switch {
		case broken && (got.Status != catalog.ImportFailed || dp != 0 || dv != 0 || len(got.Errors) == 0):
			rt.Fatalf("có lỗi cài: %s, +%d mẫu, +%d biến thể, lỗi %+v", got.Status, dp, dv, got.Errors)
		case !broken && (got.Status != catalog.ImportApplied || dp != models || dv != len(rows)):
			rt.Fatalf("không lỗi: %s, +%d/%d mẫu, +%d/%d biến thể", got.Status, dp, models, dv, len(rows))
		}
	})
}
