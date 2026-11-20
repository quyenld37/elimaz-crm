package fake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	// SignatureHeader là header chữ ký của webhook hãng giả.
	SignatureHeader = "X-Fake-Signature"
	signaturePrefix = "sha256="
)

// Sign trả giá trị header chữ ký cho thân raw (tools/fakecarrier và test dùng).
func Sign(raw []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	return signaturePrefix + hex.EncodeToString(m.Sum(nil))
}

// Verify kiểm x-fake-signature: sha256=<hex HMAC-SHA256(raw, secret)>, so bằng hmac.Equal. Bí mật rỗng → luôn sai.
func Verify(raw []byte, h http.Header, secret string) bool {
	got := h.Get(SignatureHeader)
	if secret == "" || !strings.HasPrefix(got, signaturePrefix) {
		return false
	}
	// So chuỗi hex đã chuẩn hóa chữ thường, thời gian hằng.
	return hmac.Equal([]byte(strings.ToLower(got)), []byte(Sign(raw, secret)))
}

// Event là một sự kiện trong gói webhook của hãng giả; Code là mã vận đơn của CRM (có khi kiện tạo qua M3). Status
// chỉ để người đọc; CRM không tin nó (B10).
type Event struct {
	EventID    string    `json:"eventId"`
	AccountKey string    `json:"accountKey"`
	Tracking   string    `json:"tracking"`
	Code       string    `json:"code,omitempty"`
	Status     string    `json:"status"`
	SentAt     time.Time `json:"sentAt"`
}

// Packet là gói webhook {"events":[…]}.
type Packet struct {
	Events []Event `json:"events"`
}
