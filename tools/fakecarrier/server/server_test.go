package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const secret = "0123456789abcdef0123456789abcdef"

// hookSink nhận webhook và kiểm chữ ký bằng fake.Verify của CRM.
type hookSink struct {
	mu   sync.Mutex
	got  []fake.Event
	srv  *httptest.Server
	auth []bool
}

func newSink(t *testing.T) *hookSink {
	t.Helper()
	k := &hookSink{}
	k.srv = httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var p fake.Packet
		_ = json.Unmarshal(b, &p)
		k.mu.Lock()
		k.got = append(k.got, p.Events...)
		k.auth = append(k.auth, fake.Verify(b, r.Header, secret))
		k.mu.Unlock()
	}))
	t.Cleanup(k.srv.Close)
	return k
}

type rig struct {
	t    *testing.T
	srv  *httptest.Server
	cli  *fake.Client
	sink *hookSink
	path string
}

func newRig(t *testing.T, path string) *rig {
	t.Helper()
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sink := newSink(t)
	cfg := Options{Store: st, Secret: secret, WebhookURL: sink.srv.URL, Hang: 2 * time.Second, HTTPClient: &http.Client{}, Logger: slog.New(slog.DiscardHandler)}
	srv := httptest.NewServer(New(cfg))
	t.Cleanup(srv.Close)
	return &rig{t: t, srv: srv, cli: fake.NewClient(srv.URL, secret, srv.Client()), sink: sink, path: path}
}

