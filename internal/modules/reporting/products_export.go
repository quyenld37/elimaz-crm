package reporting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Danh sách sản phẩm xuất Excel (Q25, Q-X6, Task 237). Báo cáo này chỉ xuất tệp: JSON luôn nil. Câu đọc ở
// products_export_dynsql.go.

// ProductsExportParams là tham số của danh sách sản phẩm: trạng thái mẫu (draft, active, discontinued; nil là mọi
// trạng thái) và tiền tố SKU (q, như báo cáo tồn). Không chữ tự do khác; trường lạ → ErrBadParams (DecodeParams).
type ProductsExportParams struct {
	Status    *string `json:"status,omitempty"`
	SKUPrefix string  `json:"q,omitempty"`
}

// Validate kiểm trạng thái và độ dài tiền tố SKU.
func (p *ProductsExportParams) Validate() error {
	if p.Status != nil {
		switch *p.Status {
		case "draft", "active", "discontinued":
		default:
			return fmt.Errorf("trạng thái mẫu %q: %w", *p.Status, ErrBadParams)
		}
	}
	if len(strings.TrimSpace(p.SKUPrefix)) > maxSKUPrefix {
		return fmt.Errorf("tiền tố SKU dài quá %d: %w", maxSKUPrefix, ErrBadParams)
	}
	return nil
}

func productsDef() Def {
	d := newDef[ProductsExportParams](KeyProducts, FamilyInventory, permInventoryRead)
	d.Rows, d.Columns, d.FileName = productsExportRows, productsColumns, productsFileName
	return d
}

// productsPage là số biến thể mỗi lần đọc của danh sách sản phẩm (keyset, không giữ cả bảng).
const productsPage = 1000

// Chữ trạng thái như tệp mẫu nhập của M1 (importer: Đang nhập, Đang bán, Ngừng bán).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var productStatusLabels = map[string]string{"draft": "Đang nhập", "active": "Đang bán", "discontinued": "Ngừng bán"}

// productsColumns: các cột cùng nghĩa trùng tên, thứ tự với tệp mẫu nhập của M1 (Mã mẫu*, Tên mẫu*, Trạng thái, Màu*,
// Size*, SKU*, Barcode, Giá bán*), thêm Trạng thái biến thể, Có thể bán ở cuối; không giá vốn.
func productsColumns(Params) []xlsx.Column {
	return []xlsx.Column{
		col("Mã mẫu*", xlsx.KindString), col("Tên mẫu*", xlsx.KindString), col("Trạng thái", xlsx.KindString),
		col("Màu*", xlsx.KindString), col("Size*", xlsx.KindString), col("SKU*", xlsx.KindString),
		col("Barcode", xlsx.KindString), col("Giá bán*", xlsx.KindMoney), col("Trạng thái biến thể", xlsx.KindString),
		col("Có thể bán", xlsx.KindInt),
	}
}

func productsFileName(Params) string { return "danh-sach-san-pham_" + Today(time.Now()) + ".xlsx" }

func productsExportRows(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error {
	pp, err := typed[ProductsExportParams](p)
	if err != nil {
		return err
	}
	var after *productKey
	for {
		rows, err := productRows(ctx, tx, s, pp, after, productsPage)
		if err != nil {
			return err
		}
		for _, r := range rows {
			barcode := xlsx.Empty()
			if r.Barcode != nil {
				barcode = xlsx.Str(*r.Barcode)
			}
			if err := emit([]xlsx.Cell{
				xlsx.Str(r.ProductCode), xlsx.Str(r.ProductName), xlsx.Str(orDefault(productStatusLabels[r.ProductStatus], r.ProductStatus)),
				xlsx.Str(r.Color), xlsx.Str(r.Size), xlsx.Str(r.SKU), barcode, xlsx.Money(r.Price),
				xlsx.Str(orDefault(productStatusLabels[r.VariantStatus], r.VariantStatus)), xlsx.Int(r.Available),
			}); err != nil {
				return err
			}
		}
		if len(rows) < productsPage {
			return nil
		}
		last := rows[len(rows)-1]
		after = &productKey{code: last.ProductCode, sku: last.SKU}
	}
}
