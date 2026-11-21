package identity

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"
)

// AuditPayload mở auditPayload cho test quy luật của gói identity_test.
func AuditPayload(before, after map[string]any) (changed []string, beforeJSON, afterJSON []byte, err error) {
	return auditPayload(before, after)
}

// SymmetricDiff mở symmetricDiff cho test quy luật quản lý nhân viên.
func SymmetricDiff(a, b []GrantInput) []GrantInput { return symmetricDiff(a, b) }

// CreateServiceKeyWith mở createServiceKey với nguồn ngẫu nhiên của test (tiền tố trùng có chủ đích).
func CreateServiceKeyWith(ctx context.Context, tx pgx.Tx, in CreateServiceKeyInput, r io.Reader) (ServiceKey, string, error) {
	return createServiceKey(ctx, tx, in, r)
}
