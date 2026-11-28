package reporting

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// productRow là một biến thể của danh sách sản phẩm; Available là tổng available trên các kho trong phạm vi.
type productRow struct {
	ProductCode, ProductName, ProductStatus string
	SKU                                     string
	Barcode                                 *string
	Color, Size, VariantStatus              string
	Price, Available                        int64
}

// productKey là vị trí keyset (mã mẫu, SKU) theo thứ tự byte.
type productKey struct{ code, sku string }

// productRows đọc tối đa limit biến thể của người bán own sau after (nil: từ đầu), sắp theo mã mẫu, SKU (COLLATE
// "C"); bỏ mẫu đã xóa; Available cộng inventory.stock_available_v trên các kho trong phạm vi report.inventory.read.
func productRows(ctx context.Context, tx pgx.Tx, s DataScope, p *ProductsExportParams, after *productKey, limit int) ([]productRow, error) {
	if s.SellerID == uuid.Nil {
		return nil, fmt.Errorf("danh sách sản phẩm thiếu người bán: %w", ErrBadParams)
	}
	b := &sqlBuilder{}
	join := "a.variant_id = v.variant_id AND a.seller_id = v.seller_id"
	if !s.AllWarehouses {
		join += " AND a.warehouse_id = ANY(" + b.Arg(append([]uuid.UUID{}, s.WarehouseIDs...)) + "::uuid[])"
	}
	where := "v.seller_id = " + b.Arg(s.SellerID) + " AND p.deleted_at IS NULL"
	if p.Status != nil {
		where += " AND v.product_status = " + b.Arg(*p.Status)
	}
	if prefix := strings.ToUpper(strings.TrimSpace(p.SKUPrefix)); prefix != "" {
		where += " AND v.sku ~>=~ " + b.Arg(prefix) + " AND v.sku ~<~ " + b.Arg(prefix+"\U0010FFFF")
	}
	if after != nil {
		where += ` AND (v.product_code COLLATE "C", v.sku COLLATE "C") > (` + b.Arg(after.code) + ", " + b.Arg(after.sku) + ")"
	}
	q := `SELECT v.product_code, v.product_name, v.product_status, v.sku, v.barcode, v.color, v.size, v.variant_status,
	v.price::bigint, coalesce(sum(a.available), 0)::bigint
FROM catalog.variants_v v
JOIN catalog.products_v p ON p.product_id = v.product_id AND p.seller_id = v.seller_id
LEFT JOIN inventory.stock_available_v a ON ` + join + `
WHERE ` + where + `
GROUP BY v.variant_id, v.product_code, v.product_name, v.product_status, v.sku, v.barcode, v.color, v.size,
	v.variant_status, v.price
ORDER BY v.product_code COLLATE "C", v.sku COLLATE "C"
LIMIT ` + b.Arg(limit)
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách sản phẩm: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (productRow, error) {
		var x productRow
		err := r.Scan(&x.ProductCode, &x.ProductName, &x.ProductStatus, &x.SKU, &x.Barcode, &x.Color, &x.Size,
			&x.VariantStatus, &x.Price, &x.Available)
		return x, err //nolint:wrapcheck // CollectRows trả lỗi, bọc ngay dưới
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng danh sách sản phẩm: %w", err)
	}
	return out, nil
}
