package imager_test

// Ca biên thêm ở Task 403 (mutation testing vùng cao M1): mỗi test giết một nhóm đột biến còn sống của gremlins
// (docs/mutation.md). Không cần libvips: Runner giả.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

// oneJPEG là một đầu ra JPEG rộng 320 PUT lên kho giả (đo kích thước bằng mã Go, không gọi vipsheader lần hai).
func oneJPEG(st *store) []imager.Output {
	return []imager.Output{{Width: 320, Format: "jpeg", PutURL: st.srv.URL + "/put/320.jpeg"}}
}

// summaryRunner: vipsheader trả line(path); vips giao cho next.
type summaryRunner struct {
	line func(path string) string
	next imager.Runner
}

func (r summaryRunner) Run(ctx context.Context, name string, args []string, mem int64) ([]byte, error) {
	if strings.HasSuffix(name, "vipsheader") {
		return []byte(r.line(args[0])), nil
	}
	return r.next.Run(ctx, name, args, mem) //nolint:wrapcheck // chuyển tiếp Runner giả
}

// Dòng tóm tắt của vipsheader thiếu tên tệp, có chiều 0 hay số tràn int → decode_failed, không giải mã.
func TestImager_SummaryLineRejected(t *testing.T) {
	st := newStore(t)
	src := st.addSource(t, "photo", readImage(t, "photo-gps.jpg"))
	const tail = " uchar, 3 bands, srgb, jpegload\n"
	for name, line := range map[string]func(string) string{
		"thiếu tên tệp": func(string) string { return "2400x1600" + tail },
		"rộng 0":        func(p string) string { return p + ": 0x1600" + tail },
		"cao 0":         func(p string) string { return p + ": 2400x0" + tail },
		"rộng tràn số":  func(p string) string { return p + ": 99999999999999999999x1600" + tail },
		"cao tràn số":   func(p string) string { return p + ": 2400x99999999999999999999" + tail },
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{next: summaryRunner{line: line, next: jpegFake()}}
			res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second), request(src, oneJPEG(st)), limits(t))
			if res.Status != imager.StatusRejected || res.Reason != imager.ReasonDecodeFailed {
				t.Fatalf("kết quả %+v", res)
			}
			if rec.count("vips") != 0 {
				t.Fatal("vips không được chạy khi dòng tóm tắt hỏng")
			}
		})
	}
}

// Đúng MaxPixels điểm ảnh được nhận; hơn một điểm thì too_many_pixels.
func TestImager_MaxPixelsBoundary(t *testing.T) {
	st := newStore(t)
	src := st.addSource(t, "photo", readImage(t, "photo-gps.jpg"))
	for limit, want := range map[int64]string{5000: "", 4999: imager.ReasonTooManyPixels} {
		req := request(src, oneJPEG(st))
		req.MaxPixels = limit
		res := imager.Process(t.Context(), &fakeRunner{width: 100, height: 50, loader: "jpegload"},
			imager.NewHTTPClient(5*time.Second), req, limits(t))
		if res.Reason != want {
			t.Fatalf("MaxPixels %d: kết quả %+v, cần lý do %q", limit, res, want)
		}
	}
}

// Nguồn đúng MaxBytes byte được nhận, có hay không có Content-Length; hơn một byte thì too_large.
func TestImager_MaxBytesBoundary(t *testing.T) {
	st := newStore(t)
	body := readImage(t, "photo-gps.jpg")
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		half := len(body) / 2
		_, _ = w.Write(body[:half])
		w.(http.Flusher).Flush() // gửi phần đầu trước: không có Content-Length, imager phải tự đếm
		_, _ = w.Write(body[half:])
	}))
	t.Cleanup(chunked.Close)
	sized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body))) // khai trước: imager từ chối ngay khi khai quá
		_, _ = w.Write(body)
	}))
	t.Cleanup(sized.Close)
	for _, c := range []struct {
		name, url string
		limit     int64
		want      string
	}{
		{"có Content-Length, vừa đủ", sized.URL, int64(len(body)), ""},
		{"có Content-Length, thừa một byte", sized.URL, int64(len(body)) - 1, imager.ReasonTooLarge},
		{"không Content-Length, vừa đủ", chunked.URL, int64(len(body)), ""},
		{"không Content-Length, thừa một byte", chunked.URL, int64(len(body)) - 1, imager.ReasonTooLarge},
	} {
		req := request(c.url, oneJPEG(st))
		req.MaxBytes = c.limit
		res := imager.Process(t.Context(), jpegFake(), imager.NewHTTPClient(5*time.Second), req, limits(t))
		if res.Reason != c.want {
			t.Fatalf("%s: kết quả %+v, cần lý do %q", c.name, res, c.want)
		}
	}
}

