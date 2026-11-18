package catalog

import (
	"testing"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/importer"
)

// TestImporterRulesMatchCatalog: importer giữ bản sao luật dữ liệu của catalog (không nhập ngược được gói này); bản
// sao lệch thì tệp Excel được báo "không lỗi" mà lúc ghi (Task M1-47) lại hỏng, hoặc ngược lại.
func TestImporterRulesMatchCatalog(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"mẫu mã, SKU", importer.CodePattern.String(), codePattern.String()},
		{"barcode", importer.BarcodePattern.String(), barcodePattern.String()},
		{"mã màu", importer.ColorHexPattern.String(), colorHexPattern.String()},
		{"tên", importer.MaxName, maxName},
		{"mô tả", importer.MaxDescription, maxDescription},
		{"màu, size", importer.MaxOption, maxOption},
		{"số biến thể", importer.MaxVariants, maxVariants},
		{"giá tối đa", int64(importer.MaxMoney), MaxMoney},
		{"draft", importer.StatusDraft, string(StatusDraft)},
		{"active", importer.StatusActive, string(StatusActive)},
		{"discontinued", importer.StatusDiscontinued, string(StatusDiscontinued)},
	} {
		if c.got != c.want {
			t.Errorf("%s: importer %v, catalog %v", c.name, c.got, c.want)
		}
	}
}
