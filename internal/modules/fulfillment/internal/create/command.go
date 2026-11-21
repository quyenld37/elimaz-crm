package create

// Trạng thái kết thúc của lệnh (queue.StatusInDoubt, StatusDead, StatusExpired; nhắc lại ở đây để gói không nhập queue).
const (
	JobInDoubt = "in_doubt"
	JobDead    = "dead"
	JobExpired = "expired"
)

// Action là việc OnTerminal làm với vận đơn.
type Action int

// Các việc của OnTerminal.
const (
	// ActNone: không đổi gì.
	ActNone Action = iota
	// ActUnknown: create_state unknown, mở việc "không rõ đã tạo", cần người xem.
	ActUnknown
	// ActNotSent: create_state not_sent; vận đơn vẫn creating và gắn đơn, chờ người bấm "Gửi lại".
	ActNotSent
	// ActCancel: vận đơn cancelled, gỡ khỏi đơn (hãng từ chối, hay đơn đã yêu cầu hủy / đổi hãng mà hãng chưa tạo).
	ActCancel
)

// Terminal quyết việc khi lệnh tạo của vận đơn s kết thúc ở jobStatus; cancelIntent là ý định hủy của vận đơn (rỗng
// khi không có). Lệnh của lần gửi cũ, vận đơn đã qua creating → ActNone.
//
//   - in_doubt: lần gửi chưa có kết quả (pending) hay hãng vừa từ chối mà chưa ghi xong (rejected) → ActUnknown; đã
//     unknown hay not_sent → ActNone.
//   - dead, expired: hãng từ chối (rejected) → ActCancel; chắc chắn chưa tạo (pending: chưa gửi được; unknown: tra lại
//     xác nhận chưa có) → ActNotSent, hoặc ActCancel khi có ý định hủy.
func Terminal(s Shipment, jobStatus, cancelIntent string) Action {
	if !s.Creating || !s.Current {
		return ActNone
	}
	switch jobStatus {
	case JobInDoubt:
		if s.State == StatePending || s.State == StateRejected {
			return ActUnknown
		}
	case JobDead, JobExpired:
		switch s.State {
		case StateRejected:
			return ActCancel
		case StatePending, StateUnknown:
			if cancelIntent != "" {
				return ActCancel
			}
			return ActNotSent
		}
	}
	return ActNone
}

// CancelKind là shipments.cancel_kind khi Terminal trả ActCancel: ý định hủy của nhân viên nếu có, không thì
// create_rejected.
func CancelKind(cancelIntent string) string {
	if cancelIntent != "" {
		return cancelIntent
	}
	return "create_rejected"
}

// CancelState là create_state ghi cùng ActCancel: rejected khi hãng từ chối, not_sent khi chưa tạo.
func CancelState(s Shipment) string {
	if s.State == StateRejected {
		return StateRejected
	}
	return StateNotSent
}

// EffectiveIntent là ý định hủy dùng khi lệnh tạo kết thúc: ý định đã ghi trên vận đơn (cancelIntent) nếu có; không
// thì đơn đã yêu cầu hủy (orderCancelRequested: M2 đưa đơn confirmed có vận đơn sang cancel_requested) mà hãng không từ
// chối → order_cancel (vận đơn hủy ngay, đơn thành cancelled, nhả hàng). Hãng từ chối → rỗng: create_rejected, gỡ
// vận đơn (DetachShipment cũng hủy đơn cancel_requested).
func EffectiveIntent(s Shipment, cancelIntent string, orderCancelRequested bool) string {
	if cancelIntent != "" {
		return cancelIntent
	}
	if orderCancelRequested && s.State != StateRejected {
		return "order_cancel"
	}
	return ""
}
