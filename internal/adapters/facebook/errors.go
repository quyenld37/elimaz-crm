package facebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// SentState cho biết yêu cầu đã tới Meta chưa: not_sent chỉ khi yêu cầu chưa được ghi trọn lên kết nối.
type SentState string

// Trạng thái gửi.
const (
	NotSent   SentState = "not_sent"
	MaybeSent SentState = "maybe_sent"
)

// minRateLimitWait là thời gian chờ tối thiểu khi Meta giới hạn số lần gọi.
const minRateLimitWait = time.Minute

// GraphError là lỗi của một lời gọi Graph: lỗi JSON của Meta (Code, Subcode, Type, TraceID), HTTP lỗi không có JSON,
// hoặc lỗi mạng (HTTPStatus và Code bằng 0). Message đã lọc dữ liệu cá nhân và token (observability.ScrubMessage).
// RetryAfter là thời gian Meta báo phải chờ (header mức dùng, Retry-After), 0 khi không có.
type GraphError struct {
	HTTPStatus, Code, Subcode int
	Type, TraceID, Message    string
	Sent                      SentState
	RetryAfter                time.Duration
	// build: lỗi khi dựng yêu cầu (thân không mã hóa được, URL sai) — lỗi của mã, chưa gọi Meta, không thử lại.
	build bool
	err   error
}

func (e *GraphError) Error() string {
	if e.HTTPStatus == 0 && e.Code == 0 {
		return fmt.Sprintf("graph: lỗi mạng (%s): %s", e.Sent, e.Message)
	}
	return fmt.Sprintf("graph: HTTP %d, mã %d/%d %s: %s", e.HTTPStatus, e.Code, e.Subcode, e.Type, e.Message)
}

// Unwrap trả lỗi gốc (lỗi mạng, hết hạn) để errors.Is(err, context.DeadlineExceeded) khớp.
func (e *GraphError) Unwrap() error { return e.err }

func newGraphError(status, code, subcode int, typ, trace, msg string) *GraphError {
	return &GraphError{
		HTTPStatus: status, Code: code, Subcode: subcode, Type: typ, TraceID: trace, Message: scrub(msg), Sent: MaybeSent,
	}
}

// tokenRe là dạng token truy cập của Meta (bắt đầu "EAA"): Meta có khi chép token vào message lỗi.
var tokenRe = regexp.MustCompile(`EAA[A-Za-z0-9_-]{8,}`)

// scrub che token Meta rồi lọc dữ liệu cá nhân, cắt độ dài (observability.ScrubMessage).
func scrub(s string) string {
	return observability.ScrubMessage(tokenRe.ReplaceAllString(s, "[Redacted]"))
}

// redactToken che đúng token của lời gọi trong message (token thử hay dạng lạ mà tokenRe không bắt).
func redactToken(s string, token logging.Secret) string {
	if len(token) < 4 {
		return s
	}
	return strings.ReplaceAll(s, string(token), "[Redacted]")
}

func asGraphError(err error) (*GraphError, bool) { return errors.AsType[*GraphError](err) }

