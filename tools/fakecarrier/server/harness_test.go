package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// /admin/mode không có op vẫn áp cho tạo kiện như M1; {op: cancel, mode: rate_limited} → hủy trả 429 Retry-After: 7,
// tạo kiện vẫn ok; op lạ, chế độ không hợp với op → 422.
func TestModeByOp(t *testing.T) {
	r := newRig(t, "")
	if s, _ := r.admin("/admin/mode", `{"mode":"timeout_after_create"}`); s != http.StatusOK {
		t.Fatal("đặt timeout_after_create không op")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	_, err := r.cli.CreateShipment(ctx, account(), fulfillment.CreateRequest{Code: "E261120AAAAAA"})
	cancel()
	if err == nil || errors.Is(err, fulfillment.ErrNotSent) {
		t.Fatalf("không op phải áp cho tạo kiện: %v", err)
	}
	r.admin("/admin/mode", `{"mode":"ok"}`)
	if s, _ := r.admin("/admin/mode", `{"op":"cancel","mode":"rate_limited"}`); s != http.StatusOK {
		t.Fatal("đặt rate_limited cho cancel")
	}
	res, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261120BBBBBB"})
	if err != nil {
		t.Fatalf("tạo kiện phải vẫn ok: %v", err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, r.srv.URL+"/v1/parcels/"+res.TrackingCode+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "7" {
		t.Fatalf("hủy = %d Retry-After %q, muốn 429 7", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	_, err = r.cli.Cancel(t.Context(), account(), res.TrackingCode)
	if ce, ok := errors.AsType[*fulfillment.CarrierError](err); !ok || ce.Class != fulfillment.ClassRateLimited ||
		ce.RetryAfter != 7*time.Second {
		t.Fatalf("client hủy 429 = %v", err)
	}
	for _, body := range []string{`{"op":"la","mode":"ok"}`, `{"op":"cancel","mode":"timeout_after_create"}`, `{"op":"create","mode":"fail_before_send"}`} {
		if s, _ := r.admin("/admin/mode", body); s != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, muốn 422", body, s)
		}
	}
}

// Kiện có routeKey: webhook gửi tới FAKECARRIER_WEBHOOK_URL/<routeKey>, chữ ký vẫn đúng; kiện không có (M1) gửi URL gốc.
func TestWebhookRouteKey(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
		auth  []bool
	)
	src := fake.NewSource(secret)
	sink := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		mu.Lock()
		paths, auth = append(paths, r.URL.Path), append(auth, src.VerifySignature(b, r.Header))
		mu.Unlock()
	}))
	t.Cleanup(sink.Close)
	srv := httptest.NewServer(New(Options{Secret: secret, WebhookURL: sink.URL + "/hooks/fakecarrier"}))
	t.Cleanup(srv.Close)
	cli := fake.NewClient(srv.URL, secret, srv.Client())
	withRoute := account()
	withRoute.RouteKey = "rk-01"
	a, err := cli.CreateShipment(t.Context(), withRoute, fulfillment.CreateRequest{Code: "E261120CCCCCC"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261120DDDDDD"})
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, srv: srv}
	r.admin("/admin/parcels/"+a.TrackingCode+"/advance", `{"to":"picked_up"}`)
	r.admin("/admin/parcels/"+b.TrackingCode+"/advance", `{"to":"picked_up"}`)
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[0] != "/hooks/fakecarrier/rk-01" || paths[1] != "/hooks/fakecarrier" || !auth[0] || !auth[1] {
		t.Fatalf("đường webhook = %v, chữ ký %v", paths, auth)
	}
}

// Harness: Calls đếm theo thao tác, Has theo mã CRM, Push đổi trạng thái và thêm sự kiện hành trình, Reset xóa hết.
func TestHarness(t *testing.T) {
	s := New(Options{Secret: secret})
	hs := httptest.NewServer(s)
	t.Cleanup(hs.Close)
	cli := fake.NewClient(hs.URL, secret, hs.Client())
	res, err := cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261120EEEEEE"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Calls("create") != 1 || !s.Has("E261120EEEEEE") || s.Has("E261120ZZZZZZ") {
		t.Fatalf("Calls = %d, Has sai", s.Calls("create"))
	}
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	s.Push(res.TrackingCode, fulfillment.CarrierEvent{Key: "k1", RawStatus: "in_transit", OccurredAt: at})
	tr, err := cli.Track(t.Context(), account(), res.TrackingCode, "E261120EEEEEE")
	if err != nil || len(tr.Events) != 2 || tr.Events[1].RawStatus != "in_transit" ||
		tr.Events[1].Canonical != fulfillment.CanonInTransit || !tr.Events[1].OccurredAt.Equal(at) {
		t.Fatalf("Track sau Push = %+v %v", tr, err)
	}
	if s.Calls("track") != 1 {
		t.Fatalf("Calls(track) = %d", s.Calls("track"))
	}
	s.SetMode("cancel", "reject")
	if _, err := cli.Cancel(t.Context(), account(), res.TrackingCode); !errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("cancel reject = %v", err)
	}
	s.Reset()
	if s.Has("E261120EEEEEE") || s.Calls("create") != 0 {
		t.Fatal("Reset không xóa")
	}
	if _, err := cli.Cancel(t.Context(), account(), "FC00000009"); !errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("hủy kiện lạ (sau Reset, chế độ về ok) = %v, muốn ErrRejected", err)
	}
}
