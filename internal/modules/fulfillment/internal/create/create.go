// Package create là phần quyết định thuần của lệnh tạo vận đơn ở hãng (kế hoạch M3, Task M3-12): không database,
// không gọi hãng. fulfillment (create_shipment.go, area_create.go) đọc dữ liệu, gọi hãng, ghi kết quả; gói này chỉ
// nói lệnh được làm gì với một vận đơn ở trạng thái nào, để mọi đường (Execute, OnTerminal, gửi lại, xử lý "không rõ")
// dùng một bảng quyết định có test bảng và test quy luật.
//
// Khi nào không dùng: không gọi từ module khác (gói internal của fulfillment); phân lớp lỗi gọi hãng nằm ở fulfillment
// (cần kiểu lỗi của adapter).
package create

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Giá trị của fulfillment.shipments.create_state (CHECK của migration 0300).
const (
	StatePending  = "pending"
	StateNotSent  = "not_sent"
	StateUnknown  = "unknown"
	StateCreated  = "created"
	StateRejected = "rejected"
	StateManual   = "manual"
)

// DedupeKey là khóa chống trùng của lệnh tạo cho lần gửi attempt (1 là lần đầu): mỗi lần "Gửi lại" một lệnh mới.
func DedupeKey(shipmentID uuid.UUID, attempt int) string {
	return "create:" + shipmentID.String() + ":" + strconv.Itoa(attempt)
}

// Shipment là phần của vận đơn mà các quyết định cần: còn creating không, create_state, và lệnh đang xét có phải lệnh
// của lần gửi hiện tại (create_job_id khớp, hay vận đơn của M1 chưa có create_job_id).
type Shipment struct {
	Creating bool
	State    string
	Current  bool
}

// Gate là việc Execute được làm.
type Gate int

// Các việc của Execute.
const (
	// GateStale: không gửi, không tra (vận đơn đã qua creating, đã có kết quả, hay lệnh của lần gửi cũ).
	GateStale Gate = iota
	// GateSend: gửi hãng (lần gửi đang chờ kết quả).
	GateSend
	// GateLookup: "không rõ" (lệnh in_doubt được người chạy lại): tra hãng theo mã trước, không gửi lần hai.
	GateLookup
)

// ExecuteGate trả việc Execute được làm với vận đơn s.
func ExecuteGate(s Shipment) Gate {
	if !s.Creating || !s.Current {
		return GateStale
	}
	switch s.State {
	case StatePending:
		return GateSend
	case StateUnknown:
		return GateLookup
	}
	return GateStale
}

// CanResend cho biết "Gửi lại" được không: chỉ khi vận đơn creating và chắc chắn hãng chưa tạo.
func CanResend(creating bool, state string) bool { return creating && state == StateNotSent }

// CanResolve cho biết xử lý "không rõ" được không: chỉ khi vận đơn creating và create_state unknown.
func CanResolve(creating bool, state string) bool { return creating && state == StateUnknown }

// ManualTrackingCode là mã hãng của vận đơn tạo bằng tài khoản nhập tay (Task M3-15): hãng "tự giao" (self) không nhận
// mã nhập, mã hãng là mã vận đơn nội bộ code; hãng khác bắt buộc mã nhân viên dán given (cắt khoảng trắng hai đầu).
// ok = false khi đầu vào sai (self có mã nhập, hãng khác thiếu mã).
func ManualTrackingCode(self bool, code, given string) (string, bool) {
	given = strings.TrimSpace(given)
	if self {
		if given != "" {
			return "", false
		}
		return code, true
	}
	return given, given != ""
}
