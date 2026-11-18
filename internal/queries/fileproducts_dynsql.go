package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FileProduct là mẫu đang dùng một tệp làm ảnh (danh sách tệp mất, Task M1-46, TP12).
type FileProduct struct {
	ProductID uuid.UUID
	Code      string
}

// fileProductsSQL: mỗi tệp một mẫu (ảnh chưa xóa); nhiều mẫu cùng dùng một tệp thì ưu tiên mẫu chưa xóa, rồi mã mẫu
// nhỏ nhất. Câu cố định (không dựng lúc chạy); nằm ở đây vì gói queries không có khối sqlc.
const fileProductsSQL = `
SELECT DISTINCT ON (i.file_id) i.file_id, p.product_id, p.code
FROM catalog.product_images_v i
JOIN catalog.products_v p ON p.product_id = i.product_id
WHERE i.file_id = ANY($1::uuid[])
ORDER BY i.file_id, p.deleted_at IS NOT NULL, p.code, p.product_id`

// FileProducts trả mẫu dùng từng tệp trong fileIDs làm ảnh (tệp không gắn mẫu nào thì không có trong kết quả), đọc qua
// catalog.product_images_v và catalog.products_v trong tenant của tx (tenancy.WithTenant). Mọi người bán của tenant:
// bên gọi là việc quản trị kho tệp (platform.files.manage), không phải màn của một người bán.
func FileProducts(ctx context.Context, tx pgx.Tx, fileIDs []uuid.UUID) (map[uuid.UUID]FileProduct, error) {
	out := make(map[uuid.UUID]FileProduct, len(fileIDs))
	if len(fileIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, fileProductsSQL, fileIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu theo tệp: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var file uuid.UUID
		var p FileProduct
		if err := rows.Scan(&file, &p.ProductID, &p.Code); err != nil {
			return nil, fmt.Errorf("đọc mẫu theo tệp: %w", err)
		}
		out[file] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("đọc mẫu theo tệp: %w", err)
	}
	return out, nil
}
