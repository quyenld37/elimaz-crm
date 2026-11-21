package vtp_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Nhãn hai vận đơn một lần (LabelsBatch): printing-code nhận đủ mã và hạn mã in trong tương lai; tải PDF từ máy in theo
// mẫu link của tài liệu với mã in đã escape; không gửi header Token sang máy in; thân bắt đầu %PDF-, số trang = số mã.
func TestLabelsBatch(t *testing.T) {
	e := newEnv(t)
	t1, t2 := e.mustCreate(t, "E261121LBL001"), e.mustCreate(t, "E261121LBL002")
	before := time.Now()
	l, err := e.ad.Labels(t.Context(), e.acc, []string{t1, t2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(l.PDF), "%PDF-") || l.Pages != 2 {
		t.Errorf("nhãn: %d trang, đầu %q", l.Pages, string(l.PDF[:min(len(l.PDF), 8)]))
	}
	b := bodyField(t, e.srv, carriertest.OpLabels, 0)
	exp, _ := b["EXPIRY_TIME"].(float64)
	if fmt.Sprint(b["ORDER_ARRAY"]) != fmt.Sprintf("[%s %s]", t1, t2) || int64(exp) <= before.UnixMilli() {
		t.Errorf("thân printing-code = %v", b)
	}
	prints := e.srv.Prints()
	if len(prints) != 1 {
		t.Fatalf("máy in nhận %d yêu cầu, muốn 1", len(prints))
	}
	p := prints[0]
	if p.Token != "" || p.Type != "2" || p.ShowPostage != "0" || !strings.ContainsAny(p.Bill, "+/=") {
		t.Errorf("yêu cầu tải nhãn = %+v (muốn không Token, type 2, showPostage 0, mã in có ký tự phải escape)", p)
	}
}

// Số mã sai (0, hơn 100 — giới hạn của tài liệu, mã rỗng) → từ chối, không gọi hãng.
func TestLabelsBadInput(t *testing.T) {
	e := newEnv(t)
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("1587818%04d", i)
	}
	for name, codes := range map[string][]string{"rỗng": nil, "101 mã": many, "mã rỗng": {"1587818", ""}} {
		_, err := e.ad.Labels(t.Context(), e.acc, codes)
		wantClass(t, name, err, fulfillment.ClassRejected)
	}
	if n := e.srv.Calls(carriertest.OpLabels); n != 0 {
		t.Errorf("Calls(labels) = %d, muốn 0", n)
	}
}

// Lỗi của nhãn (việc đọc): mã vận đơn hãng không biết → từ chối; 429 → chờ; quá giờ → chưa gửi (đọc lại được).
func TestLabelsErrors(t *testing.T) {
	e := newEnv(t)
	_, err := e.ad.Labels(t.Context(), e.acc, []string{"9999999999"})
	wantClass(t, "mã lạ", err, fulfillment.ClassRejected)
	tracking := e.mustCreate(t, "E261121LBL003")
	e.srv.SetMode(carriertest.OpLabels, carriertest.ModeRateLimited)
	_, err = e.ad.Labels(t.Context(), e.acc, []string{tracking})
	wantClass(t, "429", err, fulfillment.ClassRateLimited)
	e.srv.SetMode(carriertest.OpLabels, carriertest.ModeTimeoutAfterSend)
	ctx, cancel := contextTimeout(t, 300*time.Millisecond)
	defer cancel()
	_, err = e.ad.Labels(ctx, e.acc, []string{tracking})
	wantClass(t, "quá giờ", err, fulfillment.ClassNotSentRetry)
}

// printServer là máy in giả do test điều khiển: trả thân body với mã status, header h; đếm lời gọi.
func printServer(t *testing.T, status int, h http.Header, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		for k, v := range h {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(hs.Close)
	return hs, &n
}

// Máy in trả HTML (không phải PDF), quá 20 MB, chuyển hướng sang máy khác (SSRF), 404 → lỗi permanent; chuyển hướng
// không được theo (máy đích không nhận lời gọi nào).
func TestLabelsDownloadGuards(t *testing.T) {
	target, targetCalls := printServer(t, http.StatusOK, nil, "%PDF-1.4 không được tới đây")
	big := "%PDF-" + strings.Repeat("x", 20<<20)
	for _, c := range []struct {
		name   string
		status int
		h      http.Header
		body   string
		code   string
	}{
		{"không phải PDF", http.StatusOK, nil, "<html>Không tìm thấy</html>", "invalid_pdf"},
		{"quá 20 MB", http.StatusOK, nil, big, "too_large"},
		{"chuyển hướng", http.StatusFound, http.Header{"Location": {target.URL + "/x"}}, "", "vtp_label_redirect"},
		{"404", http.StatusNotFound, nil, "", "http_404"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			ps, calls := printServer(t, c.status, c.h, c.body)
			ad := vtp.New(vtp.Config{BaseURL: e.hs.URL, PrintURL: ps.URL, HTTPClient: e.hs.Client()})
			tracking := e.mustCreate(t, "E261121DLG001")
			_, err := ad.Labels(t.Context(), e.acc, []string{tracking})
			ce := carrierErr(t, c.name, err, fulfillment.ClassPermanent)
			if ce.Code != c.code || calls.Load() != 1 {
				t.Errorf("%s: mã %q, máy in nhận %d lời gọi", c.name, ce.Code, calls.Load())
			}
		})
	}
	if targetCalls.Load() != 0 {
		t.Errorf("máy đích của chuyển hướng nhận %d lời gọi, muốn 0", targetCalls.Load())
	}
}

// Máy in theo môi trường khi không cấu hình (tài liệu: dev-release-print cho Development, digitalize cho Production);
// có BaseURL (test) mà không có PrintURL → dùng BaseURL, không bao giờ gọi máy thật của hãng.
func TestPrintHostByEnvironment(t *testing.T) {
	var mu sync.Mutex
	var hosts []string
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		body := `{"status":200,"error":false,"message":"OK","data":{"token":"t","expired":0}}`
		switch r.URL.Path {
		case "/v2/order/printing-code":
			body = `{"status":200,"error":false,"message":"ma+in/1=","data":null}`
		case "/DigitalizePrint/report.do":
			mu.Lock()
			hosts = append(hosts, r.URL.Scheme+"://"+r.URL.Host)
			mu.Unlock()
			body = "%PDF-1.4"
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	acc := newEnv(t).acc
	ad := vtp.New(vtp.Config{HTTPClient: &http.Client{Transport: rt}})
	for _, envName := range []string{"sandbox", "production"} {
		acc.Environment = envName
		if _, err := ad.Labels(t.Context(), acc, []string{"1587818"}); err != nil {
			t.Fatalf("%s: %v", envName, err)
		}
	}
	test := vtp.New(vtp.Config{BaseURL: "http://vtp.invalid", HTTPClient: &http.Client{Transport: rt}})
	if _, err := test.Labels(t.Context(), acc, []string{"1587818"}); err != nil {
		t.Fatal(err)
	}
	want := "[https://dev-release-print.viettelpost.vn https://digitalize.viettelpost.vn http://vtp.invalid]"
	if fmt.Sprint(hosts) != want {
		t.Errorf("máy in = %v, muốn %s", hosts, want)
	}
	acc.Environment = "staging"
	_, err := ad.Labels(t.Context(), acc, []string{"1587818"})
	if ce, ok := errors.AsType[*fulfillment.CarrierError](err); !ok || ce.Code != "unknown_environment" {
		t.Errorf("môi trường lạ: %v", err)
	}
}
