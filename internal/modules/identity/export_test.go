package identity

// AuditPayload mở auditPayload cho test quy luật của gói identity_test.
func AuditPayload(before, after map[string]any) (changed []string, beforeJSON, afterJSON []byte, err error) {
	return auditPayload(before, after)
}

// SymmetricDiff mở symmetricDiff cho test quy luật quản lý nhân viên.
func SymmetricDiff(a, b []GrantInput) []GrantInput { return symmetricDiff(a, b) }
