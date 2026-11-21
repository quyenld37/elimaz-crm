package vtp

import (
	"context"
	"fmt"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Track trả ErrNotSupported: tài liệu VTP không có API đọc hành trình (danh sách API: địa danh, dịch vụ, tính cước,
// tạo đơn, cập nhật đơn, cập nhật trạng thái, link in; trang "Đồng bộ hành trình" của bảng điều khiển là thao tác tay
// có captcha). Trạng thái chỉ về qua webhook, mà luật B10 không tin nội dung webhook: vận đơn VTP chờ người (điểm chờ
// chủ shop, docs/carriers/vtp.md mục Webhook).
func (*Adapter) Track(context.Context, fulfillment.Account, string, string) (fulfillment.Tracking, error) {
	return fulfillment.Tracking{}, fmt.Errorf("tra hành trình Viettel Post: %w", fulfillment.ErrNotSupported)
}

// GetStatus trả ErrNotSupported như Track (không có API đọc trạng thái).
func (*Adapter) GetStatus(context.Context, fulfillment.Account, string) (fulfillment.StatusResult, error) {
	return fulfillment.StatusResult{}, fmt.Errorf("đọc trạng thái Viettel Post: %w", fulfillment.ErrNotSupported)
}