func (r *rig) admin(path, body string) (int, map[string]any) {
	r.t.Helper()
	req, _ := http.NewRequestWithContext(r.t.Context(), http.MethodPost, r.srv.URL+path, bytes.NewBufferString(body))
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func account() fulfillment.Account {
	return fulfillment.Account{ID: uuid.New(), ExternalAccountRef: "fake-1"}
}

// Tạo kiện (cùng code lần hai trả kiện cũ), tra theo code, đọc theo tracking qua client CRM; thiếu Bearer → 401.
func TestParcelsAPI(t *testing.T) {
	r := newRig(t, "")
	a, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118AAAAAA", CODAmount: 530000})
	if err != nil || a.TrackingCode == "" || a.Fee != 25000 {
		t.Fatalf("tạo kiện = %+v %v", a, err)
	}
	b, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118AAAAAA", CODAmount: 530000})
	if err != nil || b.TrackingCode != a.TrackingCode {
		t.Fatalf("cùng code lần hai = %+v %v, muốn %s", b, err, a.TrackingCode)
	}
	lr, err := r.cli.LookupByCode(t.Context(), account(), "E261118AAAAAA")
	if err != nil || !lr.Found || lr.TrackingCode != a.TrackingCode || lr.RawStatus != rawCreated {
		t.Fatalf("tra theo code = %+v %v", lr, err)
	}
	if lr, err := r.cli.LookupByCode(t.Context(), account(), "E261118ZZZZZZ"); err != nil || lr.Found {
		t.Fatalf("tra code lạ = %+v %v", lr, err)
	}
	sr, err := r.cli.GetStatus(t.Context(), account(), a.TrackingCode)
	if err != nil || sr.RawStatus != rawCreated || sr.EventKey != "created:1" {
		t.Fatalf("đọc kiện = %+v %v", sr, err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, r.srv.URL+"/v1/parcels/"+a.TrackingCode, nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("thiếu Bearer = %d, muốn 401", resp.StatusCode)
	}
	bad := fake.NewClient(r.srv.URL, "sai", nil)
	if _, err := bad.GetStatus(t.Context(), account(), a.TrackingCode); err == nil {
		t.Fatal("Bearer sai mà đọc được")
	}
}

// advance đổi trạng thái rồi gửi webhook có chữ ký đúng; forge: webhook nói delivered, API vẫn trả trạng thái thật.
func TestAdvanceAndForge(t *testing.T) {
	r := newRig(t, "")
	a, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118BBBBBB"})
	if err != nil {
		t.Fatal(err)
	}
	if s, m := r.admin("/admin/parcels/"+a.TrackingCode+"/advance", `{"to":"picked_up"}`); s != http.StatusOK || m["webhookStatus"] != float64(200) {
		t.Fatalf("advance = %d %v", s, m)
	}
	if sr, _ := r.cli.GetStatus(t.Context(), account(), a.TrackingCode); sr.RawStatus != rawPickedUp || sr.EventKey != "picked_up:2" {
		t.Fatalf("sau advance = %+v", sr)
	}
	if s, _ := r.admin("/admin/mode", `{"mode":"forge"}`); s != http.StatusOK {
		t.Fatal("đặt forge")
	}
	if s, _ := r.admin("/admin/parcels/"+a.TrackingCode+"/advance", `{"to":"picked_up"}`); s != http.StatusOK {
		t.Fatal("advance forge")
	}
	if sr, _ := r.cli.GetStatus(t.Context(), account(), a.TrackingCode); sr.RawStatus != rawPickedUp {
		t.Fatalf("forge đổi trạng thái thật: %+v", sr)
	}
	r.sink.mu.Lock()
	defer r.sink.mu.Unlock()
	if len(r.sink.got) != 2 || r.sink.got[0].Status != rawPickedUp || r.sink.got[1].Status != rawDelivered ||
		r.sink.got[0].AccountKey != "fake-1" || r.sink.got[0].EventID == r.sink.got[1].EventID ||
		!r.sink.auth[0] || !r.sink.auth[1] {
		t.Fatalf("webhook = %+v, chữ ký %v", r.sink.got, r.sink.auth)
	}
	if s, _ := r.admin("/admin/mode", `{"mode":"la"}`); s != http.StatusUnprocessableEntity {
		t.Fatal("chế độ lạ phải 422")
	}
	if s, _ := r.admin("/admin/parcels/FCNOPE/advance", `{"to":"delivered"}`); s != http.StatusNotFound {
		t.Fatal("kiện lạ phải 404")
	}
}

// fail_before_create → ErrNotSent, không kiện; timeout_after_create → kiện đã tạo nhưng bên gọi quá giờ (không phải
// ErrNotSent), tra theo code thấy kiện.
func TestFailureModes(t *testing.T) {
	r := newRig(t, "")
	r.admin("/admin/mode", `{"mode":"fail_before_create"}`)
	if _, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118CCCCCC"}); !errors.Is(err, fulfillment.ErrNotSent) {
		t.Fatalf("fail_before_create: %v", err)
	}
	if lr, _ := r.cli.LookupByCode(t.Context(), account(), "E261118CCCCCC"); lr.Found {
		t.Fatal("fail_before_create mà có kiện")
	}
	r.admin("/admin/mode", `{"mode":"timeout_after_create"}`)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err := r.cli.CreateShipment(ctx, account(), fulfillment.CreateRequest{Code: "E261118DDDDDD"})
	if err == nil || errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("timeout_after_create: %v, muốn lỗi không rõ", err)
	}
	if lr, _ := r.cli.LookupByCode(t.Context(), account(), "E261118DDDDDD"); !lr.Found {
		t.Fatal("timeout_after_create: hãng phải đã tạo kiện")
	}
}

// Trạng thái sống qua khởi động lại (tệp JSON).
func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fc.json")
	r := newRig(t, path)
	a, err := r.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118EEEEEE"})
	if err != nil {
		t.Fatal(err)
	}
	r.admin("/admin/parcels/"+a.TrackingCode+"/advance", `{"to":"delivered"}`)
	r2 := newRig(t, path)
	if sr, err := r2.cli.GetStatus(t.Context(), account(), a.TrackingCode); err != nil || sr.RawStatus != rawDelivered {
		t.Fatalf("sau khởi động lại = %+v %v", sr, err)
	}
	if b, _ := r2.cli.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118FFFFFF"}); b.TrackingCode == a.TrackingCode {
		t.Fatal("mã hãng bị dùng lại sau khởi động lại")
	}
}
