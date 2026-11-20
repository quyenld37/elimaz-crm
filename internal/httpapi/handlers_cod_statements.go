package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng cod_statements (kế hoạch M3, Task M3-31, M3-32): bảng kê COD, lệch đối soát, quá hạn chưa có tiền.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ImportCodStatement (POST /api/v1/cod-statements).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ImportCodStatement(_ context.Context, _ *gen.ImportCodStatementRequest, _ gen.ImportCodStatementParams) (gen.ImportCodStatementRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListCodStatements (GET /api/v1/cod-statements).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCodStatements(_ context.Context, _ gen.ListCodStatementsParams) (gen.ListCodStatementsRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetCodStatement (GET /api/v1/cod-statements/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetCodStatement(_ context.Context, _ gen.GetCodStatementParams) (gen.GetCodStatementRes, error) {
	return nil, ht.ErrNotImplemented
}

// ConfirmCodStatement (POST /api/v1/cod-statements/{id}/confirm).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ConfirmCodStatement(_ context.Context, _ *gen.VersionRequest, _ gen.ConfirmCodStatementParams) (gen.ConfirmCodStatementRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListCodDiscrepancies (GET /api/v1/cod/discrepancies).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCodDiscrepancies(_ context.Context, _ gen.ListCodDiscrepanciesParams) (gen.ListCodDiscrepanciesRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListCodOverdue (GET /api/v1/cod/overdue).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCodOverdue(_ context.Context, _ gen.ListCodOverdueParams) (gen.ListCodOverdueRes, error) {
	return nil, ht.ErrNotImplemented
}
