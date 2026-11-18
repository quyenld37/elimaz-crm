package imager_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

func newServer(t *testing.T, r imager.Runner, lim imager.Limits, parallel int, logs io.Writer) *httptest.Server {
	t.Helper()
	if logs == nil {
		logs = io.Discard
	}
	s := imager.NewServer(r, imager.NewHTTPClient(5*time.Second), lim, parallel, slog.New(slog.NewJSONHandler(logs, nil)))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

type reply struct {
	StatusCode int
	Header     http.Header
}

func post(t *testing.T, url string, body any) (reply, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/process", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req) //nolint:forbidigo // client của test, httptest cục bộ
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{resp.StatusCode, resp.Header}, b
}

func decodeResult(t *testing.T, b []byte) imager.Result {
	t.Helper()
	var res imager.Result
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("thân không phải Result: %v\n%s", err, b)
	}
	return res
}

func TestServer_OKAndRejectedBothReturn200(t *testing.T) {
	st := newStore(t)
	var logs bytes.Buffer
	srv := newServer(t, jpegFake(), limits(t), 2, &logs)

	src := st.addSource(t, "ok", readImage(t, "photo-gps.jpg"))
	resp, b := post(t, srv.URL, request(src, twelve(st.srv.URL)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mã %d: %s", resp.StatusCode, b)
	}
	if res := decodeResult(t, b); res.Status != imager.StatusOK || len(res.Outputs) != 12 {
		t.Fatalf("kết quả %+v", res)
	}
	if !strings.Contains(string(b), `"sourceSha256"`) || !strings.Contains(string(b), `"pixelWidth"`) {
		t.Fatalf("tên trường JSON khác hợp đồng: %s", b)
	}

	resp, b = post(t, srv.URL, request(st.addSource(t, "html", readImage(t, "html-as.jpg")), twelve(st.srv.URL)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rejected vẫn phải 200, nhận %d", resp.StatusCode)
	}
	if res := decodeResult(t, b); res.Status != imager.StatusRejected || res.Reason != imager.ReasonNotImage {
		t.Fatalf("kết quả %+v", res)
	}
	// Log không có đường dẫn ký sẵn (chứa chữ ký).
	if strings.Contains(logs.String(), st.srv.URL) || strings.Contains(logs.String(), "/put/") {
		t.Fatalf("log có đường dẫn:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "not_image") {
		t.Fatalf("log thiếu lý do:\n%s", logs.String())
	}
}

func TestServer_BusyReturns503(t *testing.T) {
	st := newStore(t)
	block := make(chan struct{})
	fr := jpegFake()
	fr.block = block
	srv := newServer(t, fr, limits(t), 2, nil)
	src := st.addSource(t, "p", readImage(t, "photo-gps.jpg"))

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() {
			resp, _ := post(t, srv.URL, request(src, twelve(st.srv.URL)))
			codes <- resp.StatusCode
		})
	}
	waitInflight(t, fr, 2)
	resp, b := post(t, srv.URL, request(src, twelve(st.srv.URL)))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("yêu cầu thứ 3 nhận %d, cần 503: %s", resp.StatusCode, b)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("503 phải có Retry-After")
	}
	close(block)
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != http.StatusOK {
			t.Fatalf("hai yêu cầu đầu phải 200, nhận %d", c)
		}
	}
	// Hết bận thì nhận lại.
	if resp, _ := post(t, srv.URL, request(src, twelve(st.srv.URL))); resp.StatusCode != http.StatusOK {
		t.Fatalf("sau khi rảnh nhận %d", resp.StatusCode)
	}
}

// waitInflight chờ tới khi n yêu cầu đang đứng ở bước vips (đã giữ chỗ).
func waitInflight(t *testing.T, fr *fakeRunner, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for fr.waiting() < n {
		if time.Now().After(deadline) {
			t.Fatalf("chỉ %d yêu cầu đang chạy", fr.waiting())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServer_BadRequest(t *testing.T) {
	st := newStore(t)
	srv := newServer(t, jpegFake(), limits(t), 2, nil)
	good := request(st.srv.URL+"/src/x", twelve(st.srv.URL))
	mut := func(f func(*imager.Request)) imager.Request {
		r := good
		r.Outputs = append([]imager.Output(nil), good.Outputs...)
		f(&r)
		return r
	}
	cases := map[string]any{
		"không phải JSON":         "{",
		"trường lạ":               `{"sourceUrl":"http://a/b","bogus":1}`,
		"nguồn file://":           mut(func(r *imager.Request) { r.SourceURL = "file:///etc/passwd" }),
		"MaxBytes 0":              mut(func(r *imager.Request) { r.MaxBytes = 0 }),
		"MaxPixels 0":             mut(func(r *imager.Request) { r.MaxPixels = 0 }),
		"không có đầu ra":         mut(func(r *imager.Request) { r.Outputs = nil }),
		"định dạng lạ":            mut(func(r *imager.Request) { r.Outputs[0].Format = "gif" }),
		"rộng 0":                  mut(func(r *imager.Request) { r.Outputs[0].Width = 0 }),
		"rộng quá lớn":            mut(func(r *imager.Request) { r.Outputs[0].Width = 20000 }),
		"chất lượng 101":          mut(func(r *imager.Request) { r.Outputs[0].Quality = 101 }),
		"PutURL không http":       mut(func(r *imager.Request) { r.Outputs[0].PutURL = "gopher://a/b" }),
		"Allowed rỗng":            mut(func(r *imager.Request) { r.Allowed = nil }),
		"trùng rộng và định dạng": mut(func(r *imager.Request) { r.Outputs[1] = r.Outputs[0] }),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp, b := post(t, srv.URL, body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("mã %d, cần 400: %s", resp.StatusCode, b)
			}
			if !strings.Contains(string(b), `"error":"bad_request"`) {
				t.Fatalf("thân %s", b)
			}
		})
	}
}

func TestServer_HealthAndMethods(t *testing.T) {
	srv := newServer(t, jpegFake(), limits(t), 2, nil)
	resp, err := http.Get(srv.URL + "/health/live") //nolint:noctx // test cục bộ
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/health/live %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/v1/process") //nolint:noctx // test cục bộ
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/process %d, cần 405", resp.StatusCode)
	}
}

func TestServer_StillAnswersAfterOOM(t *testing.T) {
	st := newStore(t)
	lim := oomLimits(t)
	srv := newServer(t, execRunner(), lim, 2, nil)
	src := st.addSource(t, "oom", readImage(t, "photo-gps.jpg"))
	for i := range 2 {
		resp, b := post(t, srv.URL, request(src, twelve(st.srv.URL)))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("lần %d: mã %d", i+1, resp.StatusCode)
		}
		if res := decodeResult(t, b); res.Reason != imager.ReasonDecodeFailed {
			t.Fatalf("lần %d: %+v", i+1, res)
		}
	}
}
