package fulfillment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/inbound"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// SignalType là webhooks.InboundEvent.Type của tín hiệu hãng: "vận đơn này vừa đổi, hãy hỏi lại".
const SignalType = inbound.SignalType

// Signal là payload chuẩn của một tín hiệu hãng (SplitSignals dựng, handler vào của module đọc). Không mang trạng
// thái: trạng thái thật chỉ đọc qua API có xác thực (B10).
type Signal = inbound.Signal

// ErrShipmentNotYetKnown báo tài khoản có nhưng chưa có vận đơn mang mã của tín hiệu; lỗi tạm (việc vào thử lại theo
// lịch của hàng đợi; ở lượt thử cuối handler ghi "ngoài hệ thống" và xong thay vì dead, Task 265).
var ErrShipmentNotYetKnown = inbound.ErrShipmentNotYetKnown

// ErrNoRouteKey báo gói webhook hãng nhận ở đường không có khóa tuyến (/hooks/<hãng>); hãng chỉ được đăng ký đường
// /hooks/<hãng>/<route_key>.
var ErrNoRouteKey = errors.New("fulfillment: webhook hãng không có khóa tuyến")

// skippedInvalid là nhãn SplitResult.Skipped của tín hiệu không có mã vận đơn nào.
const skippedInvalid = "invalid"

// SplitSignals tách gói webhook raw của hãng a nhận ở đường có khóa tuyến routeKey: a.SplitWebhook, mỗi tín hiệu →
// InboundEvent{Type: SignalType, DedupeKey: routeKey:eventKey (hãng không cho khóa: sha256 của thân + ":" + chỉ số),
// Route: carrier_account:routeKey, OrderKey: shipment:<mã CRM hoặc mã hãng>, Payload: Signal}. Tín hiệu không có mã
// nào → Skipped["invalid"]. routeKey rỗng → ErrNoRouteKey; thân không đọc được → lỗi (ingest coi là thân hỏng).
func SplitSignals(a CarrierAdapter, raw []byte, routeKey string) (webhooks.SplitResult, error) {
	if routeKey == "" {
		return webhooks.SplitResult{}, ErrNoRouteKey
	}
	sigs, err := a.SplitWebhook(raw)
	if err != nil {
		return webhooks.SplitResult{}, fmt.Errorf("tách webhook hãng %s: %w", a.Key(), err)
	}
	res := webhooks.SplitResult{Skipped: map[string]int{}}
	var bodyHash string
	for i, s := range sigs {
		code := s.ShipmentCode
		if code == "" {
			code = s.TrackingCode
		}
		if code == "" {
			res.Skipped[skippedInvalid]++
			continue
		}
		key := s.EventKey
		if key == "" {
			if bodyHash == "" {
				sum := sha256.Sum256(raw)
				bodyHash = hex.EncodeToString(sum[:])
			}
			key = bodyHash + ":" + strconv.Itoa(i)
		}
		payload, err := json.Marshal(Signal{
			RouteKey: routeKey, EventKey: key, TrackingCode: s.TrackingCode, ShipmentCode: s.ShipmentCode,
		})
		if err != nil {
			return webhooks.SplitResult{}, fmt.Errorf("mã hóa tín hiệu hãng: %w", err)
		}
		res.Events = append(res.Events, webhooks.InboundEvent{
			Type: SignalType, DedupeKey: routeKey + ":" + key,
			Route:    webhooks.Route{Kind: RouteKindCarrierAccount, ExternalKey: routeKey},
			OrderKey: "shipment:" + code, Payload: payload,
		})
	}
	return res, nil
}
