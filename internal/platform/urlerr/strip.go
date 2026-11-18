// Package urlerr bỏ URL khỏi lỗi của net/http trước khi bọc hay ghi log: *url.Error chép nguyên URL của yêu cầu, mà
// URL có thể chứa token (Telegram bot) hay khóa của dịch vụ nhận nhịp sống. Chỉ dùng thư viện chuẩn, vì
// scripts/external-probe (chạy ngoài VPS, chỉ thư viện chuẩn) cũng nhập gói này.
//
// Khi nào không dùng: không thay cho bộ che dữ liệu cá nhân của logging (gói này chỉ bóc *url.Error); địa chỉ máy trong
// *net.OpError, *net.DNSError không được bóc — nơi cần thì tự xử lý thêm (như metrics.Pinger).
package urlerr

import (
	"errors"
	"net/url"
)

// Strip trả lỗi gốc bên trong *url.Error đầu tiên của chuỗi lỗi (bỏ Op, URL và phần bọc phía ngoài), hoặc err nguyên
// vẹn khi không có *url.Error. Lỗi hết hạn vẫn khớp errors.Is(err, context.DeadlineExceeded).
func Strip(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}
