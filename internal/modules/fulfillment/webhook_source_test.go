package fulfillment_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// splitCarrier là adapter thử chỉ có SplitWebhook: trả sigs, hoặc err.
type splitCarrier struct {
	fulfillment.Unsupported
	sigs []fulfillment.WebhookSignal
	err  error
}

func (splitCarrier) Key() string { return "fake" }

func (c splitCarrier) SplitWebhook([]byte) ([]fulfillment.WebhookSignal, error) { return c.sigs, c.err }

const (
	rkA = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rkB = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

// SplitSignals: mỗi tín hiệu → carrier.signal định tuyến carrier_account:<routeKey>, chống trùng routeKey:eventKey
// (hãng không cho khóa: sha256 của thân + chỉ số), OrderKey shipment:<mã CRM hoặc mã hãng>; thiếu cả hai mã → invalid.
func TestSplitSignals(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"any":"body"}`)
	sum := sha256.Sum256(raw)
	h := hex.EncodeToString(sum[:])
	a := splitCarrier{sigs: []fulfillment.WebhookSignal{
		{EventKey: "e1", TrackingCode: "FK0001"},
		{EventKey: "e2", TrackingCode: "FK0002", ShipmentCode: "E261121AAAAAA"},
		{TrackingCode: "FK0003"},
		{EventKey: "e4"},
	}}
	res, err := fulfillment.SplitSignals(a, raw, rkA)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 3 || res.Skipped["invalid"] != 1 {
		t.Fatalf("Split = %d sự kiện, bỏ %v", len(res.Events), res.Skipped)
	}
	want := []struct {
		dedupe, order string
		sig           fulfillment.Signal
	}{
		{rkA + ":e1", "shipment:FK0001", fulfillment.Signal{RouteKey: rkA, EventKey: "e1", TrackingCode: "FK0001"}},
		{rkA + ":e2", "shipment:E261121AAAAAA", fulfillment.Signal{
			RouteKey: rkA, EventKey: "e2", TrackingCode: "FK0002", ShipmentCode: "E261121AAAAAA",
		}},
		{rkA + ":" + h + ":2", "shipment:FK0003", fulfillment.Signal{RouteKey: rkA, EventKey: h + ":2", TrackingCode: "FK0003"}},
	}
	for i, w := range want {
		e := res.Events[i]
		var sig fulfillment.Signal
		if err := json.Unmarshal(e.Payload, &sig); err != nil {
			t.Fatal(err)
		}
		if e.Type != fulfillment.SignalType || e.DedupeKey != w.dedupe || e.OrderKey != w.order ||
			e.Route != (webhooks.Route{Kind: fulfillment.RouteKindCarrierAccount, ExternalKey: rkA}) || sig != w.sig {
			t.Errorf("sự kiện %d = %+v, tín hiệu %+v; muốn %+v", i, e, sig, w)
		}
	}
	// Cùng thân ở tài khoản khác: khóa chống trùng khác (theo tài khoản hãng).
	other, err := fulfillment.SplitSignals(a, raw, rkB)
	if err != nil || other.Events[0].DedupeKey == res.Events[0].DedupeKey || other.Events[2].DedupeKey == res.Events[2].DedupeKey {
		t.Fatalf("tài khoản khác cùng khóa chống trùng: %+v %v", other.Events, err)
	}
}

// Gói không có routeKey (đường /hooks/<hãng> cũ) → ErrNoRouteKey; thân hãng không đọc được → lỗi.
func TestSplitSignals_Errors(t *testing.T) {
	t.Parallel()
	ok := splitCarrier{sigs: []fulfillment.WebhookSignal{{EventKey: "e1", TrackingCode: "FK1"}}}
	if _, err := fulfillment.SplitSignals(ok, []byte(`{}`), ""); !errors.Is(err, fulfillment.ErrNoRouteKey) {
		t.Fatalf("không routeKey = %v, muốn ErrNoRouteKey", err)
	}
	bad := splitCarrier{err: errors.New("thân hỏng")}
	if _, err := fulfillment.SplitSignals(bad, []byte(`x`), rkA); err == nil {
		t.Fatal("thân không đọc được mà không lỗi")
	}
}
