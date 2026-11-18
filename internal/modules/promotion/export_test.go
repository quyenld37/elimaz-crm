package promotion

// ErrOrderHasCoupon cho test gói ngoài: đơn đã có lượt mã đang hiệu lực (lỗi lập trình, không ánh xạ HTTP).
var ErrOrderHasCoupon = errOrderHasCoupon

// WithCodeGen trả w sinh mã bằng gen thay cho NewCode (test ca mã sinh trùng).
func WithCodeGen(w Writer, gen func(prefix string) (string, error)) Writer {
	w.newCode = gen
	return w
}
