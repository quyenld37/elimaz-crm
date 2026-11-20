package fake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Key là tên nguồn webhook (/hooks/fakecarrier).
const Key = "fakecarrier"

const (
	// SignatureHeader là header chữ ký của webhook hãng giả.
	SignatureHeader = "X-Fake-Signature"
	signaturePrefix = "sha256="
	skippedInvalid  = "invalid"
)

// Source hiện thực webhooks.Source cho hãng giả.
type Source struct {
	secret []byte
}

// NewSource tạo nguồn với bí mật FAKECARRIER_WEBHOOK_SECRET (worker chỉ tách gói, dựng với bí mật rỗng).
func NewSource(secret string) *Source { return &Source{secret: []byte(secret)} }

// Key trả "fakecarrier".
func (*Source) Key() string { return Key }

// Challenge luôn trả false: hãng giả không có bước xác minh.
func (*Source) Challenge(url.Values) (string, bool) { return "", false }

// Sign trả giá trị header chữ ký cho thân raw (tools/fakecarrier và test dùng).
func Sign(raw []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	return signaturePrefix + hex.EncodeToString(m.Sum(nil))
}

// VerifySignature kiểm x-fake-signature: sha256=<hex HMAC-SHA256(raw, bí mật)>, so bằng hmac.Equal. Bí mật rỗng →
// luôn sai.
func (s *Source) VerifySignature(raw []byte, h http.Header) bool {
	got := h.Get(SignatureHeader)
	if len(s.secret) == 0 || !strings.HasPrefix(got, signaturePrefix) {
		return false
	}
	// So chuỗi hex đã chuẩn hóa chữ thường, thời gian hằng.
	return hmac.Equal([]byte(strings.ToLower(got)), []byte(Sign(raw, string(s.secret))))
}

// Event là một sự kiện trong gói webhook của hãng giả. Status chỉ để người đọc; CRM không tin nó (B10).
type Event struct {
	EventID    string    `json:"eventId"`
	AccountKey string    `json:"accountKey"`
	Tracking   string    `json:"tracking"`
	Status     string    `json:"status"`
	SentAt     time.Time `json:"sentAt"`
}

// Packet là gói webhook {"events":[…]}.
type Packet struct {
	Events []Event `json:"events"`
}

// Split đọc gói: mỗi sự kiện đủ eventId, accountKey, tracking, sentAt thành một tín hiệu carrier.signal định tuyến
// theo (carrier_account, accountKey), chống trùng theo eventId, thứ tự theo tracking; thiếu trường → Skipped["invalid"].
// Thân không phải JSON đúng dạng là lỗi.
func (*Source) Split(raw []byte) (webhooks.SplitResult, error) {
	var p struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return webhooks.SplitResult{}, fmt.Errorf("đọc gói fakecarrier: %w", err)
	}
	res := webhooks.SplitResult{Skipped: map[string]int{}}
	for _, r := range p.Events {
		var e Event
		if err := json.Unmarshal(r, &e); err != nil || e.EventID == "" || e.AccountKey == "" || e.Tracking == "" ||
			e.SentAt.IsZero() {
			res.Skipped[skippedInvalid]++
			continue
		}
		payload, err := json.Marshal(fulfillment.Signal{
			CarrierKey: CarrierKey, AccountKey: e.AccountKey, TrackingCode: e.Tracking, EventID: e.EventID,
		})
		if err != nil {
			return webhooks.SplitResult{}, fmt.Errorf("mã hóa tín hiệu: %w", err)
		}
		res.Events = append(res.Events, webhooks.InboundEvent{
			Type: fulfillment.SignalType, DedupeKey: e.EventID,
			Route:    webhooks.Route{Kind: fulfillment.RouteKindCarrierAccount, ExternalKey: e.AccountKey},
			OrderKey: e.Tracking, OccurredAt: e.SentAt.UTC(), Payload: payload,
		})
	}
	return res, nil
}
