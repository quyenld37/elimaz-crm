package vtp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// kind là loại việc của một lời gọi: quyết định hạn giờ và nghĩa của lỗi sau khi đã gửi. Việc đổi đơn ở hãng (tạo,
// hủy, phát tiếp) có "không rõ hãng đã làm chưa"; đăng nhập, báo phí, địa giới, lấy mã in và tải nhãn không đổi gì ở
// hãng nên lỗi tạm là chưa gửi, gửi lại được.
type kind int

const (
	kindRead kind = iota
	kindQuote
	kindCreate
	kindLogin
	kindCancel
	kindUpdate
	kindLabel
)

// Hạn giờ client (Global Constraints M3): báo phí 10 s, tạo vận đơn 20 s, còn lại (tra, hủy, nhãn, đăng nhập) 15 s.
func (k kind) timeout() time.Duration {
	switch k {
	case kindQuote:
		return 10 * time.Second
	case kindCreate:
		return 20 * time.Second
	case kindRead, kindLogin, kindCancel, kindUpdate, kindLabel:
	}
	return 15 * time.Second
}

// changes: việc đổi đơn ở hãng — lỗi sau khi gửi là không rõ (in_doubt), không tự gửi lại.
func (k kind) changes() bool { return k == kindCreate || k == kindCancel || k == kindUpdate }

// Mã lỗi (CarrierError.Code) của adapter; mã của VTP là vtp_<status trong thân>.
const (
	codeToken        = "vtp_token"
	codeLogin        = "vtp_login"
	codeDuplicate    = "vtp_206_duplicate"
	codeUnmapped     = "address_unmapped"
	codeNoCreds      = "vtp_no_credentials" //nolint:gosec // G101: mã lỗi, không phải khóa
	codeEnvironment  = "unknown_environment"
	codeBadReply     = "bad_reply"
	codeTransport    = "transport"
	codeDial         = "dial"
	codeBuildRequest = "build_request"
	codeVTPError     = "vtp_error"
)

// Mã trong thân trả lời (khóa error-status của tài liệu).
const (
	vtpOK        = 200
	vtpCancelled = 201
	vtpToken     = 202
	vtpMissing   = 203
	vtpInvalid   = 204
	vtpSystem    = 205
	vtpDuplicate = 206
)

// retryAfterDefault là thời gian chờ khi hãng trả 429 mà không có Retry-After đọc được.
const retryAfterDefault = 30 * time.Second

var (
	errStatus        = errors.New("vtp: hãng trả lời lỗi")
	errUnknown       = errors.New("vtp: không rõ hãng đã tạo đơn chưa")
	errUnknownChange = errors.New("vtp: không rõ hãng đã cập nhật đơn chưa")
)

// tokenMessages là thông báo lỗi token trong bảng thông báo lỗi của tài liệu.
func tokenMessage(msg string) bool {
	switch strings.TrimSpace(msg) {
	case "Header Token is required", "Token invalid", "Token invalid or expired":
		return true
	}
	return false
}

// classifyHTTP phân lớp trả lời có mã HTTP khác 200 (tài liệu không nói, theo thông lệ): 401, 403 → lỗi token (bên
// gọi đăng nhập lại một lần rồi mới thành auth); 429 → rate_limited theo Retry-After; 503 → chưa gửi; 4xx khác → hãng
// từ chối; 5xx khác → tạo vận đơn không rõ, việc khác chưa gửi.
func classifyHTTP(k kind, status int, h http.Header) *fulfillment.CarrierError {
	code := "http_" + strconv.Itoa(status)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return authErr(codeToken)
	case status == http.StatusTooManyRequests:
		return &fulfillment.CarrierError{
			Class: fulfillment.ClassRateLimited, Code: code, RetryAfter: retryAfter(h), Err: fulfillment.ErrNotSent,
		}
	case status == http.StatusServiceUnavailable:
		return notSent(code)
	case status >= 400 && status < 500:
		return rejected(code)
	}
	return afterSend(k, code)
}

