package order

import "github.com/google/uuid"

// ListSQL cho test kế hoạch (list_explain_test) dựng câu danh sách của List với q đã phân loại không cần database
// (mã đơn, SĐT; q là tên không dùng được ở đây) và nhóm khách roots (gốc của bộ lọc CustomerID).
func ListSQL(f ListFilter, q string, roots []uuid.UUID, c Cursor, limit int) (string, []any, error) {
	m, err := classifyQ(q)
	if err != nil {
		return "", nil, err
	}
	return listSQL(f, m, roots, c, limit)
}
