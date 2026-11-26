package facebook_test

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: chỉ để dựng chữ ký sha1 cũ mà adapter phải từ chối
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/platform/logging"
)

const testVerifyToken = "verify-token-thu-0123456789abcdef0123"

// inbound dựng adapter chỉ để dùng phần vào (không gọi Graph): app secret và verify token.
func inbound(appSecret, verifyToken string) *facebook.Channel {
	return facebook.New(facebook.Options{AppSecret: logging.Secret(appSecret), VerifyToken: logging.Secret(verifyToken)})
}

func newAdapter() *facebook.Channel { return inbound(testAppSecret, testVerifyToken) }

// sign trả header X-Hub-Signature-256 của raw ký bằng secret như Meta.
func sign(raw []byte, secret string) http.Header {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	h := http.Header{}
	h.Set(facebook.SignatureHeader, "sha256="+hex.EncodeToString(m.Sum(nil)))
	return h
}

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// mustRead đọc fixture testdata/name ở chỗ không có testing.TB (Harness của channeltest).
func mustRead(name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return b
}

// Bước GET xác minh của Meta: chỉ hub.mode=subscribe kèm verify token đúng mới trả hub.challenge.
func TestChallenge(t *testing.T) {
	t.Parallel()
	a := newAdapter()
	q := func(mode, token, challenge string) url.Values {
		return url.Values{"hub.mode": {mode}, "hub.verify_token": {token}, "hub.challenge": {challenge}}
	}
	if got, ok := a.Challenge(q("subscribe", testVerifyToken, "1158201444")); !ok || got != "1158201444" {
		t.Errorf("token đúng: (%q, %v), mong (1158201444, true)", got, ok)
	}
	if got, ok := a.Challenge(q("subscribe", testVerifyToken, "abc")); !ok || got != "abc" {
		t.Errorf("challenge chữ: (%q, %v)", got, ok)
	}
	for name, v := range map[string]url.Values{
		"sai token":         q("subscribe", testVerifyToken+"x", "1158201444"),
		"thiếu token":       q("subscribe", "", "1158201444"),
		"unsubscribe":       q("unsubscribe", testVerifyToken, "1158201444"),
		"thiếu mode":        q("", testVerifyToken, "1158201444"),
		"challenge rỗng":    q("subscribe", testVerifyToken, ""),
		"challenge có thẻ":  q("subscribe", testVerifyToken, "<script>alert(1)</script>"),
		"challenge quá dài": q("subscribe", testVerifyToken, string(make([]byte, 129))),
	} {
		if got, ok := a.Challenge(v); ok || got != "" {
			t.Errorf("%s: (%q, %v), mong từ chối", name, got, ok)
		}
	}
	// Vai trò không có verify token (worker): mọi bước GET đều sai, kể cả token rỗng gửi lên.
	empty := inbound(testAppSecret, "")
	if _, ok := empty.Challenge(q("subscribe", "", "1")); ok {
		t.Error("verify token rỗng không được khớp token rỗng")
	}
}

// Chữ ký X-Hub-Signature-256 là HMAC-SHA256 của thân thô bằng app secret.
func TestVerifySignature(t *testing.T) {
	t.Parallel()
	a := newAdapter()
	raw := fixture(t, "message_text.json")
	if !a.VerifySignature(raw, sign(raw, testAppSecret)) {
		t.Fatal("chữ ký đúng phải đạt")
	}
	// Đổi một byte bất kỳ của thân → sai (contract (4)).
	for _, i := range []int{0, len(raw) / 2, len(raw) - 1} {
		bad := append([]byte(nil), raw...)
		bad[i] ^= 0x01
		if a.VerifySignature(bad, sign(raw, testAppSecret)) {
			t.Errorf("đổi byte %d vẫn đạt", i)
		}
	}
	if a.VerifySignature(raw, http.Header{}) {
		t.Error("thiếu header phải sai")
	}
	if a.VerifySignature(raw, sign(raw, testAppSecret+"0")) {
		t.Error("ký bằng secret khác phải sai")
	}
	m := hmac.New(sha1.New, []byte(testAppSecret))
	m.Write(raw)
	sha1Only := http.Header{}
	sha1Only.Set("X-Hub-Signature", "sha1="+hex.EncodeToString(m.Sum(nil)))
	if a.VerifySignature(raw, sha1Only) {
		t.Error("chỉ có X-Hub-Signature sha1 phải sai")
	}
	for _, v := range []string{"sha256=zz", "sha256=", "sha1=" + hex.EncodeToString(m.Sum(nil)), "abc"} {
		h := http.Header{}
		h.Set(facebook.SignatureHeader, v)
		if a.VerifySignature(raw, h) {
			t.Errorf("header %q phải sai", v)
		}
	}
	// Không có app secret (cấu hình thiếu): không chữ ký nào đạt, kể cả HMAC bằng khóa rỗng.
	if inbound("", testVerifyToken).VerifySignature(raw, sign(raw, "")) {
		t.Error("app secret rỗng phải luôn sai")
	}
}
