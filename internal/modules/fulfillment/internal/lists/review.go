package lists

const (
	kindInDoubtCreate = "in_doubt_create"
	kindInDoubtCancel = "in_doubt_cancel"
	kindManualCancel  = "manual_cancel"
	// genericLabel: loại chưa có câu riêng (task sau thêm giá trị vào CHECK kind).
	genericLabel = "Vận đơn cần người xem — mở vận đơn, đối chiếu với trang hãng rồi đóng việc kèm cách đã xử lý"
)

// kindLabels: câu tiếng Việt và hướng xử lý (sau " — ") của từng loại anomaly, theo thứ tự CHECK kind của bảng
// (migration 0300, 0312).
func kindLabels() [][2]string {
	return [][2]string{
		{kindInDoubtCreate, "Không rõ hãng đã tạo vận đơn chưa — kiểm trên trang hãng rồi bấm Đã tạo / Chưa tạo"},
		{kindInDoubtCancel, "Không rõ hãng đã hủy vận đơn chưa — kiểm trên trang hãng rồi bấm Ghi tay trạng thái: " +
			"Hãng đã hủy / Hãng không cho hủy"},
		{kindManualCancel, "Hãng chưa hủy được qua API — hủy trên trang hãng rồi bấm Ghi tay trạng thái: Hãng đã hủy " +
			"(hãng không cho hủy: Hãng không cho hủy)"},
		{"invalid_edge", "Hãng báo trạng thái không đi tiếp được từ trạng thái hiện tại — đối chiếu lịch sử trên trang " +
			"hãng, sửa tay trạng thái nếu cần"},
		{"unmapped_status", "Hãng báo trạng thái CRM chưa biết — xem trạng thái trên trang hãng rồi cập nhật tay"},
		{"tracking_conflict", "Mã hãng trùng hoặc khác với mã đã lưu — kiểm mã trên trang hãng rồi sửa vận đơn"},
		{"cancel_conflict", "Đã yêu cầu hủy nhưng hãng vẫn báo đang đi — liên hệ hãng chặn hàng, chờ hoàn"},
		{"partial_reported", "Hãng báo giao một phần — xác nhận số món khách nhận rồi ghi giao một phần"},
		{"lost_or_damaged", "Hãng báo mất hoặc hỏng hàng — liên hệ hãng làm hồ sơ bồi thường"},
		{"missing_items", "Hàng hoàn về thiếu món — đối chiếu với hãng, ghi nhận phần thiếu"},
		{"not_current_moved", "Vận đơn cũ (không còn là vận đơn của đơn) vẫn có hàng đi — kiểm trên trang hãng, chặn " +
			"hoặc nhận hoàn"},
		{"stock_short", "Hãng đã lấy hoặc giao nhưng tồn thực không đủ — điều chỉnh tồn kho rồi đóng việc"},
	}
}

// Kinds trả các loại anomaly có câu riêng, theo thứ tự CHECK kind của bảng.
func Kinds() []string {
	ls := kindLabels()
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l[0])
	}
	return out
}

// Label trả câu tiếng Việt kèm hướng xử lý của loại kind; loại lạ → câu chung (không bao giờ rỗng).
func Label(kind string) string {
	for _, l := range kindLabels() {
		if l[0] == kind {
			return l[1]
		}
	}
	return genericLabel
}

// Resolvable cho biết việc loại kind có được đóng bằng "Đã xử lý" (resolveShipmentAnomaly). Không được, vì kết quả phải
// đổi vận đơn và đường riêng tự đóng việc: in_doubt_create (resolveShipmentCreate: Đã tạo / Chưa tạo); manual_cancel,
// in_doubt_cancel (setShipmentManualStatus: Hãng đã hủy / Hãng không cho hủy — chỉ mở khi việc hủy tay còn mở, nên
// đóng trước thì vận đơn kẹt ở cancel_requested).
func Resolvable(kind string) bool {
	switch kind {
	case kindInDoubtCreate, kindManualCancel, kindInDoubtCancel:
		return false
	}
	return true
}

// InDoubtKinds là các loại "không rõ" (counts.inDoubt).
func InDoubtKinds() []string { return []string{kindInDoubtCreate, kindInDoubtCancel} }
