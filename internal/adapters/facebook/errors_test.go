package facebook_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
)

// TestClassify: mỗi dòng của bảng phân loại trong kế hoạch Task 146 một ca (14 ca), cùng lý do và câu tiếng Việt.
func TestClassify(t *testing.T) {
	t.Parallel()
	const (
		lostPage = "Mất kết nối với Page, cần quản trị kết nối lại"
		window   = "Đã quá 24 giờ từ tin cuối của khách"
		blocked  = "Khách đã chặn tin từ Page hoặc không nhận tin"
		private  = "Đã nhắn riêng cho comment này, Facebook chỉ cho một tin"
		noPerm   = "Page chưa cấp quyền cho việc này"
		notSent  = "Chưa gửi được tới Facebook, sẽ thử lại"
		unknown  = "Không rõ Facebook đã nhận chưa"
		limited  = "Facebook đang giới hạn số lần gọi, thử lại sau"
	)
	cases := []struct {
		name   string
		err    *facebook.GraphError
		class  channel.ErrorClass
		reason channel.DenyReason
		text   string
	}{
		{"lỗi nối trước khi gửi trọn", &facebook.GraphError{Sent: facebook.NotSent}, channel.ErrorNotSentRetry, "", notSent},
		{"HTTP 429", &facebook.GraphError{HTTPStatus: 429, Sent: facebook.MaybeSent}, channel.ErrorRateLimited, "", limited},
		{"mã giới hạn 4/17/32/613/80001/80006", &facebook.GraphError{HTTPStatus: 400, Code: 80006, Sent: facebook.MaybeSent}, channel.ErrorRateLimited, "", limited},
		{"token 190", &facebook.GraphError{HTTPStatus: 400, Code: 190, Subcode: 460, Sent: facebook.MaybeSent}, channel.ErrorAuth, "", lostPage},
		{"token 102/463/467", &facebook.GraphError{HTTPStatus: 400, Code: 467, Sent: facebook.MaybeSent}, channel.ErrorAuth, "", lostPage},
		{"10/2018278 quá 24 giờ", &facebook.GraphError{HTTPStatus: 400, Code: 10, Subcode: 2018278, Sent: facebook.MaybeSent}, channel.ErrorPolicy, channel.DenyWindowClosed, window},
		{"551 khách chặn", &facebook.GraphError{HTTPStatus: 400, Code: 551, Sent: facebook.MaybeSent}, channel.ErrorPolicy, channel.DenyBlockedByUser, blocked},
		{"10900 đã nhắn riêng", &facebook.GraphError{HTTPStatus: 400, Code: 10900, Sent: facebook.MaybeSent}, channel.ErrorPolicy, channel.DenyPrivateReplyUsed, private},
		{"200 thiếu quyền", &facebook.GraphError{HTTPStatus: 403, Code: 200, Sent: facebook.MaybeSent}, channel.ErrorPolicy, channel.DenyNoPermission, noPerm},
		{"10 phụ mã khác", &facebook.GraphError{HTTPStatus: 400, Code: 10, Subcode: 1234, Sent: facebook.MaybeSent}, channel.ErrorPolicy, channel.DenyNoPermission, noPerm},
		{"100 tham số sai", &facebook.GraphError{HTTPStatus: 400, Code: 100, Sent: facebook.MaybeSent}, channel.ErrorPermanent, "", "Facebook từ chối (mã 100)"},
		{"5xx không JSON", &facebook.GraphError{HTTPStatus: 503, Sent: facebook.MaybeSent}, channel.ErrorUnknown, "", unknown},
		{"hết giờ sau khi gửi trọn", &facebook.GraphError{Sent: facebook.MaybeSent}, channel.ErrorUnknown, "", unknown},
		{"mã lạ", &facebook.GraphError{HTTPStatus: 400, Code: 9999, Sent: facebook.MaybeSent}, channel.ErrorPermanent, "", "Facebook từ chối (mã 9999)"},
	}
	for _, tc := range cases {
		wrapped := fmt.Errorf("gửi tin: %w", tc.err)
		if got := facebook.Classify(wrapped); got != tc.class {
			t.Errorf("%s: lớp %s, mong %s", tc.name, got, tc.class)
		}
		reason, text := facebook.Reason(wrapped)
		if reason != tc.reason || text != tc.text {
			t.Errorf("%s: Reason = (%q, %q), mong (%q, %q)", tc.name, reason, text, tc.reason, tc.text)
		}
	}
	for _, code := range []int{4, 17, 32, 613, 80001, 80006} {
		if got := facebook.Classify(&facebook.GraphError{HTTPStatus: 400, Code: code}); got != channel.ErrorRateLimited {
			t.Errorf("mã %d: %s, mong rate_limited", code, got)
		}
	}
	for _, code := range []int{190, 102, 463, 467} {
		if got := facebook.Classify(&facebook.GraphError{HTTPStatus: 400, Code: code}); got != channel.ErrorAuth {
			t.Errorf("mã %d: %s, mong auth", code, got)
		}
	}
}

func TestClassify_NotGraphError(t *testing.T) {
	t.Parallel()
	if got := facebook.Classify(nil); got != "" {
		t.Errorf("nil: %q", got)
	}
	// Lỗi không rõ nguồn: không được coi là chắc chắn chưa gửi.
	if got := facebook.Classify(context.Canceled); got != channel.ErrorUnknown {
		t.Errorf("lỗi lạ: %s, mong unknown", got)
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"không mức dùng: tối thiểu 60 s", &facebook.GraphError{HTTPStatus: 400, Code: 613}, time.Minute},
		{"lấy theo regain 5 phút", &facebook.GraphError{HTTPStatus: 400, Code: 4, RetryAfter: 5 * time.Minute}, 5 * time.Minute},
		{"regain nhỏ hơn 60 s nâng lên", &facebook.GraphError{HTTPStatus: 429, RetryAfter: 10 * time.Second}, time.Minute},
		{"không phải giới hạn: 0", &facebook.GraphError{HTTPStatus: 400, Code: 100, RetryAfter: time.Hour}, 0},
	}
	for _, tc := range cases {
		if got := facebook.RetryAfter(tc.err); got != tc.want {
			t.Errorf("%s: %v, mong %v", tc.name, got, tc.want)
		}
	}
}

func TestGraphError_MessageScrubbed(t *testing.T) {
	t.Parallel()
	err := facebook.NewGraphErrorForTest(400, 100, 0, "OAuthException", "tr1", "Gọi 0912345678 tại 12 Lý Thường Kiệt")
	if got := err.Error(); strings.Contains(got, "0912345678") || strings.Contains(err.Message, "0912345678") {
		t.Fatalf("lộ SĐT: %q / %q", got, err.Message)
	}
}
