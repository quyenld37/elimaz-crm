package fake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const (
	// SignatureHeader là header chữ ký của webhook kênh giả: sha256=<hex HMAC-SHA256(thân, bí mật)>.
	SignatureHeader = "X-Fake-Signature"
	signaturePrefix = "sha256="
	// RouteKind là loại tuyến tenant của Page kênh giả (platform.tenant_routes.kind; Facebook dùng fb_page).
	RouteKind = "fakechat_page"
)

// Lỗi mẫu của kênh giả, mỗi lỗi một lớp ClassifyError (test của task hội thoại dùng để giả lập lỗi kênh).
var (
	ErrNotSent     = errors.New("fakechat: chưa gửi, thử lại được")
	ErrRateLimited = errors.New("fakechat: quá giới hạn gửi")
	ErrAuth        = errors.New("fakechat: token hỏng")
	ErrPolicy      = errors.New("fakechat: vi phạm chính sách kênh")
	ErrPermanent   = errors.New("fakechat: yêu cầu sai, không thử lại")
)

// Sign trả giá trị header chữ ký cho thân raw với bí mật secret (test và công cụ dev dùng).
func Sign(raw []byte, secret string) string {
	return signaturePrefix + hex.EncodeToString(mac(raw, secret))
}

// mac là HMAC-SHA256 của raw với secret.
func mac(raw []byte, secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	return m.Sum(nil)
}

// SentMessage là một lần kênh giả đã nhận lệnh gửi (giữ trong bộ nhớ).
type SentMessage struct {
	ExternalID string
	Command    channel.OutboundCommand
	At         time.Time
}

// Chat là adapter kênh nhắn tin giả: chữ ký HMAC, tách gói JSON {"events":[…]} thành sự kiện chuẩn hóa của channel, gửi
// ghi vào bộ nhớ, cửa sổ trả lời 24 giờ đơn giản. An toàn khi dùng từ nhiều goroutine.
type Chat struct {
	secret string
	now    func() time.Time

	mu       sync.Mutex
	behavior string
	sent     []SentMessage
}

// New dựng kênh giả với bí mật webhook secret (rỗng: mọi chữ ký sai).
func New(secret string) *Chat {
	return &Chat{secret: secret, now: time.Now, behavior: "ok"}
}

// Key là khóa kênh giả (fakechat).
func (*Chat) Key() string { return Key }

// Features trả tính năng của kênh giả: có mọi tính năng trừ comment live và thẻ nhân viên.
func (*Chat) Features() channel.Features {
	return channel.Features{Comments: true, PrivateReply: true, HideComment: true, QuickReplies: true}
}

// Challenge trả false: kênh giả không có bước xác minh GET.
func (*Chat) Challenge(url.Values) (string, bool) { return "", false }

// VerifySignature kiểm X-Fake-Signature trên thân gốc (so thời gian hằng); bí mật rỗng → luôn sai.
func (a *Chat) VerifySignature(raw []byte, h http.Header) bool {
	sig, ok := strings.CutPrefix(strings.ToLower(h.Get(SignatureHeader)), signaturePrefix)
	got, err := hex.DecodeString(sig)
	return ok && err == nil && a.secret != "" && hmac.Equal(got, mac(raw, a.secret))
}

// Backfill trả chuỗi rỗng: kênh giả không có gì để đọc bù.
func (*Chat) Backfill(context.Context, channel.ChannelAccount, channel.Credentials, time.Time) iter.Seq2[webhooks.InboundEvent, error] {
	return func(func(webhooks.InboundEvent, error) bool) {}
}

// HealthCheck báo tài khoản giả luôn khỏe khi có token.
func (*Chat) HealthCheck(_ context.Context, _ channel.ChannelAccount, cred channel.Credentials) (channel.Health, error) {
	ok := cred.AccessToken != ""
	return channel.Health{TokenValid: ok, PermissionsOK: ok, WebhookSubscribed: ok}, nil
}

// ClassifyError xếp lỗi mẫu của kênh giả vào lớp tương ứng; lỗi khác là unknown.
func (*Chat) ClassifyError(err error) channel.ErrorClass {
	for _, c := range []struct {
		err   error
		class channel.ErrorClass
	}{
		{ErrNotSent, channel.ErrorNotSentRetry},
		{ErrRateLimited, channel.ErrorRateLimited},
		{ErrAuth, channel.ErrorAuth},
		{ErrPolicy, channel.ErrorPolicy},
		{ErrPermanent, channel.ErrorPermanent},
	} {
		if errors.Is(err, c.err) {
			return c.class
		}
	}
	return channel.ErrorUnknown
}
