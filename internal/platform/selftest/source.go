// Package selftest là nguồn webhook thử "selftest" (/hooks/selftest) dùng để kiểm cả đường vào khi chưa có kênh thật:
// ký bằng HMAC-SHA256 với SELFTEST_WEBHOOK_SECRET, có bước GET xác minh, tách gói {"events":[…]}.
package selftest

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Key là tên nguồn và Route.Kind của sự kiện selftest.
const Key = "selftest"

const (
	signatureHeader = "X-Selftest-Signature"
	signaturePrefix = "sha256="
	// pingType là InboundEvent.Type của sự kiện kind "ping".
	pingType = "selftest.ping"
	// skippedInvalid đếm sự kiện ping thiếu eventId, tenantKey hoặc sentAt.
	skippedInvalid = "invalid"
)

// Source hiện thực webhooks.Source cho nguồn selftest.
type Source struct {
	secret []byte
}

// NewSource tạo nguồn selftest với bí mật secret (SELFTEST_WEBHOOK_SECRET).
func NewSource(secret string) *Source { return &Source{secret: []byte(secret)} }

// Key trả "selftest".
func (*Source) Key() string { return Key }

// Challenge trả q["challenge"] khi q["verify_token"] bằng bí mật (so thời gian hằng) và challenge khác rỗng.
func (s *Source) Challenge(q url.Values) (string, bool) {
	token, challenge := q.Get("verify_token"), q.Get("challenge")
	if challenge == "" || subtle.ConstantTimeCompare([]byte(token), s.secret) != 1 {
		return "", false
	}
	return challenge, true
}

// VerifySignature kiểm header x-selftest-signature: sha256=<hex HMAC-SHA256(raw, bí mật)>, so bằng hmac.Equal.
func (s *Source) VerifySignature(raw []byte, h http.Header) bool {
	hexSig, ok := strings.CutPrefix(h.Get(signatureHeader), signaturePrefix)
	if !ok {
		return false
	}
	sig, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, s.secret)
	m.Write(raw)
	return hmac.Equal(sig, m.Sum(nil))
}

type packet struct {
	Events []json.RawMessage `json:"events"`
}

type event struct {
	Kind      string    `json:"kind"`
	EventID   string    `json:"eventId"`
	TenantKey string    `json:"tenantKey"`
	SentAt    time.Time `json:"sentAt"`
}

// Split đọc {"events":[{"kind","eventId","tenantKey","sentAt"}]}: kind "ping" thành sự kiện selftest.ping định tuyến
// theo ("selftest", tenantKey); kind khác vào Skipped theo kind; ping thiếu trường vào Skipped["invalid"]. Thân
// không phải JSON đúng dạng là lỗi.
func (*Source) Split(raw []byte) (webhooks.SplitResult, error) {
	var p packet
	if err := json.Unmarshal(raw, &p); err != nil {
		return webhooks.SplitResult{}, fmt.Errorf("đọc gói selftest: %w", err)
	}
	res := webhooks.SplitResult{Skipped: map[string]int{}}
	for _, r := range p.Events {
		var e event
		if err := json.Unmarshal(r, &e); err != nil {
			res.Skipped[skippedInvalid]++
			continue
		}
		switch {
		case e.Kind != "ping":
			res.Skipped[e.Kind]++
		case e.EventID == "" || e.TenantKey == "" || e.SentAt.IsZero():
			res.Skipped[skippedInvalid]++
		default:
			res.Events = append(res.Events, webhooks.InboundEvent{
				Type:       pingType,
				DedupeKey:  e.EventID,
				Route:      webhooks.Route{Kind: Key, ExternalKey: e.TenantKey},
				OrderKey:   e.TenantKey,
				OccurredAt: e.SentAt.UTC(),
				Payload:    r,
			})
		}
	}
	return res, nil
}