// Kho ngắt kết nối giữa chừng (Content-Length khai nhiều hơn phần đã gửi) → source_failed, không xử lý phần dở.
func TestImager_SourceCutMidway(t *testing.T) {
	st := newStore(t)
	body := readImage(t, "photo-gps.jpg")
	cut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(cut.Close)
	rec := &recorder{next: jpegFake()}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second), request(cut.URL, oneJPEG(st)), limits(t))
	wantSourceFailedNoCalls(t, res, rec)
}

// wantSourceFailedNoCalls: kết quả rejected source_failed và không lệnh con nào chạy.
func wantSourceFailedNoCalls(t *testing.T, res imager.Result, rec *recorder) {
	t.Helper()
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonSourceFailed {
		t.Fatalf("kết quả %+v", res)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("không được gọi lệnh con: %v", rec.calls)
	}
}

// processPhoto xử lý ảnh photo-gps.jpg với các đầu ra outs; kết quả phải ok.
func processPhoto(t *testing.T, st *store, run imager.Runner, outs []imager.Output) {
	t.Helper()
	res := imager.Process(t.Context(), run, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "photo", readImage(t, "photo-gps.jpg")), outs), limits(t))
	if res.Status != imager.StatusOK {
		t.Fatalf("kết quả %+v", res)
	}
}

// Quality 0 → không truyền Q (vips dùng mặc định); Quality > 0 → Q=<n>.
func TestImager_QualityOption(t *testing.T) {
	st := newStore(t)
	rec := &recorder{next: jpegFake()}
	outs := []imager.Output{
		{Width: 320, Format: "jpeg", PutURL: st.srv.URL + "/put/320.jpeg"},
		{Width: 640, Format: "jpeg", Quality: 80, PutURL: st.srv.URL + "/put/640.jpeg"},
	}
	processPhoto(t, st, rec, outs)
	want := map[string]string{" 320 ": "[keep=none] 320 ", " 640 ": "[Q=80,keep=none] 640 "}
	for _, call := range rec.calls {
		if !strings.HasPrefix(call, "vips ") {
			continue
		}
		for width, opts := range want {
			if strings.Contains(call, width) {
				if !strings.Contains(call, opts) {
					t.Errorf("lệnh %q thiếu %q", call, opts)
				}
				delete(want, width)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("không thấy lệnh vips cho %v", want)
	}
}

// PUT không có Content-Type của bên gọi → image/<định dạng>; có thì giữ nguyên (đường dẫn ký sẵn ký cả header).
func TestImager_PutContentType(t *testing.T) {
	st := newStore(t)
	outs := []imager.Output{
		{Width: 320, Format: "jpeg", PutURL: st.srv.URL + "/put/320.jpeg"},
		{Width: 640, Format: "jpeg", PutURL: st.srv.URL + "/put/640.jpeg", Headers: map[string]string{"Content-Type": "image/x-ky-san"}},
	}
	processPhoto(t, st, jpegFake(), outs)
	for key, want := range map[string]string{"320.jpeg": "image/jpeg", "640.jpeg": "image/x-ky-san"} {
		if o, ok := st.object(key); !ok || o.contentType != want {
			t.Errorf("%s: Content-Type %q, cần %q", key, o.contentType, want)
		}
	}
}

// Mọi mã 2xx của kho khi PUT là ghi xong (299 là biên trên); 300 là lỗi ghi.
func TestImager_PutStatusRange(t *testing.T) {
	st := newStore(t)
	src := st.addSource(t, "photo", readImage(t, "photo-gps.jpg"))
	for code, want := range map[int]string{http.StatusOK: "", 299: "", http.StatusMultipleChoices: imager.ReasonUploadFailed} {
		put := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		res := imager.Process(t.Context(), jpegFake(), imager.NewHTTPClient(5*time.Second),
			request(src, []imager.Output{{Width: 320, Format: "jpeg", PutURL: put.URL + "/o"}}), limits(t))
		put.Close()
		if res.Reason != want {
			t.Fatalf("PUT trả %d: kết quả %+v, cần lý do %q", code, res, want)
		}
	}
}

// ctx của yêu cầu đã hủy (bên gọi bỏ, server dừng) → timeout như hết hạn, không phải source_failed.
func TestImager_CanceledCountsAsTimeout(t *testing.T) {
	st := newStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res := imager.Process(ctx, jpegFake(), imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "photo", readImage(t, "photo-gps.jpg")), oneJPEG(st)), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTimeout {
		t.Fatalf("kết quả %+v", res)
	}
}

// zeroJPEG: vips giả ghi một đầu JPEG (JFIF + SOF0) khai rộng w, cao h — DecodeConfig đọc được, không lỗi.
type zeroJPEG struct{ w, h int }

func (z zeroJPEG) Run(ctx context.Context, name string, args []string, mem int64) ([]byte, error) {
	if strings.HasSuffix(name, "vipsheader") {
		return jpegFake().Run(ctx, name, args, mem)
	}
	p := args[2]
	if m := outPath.FindStringSubmatch(p); m != nil {
		p = m[1]
	}
	b := []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0xFF, 0xC0, 0x00, 0x0B, 0x08, byte(z.h >> 8), byte(z.h), byte(z.w >> 8), byte(z.w), 0x01, 0x01, 0x11, 0x00, //nolint:gosec // G115: test, chiều ≤ 65535 tách hai byte
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return nil, errors.New("zeroJPEG: không ghi được tệp")
	}
	return nil, nil
}

