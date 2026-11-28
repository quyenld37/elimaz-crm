package reporting_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/platform/xlsx/xlsxtest"
)

// productHeader là tiêu đề của tệp danh sách sản phẩm: cột cùng nghĩa trùng tên, thứ tự với tệp mẫu nhập của M1
// (catalog/internal/importer: Mã mẫu*, Tên mẫu*, Trạng thái, Màu*, Size*, SKU*, Barcode, Giá bán*), thêm hai cột ở cuối.
func productHeader() []string {
	return []string{
		"Mã mẫu*", "Tên mẫu*", "Trạng thái", "Màu*", "Size*", "SKU*", "Barcode", "Giá bán*",
		"Trạng thái biến thể", "Có thể bán",
	}
}

// Danh sách sản phẩm (Q25, Q-X6): 3 mẫu, 5 biến thể của người bán own (kịch bản chín đơn + AOA-L) và 1 mẫu của người bán
// partner → 5 dòng, không mẫu partner; ô giá là số; "Có thể bán" = tổng available trên các kho trong phạm vi (người chỉ
// có kho W1 → chỉ W1); tên mẫu là công thức → ô bắt đầu bằng "'"; đọc lại bằng xlsx.Read với tiêu đề của tệp mẫu nhập →
// giá trị đúng; không cột giá vốn; tên tệp danh-sach-san-pham_<hôm nay>.xlsx.
func TestProductsExport_File(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	own := e.seed.Own()
	large := e.seed.Variant(own, "Áo A", "AOA-L")
	partner := e.seed.Seller("DOITAC", "partner")
	e.seed.Variant(partner, "Đồ đối tác", "DT-1")
	w1, w2 := e.seed.Warehouse("W1"), e.seed.Warehouse("W2")
	at := time.Now().Add(-time.Hour)
	e.seed.Ledger(e.sc.ASmall, w1, "on_hand", 3, "import", at)
	e.seed.Ledger(e.sc.ASmall, w2, "on_hand", 5, "import", at)
	e.seed.Ledger(e.sc.PantsB, w1, "on_hand", 2, "import", at)
	e.seed.Ledger(large, w2, "on_hand", 9, "import", at)
	evil := `=HYPERLINK("http://evil","x")`
	e.ownerExec(t, `UPDATE catalog.products SET name = $1 WHERE name = 'Váy C'`, evil)

	u := e.user(t, "kho@elimaz.test", []string{"report.inventory.read"}, "warehouse", w1.String())
	e.st.Grant(t, e.tenant, u.UserID, []string{"report.export"}, "all", "")
	u = e.reload(t, u.UserID)
	run := e.mustCreate(t, u, xlsxInput(reporting.FamilyInventory, reporting.KeyProducts, `{}`))
	e.exec(t, e.xhandler(config.ReportConfig{}, nil, nil, nil), run.ID)
	r := e.exportRow(t, run.ID)
	if r.status != "ready" || r.rowCount == nil || *r.rowCount != 5 {
		t.Fatalf("lần chạy %+v", r)
	}
	if r.fileName != "danh-sach-san-pham_"+reporting.Today(time.Now())+".xlsx" || !keyPattern(e.tenant, run.ID).MatchString(r.key) {
		t.Errorf("tên tệp %q, khóa %q", r.fileName, r.key)
	}
	b, data := e.book(t, r.key)
	if parts := b.FormulaParts(); len(parts) != 0 {
		t.Errorf("có công thức: %v", parts)
	}
	checkProductCells(t, sheetRows(t, b), evil)
	got, order := productValues(t, data)
	want := map[string][]string{
		"AOA-S": {"Áo A", "Đang bán", "Mặc định", "AOA-S", "100000", "Đang bán", "3"},
		"AOA-M": {"Áo A", "Đang bán", "Mặc định", "AOA-M", "100000", "Đang bán", "0"},
		"AOA-L": {"Áo A", "Đang bán", "Mặc định", "AOA-L", "100000", "Đang bán", "0"},
		"QUANB": {"Quần B", "Đang bán", "Mặc định", "QUANB", "100000", "Đang bán", "2"},
		"VAYC":  {evil, "Đang bán", "Mặc định", "VAYC", "100000", "Đang bán", "0"},
	}
	if len(got) != len(want) {
		t.Errorf("SKU trong tệp = %v", got)
	}
	for sku, w := range want {
		if !slices.Equal(got[sku], w) {
			t.Errorf("%s = %q, mong %q", sku, got[sku], w)
		}
	}
	if !slices.IsSorted(order) {
		t.Errorf("thứ tự (mã mẫu, SKU) = %v", order)
	}
}

