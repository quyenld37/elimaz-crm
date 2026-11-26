package facebook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// Callback xóa dữ liệu của Meta (kế hoạch M4, Task 179; spec 02 mục 5, Q30): Meta POST form signed_request =
// "<chữ ký base64url>.<payload base64url>", chữ ký là HMAC-SHA256 của phần payload đã mã hóa bằng app secret; payload có
// user_id là mã người dùng app (app-scoped), không phải PSID khách.

// signedRequestAlgorithm là thuật toán duy nhất được nhận.
const signedRequestAlgorithm = "HMAC-SHA256"

var (
	// ErrSignedRequest báo signed_request sai dạng, sai chữ ký, sai thuật toán hoặc thiếu user_id.
	ErrSignedRequest = errors.New("facebook: signed_request không hợp lệ")
	// ErrNoUserID báo Meta trả lời me?fields=id không có id.
	ErrNoUserID = errors.New("facebook: Meta không trả mã người dùng")
)

// ParseSignedRequest kiểm signed_request sr bằng appSecret (so chữ ký bằng hmac.Equal) và trả user_id của payload.
// Mọi lỗi (gồm app secret rỗng) → ErrSignedRequest; lỗi không mang nội dung sr.
func ParseSignedRequest(appSecret logging.Secret, sr string) (string, error) {
	sigPart, payloadPart, ok := strings.Cut(sr, ".")
	if !ok || appSecret == "" || sigPart == "" || payloadPart == "" {
		return "", ErrSignedRequest
	}
	sig, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(sigPart, "="))
	if err != nil {
		return "", ErrSignedRequest
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(payloadPart))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", ErrSignedRequest
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payloadPart, "="))
	if err != nil {
		return "", ErrSignedRequest
	}
	var p struct {
		Algorithm string `json:"algorithm"`
		UserID    string `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || !strings.EqualFold(p.Algorithm, signedRequestAlgorithm) || p.UserID == "" {
		return "", ErrSignedRequest
	}
	return p.UserID, nil
}

// AppUserID đọc mã người dùng app của token người dùng (me?fields=id): callback xóa dữ liệu gửi đúng mã này.
func (c *Channel) AppUserID(ctx context.Context, userToken logging.Secret) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if _, err := c.client.Do(ctx, http.MethodGet, "me", userToken, url.Values{"fields": {"id"}}, nil, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", ErrNoUserID
	}
	return out.ID, nil
}