// Đầu ra JPEG khai rộng hay cao 0 → decode_failed, không PUT.
func TestImager_OutputZeroDimensionRejected(t *testing.T) {
	st := newStore(t)
	src := st.addSource(t, "photo", readImage(t, "photo-gps.jpg"))
	for name, z := range map[string]zeroJPEG{"rộng 0": {0, 10}, "cao 0": {10, 0}} {
		res := imager.Process(t.Context(), z, imager.NewHTTPClient(5*time.Second), request(src, oneJPEG(st)), limits(t))
		if res.Status != imager.StatusRejected || res.Reason != imager.ReasonDecodeFailed {
			t.Fatalf("%s: kết quả %+v", name, res)
		}
		if _, ok := st.object("320.jpeg"); ok {
			t.Fatalf("%s: không được PUT đầu ra hỏng", name)
		}
	}
}

// Retry-After của imager: 1..300 giây được theo; 0, quá 300 hay không phải số → 1 giây.
func TestClient_RetryAfterBounds(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"300": 300 * time.Second, "1": time.Second, "0": time.Second, "301": time.Second, "abc": time.Second,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", header)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		_, err := imager.NewClient(srv.URL, srv.Client(), 1).Process(ctx, clientRequest())
		cancel()
		srv.Close()
		if be, ok := errors.AsType[*imager.BusyError](err); !ok || be.RetryAfter != want {
			t.Fatalf("Retry-After %q: lỗi %v, cần chờ %v", header, err, want)
		}
	}
}

// stdout vừa đúng giới hạn 64 KiB không phải tràn.
func TestExecRunner_StdoutExactlyAtLimit(t *testing.T) {
	out, err := execRunner().Run(t.Context(), "/bin/sh", []string{"-c", "head -c 65536 /dev/zero"}, 0)
	if err != nil || len(out) != 65536 {
		t.Fatalf("%d byte, lỗi %v", len(out), err)
	}
}

