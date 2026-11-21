package vtp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/carrier/vtp"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const webhookSecret = "vtp-webhook-bi-mat-0123456789"

// webhookBody đọc trường "request" (thân webhook mẫu) của testdata/<name>.
func webhookBody(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Source  string          `json:"_source"`
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(b, &f); err != nil || f.Source == "" || len(f.Request) == 0 {
		t.Fatalf("testdata/%s: thiếu _source hay request: %v", name, err)
	}
	return f.Request
}

var phoneRe = regexp.MustCompile(`0[0-9]{9}|84[0-9]{9}`)

// TI02/VC12: 5 webhook mẫu (một hành trình) → mỗi thân một tín hiệu: mã vận đơn VTP (ORDER_NUMBER), mã vận đơn CRM
// (ORDER_REFERENCE), khóa sự kiện theo mã trạng thái và thời điểm giờ Việt Nam đổi sang UTC. Không SĐT, tên, tiền.
func TestSplitWebhookSamples(t *testing.T) {
	ad := vtp.New(vtp.Config{})
	for _, c := range []struct {
		file string
		want fulfillment.WebhookSignal
	}{
		{"webhook_104.json", fulfillment.WebhookSignal{
			EventKey: "vtp:TUANPAD3024822076:104:2025-11-10T04:07:16Z", TrackingCode: "TUANPAD3024822076",
			ShipmentCode: "TUANPAD3024822076",
		}},
		{"webhook_200.json", fulfillment.WebhookSignal{
			EventKey: "vtp:1587818000200:200:2025-11-11T01:30:00Z", TrackingCode: "1587818000200", ShipmentCode: "E261118ABC200",
		}},
		{"webhook_300.json", fulfillment.WebhookSignal{
			EventKey: "vtp:1587818000300:300:2025-11-11T10:45:10Z", TrackingCode: "1587818000300", ShipmentCode: "E261118ABC300",
		}},
		{"webhook_500.json", fulfillment.WebhookSignal{
			EventKey: "vtp:1587818000500:500:2025-11-13T00:05:59Z", TrackingCode: "1587818000500", ShipmentCode: "E261118ABC500",
		}},
		{"webhook_501.json", fulfillment.WebhookSignal{
			EventKey: "vtp:1587818000501:501:2025-11-13T16:59:59Z", TrackingCode: "1587818000501", ShipmentCode: "E261118ABC501",
		}},
	} {
		t.Run(c.file, func(t *testing.T) {
			sigs, err := ad.SplitWebhook(webhookBody(t, c.file))
			if err != nil || len(sigs) != 1 || sigs[0] != c.want {
				t.Fatalf("SplitWebhook = %+v %v, muốn %+v", sigs, err, c.want)
			}
			if s := strings.Join([]string{sigs[0].EventKey, sigs[0].TrackingCode, sigs[0].ShipmentCode}, " "); phoneRe.MatchString(s) {
				t.Errorf("tín hiệu có SĐT: %s", s)
			}
		})
	}
}

// Thân sai → lỗi (không phải JSON, thiếu DATA, DATA không phải đối tượng, thiếu cả hai mã). Thời điểm không đọc được
// hay thiếu mã trạng thái → khóa sự kiện rỗng (lõi chống trùng theo băm thân), vẫn là tín hiệu.
func TestSplitWebhookBadBodies(t *testing.T) {
	ad := vtp.New(vtp.Config{})
	for name, body := range map[string]string{
		"không phải JSON": `không phải json`,
		"thiếu DATA":      `{"TOKEN":"x"}`,
		"DATA null":       `{"DATA":null}`,
		"DATA là mảng":    `{"DATA":[{"ORDER_NUMBER":"1"}]}`,
		"thiếu mã":        `{"DATA":{"ORDER_STATUS":501,"ORDER_STATUSDATE":"10/11/2025 11:07:16"}}`,
		"trạng thái hỏng": `{"DATA":{"ORDER_NUMBER":"1","ORDER_STATUS":{"x":1}}}`,
	} {
		if sigs, err := ad.SplitWebhook([]byte(body)); err == nil {
			t.Errorf("%s: SplitWebhook = %+v, muốn lỗi", name, sigs)
		}
	}
	for name, body := range map[string]string{
		"giờ sai dạng":       `{"DATA":{"ORDER_NUMBER":"1587818","ORDER_STATUS":501,"ORDER_STATUSDATE":"2025-11-10 11:07"}}`,
		"thiếu trạng thái":   `{"DATA":{"ORDER_NUMBER":"1587818","ORDER_STATUSDATE":"10/11/2025 11:07:16"}}`,
		"ngày không có thật": `{"DATA":{"ORDER_NUMBER":"1587818","ORDER_STATUS":501,"ORDER_STATUSDATE":"31/02/2025 11:07:16"}}`,
	} {
		sigs, err := ad.SplitWebhook([]byte(body))
		if err != nil || len(sigs) != 1 || sigs[0] != (fulfillment.WebhookSignal{TrackingCode: "1587818"}) {
			t.Errorf("%s: SplitWebhook = %+v %v, muốn một tín hiệu khóa rỗng", name, sigs, err)
		}
	}
}

// VerifyWebhook: header Authorization (có hay không tiền tố Bearer) khớp bí mật → true; sai, thiếu, bí mật rỗng →
// false. Thân không ký nên không kiểm được thân (Quyết định Q3).
func TestVerifyWebhook(t *testing.T) {
	ad := vtp.New(vtp.Config{})
	body := webhookBody(t, "webhook_104.json")
	for name, c := range map[string]struct {
		h      fulfillment.Header
		secret string
		want   bool
	}{
		"đúng":                 {fulfillment.Header{"Authorization": {webhookSecret}}, webhookSecret, true},
		"đúng có Bearer":       {fulfillment.Header{"Authorization": {"Bearer " + webhookSecret}}, webhookSecret, true},
		"sai":                  {fulfillment.Header{"Authorization": {webhookSecret + "x"}}, webhookSecret, false},
		"tiền tố":              {fulfillment.Header{"Authorization": {webhookSecret[:10]}}, webhookSecret, false},
		"thiếu header":         {fulfillment.Header{}, webhookSecret, false},
		"header rỗng":          {fulfillment.Header{"Authorization": {""}}, "", false},
		"bí mật rỗng":          {fulfillment.Header{"Authorization": {webhookSecret}}, "", false},
		"hai giá trị":          {fulfillment.Header{"Authorization": {"khac", webhookSecret}}, webhookSecret, false},
		"chỉ Bearer":           {fulfillment.Header{"Authorization": {"Bearer "}}, webhookSecret, false},
		"đúng, khoảng trắng":   {fulfillment.Header{"Authorization": {" " + webhookSecret + " "}}, webhookSecret, true},
		"Token thay vì Author": {fulfillment.Header{"Token": {webhookSecret}}, webhookSecret, false},
	} {
		if got := ad.VerifyWebhook(body, c.h, c.secret); got != c.want {
			t.Errorf("%s: VerifyWebhook = %v, muốn %v", name, got, c.want)
		}
	}
}