// checkProductCells: tiêu đề như tệp mẫu nhập, không cột giá vốn, ô công thức có dấu nháy đứng đầu, ô giá là số.
func checkProductCells(t *testing.T, raw []xlsxtest.Row, evil string) {
	t.Helper()
	if !slices.Equal(values(raw[0]), productHeader()) {
		t.Errorf("tiêu đề = %q", values(raw[0]))
	}
	for _, h := range values(raw[0]) {
		if strings.Contains(h, "nhập") || strings.Contains(h, "vốn") {
			t.Errorf("có cột giá vốn %q", h)
		}
	}
	for _, row := range raw[1:] {
		for _, c := range row.Cells {
			if strings.Contains(c.Value, "HYPERLINK") && c.Value != "'"+evil {
				t.Errorf("ô tên công thức = %q", c.Value)
			}
		}
		if price := row.Cells[7]; price.Type != "" {
			t.Errorf("ô giá %s kiểu %q, mong số", price.Ref, price.Type)
		}
	}
}

// productValues đọc lại tệp bằng bộ đọc của M1 với tiêu đề tệp mẫu nhập: giá trị theo SKU và thứ tự (mã mẫu/SKU).
func productValues(t *testing.T, data []byte) (map[string][]string, []string) {
	t.Helper()
	rows, cellErrs, err := xlsx.Read(bytes.NewReader(data), int64(len(data)), xlsx.DefaultLimits(), productHeader()[:8])
	if err != nil || len(cellErrs) != 0 {
		t.Fatalf("xlsx.Read: %v %v", err, cellErrs)
	}
	got := map[string][]string{}
	var order []string
	for _, row := range rows {
		c := row.Cells
		got[c["SKU*"]] = []string{c["Tên mẫu*"], c["Trạng thái"], c["Màu*"], c["Size*"], c["Giá bán*"], c["Trạng thái biến thể"], c["Có thể bán"]}
		order = append(order, c["Mã mẫu*"]+"/"+c["SKU*"])
	}
	return got, order
}

// Lọc trạng thái mẫu, tiền tố SKU; tham số lạ, trạng thái lạ → ErrBadParams; dạng json → ErrBadParams; không có
// report.export → ErrExportForbidden.
func TestProductsExport_Params(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	e.ownerExec(t, `UPDATE catalog.products SET status = 'discontinued' WHERE name = 'Quần B'`)
	u := e.exporter(t, "an@elimaz.test")
	h := e.xhandler(config.ReportConfig{}, nil, nil, nil)
	for _, c := range []struct {
		params string
		skus   []string
	}{
		{`{"status":"discontinued"}`, []string{"QUANB"}},
		{`{"q":"aoa"}`, []string{"AOA-M", "AOA-S"}},
		{`{"status":"active","q":"V"}`, []string{"VAYC"}},
	} {
		run := e.mustCreate(t, u, xlsxInput(reporting.FamilyInventory, reporting.KeyProducts, c.params))
		e.exec(t, h, run.ID)
		var skus []string
		for _, row := range sheetRows(t, mustBook(t, e, e.exportRow(t, run.ID).key))[1:] {
			skus = append(skus, row.Cells[5].Value)
		}
		slices.Sort(skus)
		if !slices.Equal(skus, c.skus) {
			t.Errorf("%s → %v, mong %v", c.params, skus, c.skus)
		}
	}
	noExport := e.user(t, "xem@elimaz.test", []string{"report.inventory.read"}, "all", "")
	e.expectRefused(t, []refusedRun{
		{"trường lạ", u, xlsxInput(reporting.FamilyInventory, reporting.KeyProducts, `{"name":"Áo"}`), reporting.ErrBadParams},
		{"trạng thái lạ", u, xlsxInput(reporting.FamilyInventory, reporting.KeyProducts, `{"status":"x"}`), reporting.ErrBadParams},
		{"dạng json", u, reporting.CreateRunInput{
			Family: reporting.FamilyInventory, Report: reporting.KeyProducts,
			Format: reporting.FormatJSON, Params: []byte(`{}`),
		}, reporting.ErrBadParams},
		{"không report.export", noExport, xlsxInput(reporting.FamilyInventory, reporting.KeyProducts, `{}`), reporting.ErrExportForbidden},
	})
}
