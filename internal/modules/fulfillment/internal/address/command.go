package address

// Kết quả hãng trả cho lệnh sửa địa chỉ (cùng chuỗi với fulfillment.UpdateDone, UpdateRejected, UpdatePending).
const (
	carrierDone     = "done"
	carrierRejected = "rejected"
	carrierPending  = "pending"
)

// FromCarrier đổi kết quả hãng res (fulfillment.UpdateResult) ra trạng thái yêu cầu: done → confirmed (hãng đã nhận),
// rejected → rejected, pending → sent (hãng chưa trả lời chắc; không tra lại được ở mức chung, nhân viên xác nhận).
// Kết quả lạ → false (bên gọi coi như không rõ: in_doubt).
func FromCarrier(res string) (string, bool) {
	switch res {
	case carrierDone:
		return StatusConfirmed, true
	case carrierRejected:
		return StatusRejected, true
	case carrierPending:
		return StatusSent, true
	}
	return "", false
}

// Terminal là trạng thái yêu cầu khi lệnh kết thúc không có kết quả hãng (OnTerminal): in_doubt → unknown (không rõ
// hãng đã nhận chưa); dead, expired (hãng từ chối, chắc chưa gửi được) → rejected (nhân viên tạo yêu cầu mới hay gọi
// hãng). Trạng thái khác → "" (không đổi).
func Terminal(jobStatus string) string {
	switch jobStatus {
	case "in_doubt":
		return StatusUnknown
	case "dead", "expired":
		return StatusRejected
	}
	return ""
}
