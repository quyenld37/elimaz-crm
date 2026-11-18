package pricing

// RefundFor là tiền hoàn khi trả returnNow đơn vị của một dòng có doanh thu net (LineTotals.Net) và số lượng qty, khi
// trước đó đã trả returnedBefore đơn vị và đã hoàn refundedBefore đồng. Lần trả không phải lần cuối: floor(net ×
// returnNow / qty). Lần trả hết dòng (returnedBefore + returnNow == qty): net − refundedBefore, nên đơn vị cuối nhận
// phần lẻ và tổng mọi lần hoàn đúng bằng net.
//
// Đầu vào sai (qty ≤ 0, returnNow ≤ 0, số âm, trả quá số còn lại, đã hoàn quá net) trả 0; bên gọi kiểm trước
// (Task M2-22). Kết quả không bao giờ âm và không vượt net − refundedBefore.
func RefundFor(net, qty, returnedBefore, returnNow, refundedBefore int64) int64 {
	if net < 0 || qty <= 0 || returnedBefore < 0 || returnNow <= 0 || returnNow > qty-returnedBefore ||
		refundedBefore < 0 || refundedBefore > net {
		return 0
	}
	left := net - refundedBefore
	if returnedBefore+returnNow == qty {
		return left
	}
	part := mulDiv(net, returnNow, qty) // returnNow < qty nên thương < net
	return min(part, left)
}
