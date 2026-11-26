package facebook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// signedRequest dựng signed_request như Meta: base64url(HMAC-SHA256(payload đã mã hóa, secret)) + "." + payload.
func signedRequest(payloadJSON, secret string) string {
	p := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(p))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)) + "." + p
}

func TestParseSignedRequest_Valid(t *testing.T) {
	t.Parallel()
	sr := signedRequest(`{"algorithm":"HMAC-SHA256","expires":1700000000,"issued_at":1699990000,"user_id":"218471"}`, testAppSecret)
	id, err := facebook.ParseSignedRequest(logging.Secret(testAppSecret), sr)
	if err != nil || id != "218471" {
		t.Fatalf("ParseSignedRequest = %q, %v; muốn 218471", id, err)
	}
	// Meta có lúc thêm "=" đệm vào chữ ký: vẫn nhận.
	p := base64.RawURLEncoding.EncodeToString([]byte(`{"algorithm":"HMAC-SHA256","user_id":"9"}`))
	m := hmac.New(sha256.New, []byte(testAppSecret))
	m.Write([]byte(p))
	padded := base64.URLEncoding.EncodeToString(m.Sum(nil)) + "." + p
	if id, err := facebook.ParseSignedRequest(logging.Secret(testAppSecret), padded); err != nil || id != "9" {
		t.Errorf("chữ ký có đệm: %q, %v", id, err)
	}
}

func TestParseSignedRequest_Rejects(t *testing.T) {
	t.Parallel()
	good := signedRequest(`{"algorithm":"HMAC-SHA256","user_id":"218471"}`, testAppSecret)
	sig, payload, _ := strings.Cut(good, ".")
	cases := []struct{ name, secret, sr string }{
		{"rỗng", testAppSecret, ""},
		{"không có dấu chấm", testAppSecret, sig + payload},
		{"ký bằng secret khác", testAppSecret, signedRequest(`{"algorithm":"HMAC-SHA256","user_id":"218471"}`, "ffffffffffffffffffffffffffffffff")},
		{"payload bị sửa", testAppSecret, sig + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"algorithm":"HMAC-SHA256","user_id":"1"}`))},
		{"chữ ký không phải base64", testAppSecret, "%%%." + payload},
		{"thuật toán khác", testAppSecret, signedRequest(`{"algorithm":"HMAC-SHA1","user_id":"218471"}`, testAppSecret)},
		{"thiếu thuật toán", testAppSecret, signedRequest(`{"user_id":"218471"}`, testAppSecret)},
		{"thiếu user_id", testAppSecret, signedRequest(`{"algorithm":"HMAC-SHA256"}`, testAppSecret)},
		{"payload không phải JSON", testAppSecret, signedRequest(`không phải json`, testAppSecret)},
		{"app secret rỗng", "", signedRequest(`{"algorithm":"HMAC-SHA256","user_id":"218471"}`, "")},
	}
	for _, c := range cases {
		id, err := facebook.ParseSignedRequest(logging.Secret(c.secret), c.sr)
		if !errors.Is(err, facebook.ErrSignedRequest) || id != "" {
			t.Errorf("%s: %q, %v; muốn ErrSignedRequest", c.name, id, err)
		}
	}
}

func TestAppUserID(t *testing.T) {
	t.Parallel()
	cg := newCannedGraph(t, map[string]func(map[string][]string) string{
		"me": func(map[string][]string) string { return `{"id":"218471"}` },
	})
	id, err := cg.channel().AppUserID(t.Context(), "EAAB-user-1")
	if err != nil || id != "218471" {
		t.Fatalf("AppUserID = %q, %v", id, err)
	}
	if q := cg.queries("me"); len(q) != 1 || q[0]["fields"][0] != "id" {
		t.Errorf("query me = %v", q)
	}
	empty := newCannedGraph(t, map[string]func(map[string][]string) string{
		"me": func(map[string][]string) string { return `{}` },
	})
	if _, err := empty.channel().AppUserID(t.Context(), "EAAB-user-1"); !errors.Is(err, facebook.ErrNoUserID) {
		t.Errorf("Meta không trả id: %v; muốn ErrNoUserID", err)
	}
}
