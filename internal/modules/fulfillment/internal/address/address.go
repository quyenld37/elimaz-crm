// Package address là phần quyết định thuần của đổi địa chỉ sau khi đã gửi hãng (kế hoạch M3, Task M3-17, DH15): không
// database, không gọi hãng. fulfillment (area_address.go, address_command.go) khóa vận đơn, yêu cầu rồi hỏi gói này vận
// đơn có đổi được địa chỉ không, nhân viên được xác nhận tay yêu cầu nào; command.go đổi kết quả của lệnh sửa địa chỉ ở
// hãng ra trạng thái của yêu cầu.
//
// Khi nào không dùng: không gọi từ module khác (gói internal của fulfillment); kiểm dạng địa chỉ nằm ở
// order.CheckShipAddress, khớp mã địa giới của hãng ở fulfillment (checkMapped), không lặp ở đây.
package address

import "github.com/google/uuid"

// Trạng thái của yêu cầu (CHECK của fulfillment.address_change_requests.status).
const (
	StatusPending   = "pending"
	StatusSent      = "sent"
	StatusConfirmed = "confirmed"
	StatusRejected  = "rejected"
	StatusUnknown   = "unknown"
)

// Chế độ của yêu cầu: api (lệnh sửa địa chỉ ở hãng), manual (nhân viên gọi hãng rồi xác nhận tay).
const (
	ModeAPI    = "api"
	ModeManual = "manual"
)

// Changeable cho biết vận đơn ở trạng thái status còn đổi được địa chỉ giao: hàng ở hãng mà chưa giao xong
// (awaiting_pickup, shipping, delivery_failed). Đang tạo, đang hủy, đã hủy, đã giao, đang hoàn → không.
func Changeable(status string) bool {
	switch status {
	case "awaiting_pickup", "shipping", "delivery_failed":
		return true
	}
	return false
}

// Open cho biết yêu cầu ở trạng thái status còn mở (khớp index address_change_requests_open: một yêu cầu mở mỗi vận đơn).
func Open(status string) bool {
	return status == StatusPending || status == StatusSent || status == StatusUnknown
}

// Resolvable cho biết nhân viên được xác nhận tay kết quả của yêu cầu (mode, status): yêu cầu manual còn chờ, yêu cầu
// đã gửi hãng mà hãng chưa trả lời chắc (sent) hay không rõ đã gửi chưa (unknown). Yêu cầu api còn chờ lệnh: không.
func Resolvable(mode, status string) bool {
	switch status {
	case StatusPending:
		return mode == ModeManual
	case StatusSent, StatusUnknown:
		return true
	}
	return false
}

// Resolution đổi kết quả nhân viên chọn (resolveAddressChange.result) ra trạng thái yêu cầu; chỉ confirmed, rejected.
func Resolution(result string) (string, bool) {
	switch result {
	case StatusConfirmed, StatusRejected:
		return result, true
	}
	return "", false
}

// DedupeKey là khóa chống trùng của lệnh sửa địa chỉ cho yêu cầu id.
func DedupeKey(id uuid.UUID) string { return "address:" + id.String() }
