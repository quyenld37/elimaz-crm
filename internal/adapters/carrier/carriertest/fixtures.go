package carriertest

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Thao tác (op) của máy chủ giả, dùng trong Harness.SetMode và Harness.Calls.
const (
	OpCreate = "create"
	OpCancel = "cancel"
	OpQuote  = "quote"
	OpTrack  = "track"
	OpLabels = "labels"
)

// Chế độ lỗi của máy chủ giả. ModeTimeoutAfterCreate, ModeFailBeforeCreate chỉ cho OpCreate; ModeTimeoutAfterSend,
// ModeFailBeforeSend cho op khác.
const (
	ModeOK                 = "ok"
	ModeTimeoutAfterCreate = "timeout_after_create"
	ModeFailBeforeCreate   = "fail_before_create"
	ModeReject             = "reject"
	ModeAuthRevoked        = "auth_revoked"
	ModeRateLimited        = "rate_limited"
	ModeTimeoutAfterSend   = "timeout_after_send"
	ModeFailBeforeSend     = "fail_before_send"
)

// SamplePhone là SĐT mẫu trong dữ liệu gửi đi; contract kiểm không kết quả hay lỗi nào chứa chuỗi SĐT.
const SamplePhone = "0912345678"

// phoneRe là mẫu SĐT Việt Nam quét trong kết quả và lỗi.
var phoneRe = regexp.MustCompile(`0[0-9]{9}`)

//go:embed testdata
var testdata embed.FS

// webhookSample là testdata/<hãng>/webhook.json: thân mẫu (chuỗi, đúng từng byte), header có chữ ký đúng với bí mật,
// và tín hiệu SplitWebhook phải trả.
type webhookSample struct {
	Secret  string                      `json:"secret"`
	Headers map[string][]string         `json:"headers"`
	Body    string                      `json:"body"`
	Signals []fulfillment.WebhookSignal `json:"signals"`
}

// statuses đọc testdata/<hãng>/statuses.json: mã thô → trạng thái chuẩn.
func statuses(key string) (map[string]fulfillment.Canonical, error) {
	b, err := fs.ReadFile(testdata, "testdata/"+key+"/statuses.json")
	if err != nil {
		return nil, fmt.Errorf("đọc statuses.json của %s: %w", key, err)
	}
	var m map[string]fulfillment.Canonical
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("giải statuses.json của %s: %w", key, err)
	}
	return m, nil
}

// webhook đọc testdata/<hãng>/webhook.json.
func webhook(key string) (webhookSample, error) {
	var w webhookSample
	b, err := fs.ReadFile(testdata, "testdata/"+key+"/webhook.json")
	if err != nil {
		return w, fmt.Errorf("đọc webhook.json của %s: %w", key, err)
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return w, fmt.Errorf("giải webhook.json của %s: %w", key, err)
	}
	return w, nil
}

// sampleTo là địa chỉ nhận mẫu (có SĐT để quét rò rỉ).
func sampleTo() fulfillment.Address {
	return fulfillment.Address{
		Name: "Nguyễn Văn Thử", Phone: SamplePhone, Line: "12 Lý Thường Kiệt", ProvinceCode: "01", WardCode: "00004",
	}
}

func sampleParcel() fulfillment.Parcel {
	return fulfillment.Parcel{WeightGrams: 700, LengthCm: 20, WidthCm: 15, HeightCm: 8, DeclaredValue: 450000, ItemCount: 2}
}

// sampleCreate là yêu cầu tạo kiện mẫu theo mã code.
func sampleCreate(acc fulfillment.Account, code string) fulfillment.CreateRequest {
	to := sampleTo()
	return fulfillment.CreateRequest{
		Code: code, CODAmount: 530000, ReceiverName: to.Name, ReceiverPhone: to.Phone, Address: to.Line,
		From: acc.Pickup, To: to, Parcel: sampleParcel(), Service: "", Options: fulfillment.Options{Payer: "shop", Pickup: true},
	}
}

// sampleQuote là dữ liệu hỏi phí mẫu.
func sampleQuote(acc fulfillment.Account) fulfillment.QuoteInput {
	return fulfillment.QuoteInput{
		From: acc.Pickup, To: sampleTo(), Parcel: sampleParcel(), COD: 530000,
		Options: fulfillment.Options{Payer: "shop"},
	}
}

// sampleEvent là sự kiện đẩy vào máy chủ giả.
func sampleEvent(key, raw string) fulfillment.CarrierEvent {
	return fulfillment.CarrierEvent{Key: key, RawStatus: raw, OccurredAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)}
}
