package vtp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Loại cập nhật của POST /v2/order/UpdateOrder (trang "Cập nhật trạng thái vận đơn"): 3 phát tiếp (đơn ở 505 "Thông
// báo chuyển hoàn"), 4 hủy (chỉ đơn chưa nhận thành công, ORDER_STATUS < 200). 1 duyệt đơn, 2 duyệt hoàn, 5 xóa đơn đã
// hủy: CRM không dùng.
const (
	updateRedeliver = 3
	updateCancel    = 4
	// maxNoteRunes là độ dài tối đa của NOTE ("không quá 150 ký tự").
	maxNoteRunes = 150
	cancelNote   = "Shop hủy đơn"
	codeNoTrack  = "vtp_no_tracking"
)

// updateBody là thân UpdateOrder.
type updateBody struct {
	Type        int    `json:"TYPE"`
	OrderNumber string `json:"ORDER_NUMBER"`
	Note        string `json:"NOTE,omitempty"`
}

// Cancel hủy vận đơn trackingCode (mã vận đơn VTP) bằng UpdateOrder TYPE 4: hãng nhận → CancelDone (trạng thái 107 về
// qua webhook). Hãng từ chối (đơn đã lấy, đã hủy, mã sai) → lỗi lớp rejected: tài liệu không có mã lỗi riêng cho "đã
// lấy hàng" nên không trả CancelRejectedPickedUp (người xem việc hủy, docs/carriers/vtp.md). Quá giờ, 205, 5xx, thân
// không đọc được → lỗi không rõ (in_doubt, không gửi lại); hủy lại đơn đã hủy chưa kiểm chứng nên CancelIdempotent false.
func (a *Adapter) Cancel(ctx context.Context, acc fulfillment.Account, trackingCode string) (fulfillment.CancelResult, error) {
	if trackingCode == "" {
		return "", fmt.Errorf("hủy vận đơn Viettel Post: %w", rejected(codeNoTrack))
	}
	body := updateBody{Type: updateCancel, OrderNumber: trackingCode, Note: cancelNote}
	if err := a.call(ctx, acc, kindCancel, http.MethodPost, "/v2/order/UpdateOrder", body, nil); err != nil {
		return "", fmt.Errorf("hủy vận đơn Viettel Post: %w", err)
	}
	return fulfillment.CancelDone, nil
}

// Redeliver yêu cầu phát tiếp vận đơn trackingCode bằng UpdateOrder TYPE 3 (chỉ khi đơn ở 505), ghi chú cắt còn 150
// ký tự: hãng nhận → UpdateDone; hãng từ chối (đơn không ở 505, mã sai) → UpdateRejected, không lỗi; lỗi khác theo
// lớp của nó (quá giờ → không rõ).
func (a *Adapter) Redeliver(ctx context.Context, acc fulfillment.Account, trackingCode, note string) (fulfillment.UpdateResult, error) {
	if trackingCode == "" {
		return "", fmt.Errorf("phát tiếp vận đơn Viettel Post: %w", rejected(codeNoTrack))
	}
	body := updateBody{Type: updateRedeliver, OrderNumber: trackingCode, Note: truncateRunes(note, maxNoteRunes)}
	err := a.call(ctx, acc, kindUpdate, http.MethodPost, "/v2/order/UpdateOrder", body, nil)
	if ce, ok := errors.AsType[*fulfillment.CarrierError](err); ok && ce.Class == fulfillment.ClassRejected {
		return fulfillment.UpdateRejected, nil
	}
	if err != nil {
		return "", fmt.Errorf("phát tiếp vận đơn Viettel Post: %w", err)
	}
	return fulfillment.UpdateDone, nil
}

// truncateRunes cắt s còn tối đa n ký tự (rune), không cắt giữa một chữ có dấu.
func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
