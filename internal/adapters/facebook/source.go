package facebook

import (
	"crypto/subtle"
	"net/url"
	"regexp"

	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// RouteKind là loại tuyến của sự kiện Facebook trong platform.tenant_routes: khóa ngoài là Page ID.
const RouteKind = "fb_page"

// challengePattern: hub.challenge Meta gửi là dãy số; chỉ nhận chữ, số, gạch, tối đa 128 ký tự để bước GET không
// phản chiếu chuỗi tùy ý.
var challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

var _ webhooks.Source = (*Channel)(nil)

// Key trả "facebook" (đường /hooks/facebook, cột source).
func (*Channel) Key() string { return Key }

// Challenge trả lời bước GET xác minh của Meta: hub.mode = subscribe, hub.verify_token khớp verify token (so thời gian
// hằng; token cấu hình rỗng thì luôn sai) và hub.challenge đúng mẫu → trả hub.challenge.
func (c *Channel) Challenge(q url.Values) (string, bool) {
	challenge := q.Get("hub.challenge")
	if q.Get("hub.mode") != "subscribe" || c.o.VerifyToken == "" || !challengePattern.MatchString(challenge) ||
		subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(c.o.VerifyToken)) != 1 {
		return "", false
	}
	return challenge, true
}