// stderr dài quá 4 KiB của lệnh lỗi: phần đầu (tới giới hạn) vẫn có trong lỗi để chẩn đoán.
func TestExecRunner_StderrKeptUpToLimit(t *testing.T) {
	_, err := execRunner().Run(t.Context(), "/bin/sh", []string{"-c", "head -c 6000 /dev/zero | tr '\\000' e >&2; exit 1"}, 0)
	if err == nil {
		t.Fatal("thoát mã 1 phải là lỗi")
	}
	if n := strings.Count(err.Error(), "e"+"eeeeeeee"); n == 0 {
		t.Fatalf("lỗi không giữ phần đầu của stderr: %.200s", err)
	}
}

// Validate nhận đúng biên (32 đầu ra, rộng 8192, chất lượng 0 và 100, 8 header) và từ chối khi vượt một đơn vị.
func TestRequestValidate_Boundaries(t *testing.T) {
	base := func() imager.Request {
		return imager.Request{
			SourceURL: "http://kho/src", MaxBytes: 1, MaxPixels: 1, Allowed: []string{"image/jpeg"},
			Outputs: []imager.Output{{Width: 320, Format: "jpeg", PutURL: "http://kho/o"}},
		}
	}
	outputs := func(n int) []imager.Output {
		outs := make([]imager.Output, n)
		for i := range outs {
			outs[i] = imager.Output{Width: i + 1, Format: "jpeg", PutURL: "http://kho/o"}
		}
		return outs
	}
	headers := func(n int) map[string]string {
		h := map[string]string{}
		for i := range n {
			h["X-H"+strconv.Itoa(i)] = "v"
		}
		return h
	}
	for name, c := range map[string]struct {
		edit func(*imager.Request)
		ok   bool
	}{
		"32 đầu ra":        {func(r *imager.Request) { r.Outputs = outputs(32) }, true},
		"33 đầu ra":        {func(r *imager.Request) { r.Outputs = outputs(33) }, false},
		"rộng 8192":        {func(r *imager.Request) { r.Outputs[0].Width = 8192 }, true},
		"rộng 8193":        {func(r *imager.Request) { r.Outputs[0].Width = 8193 }, false},
		"chất lượng 0":     {func(r *imager.Request) { r.Outputs[0].Quality = 0 }, true},
		"chất lượng -1":    {func(r *imager.Request) { r.Outputs[0].Quality = -1 }, false},
		"chất lượng 100":   {func(r *imager.Request) { r.Outputs[0].Quality = 100 }, true},
		"chất lượng 101":   {func(r *imager.Request) { r.Outputs[0].Quality = 101 }, false},
		"8 header":         {func(r *imager.Request) { r.Outputs[0].Headers = headers(8) }, true},
		"9 header":         {func(r *imager.Request) { r.Outputs[0].Headers = headers(9) }, false},
		"MaxBytes âm":      {func(r *imager.Request) { r.MaxBytes = -1 }, false},
		"MaxPixels âm":     {func(r *imager.Request) { r.MaxPixels = -1 }, false},
		"rộng âm":          {func(r *imager.Request) { r.Outputs[0].Width = -1 }, false},
		"tối thiểu hợp lệ": {func(*imager.Request) {}, true},
	} {
		r := base()
		c.edit(&r)
		if err := r.Validate(); (err == nil) != c.ok || (err != nil && !errors.Is(err, imager.ErrBadRequest)) {
			t.Errorf("%s: lỗi %v, cần hợp lệ = %v", name, err, c.ok)
		}
	}
}

// Kho treo khi PUT tới hết hạn của bước → timeout, không phải upload_failed.
func TestImager_PutTimeout(t *testing.T) {
	st := newStore(t)
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // đọc hết thân thì server mới thấy bên gọi đóng kết nối
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(release) }) // chạy trước hang.Close
	lim := limits(t)
	lim.StepTimeout = 300 * time.Millisecond
	res := imager.Process(t.Context(), jpegFake(), imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "photo", readImage(t, "photo-gps.jpg")),
			[]imager.Output{{Width: 320, Format: "jpeg", PutURL: hang.URL + "/o"}}), lim)
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTimeout {
		t.Fatalf("kết quả %+v", res)
	}
}
