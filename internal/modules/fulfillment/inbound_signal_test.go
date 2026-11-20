package fulfillment_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// refreshJobs đếm lệnh tra do tín hiệu webhook ghi (khóa sig:) của vận đơn id.
func (e *shipEnv) refreshJobs(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'
		AND payload->>'shipmentId' = $1 AND dedupe_key LIKE 'sig:%'`, id.String())
}

func metricCount(reg *metrics.Registry, name, label string) int64 {
	return metricRows(reg.Drain(), name, label)
}

func metricRows(rows []metrics.Row, name, label string) int64 {
	var n int64
	for _, r := range rows {
		if r.Name == name && r.Label == label {
			n += r.Count
		}
	}
	return n
}

// Tín hiệu có mã hãng → một lệnh tra {shipmentId, reason: webhook}, khóa sig:<id>:<eventKey>, OrderKey
// shipment:<id>; vận đơn không đổi (chỉ là tín hiệu). Mã CRM tìm được cả khi mã hãng sai.
func TestSignal_EnqueuesRefreshOnly(t *testing.T) {
	e := newShipEnv(t)
	_, _, s := e.shipped(1)
	e.signal(s.TrackingCode, "ev-1")
	var dedupe, reason, order string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT dedupe_key, payload->>'reason', order_key FROM platform.jobs
			WHERE kind = 'fulfillment.refresh_status' AND dedupe_key LIKE 'sig:%'`).Scan(&dedupe, &reason, &order)
	}); err != nil {
		t.Fatal(err)
	}
	if dedupe != "sig:"+s.ID.String()+":"+e.accountRef+"/ev-1" || reason != "webhook" || order != "shipment:"+s.ID.String() {
		t.Fatalf("lệnh tra = %q %q %q", dedupe, reason, order)
	}
	if got := e.shipment(s.ID); got.Status != s.Status || got.Version != s.Version {
		t.Fatalf("vận đơn đổi ở bước tín hiệu: %s v%d", got.Status, got.Version)
	}
	sig := fulfillment.Signal{RouteKey: e.routeKey(e.account), EventKey: "by-code", TrackingCode: "SAI-MA", ShipmentCode: s.Code}
	if err := e.trySignal(sig, nil); err != nil {
		t.Fatal(err)
	}
	if n := e.refreshJobs(s.ID); n != 2 {
		t.Fatalf("lệnh tra = %d, muốn 2 (theo mã hãng, theo mã CRM)", n)
	}
}

// VC10: tín hiệu đến khi vận đơn chưa có mã hãng → ErrShipmentNotYetKnown (lỗi tạm, không ErrPermanent), không lệnh
// nào; gắn mã hãng rồi giao lại → một lệnh tra. Vận đơn của tài khoản khác không được tìm thấy (Q-7).
func TestSignal_ShipmentNotYetKnown(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	sig := fulfillment.Signal{RouteKey: e.routeKey(e.account), EventKey: "early", TrackingCode: "TRK-LATE"}
	err := e.trySignal(sig, nil)
	if !errors.Is(err, fulfillment.ErrShipmentNotYetKnown) || errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("tín hiệu sớm = %v, muốn ErrShipmentNotYetKnown tạm", err)
	}
	if n := e.refreshJobs(s.ID); n != 0 {
		t.Fatalf("lệnh tra = %d", n)
	}
	// Cùng mã hãng nhưng gửi vào tuyến của tài khoản đối tác: không thấy.
	e.ownerExec("UPDATE fulfillment.shipments SET tracking_code = 'TRK-LATE' WHERE id = $1", s.ID)
	partner := sig
	partner.RouteKey = e.routeKey(e.shop.PartnerAccount)
	if err := e.trySignal(partner, nil); !errors.Is(err, fulfillment.ErrShipmentNotYetKnown) {
		t.Fatalf("tuyến tài khoản khác = %v, muốn ErrShipmentNotYetKnown", err)
	}
	if err := e.trySignal(sig, nil); err != nil {
		t.Fatal(err)
	}
	if n := e.refreshJobs(s.ID); n != 1 {
		t.Fatalf("lệnh tra = %d, muốn 1", n)
	}
}

