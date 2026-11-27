package reporting

import (
	"time"

	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Nhãn tiếng Việt của dòng báo cáo (QĐ-230-3, theo chữ của mockup Task 224). Khóa của dòng vẫn là mã máy (nhãn gộp,
// khóa kênh, khóa hãng, ngày, uuid, unknown), nên màn hình đổi chữ được mà không đổi API.

// TotalKey là khóa của dòng tổng.
const TotalKey = "total"

const totalLabel = "Tổng"

// unknownLabels: nhãn của nhóm không xác định theo chiều (mặc định unknownLabel).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var unknownLabels = map[Dimension]string{DimCareBy: "Chưa phân công", DimCarrier: "Chưa gửi hãng"}

const unknownLabel = "Không xác định"

// statusLabels: chữ của 17 nhãn gộp (chú thích của orderview).
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var statusLabels = map[orderview.LabelKey]string{
	orderview.LabelDraft: "Nháp", orderview.LabelNew: "Mới", orderview.LabelAwaitingDiscountApproval: "Chờ duyệt giảm giá",
	orderview.LabelAwaitingCustomer: "Chờ khách xác nhận", orderview.LabelOutOfStock: "Hết hàng",
	orderview.LabelConfirmed: "Đã xác nhận", orderview.LabelPacking: "Đang đóng gói", orderview.LabelPacked: "Đã đóng gói",
	orderview.LabelSending: "Đang gửi hãng", orderview.LabelAwaitingPickup: "Chờ thu gom", orderview.LabelShipping: "Đang chuyển",
	orderview.LabelDeliveryFailed: "Thất bại", orderview.LabelReturning: "Đang chuyển hoàn",
	orderview.LabelCancelRequested: "Đang yêu cầu hủy", orderview.LabelCancelled: "Đã hủy",
	orderview.LabelDelivered: "Thành công", orderview.LabelReturned: "Đã hoàn",
}

// channelLabels: chữ của kênh đơn; kênh lạ giữ nguyên khóa.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var channelLabels = map[string]string{"facebook": "Facebook", "web": "Website", "manual": "Nhập tay"}

// carrierLabels: tên hãng theo khóa; khóa lạ giữ nguyên.
//
//nolint:gochecknoglobals // bảng hằng chỉ đọc
var carrierLabels = map[string]string{"vtp": "Viettel Post", "best": "BEST Express", "spx": "SPX Express", "self": "Tự giao"}

// rowLabel là nhãn của dòng có khóa key theo chiều d; name là tên đọc từ view (nguồn, nhân viên, Page), rỗng khi không có.
func rowLabel(d Dimension, key, name string) string {
	if key == UnknownKey {
		if l, ok := unknownLabels[d]; ok {
			return l
		}
		return unknownLabel
	}
	switch d {
	case DimStatus:
		return orDefault(statusLabels[orderview.LabelKey(key)], key)
	case DimChannel:
		return orDefault(channelLabels[key], key)
	case DimCarrier:
		return orDefault(carrierLabels[key], key)
	case DimDay:
		if t, err := time.Parse(dayLayout, key); err == nil {
			return t.Format("02/01/2006")
		}
		return key
	case DimSource, DimCareBy, DimCreatedBy, DimPage:
		return orDefault(name, unknownLabel)
	}
	return key
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
