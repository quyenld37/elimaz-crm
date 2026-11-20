package fake_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const secret = "0123456789abcdef0123456789abcdef"

func account() fulfillment.Account {
	return fulfillment.Account{ID: uuid.New(), ExternalAccountRef: "fake-1"}
}

// stub là API hãng giả tối thiểu: ghi lại yêu cầu, trả theo status/body cho sẵn.
func stub(t *testing.T, status int, body string, seen *http.Request) *fake.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r.Clone(r.Context())
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return fake.NewClient(srv.URL+"/", secret, srv.Client())
}

// CreateShipment: 200 → mã hãng, phí, có Bearer; 503 → ErrNotSent; 422 → ErrRejected; 500 → lỗi không rõ (không
// phải hai lỗi trên); không nối được → ErrNotSent.
func TestClientCreateShipment(t *testing.T) {
	var seen http.Request
	c := stub(t, http.StatusOK, `{"tracking":"FK1","fee":25000}`, &seen)
	res, err := c.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E261118ABCDEF", CODAmount: 530000})
	if err != nil || res != (fulfillment.CreateResult{TrackingCode: "FK1", Fee: 25000, FeeKnown: true}) {
		t.Fatalf("CreateShipment = %+v %v", res, err)
	}
	if seen.Method != http.MethodPost || seen.URL.Path != "/v1/parcels" || seen.Header.Get("Authorization") != "Bearer "+secret {
		t.Fatalf("yêu cầu = %s %s %q", seen.Method, seen.URL.Path, seen.Header.Get("Authorization"))
	}
	cases := []struct {
		status    int
		want      error
		notTarget []error
	}{
		{http.StatusServiceUnavailable, fulfillment.ErrNotSent, nil},
		{http.StatusUnprocessableEntity, fulfillment.ErrRejected, nil},
		{http.StatusInternalServerError, nil, []error{fulfillment.ErrNotSent, fulfillment.ErrRejected}},
	}
	for _, cs := range cases {
		_, err := stub(t, cs.status, `{}`, nil).CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E1"})
		if err == nil || (cs.want != nil && !errors.Is(err, cs.want)) {
			t.Errorf("%d: %v, muốn %v", cs.status, err, cs.want)
		}
		for _, nt := range cs.notTarget {
			if errors.Is(err, nt) {
				t.Errorf("%d: lỗi không rõ không được là %v", cs.status, nt)
			}
		}
	}
	dead := fake.NewClient("http://127.0.0.1:1", secret, nil)
	if _, err := dead.CreateShipment(t.Context(), account(), fulfillment.CreateRequest{Code: "E1"}); !errors.Is(err, fulfillment.ErrNotSent) {
		t.Fatalf("không nối được: %v, muốn ErrNotSent", err)
	}
	// Quá giờ sau khi gửi → lỗi không rõ.
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = fake.NewClient(slow.URL, secret, nil).CreateShipment(ctx, account(), fulfillment.CreateRequest{Code: "E1"})
	if err == nil || errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("quá giờ: %v, muốn lỗi không rõ", err)
	}
}

// LookupByCode: 200 → Found; 404 → không có. GetStatus: 200 → EventKey <trạng thái>:<lần>; 404 → ErrRejected.
func TestClientLookupAndStatus(t *testing.T) {
	var seen http.Request
	lr, err := stub(t, http.StatusOK, `{"tracking":"FK1","status":"created"}`, &seen).LookupByCode(t.Context(), account(), "E1")
	if err != nil || lr != (fulfillment.LookupResult{Found: true, TrackingCode: "FK1", RawStatus: "created"}) ||
		seen.URL.Query().Get("code") != "E1" {
		t.Fatalf("LookupByCode = %+v %v %s", lr, err, seen.URL)
	}
	if lr, err := stub(t, http.StatusNotFound, ``, nil).LookupByCode(t.Context(), account(), "E1"); err != nil || lr.Found {
		t.Fatalf("LookupByCode 404 = %+v %v", lr, err)
	}
	sr, err := stub(t, http.StatusOK, `{"tracking":"FK1","status":"picked_up","seq":2,"updatedAt":"2026-11-18T01:00:00Z"}`, &seen).
		GetStatus(t.Context(), account(), "FK1")
	if err != nil || sr.RawStatus != "picked_up" || sr.EventKey != "picked_up:2" || seen.URL.Path != "/v1/parcels/FK1" {
		t.Fatalf("GetStatus = %+v %v", sr, err)
	}
	if _, err := stub(t, http.StatusNotFound, ``, nil).GetStatus(t.Context(), account(), "FK1"); !errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("GetStatus 404: %v", err)
	}
	c := fake.NewClient("http://x", secret, nil)
	for raw, want := range map[string]fulfillment.Status{
		"created": fulfillment.StatusAwaitingPickup, "picked_up": fulfillment.StatusShipping, "delivered": fulfillment.StatusDelivered,
	} {
		if got, ok := c.MapStatus(raw); !ok || got != want {
			t.Errorf("MapStatus(%s) = %s %v", raw, got, ok)
		}
	}
	if _, ok := c.MapStatus("lost"); ok {
		t.Error("MapStatus(lost) phải false")
	}
}

