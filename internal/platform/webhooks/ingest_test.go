package webhooks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/webhooks"
)

func pingBody(id string) []byte {
	return fmt.Appendf(nil, `{"events":[{"kind":"ping","eventId":%q,"tenantKey":"ka","sentAt":"2026-11-18T01:02:03Z"}]}`, id)
}

func TestPostValidStoresRawBodyAndOneSplitJob(t *testing.T) {
	d := openDB(t)
	r := newRig(t, d, nil)
	body := pingBody("e1")

	got := r.post(t, "selftest", body, "", "", nil)
	if got.status != http.StatusOK || strings.TrimSpace(got.body) != `{"received":true}` {
		t.Fatalf("POST = %d %q", got.status, got.body)
	}
	var raw []byte
	var source string
	if err := d.Pool.QueryRow(t.Context(), "SELECT source, body FROM platform.webhook_deliveries").Scan(&source, &raw); err != nil {
		t.Fatal(err)
	}
	if source != "selftest" || !bytes.Equal(raw, body) {
		t.Fatalf("dòng lưu = %q %q, muốn byte gốc", source, raw)
	}
	var payload []byte
	var tenant *string
	if err := d.Pool.QueryRow(t.Context(),
		"SELECT payload, tenant_id::text FROM platform.jobs WHERE kind = 'webhook.split'").Scan(&payload, &tenant); err != nil {
		t.Fatal(err)
	}
	var p struct{ DeliveryID string }
	if err := json.Unmarshal(payload, &p); err != nil || p.DeliveryID == "" || tenant != nil {
		t.Fatalf("payload việc tách gói = %s, tenant %v", payload, tenant)
	}

	again := r.post(t, "selftest", body, "", "", nil)
	if again.status != http.StatusOK {
		t.Fatalf("gửi lại = %d", again.status)
	}
	if n, j := counts(t, d); n != 1 || j != 1 {
		t.Fatalf("sau khi gửi lại: %d dòng, %d việc; muốn 1, 1", n, j)
	}
}

func TestConcurrentSameBodyOneRowOneJob(t *testing.T) {
	d := openDB(t)
	r := newRig(t, d, nil)
	body := pingBody("same")
	const n = 8
	statuses := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			statuses[i] = r.post(t, "selftest", body, "", "", nil).status
		})
	}
	close(start)
	wg.Wait()
	for i, s := range statuses {
		if s != http.StatusOK {
			t.Fatalf("yêu cầu %d = %d", i, s)
		}
	}
	if rows, jobs := counts(t, d); rows != 1 || jobs != 1 {
		t.Fatalf("%d dòng, %d việc; muốn 1, 1", rows, jobs)
	}
}

func TestRejects(t *testing.T) {
	d := openDB(t)
	r := newRig(t, d, nil)
	body := pingBody("e1")

	if got := r.post(t, "selftest", body, "", "sha256=00", nil); got.status != http.StatusUnauthorized {
		t.Fatalf("chữ ký sai = %d", got.status)
	}
	if got := r.post(t, "nope", body, "", "", nil); got.status != http.StatusNotFound {
		t.Fatalf("nguồn lạ = %d", got.status)
	}
	big := bytes.Repeat([]byte("a"), webhooks.MaxBody+1)
	if got := r.post(t, "selftest", big, "", "", nil); got.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("1 MiB + 1 = %d", got.status)
	}
	if rows, jobs := counts(t, d); rows != 0 || jobs != 0 {
		t.Fatalf("%d dòng, %d việc; muốn 0", rows, jobs)
	}
	// Đúng 1 MiB vẫn nhận.
	exact := bytes.Repeat([]byte("b"), webhooks.MaxBody)
	if got := r.post(t, "selftest", exact, "", "", nil); got.status != http.StatusOK {
		t.Fatalf("đúng 1 MiB = %d", got.status)
	}
	if rows := counter(r.reg.Drain(), "hooks_bad_signature", "selftest"); rows != 1 {
		t.Fatalf("hooks_bad_signature = %d, muốn 1", rows)
	}
}