// decodeError giải {"error":{…}} của Meta; HTTP ≥ 400 không có JSON lỗi thì chỉ có HTTPStatus; trả lời tốt thì nil.
func decodeError(status int, raw []byte, token logging.Secret) *GraphError {
	var env struct {
		Error *struct {
			Message   string `json:"message"`
			Type      string `json:"type"`
			Code      int    `json:"code"`
			Subcode   int    `json:"error_subcode"`
			FBTraceID string `json:"fbtrace_id"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Error != nil && env.Error.Code != 0 {
		e := env.Error
		return newGraphError(status, e.Code, e.Subcode, e.Type, e.FBTraceID, redactToken(e.Message, token))
	}
	if status >= http.StatusBadRequest {
		return &GraphError{HTTPStatus: status, Sent: MaybeSent, Message: http.StatusText(status)}
	}
	return nil
}

// retryHint là thời gian chờ Meta báo: lớn hơn giữa estimated_time_to_regain_access và Retry-After (giây).
func retryHint(u Usage, h http.Header) time.Duration {
	d := u.RegainAfter
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 {
		d = max(d, time.Duration(s)*time.Second)
	}
	return d
}

// Mã lỗi giới hạn số lần gọi và mã lỗi token của Meta (bảng kế hoạch Task 146).
func rateLimitCodes() []int { return []int{4, 17, 32, 613, 80001, 80006} }

func authCodes() []int { return []int{190, 102, 463, 467} }

// policyReason trả lý do chính sách của lỗi Meta (bảng kế hoạch Task 146; đối chiếu lại bằng payload thật ở E3, E8).
func policyReason(ge *GraphError) (channel.DenyReason, string, bool) {
	switch {
	case ge.Code == 10 && ge.Subcode == 2018278:
		return channel.DenyWindowClosed, "Đã quá 24 giờ từ tin cuối của khách", true
	case ge.Code == 551:
		return channel.DenyBlockedByUser, "Khách đã chặn tin từ Page hoặc không nhận tin", true
	case ge.Code == 10900:
		return channel.DenyPrivateReplyUsed, "Đã nhắn riêng cho comment này, Facebook chỉ cho một tin", true
	case ge.Code == 10 || (ge.Code >= 200 && ge.Code <= 299):
		return channel.DenyNoPermission, "Page chưa cấp quyền cho việc này", true
	}
	return "", "", false
}

// Classify xếp lỗi của Graph client vào một lớp của channel. Lỗi mạng trước khi gửi trọn → not_sent_retry; 429 và mã
// giới hạn → rate_limited; mã token → auth; lỗi chính sách → policy; mã 1, 2 (Meta tạm lỗi), HTTP 5xx không có JSON,
// HTTP 408 không có JSON (tầng giữa hết giờ khi yêu cầu có thể đã tới), lỗi mạng sau khi gửi trọn, lỗi không phải
// GraphError → unknown (không tự gửi lại); lỗi dựng yêu cầu (lỗi của mã, chưa gọi Meta) và còn lại → permanent.
func Classify(err error) channel.ErrorClass {
	if err == nil {
		return ""
	}
	ge, ok := asGraphError(err)
	if !ok {
		return channel.ErrorUnknown
	}
	if ge.build {
		return channel.ErrorPermanent
	}
	if ge.HTTPStatus == 0 && ge.Code == 0 {
		if ge.Sent == NotSent {
			return channel.ErrorNotSentRetry
		}
		return channel.ErrorUnknown
	}
	if _, _, policy := policyReason(ge); policy {
		return channel.ErrorPolicy
	}
	switch {
	case ge.HTTPStatus == http.StatusTooManyRequests || slices.Contains(rateLimitCodes(), ge.Code):
		return channel.ErrorRateLimited
	case slices.Contains(authCodes(), ge.Code):
		return channel.ErrorAuth
	case ge.Code == 1 || ge.Code == 2:
		return channel.ErrorUnknown
	case ge.Code == 0 && (ge.HTTPStatus >= http.StatusInternalServerError || ge.HTTPStatus < http.StatusBadRequest ||
		ge.HTTPStatus == http.StatusRequestTimeout):
		return channel.ErrorUnknown
	}
	return channel.ErrorPermanent
}

// Reason trả lý do chính sách (rỗng khi lỗi không phải chính sách) và câu tiếng Việt cho nhân viên.
func Reason(err error) (channel.DenyReason, string) {
	ge, _ := asGraphError(err)
	switch Classify(err) {
	case channel.ErrorPolicy:
		r, text, _ := policyReason(ge)
		return r, text
	case channel.ErrorNotSentRetry:
		return "", "Chưa gửi được tới Facebook, sẽ thử lại"
	case channel.ErrorRateLimited:
		return "", "Facebook đang giới hạn số lần gọi, thử lại sau"
	case channel.ErrorAuth:
		return "", "Mất kết nối với Page, cần quản trị kết nối lại"
	case channel.ErrorUnknown:
		return "", "Không rõ Facebook đã nhận chưa"
	case channel.ErrorPermanent:
		if ge.build {
			return "", "Không dựng được yêu cầu gửi Facebook"
		}
		if ge.Code == 0 {
			return "", "Facebook từ chối (HTTP " + strconv.Itoa(ge.HTTPStatus) + ")"
		}
		return "", "Facebook từ chối (mã " + strconv.Itoa(ge.Code) + ")"
	}
	return "", ""
}

// RetryAfter là thời gian phải chờ trước lần gọi sau khi lỗi là rate_limited: theo Meta báo, tối thiểu 60 s; lỗi khác
// trả 0.
func RetryAfter(err error) time.Duration {
	if Classify(err) != channel.ErrorRateLimited {
		return 0
	}
	ge, _ := asGraphError(err)
	return max(ge.RetryAfter, minRateLimitWait)
}
