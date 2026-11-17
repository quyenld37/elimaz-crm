package tenancy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// CheckSchemaRules đọc pg_catalog (bằng chủ bảng) và trả mỗi vi phạm một dòng "<schema>.<bảng>: <vấn đề>", xếp theo
// bảng; schema đúng luật trả slice rỗng. Bảng có cột tenant_id ngoài platformTables (tên "schema.bảng") phải: (a) bật
// và ép RLS, có policy, mọi policy đúng biểu thức NULLIF chuẩn; (b) có UNIQUE (tenant_id, id) nếu có cột id; (c) mọi
// unique khác khóa chính có tenant_id là cột đầu; (d) khóa ngoại tới bảng có tenant_id nối tenant_id với tenant_id.
// (e) Không bảng nào trong public (trừ bảng của extension).
func CheckSchemaRules(ctx context.Context, owner *pgxpool.Pool, platformTables []string) ([]string, error) {
	if platformTables == nil {
		platformTables = []string{}
	}
	out, err := store.New(owner).SchemaRuleViolations(ctx, platformTables)
	if err != nil {
		return nil, fmt.Errorf("kiểm quy tắc schema: %w", err)
	}
	return out, nil
}