func TestErrorBodyShape(t *testing.T) {
	r := newRig(t, openDB(t), nil)
	got := r.post(t, "selftest", pingBody("x"), "", "sha256=00", nil)
	var e struct {
		Error     string `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal([]byte(got.body), &e); err != nil || e.Error == "" || e.RequestID == "" {
		t.Fatalf("thân lỗi = %q", got.body)
	}
}

func TestChallengeGET(t *testing.T) {
	r := newRig(t, openDB(t), nil)
	ok := r.get(t, "/hooks/selftest?verify_token="+secret+"&challenge=abc")
	if ok.status != http.StatusOK || ok.body != "abc" || !strings.HasPrefix(ok.ctype, "text/plain") {
		t.Fatalf("GET đúng mã = %d %q %q", ok.status, ok.body, ok.ctype)
	}
	if got := r.get(t, "/hooks/selftest?verify_token=sai&challenge=abc"); got.status != http.StatusForbidden {
		t.Fatalf("GET sai mã = %d", got.status)
	}
	if got := r.get(t, "/hooks/nope?verify_token="+secret+"&challenge=abc"); got.status != http.StatusNotFound {
		t.Fatalf("GET nguồn lạ = %d", got.status)
	}
}

func TestHeadersStoredRedacted(t *testing.T) {
	d := openDB(t)
	r := newRig(t, d, nil)
	const token = "Bearer rat-bi-mat-1234567890"
	extra := http.Header{"Authorization": {token}, "Cookie": {"sid=bi-mat-cookie"}, "X-Request-Tag": {"giu-lai"}}
	if got := r.post(t, "selftest", pingBody("h"), "", "", extra); got.status != http.StatusOK {
		t.Fatalf("POST = %d", got.status)
	}
	var headers string
	if err := d.Pool.QueryRow(t.Context(), "SELECT headers::text FROM platform.webhook_deliveries").Scan(&headers); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(headers, "rat-bi-mat") || strings.Contains(headers, "bi-mat-cookie") {
		t.Fatalf("headers lưu nguyên văn bí mật: %s", headers)
	}
	if !strings.Contains(headers, "giu-lai") || !strings.Contains(headers, "X-Selftest-Signature") {
		t.Fatalf("headers thiếu header thường: %s", headers)
	}
}

func TestIngestServesOnlyHooksAndHealth(t *testing.T) {
	r := newRig(t, openDB(t), nil)
	for _, p := range []string{"/api/v1/stream", "/api/v1/me", "/hooks"} {
		if got := r.get(t, p); got.status != http.StatusNotFound {
			t.Fatalf("GET %s = %d, muốn 404", p, got.status)
		}
	}
	if got := r.get(t, "/health/live"); got.status != http.StatusOK {
		t.Fatalf("/health/live = %d", got.status)
	}
}

func TestDatabaseDownSpoolsAndHealthCounts(t *testing.T) {
	r := newRig(t, openDeadDB(t), nil)
	got := r.post(t, "selftest", pingBody("down"), "", "", nil)
	if got.status != http.StatusOK || strings.TrimSpace(got.body) != `{"received":true}` {
		t.Fatalf("POST khi database chết = %d %q", got.status, got.body)
	}
	names, err := r.spool.List()
	if err != nil || len(names) != 1 {
		t.Fatalf("tệp tạm = %v, %v; muốn đúng 1", names, err)
	}
	h := r.get(t, "/health")
	var body struct {
		Status                string  `json:"status"`
		SpoolFiles            int     `json:"spoolFiles"`
		OldestSpoolAgeSeconds float64 `json:"oldestSpoolAgeSeconds"`
	}
	if err := json.Unmarshal([]byte(h.body), &body); err != nil || h.status != http.StatusOK {
		t.Fatalf("/health = %d %q", h.status, h.body)
	}
	if body.SpoolFiles != 1 || body.Status != "ok" {
		t.Fatalf("/health = %+v; muốn spoolFiles 1, ok (tệp chưa quá 300 s)", body)
	}
}

func TestHealthDegradedWhenSpoolOld(t *testing.T) {
	r := newRig(t, openDeadDB(t), func(o *webhooks.IngestOptions) { o.StaleSpoolAge = time.Millisecond })
	if _, err := r.spool.Write(webhooks.Entry{Source: "selftest", Body: []byte("{}"), ReceivedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	h := r.get(t, "/health")
	if h.status != http.StatusOK || !strings.Contains(h.body, `"status":"degraded"`) {
		t.Fatalf("/health = %d %q; muốn 200 degraded", h.status, h.body)
	}
	var body struct{ OldestSpoolAgeSeconds float64 }
	_ = json.Unmarshal([]byte(h.body), &body)
	if body.OldestSpoolAgeSeconds < 3500 {
		t.Fatalf("oldestSpoolAgeSeconds = %v, muốn ~3600", body.OldestSpoolAgeSeconds)
	}
}

func TestDatabaseAndSpoolDownReturns503(t *testing.T) {
	dir := readOnlyDir(t)
	r := newRig(t, openDeadDB(t), func(o *webhooks.IngestOptions) { o.Spool = &webhooks.Spool{Dir: dir} })
	if got := r.post(t, "selftest", pingBody("x"), "", "", nil); got.status != http.StatusServiceUnavailable {
		t.Fatalf("POST khi database và tệp tạm đều lỗi = %d, muốn 503", got.status)
	}
}

func TestInFlightLimit(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	r := newRig(t, openDB(t), func(o *webhooks.IngestOptions) { o.MaxInFlight = 2 })
	webhooks.SetSave(r.in, func(ctx context.Context, _ webhooks.Entry) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return errors.New("hết hạn")
		}
	})
	var wg sync.WaitGroup
	results := make([]int, 2)
	for i := range 2 {
		wg.Go(func() { results[i] = r.post(t, "selftest", pingBody(fmt.Sprint(i)), "", "", nil).status })
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(30 * time.Second): // chỉ chống treo (Task 420), không phải tiêu chí giờ
			t.Fatal("hai yêu cầu đầu chưa tới bước lưu")
		}
	}
	if got := r.post(t, "selftest", pingBody("third"), "", "", nil); got.status != http.StatusServiceUnavailable {
		t.Fatalf("yêu cầu thứ 3 = %d, muốn 503", got.status)
	}
	close(release)
	wg.Wait()
	for i, s := range results {
		if s != http.StatusOK {
			t.Fatalf("yêu cầu %d = %d", i, s)
		}
	}
	// Hết bận thì nhận lại.
	if got := r.post(t, "selftest", pingBody("after"), "", "", nil); got.status != http.StatusOK {
		t.Fatalf("sau khi hết bận = %d", got.status)
	}
}

func TestNewIngestRejectsBadOptions(t *testing.T) {
	d := openDeadDB(t)
	base := func() webhooks.IngestOptions {
		r := webhooks.IngestOptions{
			DB: d, Spool: &webhooks.Spool{Dir: t.TempDir()},
			Lockout: webhooks.NewLockout(webhooks.LockoutOptions{}),
		}
		return r
	}
	dup := base()
	dup.Sources = []webhooks.Source{newSelftest(), newSelftest()}
	if _, err := webhooks.NewIngest(dup); err == nil {
		t.Fatal("hai nguồn trùng khóa mà NewIngest không lỗi")
	}
	noSpool := base()
	noSpool.Spool = nil
	if _, err := webhooks.NewIngest(noSpool); err == nil {
		t.Fatal("thiếu Spool mà NewIngest không lỗi")
	}
}
