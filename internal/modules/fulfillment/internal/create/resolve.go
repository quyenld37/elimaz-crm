package create

// Kết quả nhân viên chọn khi xử lý "không rõ" (ResolveShipmentCreateRequest.result).
const (
	ResultCreated    = "created"
	ResultNotCreated = "not_created"
)

// ValidResolution cho biết kết quả hợp lệ: result là created (cần mã hãng) hay not_created (không mã hãng), lý do khác rỗng.
func ValidResolution(result, trackingCode, reason string) bool {
	if reason == "" {
		return false
	}
	switch result {
	case ResultCreated:
		return trackingCode != ""
	case ResultNotCreated:
		return trackingCode == ""
	}
	return false
}
