package vtp

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Webhook của VTP (trang "Webhook"): POST JSON, header Authorization là "token xác thực", thân {"DATA": {…}, "TOKEN":
// "…"}; một URL mỗi tài khoản; hãng gửi lại tối đa 5 lần khi không nhận HTTP 200; "có thể bị trùng hoặc thừa".
// DATA.ORDER_NUMBER là mã vận đơn VTP, ORDER_REFERENCE là mã đối tác (mã vận đơn CRM gửi ở createOrder),
// ORDER_STATUSDATE dạng dd/MM/yyyy HH:mm:ss giờ Việt Nam, ORDER_STATUS là số.
const (
	statusDateLayout = "02/01/2006 15:04:05"
	bearerPrefix     = "bearer "
)

// vietnam là múi giờ của ORDER_STATUSDATE (UTC+7, không có giờ mùa hè); không dùng time.LoadLocation để không phụ
// thuộc tzdata của máy chủ.
func vietnam() *time.Location { return time.FixedZone("ICT", 7*3600) }

var errWebhookBody = errors.New("vtp: thân webhook không đúng định dạng")

type webhookEnvelope struct {
	Data json.RawMessage `json:"DATA"`
}

type webhookData struct {
	OrderNumber    string          `json:"ORDER_NUMBER"`
	OrderReference string          `json:"ORDER_REFERENCE"`
	StatusDate     string          `json:"ORDER_STATUSDATE"`
	Status         json.RawMessage `json:"ORDER_STATUS"`
}

// VerifyWebhook so header Authorization (một giá trị, bỏ khoảng trắng hai đầu và tiền tố "Bearer ") với secret bằng so
// thời gian hằng trên băm sha256. Bí mật rỗng, thiếu header, nhiều giá trị, sai → false. VTP không ký thân nên không
// phát hiện được thân bị sửa khi token lộ (docs/carriers/vtp.md); TOKEN trong thân (mẫu của tài liệu giống tên tài
// khoản) không dùng làm bí mật.
func (*Adapter) VerifyWebhook(_ []byte, h fulfillment.Header, secret string) bool {
	vs := http.Header(h).Values("Authorization")
	if secret == "" || len(vs) != 1 {
		return false
	}
	got := strings.TrimSpace(vs[0])
	if len(got) >= len(bearerPrefix) && strings.EqualFold(got[:len(bearerPrefix)], bearerPrefix) {
		got = strings.TrimSpace(got[len(bearerPrefix):])
	}
	if got == "" {
		return false
	}
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// SplitWebhook tách thân webhook thành đúng một tín hiệu: mã vận đơn VTP, mã vận đơn CRM, khóa sự kiện
// "vtp:<mã>:<mã trạng thái>:<thời điểm UTC RFC 3339>" (VTP gửi lại cùng sự kiện → cùng khóa). Không đưa trạng thái,
// tiền, tên, SĐT ra lõi (B10: tín hiệu chỉ là khóa). Thời điểm không đọc được hay thiếu mã trạng thái → khóa rỗng (lõi
// chống trùng theo băm thân). Không phải JSON, DATA không phải đối tượng, ORDER_STATUS không phải số, thiếu cả hai mã → lỗi.
func (*Adapter) SplitWebhook(raw []byte) ([]fulfillment.WebhookSignal, error) {
	var env webhookEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("tách webhook Viettel Post: %w", errWebhookBody)
	}
	d := bytes.TrimSpace(env.Data)
	if len(d) == 0 || d[0] != '{' {
		return nil, fmt.Errorf("tách webhook Viettel Post: thiếu DATA: %w", errWebhookBody)
	}
	var data webhookData
	if err := json.Unmarshal(d, &data); err != nil {
		return nil, fmt.Errorf("tách webhook Viettel Post: %w", errWebhookBody)
	}
	tracking, ref := strings.TrimSpace(data.OrderNumber), strings.TrimSpace(data.OrderReference)
	if tracking == "" && ref == "" {
		return nil, fmt.Errorf("tách webhook Viettel Post: thiếu mã vận đơn: %w", errWebhookBody)
	}
	status, err := statusCode(data.Status)
	if err != nil {
		return nil, fmt.Errorf("tách webhook Viettel Post: %w", err)
	}
	key := ""
	if at, ok := parseStatusDate(data.StatusDate); ok && status != "" {
		key = "vtp:" + cmp.Or(tracking, ref) + ":" + status + ":" + at.Format(time.RFC3339)
	}
	return []fulfillment.WebhookSignal{{EventKey: key, TrackingCode: tracking, ShipmentCode: ref}}, nil
}

// statusCode đọc ORDER_STATUS (số, hay chuỗi số): thiếu, null → ""; không phải số nguyên → lỗi.
func statusCode(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", errWebhookBody
		}
	} else {
		s = string(raw)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return "", fmt.Errorf("ORDER_STATUS: %w", errWebhookBody)
	}
	return strconv.FormatInt(n, 10), nil
}

// parseStatusDate đọc ORDER_STATUSDATE (dd/MM/yyyy HH:mm:ss, giờ Việt Nam) → UTC.
func parseStatusDate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation(statusDateLayout, strings.TrimSpace(s), vietnam())
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
