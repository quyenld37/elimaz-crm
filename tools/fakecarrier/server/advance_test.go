package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Trang quản trị đổi kiện sang mọi trạng thái thô của hãng giả (đường trọn M3, Task 139: giao thất bại, đang hoàn, đã
// hoàn, thất lạc…): mỗi lần đổi gửi một webhook đúng trạng thái và API tra trả trạng thái đó; trạng thái lạ → 422.
func TestAdvanceEveryRawStatus(t *testing.T) {
	r := newRig(t, "")
	a, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261128CCCCCC"})
	if err != nil {
		t.Fatal(err)
	}
	steps := []string{
		fake.RawPickedUp, fake.RawInTransit, fake.RawOutForDelivery, fake.RawDeliveryFailed, fake.RawOutForDelivery,
		fake.RawDelivered, fake.RawReturning, fake.RawReturned, fake.RawLost, fake.RawCancelled, fake.RawCreated,
	}
	for _, to := range steps {
		if s, m := r.admin("/admin/parcels/"+a.TrackingCode+"/advance", `{"to":"`+to+`"}`); s != http.StatusOK ||
			m["webhookStatus"] != float64(200) {
			t.Fatalf("advance %s = %d %v", to, s, m)
		}
		if sr, err := r.cli.GetStatus(t.Context(), account(), a.TrackingCode); err != nil || sr.RawStatus != to {
			t.Fatalf("sau advance %s: %+v %v", to, sr, err)
		}
	}
	r.sink.mu.Lock()
	got := append([]fake.Event(nil), r.sink.got...)
	r.sink.mu.Unlock()
	if len(got) != len(steps) {
		t.Fatalf("%d webhook, muốn %d", len(got), len(steps))
	}
	for i, to := range steps {
		if got[i].Status != to {
			t.Errorf("webhook %d = %s, muốn %s", i, got[i].Status, to)
		}
	}
	if s, _ := r.admin("/admin/parcels/"+a.TrackingCode+"/advance", `{"to":"bay_mat"}`); s != http.StatusUnprocessableEntity {
		t.Fatalf("trạng thái lạ = %d, muốn 422", s)
	}
}

// TrackDelay: mọi lần tra (theo mã hãng, hành trình) trả sau ít nhất TrackDelay (bài thử tải webhook hãng, Task 139:
// máy giả trả trong 200 ms); tạo kiện không chờ.
func TestTrackDelay(t *testing.T) {
	st, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	const delay = 150 * time.Millisecond
	srv := httptest.NewServer(New(Options{Store: st, Secret: secret, TrackDelay: delay, Logger: slog.New(slog.DiscardHandler)}))
	t.Cleanup(srv.Close)
	cli := fake.NewClient(srv.URL, secret, srv.Client())
	start := time.Now()
	a, err := cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261128DDDDDD"})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took >= delay {
		t.Errorf("tạo kiện mất %v, không được chờ TrackDelay", took)
	}
	start = time.Now()
	if _, err := cli.GetStatus(t.Context(), account(), a.TrackingCode); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < delay {
		t.Errorf("tra mất %v, muốn ≥ %v", took, delay)
	}
}
