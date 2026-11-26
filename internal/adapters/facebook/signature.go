package facebook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// SignatureHeader là header chữ ký Meta gửi kèm mỗi POST webhook: "sha256=<hex HMAC-SHA256(thân thô, app secret)>".
const SignatureHeader = "X-Hub-Signature-256"

// VerifySignature kiểm X-Hub-Signature-256 trên thân thô raw bằng app secret, so bằng hmac.Equal. Thiếu header, chỉ có
// X-Hub-Signature (sha1), hex sai hay app secret rỗng → false.
func (c *Channel) VerifySignature(raw []byte, h http.Header) bool {
	hexSig, ok := strings.CutPrefix(h.Get(SignatureHeader), "sha256=")
	if !ok || c.o.AppSecret == "" {
		return false
	}
	got, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.o.AppSecret))
	mac.Write(raw)
	return hmac.Equal(got, mac.Sum(nil))
}
