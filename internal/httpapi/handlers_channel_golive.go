package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Kênh: mở gửi Page, trả về chỉ xem (MO06, Task M7-22 = 267, để sau).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GoLivePage (POST /api/v1/channels/{id}/go-live).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GoLivePage(_ context.Context, _ *gen.GoLivePageRequest, _ gen.GoLivePageParams) (gen.GoLivePageRes, error) {
	return nil, ht.ErrNotImplemented
}

// PageBackToReadOnly (POST /api/v1/channels/{id}/back-to-read-only).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) PageBackToReadOnly(_ context.Context, _ *gen.PageBackToReadOnlyRequest, _ gen.PageBackToReadOnlyParams) (gen.PageBackToReadOnlyRes, error) {
	return nil, ht.ErrNotImplemented
}