// classifyBody phân lớp phong bì có lỗi (status khác 200 hay error = true) theo bảng mã của tài liệu: 202 hay thông
// báo token → lỗi token; đăng nhập: 205 → chưa gửi, còn lại → sai tài khoản (auth); 206 khi tạo vận đơn → đơn đã có ở
// hãng, không biết mã (không rõ); 205 → tạo vận đơn không rõ, việc khác chưa gửi; 201, 203, 204, khác → từ chối.
// Thông báo của hãng không chép vào lỗi.
func classifyBody(k kind, status int, msg string) *fulfillment.CarrierError {
	code := "vtp_" + strconv.Itoa(status)
	switch {
	case k == kindLogin && status == vtpSystem:
		return notSent(code)
	case k == kindLogin:
		return authErr(codeLogin)
	case status == vtpToken || tokenMessage(msg):
		return authErr(codeToken)
	case k == kindCreate && status == vtpDuplicate:
		return &fulfillment.CarrierError{Class: fulfillment.ClassUnknownOutcome, Code: codeDuplicate, Err: errUnknown}
	case status == vtpSystem:
		return afterSend(k, code)
	case status == vtpOK:
		return rejected(codeVTPError)
	case status == vtpCancelled, status == vtpMissing, status == vtpInvalid:
	}
	return rejected(code)
}

// afterSend là lỗi sau khi yêu cầu đã tới hãng mà không có trả lời dùng được: việc đổi đơn (tạo, hủy, phát tiếp) →
// không rõ (in_doubt); việc khác → chưa gửi, gửi lại được.
func afterSend(k kind, code string) *fulfillment.CarrierError {
	switch {
	case k == kindCreate:
		return &fulfillment.CarrierError{Class: fulfillment.ClassUnknownOutcome, Code: code, Err: errUnknown}
	case k.changes():
		return &fulfillment.CarrierError{Class: fulfillment.ClassUnknownOutcome, Code: code, Err: errUnknownChange}
	}
	return notSent(code)
}

func notSent(code string) *fulfillment.CarrierError {
	return &fulfillment.CarrierError{Class: fulfillment.ClassNotSentRetry, Code: code, Err: fulfillment.ErrNotSent}
}

func rejected(code string) *fulfillment.CarrierError {
	return &fulfillment.CarrierError{Class: fulfillment.ClassRejected, Code: code, Err: fulfillment.ErrRejected}
}

// authErr: khóa sai hay bị thu hồi; bọc ErrRejected (không tự gửi lại, như hãng giả).
func authErr(code string) *fulfillment.CarrierError {
	return &fulfillment.CarrierError{Class: fulfillment.ClassAuth, Code: code, Err: fulfillment.ErrRejected}
}

// asNotSent đổi lỗi của bước trước khi gửi yêu cầu chính (đăng nhập, chọn dịch vụ) thành chưa gửi: yêu cầu chính chưa
// đi nên lỗi "không rõ" của bước đó không được làm vận đơn thành in_doubt. Lỗi auth, rate_limited, từ chối giữ nguyên.
func asNotSent(err error) error {
	ce, ok := errors.AsType[*fulfillment.CarrierError](err)
	if !ok {
		return &fulfillment.CarrierError{Class: fulfillment.ClassNotSentRetry, Code: codeTransport, Err: fulfillment.ErrNotSent}
	}
	if ce.Class == fulfillment.ClassUnknownOutcome {
		return notSent(ce.Code)
	}
	return err
}

// retryAfter đọc Retry-After dạng số giây (tài liệu VTP không nói; theo HTTP); thiếu hay không đọc được →
// retryAfterDefault.
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.ParseUint(strings.TrimSpace(h.Get("Retry-After")), 10, 31)
	if err != nil || secs == 0 {
		return retryAfterDefault
	}
	return time.Duration(secs) * time.Second
}
