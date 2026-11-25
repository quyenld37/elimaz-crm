package httpapi

// Bảng đổi lỗi của vùng conversation (kế hoạch M4) sang mã HTTP (luật Task 441: thêm lỗi cho vùng chỉ sửa tệp này,
// x-error-codes của openapi.yaml và errors_snapshot_test.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// conversationError đổi lỗi nghiệp vụ của hội thoại sang mã HTTP: hội thoại không có hoặc không có quyền trên Page của
// nó → 404 not_found (không lộ sự tồn tại, PQ04).
func conversationError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{conversation.ErrNotFound, http.StatusNotFound, codeNotFound},
	})
}