func packet(t *testing.T, evs ...fake.Event) []byte {
	t.Helper()
	b, err := json.Marshal(fake.Packet{Events: evs})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ev(id, account string) fake.Event {
	return fake.Event{EventID: id, AccountKey: account, Tracking: "FK1", Status: "delivered", SentAt: time.Now()}
}

// Chữ ký: đúng → true; sai khóa, thiếu tiền tố, thân đổi, bí mật rỗng → false. SplitWebhook: mỗi sự kiện đủ eventId,
// tracking → một tín hiệu (code của kiện nếu có); thiếu trường bị bỏ; thân không phải JSON → lỗi.
func TestVerifyAndSplitWebhook(t *testing.T) {
	c := fake.NewClient("http://khong-dung", secret, nil)
	body := packet(t, ev("e1", "fake-1"))
	h := http.Header{}
	h.Set(fake.SignatureHeader, fake.Sign(body, secret))
	if !c.VerifyWebhook(body, fulfillment.Header(h), secret) {
		t.Fatal("chữ ký đúng bị từ chối")
	}
	for name, hdr := range map[string]string{
		"sai khóa": fake.Sign(body, "khac"), "thiếu tiền tố": fake.Sign(body, secret)[7:], "rỗng": "",
	} {
		h := http.Header{}
		h.Set(fake.SignatureHeader, hdr)
		if c.VerifyWebhook(body, fulfillment.Header(h), secret) {
			t.Errorf("%s: chữ ký được nhận", name)
		}
	}
	if c.VerifyWebhook(append(bytes.Clone(body), ' '), fulfillment.Header(h), secret) || c.VerifyWebhook(body, fulfillment.Header(h), "") {
		t.Fatal("thân đổi hay bí mật rỗng mà chữ ký vẫn đúng")
	}
	withCode := ev("e2", "fake-1")
	withCode.Code = "E261121AAAAAA"
	sigs, err := c.SplitWebhook(packet(t, ev("e1", "fake-1"), ev("", "fake-1"), withCode))
	want := []fulfillment.WebhookSignal{{EventKey: "e1", TrackingCode: "FK1"}, {EventKey: "e2", TrackingCode: "FK1", ShipmentCode: "E261121AAAAAA"}}
	if err != nil || len(sigs) != 2 || sigs[0] != want[0] || sigs[1] != want[1] {
		t.Fatalf("SplitWebhook = %+v %v", sigs, err)
	}
	if _, err := c.SplitWebhook([]byte("không phải json")); err == nil {
		t.Fatal("thân hỏng mà không lỗi")
	}
}

// Hai hàm đọc giữ nghĩa M1 (refresh_status coi ErrRejected là dừng hẳn): chỉ GetStatus 404 bọc ErrRejected; 401, 403,
// 422, 429, 500 không bọc ErrRejected (lệnh đọc thử lại) nhưng vẫn mang lớp lỗi.
func TestReadErrorsKeepM1Meaning(t *testing.T) {
	cases := []struct {
		status int
		class  fulfillment.ErrorClass
	}{
		{http.StatusUnauthorized, fulfillment.ClassAuth},
		{http.StatusForbidden, fulfillment.ClassAuth},
		{http.StatusUnprocessableEntity, fulfillment.ClassRejected},
		{http.StatusTooManyRequests, fulfillment.ClassRateLimited},
		{http.StatusInternalServerError, fulfillment.ClassUnknownOutcome},
	}
	for _, c := range cases {
		cli := stub(t, c.status, `{}`, nil)
		_, lerr := cli.LookupByCode(t.Context(), account(), "E1")
		_, serr := cli.GetStatus(t.Context(), account(), "FK1")
		for name, err := range map[string]error{"LookupByCode": lerr, "GetStatus": serr} {
			ce, ok := errors.AsType[*fulfillment.CarrierError](err)
			if errors.Is(err, fulfillment.ErrRejected) || !ok || ce.Class != c.class {
				t.Errorf("%s %d = %v, muốn lớp %s, không bọc ErrRejected", name, c.status, err, c.class)
			}
		}
	}
	if _, err := stub(t, http.StatusNotFound, ``, nil).GetStatus(t.Context(), account(), "FK1"); !errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("GetStatus 404 = %v, muốn ErrRejected", err)
	}
}
