package fake_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
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

// Chữ ký: đúng → true; sai khóa, thiếu tiền tố, thân đổi, bí mật rỗng → false. Split: tín hiệu carrier.signal định
// tuyến carrier_account:<accountKey>, chống trùng theo eventId; thiếu trường → invalid.
func TestSourceSignatureAndSplit(t *testing.T) {
	s := fake.NewSource(secret)
	body := packet(t, ev("e1", "fake-1"))
	h := http.Header{}
	h.Set(fake.SignatureHeader, fake.Sign(body, secret))
	if !s.VerifySignature(body, h) {
		t.Fatal("chữ ký đúng bị từ chối")
	}
	for name, hdr := range map[string]string{
		"sai khóa": fake.Sign(body, "khac"), "thiếu tiền tố": fake.Sign(body, secret)[7:], "rỗng": "",
	} {
		h := http.Header{}
		h.Set(fake.SignatureHeader, hdr)
		if s.VerifySignature(body, h) {
			t.Errorf("%s: chữ ký được nhận", name)
		}
	}
	if s.VerifySignature(append(bytes.Clone(body), ' '), h) || fake.NewSource("").VerifySignature(body, h) {
		t.Fatal("thân đổi hay bí mật rỗng mà chữ ký vẫn đúng")
	}
	if _, ok := s.Challenge(nil); ok || s.Key() != "fakecarrier" {
		t.Fatal("Challenge phải false, Key fakecarrier")
	}
	bad := ev("", "fake-1")
	res, err := s.Split(packet(t, ev("e1", "fake-1"), bad))
	if err != nil || len(res.Events) != 1 || res.Skipped["invalid"] != 1 {
		t.Fatalf("Split = %+v %v", res, err)
	}
	e := res.Events[0]
	var sig fulfillment.Signal
	if err := json.Unmarshal(e.Payload, &sig); err != nil {
		t.Fatal(err)
	}
	if e.Type != fulfillment.SignalType || e.DedupeKey != "e1" || e.OrderKey != "FK1" ||
		e.Route != (webhooks.Route{Kind: "carrier_account", ExternalKey: "fake-1"}) ||
		sig != (fulfillment.Signal{CarrierKey: "fake", AccountKey: "fake-1", TrackingCode: "FK1", EventID: "e1"}) {
		t.Fatalf("sự kiện = %+v, tín hiệu %+v", e, sig)
	}
}

// Đường vào thật (ingest + tách gói + handler tín hiệu): chữ ký sai → 401, không lưu; tài khoản lạ (không tuyến) →
// inbound_unrouted tăng, không việc inbound nào; cùng eventId gửi 3 lần → 1 việc inbound.
func TestIngestPipeline(t *testing.T) {
	staff := testkit.OpenStaff(t)
	tenant := staff.Tenant(t, "A")
	if err := tenancy.AddRoute(t.Context(), staff.Owner, fulfillment.RouteKindCarrierAccount, "fake-1", tenant); err != nil {
		t.Fatal(err)
	}
	reg := metrics.NewRegistry()
	src := fake.NewSource(secret)
	in, err := webhooks.NewIngest(webhooks.IngestOptions{
		Role: "ingest", DB: staff.App, Sources: []webhooks.Source{src}, Spool: &webhooks.Spool{Dir: t.TempDir()},
		Lockout: webhooks.NewLockout(webhooks.LockoutOptions{}), Metrics: reg, Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	in.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	post := func(body []byte, sig string) int {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/hooks/fakecarrier", bytes.NewReader(body))
		req.Header.Set(fake.SignatureHeader, sig)
		req.Header.Set("Content-Type", "application/json")
		status, _, _ := testkit.Do(t, req)
		return status
	}
	count := func(q string) int { return staff.Count(t, q) }

	body := packet(t, ev("e-bad", "fake-1"))
	if s := post(body, fake.Sign(body, "sai-khoa-sai-khoa-sai-khoa-sai-khoa")); s != http.StatusUnauthorized {
		t.Fatalf("chữ ký sai = %d, muốn 401", s)
	}
	if n := count("SELECT count(*) FROM platform.webhook_deliveries"); n != 0 {
		t.Fatalf("bản nhận = %d sau chữ ký sai", n)
	}

	for i := range 3 {
		b := packet(t, fake.Event{EventID: "e-dup", AccountKey: "fake-1", Tracking: "FK1", SentAt: time.Unix(int64(1_795_510_800+i), 0)})
		if s := post(b, fake.Sign(b, secret)); s != http.StatusOK {
			t.Fatalf("POST %d = %d", i, s)
		}
	}
	strange := packet(t, ev("e-x", "tai-khoan-la"))
	if s := post(strange, fake.Sign(strange, secret)); s != http.StatusOK {
		t.Fatalf("POST tài khoản lạ = %d", s)
	}

	sources := []webhooks.Source{fake.NewSource("")}
	split, splitErr := webhooks.NewSplitHandler(webhooks.SplitOptions{Sources: sources, Metrics: reg})
	inbound, inboundErr := webhooks.InboundJobHandlers(sources, []webhooks.InboundHandler{fulfillment.NewSignalHandler(fake.Key)})
	if err := errors.Join(splitErr, inboundErr); err != nil {
		t.Fatal(err)
	}
	w := queue.NewWorker(staff.App, append([]queue.Handler{split}, inbound...), queue.WorkerOptions{})
	drain(t, w)
	if n := count("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fakecarrier'"); n != 1 {
		t.Fatalf("việc inbound = %d, muốn 1 (cùng eventId 3 lần, tài khoản lạ không có việc)", n)
	}
	if n := count("SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.fakecarrier' AND status = 'done'"); n != 1 {
		t.Fatalf("việc inbound chưa done")
	}
	if unrouted := counter(reg.Drain(), "inbound_unrouted", "fakecarrier"); unrouted != 1 {
		t.Fatalf("inbound_unrouted fakecarrier = %d, muốn 1", unrouted)
	}
}

// drain chạy worker tới khi không còn việc tới hạn.
func drain(t *testing.T, w *queue.Worker) {
	t.Helper()
	for range 10 {
		n, err := w.RunOnce(t.Context(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

func counter(rows []metrics.Row, name, label string) int64 {
	var n int64
	for _, r := range rows {
		if r.Name == name && r.Label == label {
			n += r.Count
		}
	}
	return n
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
