package carrier

import (
	"net/http"
	"net/url"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// webhookSource là nguồn webhook chung của một hãng (kế hoạch M3, Task M3-10): /hooks/<khóa hãng>/<route_key>.
type webhookSource struct {
	a      fulfillment.CarrierAdapter
	secret string
}

// WebhookSource trả nguồn webhook của hãng a: Key() = a.Key(); VerifySignature gọi a.VerifyWebhook với secret (bí
// mật theo hãng; rỗng ở worker, nơi chỉ tách gói); SplitRouted tách bằng fulfillment.SplitSignals với khóa tuyến;
// Split (đường /hooks/<hãng> không có khóa tuyến) trả fulfillment.ErrNoRouteKey. Không có bước GET xác minh.
func WebhookSource(a fulfillment.CarrierAdapter, secret string) webhooks.RoutedSource { //nolint:ireturn // kiểu nguồn của ingest
	return webhookSource{a: a, secret: secret}
}

// WebhookSources trả nguồn của mọi hãng trong as khai Capabilities().Webhook (đúng tập hãng mà
// fulfillment.Module.InboundHandlers dựng handler), bí mật của mỗi hãng lấy bằng secret(khóa hãng).
func WebhookSources(as []fulfillment.CarrierAdapter, secret func(key string) string) []webhooks.Source {
	var out []webhooks.Source
	for _, a := range as {
		if a.Capabilities().Webhook {
			out = append(out, WebhookSource(a, secret(a.Key())))
		}
	}
	return out
}

func (s webhookSource) Key() string { return s.a.Key() }

func (webhookSource) Challenge(url.Values) (string, bool) { return "", false }

func (s webhookSource) VerifySignature(raw []byte, h http.Header) bool {
	return s.a.VerifyWebhook(raw, fulfillment.Header(h), s.secret)
}

func (s webhookSource) Split(raw []byte) (webhooks.SplitResult, error) {
	return fulfillment.SplitSignals(s.a, raw, "") //nolint:wrapcheck // lỗi đã mang tên việc
}

func (s webhookSource) SplitRouted(raw []byte, routeKey string) (webhooks.SplitResult, error) {
	return fulfillment.SplitSignals(s.a, raw, routeKey) //nolint:wrapcheck // lỗi đã mang tên việc
}