// Tài khoản revoked → bỏ, carrier_signal_revoked_account +1; route_key lạ hay tài khoản của hãng khác nguồn → bỏ,
// carrier_signal_unknown_account +1; không lệnh tra nào.
func TestSignal_AccountDropped(t *testing.T) {
	e := newShipEnv(t)
	_, _, s := e.shipped(1)
	reg := metrics.NewRegistry()
	rk := e.routeKey(e.account)
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'revoked' WHERE id = $1", e.account)
	if err := e.trySignal(fulfillment.Signal{RouteKey: rk, EventKey: "r", TrackingCode: s.TrackingCode}, reg); err != nil {
		t.Fatal(err)
	}
	if n := metricCount(reg, "carrier_signal_revoked_account", "fake"); n != 1 {
		t.Fatalf("revoked = %d", n)
	}
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'active' WHERE id = $1", e.account)
	other := hookCarrier{Carrier: e.carrier, key: "fake_b"}
	for _, c := range []struct {
		a  fulfillment.CarrierAdapter
		rk string
	}{{hookCarrier{Carrier: e.carrier}, "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"}, {other, rk}} {
		if err := e.trySignalFrom(c.a, fulfillment.Signal{RouteKey: c.rk, EventKey: "u", TrackingCode: s.TrackingCode}, reg); err != nil {
			t.Fatal(err)
		}
	}
	rows := reg.Drain()
	if a, b := metricRows(rows, "carrier_signal_unknown_account", "fake"), metricRows(rows, "carrier_signal_unknown_account", "fake_b"); a != 1 || b != 1 {
		t.Fatalf("unknown = fake %d, fake_b %d; muốn 1, 1", a, b)
	}
	if n := e.refreshJobs(s.ID); n != 0 {
		t.Fatalf("lệnh tra = %d, muốn 0", n)
	}
}

// VC13: A1 (fake) và A2 (fake_b) cùng có vận đơn mã hãng X123; tín hiệu vào tuyến của A2 → lệnh tra chỉ cho vận đơn
// của A2.
func TestSignal_SameTrackingTwoAccounts(t *testing.T) {
	e := newShipEnv(t)
	var a2 uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) (err error) {
		a2, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: e.seller, WarehouseID: e.shop.Warehouse, CarrierKey: "fake_b", Name: "Hãng giả B", ExternalAccountRef: "fakeb-1",
		})
		return err //nolint:wrapcheck // test
	})
	_, _, s1 := e.shipped(1)
	_, _, s2 := e.shipped(1)
	e.ownerExec("UPDATE fulfillment.shipments SET tracking_code = 'X123' WHERE id = $1", s1.ID)
	e.ownerExec("UPDATE fulfillment.shipments SET carrier_account_id = $2, carrier_key = 'fake_b', tracking_code = 'X123' WHERE id = $1", s2.ID, a2)
	b := hookCarrier{Carrier: e.carrier, key: "fake_b"}
	if err := e.trySignalFrom(b, fulfillment.Signal{RouteKey: e.routeKey(a2), EventKey: "x", TrackingCode: "X123"}, nil); err != nil {
		t.Fatal(err)
	}
	if n1, n2 := e.refreshJobs(s1.ID), e.refreshJobs(s2.ID); n1 != 0 || n2 != 1 {
		t.Fatalf("lệnh tra A1 = %d (muốn 0), A2 = %d (muốn 1)", n1, n2)
	}
}

// Loại lạ, payload thiếu trường → ErrPermanent.
func TestSignal_BadPayload(t *testing.T) {
	e := newShipEnv(t)
	for name, sig := range map[string]fulfillment.Signal{
		"thiếu route": {EventKey: "e", TrackingCode: "T"},
		"thiếu khóa":  {RouteKey: e.routeKey(e.account), TrackingCode: "T"},
		"thiếu mã":    {RouteKey: e.routeKey(e.account), EventKey: "e"},
	} {
		if err := e.trySignal(sig, nil); !errors.Is(err, queue.ErrPermanent) {
			t.Errorf("%s = %v, muốn ErrPermanent", name, err)
		}
	}
}

// routeCarrier ghi Account mà lệnh tạo truyền cho hãng.
type routeCarrier struct {
	*fake.Carrier
	got *fulfillment.Account
}

func (r routeCarrier) CreateShipment(ctx context.Context, a fulfillment.Account, req fulfillment.CreateRequest) (fulfillment.CreateResult, error) {
	*r.got = a
	return r.Carrier.CreateShipment(ctx, a, req) //nolint:wrapcheck // test
}

// Lệnh tạo vận đơn gửi cho hãng route_key của tài khoản: hãng gửi webhook về /hooks/<hãng>/<route_key> (đường không
// khóa tuyến không còn nhận).
func TestCreateShipment_SendsRouteKey(t *testing.T) {
	e := newShipEnv(t)
	var got fulfillment.Account
	o := fulfillment.CommandOptions{
		DB: e.staff.App, Adapters: []fulfillment.CarrierAdapter{routeCarrier{Carrier: e.carrier, got: &got}},
		Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{},
	}
	e.useCommands([]commands.Handler{fulfillment.NewCreateCommand(o)})
	ord, _ := e.order(1)
	e.mustCreateShipment(ord.ID)
	e.drain()
	if want := e.routeKey(e.account); got.RouteKey != want || got.ID != e.account {
		t.Fatalf("Account gửi hãng = %+v, muốn RouteKey %q", got, want)
	}
}
