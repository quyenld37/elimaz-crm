package imager_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/imager"
)

const (
	mib       = 1 << 20
	maxPixels = 50_000_000 // 50 MP: giới hạn thử, đủ cho ảnh 24 MP
)

func allowedTypes() []string { return []string{"image/jpeg", "image/png", "image/webp"} }

// ---- kho giả: GET /src/<tên> trả nguồn, PUT /put/<khóa> ghi đầu ra, /expired/<khóa> trả 403 như link ký đã hết hạn.

type store struct {
	srv *httptest.Server
	mu  sync.Mutex
	src map[string][]byte
	out map[string]storedObject
}

type storedObject struct {
	body        []byte
	contentType string
}

func newStore(t *testing.T) *store {
	t.Helper()
	s := &store{src: map[string][]byte{}, out: map[string]storedObject{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *store) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/src/"):
		s.mu.Lock()
		b, ok := s.src[strings.TrimPrefix(r.URL.Path, "/src/")]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	case r.Method == http.MethodGet && r.URL.Path == "/huge":
		// 21 MiB, không khai Content-Length (chunked) để imager phải tự đếm.
		chunk := make([]byte, mib)
		for range 21 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/put/"):
		b := new(bytes.Buffer)
		if _, err := b.ReadFrom(r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.out[strings.TrimPrefix(r.URL.Path, "/put/")] = storedObject{b.Bytes(), r.Header.Get("Content-Type")}
		s.mu.Unlock()
	case strings.HasPrefix(r.URL.Path, "/expired/"):
		http.Error(w, "<Error><Code>AccessDenied</Code><Message>Request has expired</Message></Error>", http.StatusForbidden)
	default:
		http.NotFound(w, r)
	}
}

func (s *store) addSource(t *testing.T, name string, b []byte) string {
	t.Helper()
	s.mu.Lock()
	s.src[name] = b
	s.mu.Unlock()
	return s.srv.URL + "/src/" + name
}

func (s *store) object(key string) (storedObject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.out[key]
	return o, ok
}

func readImage(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "images", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fakeVips(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "images", "fakevips", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// twelve: bộ 4 kích thước × 3 định dạng của preset v1 (Task M1-41), PUT lên kho giả theo khóa <rộng>.<định dạng>.
func twelve(base string) []imager.Output {
	var outs []imager.Output
	for _, w := range []int{320, 640, 1080, 1600} {
		for _, f := range []string{"avif", "webp", "jpeg"} {
			q := map[string]int{"avif": 50, "webp": 75, "jpeg": 80}[f]
			outs = append(outs, imager.Output{
				Width: w, Format: f, Quality: q,
				PutURL: base + "/put/" + strconv.Itoa(w) + "." + f,
			})
		}
	}
	return outs
}

func request(src string, outs []imager.Output) imager.Request {
	return imager.Request{SourceURL: src, MaxBytes: 20 * mib, MaxPixels: maxPixels, Allowed: allowedTypes(), Outputs: outs}
}

// ---- libvips thật: chỉ có trong container (make test-imager); trên máy dev bỏ qua, trừ khi IMAGER_REQUIRE_VIPS=1.

func requireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("IMAGER_REQUIRE_VIPS") == "1" {
				t.Fatalf("thiếu %s trong container test", tool)
			}
			t.Skipf("cần %s: chạy make test-imager", tool)
		}
	}
}

func limits(t *testing.T) imager.Limits {
	t.Helper()
	return imager.Limits{
		StepTimeout:   20 * time.Second,
		StepMemory:    1 << 30,
		TmpDir:        t.TempDir(),
		VipsBin:       "vips",
		VipsHeaderBin: "vipsheader",
	}
}

func execRunner() imager.ExecRunner { return imager.NewExecRunner(nil) }

// recorder đếm lần gọi theo tên chương trình rồi chuyển cho Runner thật hoặc giả.
type recorder struct {
	next  imager.Runner
	mu    sync.Mutex
	calls []string
}

func (r *recorder) Run(ctx context.Context, name string, args []string, memLimit int64) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, filepath.Base(name)+" "+strings.Join(args, " "))
	r.mu.Unlock()
	return r.next.Run(ctx, name, args, memLimit) //nolint:wrapcheck // chuyển tiếp nguyên lỗi của Runner được bọc
}

func (r *recorder) count(prog string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if strings.HasPrefix(c, prog+" ") {
			n++
		}
	}
	return n
}

// fakeRunner giả vipsheader (trả width/height/loader cố định) và vips (ghi vài byte vào tệp đầu ra), không cần libvips.
type fakeRunner struct {
	width, height int
	loader        string
	block         chan struct{} // khác nil: vips chờ tới khi kênh đóng
	mu            sync.Mutex
	blocked       int
}

func (f *fakeRunner) waiting() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blocked
}

var outPath = regexp.MustCompile(`^(.*)\[[^\]]*\]$`)

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, _ int64) ([]byte, error) {
	if strings.HasSuffix(name, "vipsheader") {
		if len(args) != 1 {
			return nil, errors.New("vipsheader giả chỉ in dòng tóm tắt")
		}
		return []byte(args[0] + ": " + strconv.Itoa(f.width) + "x" + strconv.Itoa(f.height) +
			" uchar, 3 bands, srgb, " + f.loader + "\n"), nil
	}
	if f.block != nil {
		f.mu.Lock()
		f.blocked++
		f.mu.Unlock()
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, fmt.Errorf("vips giả: %w", ctx.Err())
		}
	}
	p := args[2]
	if m := outPath.FindStringSubmatch(p); m != nil {
		p = m[1]
	}
	// Ghi một JPEG thật đúng kích thước mà vips thumbnail --size down sẽ cho (mọi định dạng: chỉ JPEG được đọc lại).
	w, _ := strconv.Atoi(args[3])
	w = min(w, f.width)
	h := max(1, w*f.height/f.width)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		return nil, fmt.Errorf("vips giả: %w", err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("vips giả: %w", err)
	}
	return nil, nil
}

func jpegFake() *fakeRunner { return &fakeRunner{width: 2400, height: 1600, loader: "jpegload"} }

func header(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "vipsheader", "-a", path).CombinedOutput()
	if err != nil {
		t.Fatalf("vipsheader -a: %v\n%s", err, out)
	}
	return string(out)
}

func writeTemp(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func field(t *testing.T, hdr, key string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `: (.*)$`)
	m := re.FindStringSubmatch(hdr)
	if m == nil {
		t.Fatalf("không có trường %s trong:\n%s", key, hdr)
	}
	return strings.TrimSpace(m[1])
}

func hexSHA(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// ---- Ca thật với libvips

func TestImager_PhotoGPS_TwelveOutputsNoMetadata(t *testing.T) {
	requireTools(t, "vips", "vipsheader", "prlimit")
	st := newStore(t)
	src := readImage(t, "photo-gps.jpg")
	if !strings.Contains(header(t, writeTemp(t, "in.jpg", src)), "GPSLatitude") {
		t.Fatal("ảnh thử phải có GPS trước khi xử lý")
	}
	res := imager.Process(t.Context(), execRunner(), imager.NewHTTPClient(10*time.Second),
		request(st.addSource(t, "gps", src), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusOK {
		t.Fatalf("kết quả %+v", res)
	}
	if res.DetectedType != "image/jpeg" || res.Width != 2400 || res.Height != 1600 || res.SourceSHA256 != hexSHA(src) {
		t.Fatalf("thông tin nguồn sai: %+v", res)
	}
	if len(res.Outputs) != 12 {
		t.Fatalf("có %d đầu ra, cần 12", len(res.Outputs))
	}
	for _, o := range res.Outputs {
		checkOutput(t, st, o)
	}
}

// checkOutput: đầu ra trên kho khớp Bytes/SHA256, đúng Content-Type, đúng định dạng thật, rộng đúng, không siêu dữ liệu.
func checkOutput(t *testing.T, st *store, o imager.OutputResult) {
	t.Helper()
	loaders := map[string]string{"avif": "heifload", "webp": "webpload", "jpeg": "jpegload"}
	noMeta := regexp.MustCompile(`(?i)exif|gps|xmp|iptc`)
	key := strconv.Itoa(o.Width) + "." + o.Format
	obj, ok := st.object(key)
	if !ok {
		t.Fatalf("thiếu đầu ra %s trên kho", key)
	}
	if o.Bytes != int64(len(obj.body)) || o.SHA256 != hexSHA(obj.body) {
		t.Fatalf("%s: Bytes/SHA256 không khớp nội dung đã ghi", key)
	}
	if obj.contentType != "image/"+o.Format {
		t.Fatalf("%s: Content-Type %q", key, obj.contentType)
	}
	hdr := header(t, writeTemp(t, "out."+o.Format, obj.body))
	// Dòng filename chứa đường dẫn thư mục tạm (có tên test "PhotoGPS"), không phải siêu dữ liệu.
	meta := regexp.MustCompile(`(?m)^filename: .*$|^\S+: \d+x\d+ .*$`).ReplaceAllString(hdr, "")
	if m := noMeta.FindString(meta); m != "" {
		t.Fatalf("%s còn siêu dữ liệu (%s):\n%s", key, m, hdr)
	}
	if got := field(t, hdr, "width"); got != strconv.Itoa(o.Width) || o.PixelWidth != o.Width {
		t.Fatalf("%s: rộng %s, PixelWidth %d", key, got, o.PixelWidth)
	}
	if got := field(t, hdr, "height"); got != strconv.Itoa(o.PixelHeight) || o.PixelHeight == 0 {
		t.Fatalf("%s: cao %s, PixelHeight %d", key, got, o.PixelHeight)
	}
	if got := field(t, hdr, "vips-loader"); got != loaders[o.Format] {
		t.Fatalf("%s: định dạng thật %s", key, got)
	}
	if o.Format == "avif" && field(t, hdr, "heif-compression") != "av1" {
		t.Fatalf("%s: không phải AV1", key)
	}
}

// Mỗi ảnh đúng 13 tiến trình con với bộ 12 đầu ra: 1 vipsheader cho nguồn + 12 vips thumbnail. Kích thước thật
// của đầu ra đọc từ JPEG cùng chiều rộng bằng mã Go (chỉ phần đầu), không thêm tiến trình (phép đo, imager-m1.md).
func TestImager_ThirteenChildProcessesPerImage(t *testing.T) {
	st := newStore(t)
	rec := &recorder{next: jpegFake()}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "n", readImage(t, "photo-gps.jpg")), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusOK {
		t.Fatalf("kết quả %+v", res)
	}
	if h, v := rec.count("vipsheader"), rec.count("vips"); h != 1 || v != 12 {
		t.Fatalf("vipsheader %d lần, vips %d lần; cần 1 và 12", h, v)
	}
	for i, o := range res.Outputs {
		want := twelve("")[i]
		if o.Width != want.Width || o.Format != want.Format {
			t.Fatalf("đầu ra %d là %d.%s, cần đúng thứ tự yêu cầu %d.%s", i, o.Width, o.Format, want.Width, want.Format)
		}
		if o.PixelWidth != o.Width || o.PixelHeight != o.Width*1600/2400 {
			t.Fatalf("đầu ra %d.%s: %dx%d", o.Width, o.Format, o.PixelWidth, o.PixelHeight)
		}
	}
	// Không có JPEG cùng chiều rộng thì đọc kích thước bằng vipsheader của chính đầu ra.
	rec = &recorder{next: jpegFake()}
	outs := []imager.Output{{Width: 320, Format: "webp", Quality: 75, PutURL: st.srv.URL + "/put/x.webp"}}
	res = imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "n2", readImage(t, "photo-gps.jpg")), outs), limits(t))
	if res.Status != imager.StatusOK || rec.count("vipsheader") != 2 || res.Outputs[0].PixelWidth != 2400 {
		// fakeRunner in kích thước nguồn cho mọi dòng tóm tắt, nên PixelWidth = 2400 ở đây.
		t.Fatalf("kết quả %+v, vipsheader %d lần", res, rec.count("vipsheader"))
	}
}

func TestImager_NoUpscale(t *testing.T) {
	requireTools(t, "vips", "vipsheader", "prlimit")
	st := newStore(t)
	res := imager.Process(t.Context(), execRunner(), imager.NewHTTPClient(10*time.Second),
		request(st.addSource(t, "tiny", readImage(t, "tiny.webp")), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusOK || res.DetectedType != "image/webp" || res.Width != 500 {
		t.Fatalf("kết quả %+v", res)
	}
	for _, o := range res.Outputs {
		want := min(500, o.Width)
		if o.PixelWidth != want {
			t.Fatalf("đầu ra %d.%s rộng %d, cần %d (không phóng to)", o.Width, o.Format, o.PixelWidth, want)
		}
		obj, _ := st.object(strconv.Itoa(o.Width) + "." + o.Format)
		if got := field(t, header(t, writeTemp(t, "o."+o.Format, obj.body)), "width"); got != strconv.Itoa(want) {
			t.Fatalf("tệp ghi rộng %s, cần %d", got, want)
		}
	}
}

// Ảnh dọc: `vips thumbnail <w>` mặc định giới hạn cả chiều cao bằng w; đầu ra phải rộng đúng w.
func TestImager_PortraitKeepsWidth(t *testing.T) {
	requireTools(t, "vips", "vipsheader", "prlimit")
	st := newStore(t)
	land := writeTemp(t, "land.jpg", readImage(t, "photo-gps.jpg"))
	portrait := filepath.Join(t.TempDir(), "portrait.jpg")
	if out, err := exec.CommandContext(t.Context(), "vips", "rot", land, portrait, "d90").CombinedOutput(); err != nil {
		t.Fatalf("vips rot: %v\n%s", err, out)
	}
	b, err := os.ReadFile(portrait)
	if err != nil {
		t.Fatal(err)
	}
	outs := []imager.Output{{Width: 320, Format: "jpeg", Quality: 80, PutURL: st.srv.URL + "/put/p.jpeg"}}
	res := imager.Process(t.Context(), execRunner(), imager.NewHTTPClient(10*time.Second),
		request(st.addSource(t, "portrait", b), outs), limits(t))
	if res.Status != imager.StatusOK || res.Width != 1600 || res.Height != 2400 {
		t.Fatalf("kết quả %+v", res)
	}
	if o := res.Outputs[0]; o.PixelWidth != 320 || o.PixelHeight != 480 {
		t.Fatalf("đầu ra %dx%d, cần 320x480", o.PixelWidth, o.PixelHeight)
	}
}

func TestImager_CMYKConvertedToSRGB(t *testing.T) {
	requireTools(t, "vips", "vipsheader", "prlimit")
	st := newStore(t)
	outs := []imager.Output{{Width: 320, Format: "jpeg", Quality: 80, PutURL: st.srv.URL + "/put/c.jpeg"}}
	res := imager.Process(t.Context(), execRunner(), imager.NewHTTPClient(10*time.Second),
		request(st.addSource(t, "cmyk", readImage(t, "cmyk.jpg")), outs), limits(t))
	if res.Status != imager.StatusOK {
		t.Fatalf("kết quả %+v", res)
	}
	obj, _ := st.object("c.jpeg")
	if got := field(t, header(t, writeTemp(t, "c.jpg", obj.body)), "interpretation"); got != "srgb" {
		t.Fatalf("interpretation %s, cần srgb", got)
	}
}

func TestImager_RejectsHTMLAsJPEG(t *testing.T) {
	st := newStore(t)
	rec := &recorder{next: jpegFake()}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "html", readImage(t, "html-as.jpg")), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonNotImage {
		t.Fatalf("kết quả %+v", res)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("không được gọi vips/vipsheader, đã gọi: %v", rec.calls)
	}
}

func TestImager_PixelBombRejectedBeforeDecode(t *testing.T) {
	requireTools(t, "vips", "vipsheader", "prlimit")
	st := newStore(t)
	rec := &recorder{next: execRunner()}
	src := readImage(t, "pixel-bomb.png")
	if len(src) > 70*1024 {
		t.Fatalf("pixel-bomb.png %d byte, cần khoảng 60 KB", len(src))
	}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "bomb", src), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTooManyPixels {
		t.Fatalf("kết quả %+v", res)
	}
	if res.Width != 30000 || res.Height != 30000 {
		t.Fatalf("kích thước khai %dx%d", res.Width, res.Height)
	}
	if n := rec.count("vips"); n != 0 {
		t.Fatalf("vips (giải mã) bị gọi %d lần", n)
	}
	if rec.count("vipsheader") == 0 {
		t.Fatal("phải đọc kích thước bằng vipsheader")
	}
}

func TestImager_TruncatedDecodeFailed(t *testing.T) {
	requireTools(t, "vips", "vipsheader", "prlimit")
	st := newStore(t)
	res := imager.Process(t.Context(), execRunner(), imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "trunc", readImage(t, "truncated.jpg")), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonDecodeFailed {
		t.Fatalf("kết quả %+v", res)
	}
	if len(st.out) != 0 {
		t.Fatalf("không được ghi đầu ra nào, đã ghi %d", len(st.out))
	}
}

func TestImager_TooLarge(t *testing.T) {
	st := newStore(t)
	rec := &recorder{next: jpegFake()}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(10*time.Second),
		request(st.srv.URL+"/huge", twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTooLarge {
		t.Fatalf("kết quả %+v", res)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("không được gọi lệnh con: %v", rec.calls)
	}
}

func TestImager_TypeNotAllowed(t *testing.T) {
	st := newStore(t)
	cases := map[string]struct {
		body    []byte
		allowed []string
		want    string
	}{
		"webp khi chỉ cho jpeg": {readImage(t, "tiny.webp"), []string{"image/jpeg"}, imager.ReasonTypeNotAllowed},
		"gif":                   {[]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04"), allowedTypes(), imager.ReasonTypeNotAllowed},
		"heic":                  {append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), make([]byte, 64)...), allowedTypes(), imager.ReasonTypeNotAllowed},
		"svg có script":         {[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), allowedTypes(), imager.ReasonNotImage},
		"tệp rỗng":              {nil, allowedTypes(), imager.ReasonNotImage},
		"zip":                   {[]byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"), allowedTypes(), imager.ReasonNotImage},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{next: jpegFake()}
			req := request(st.addSource(t, name, c.body), twelve(st.srv.URL))
			req.Allowed = c.allowed
			res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second), req, limits(t))
			if res.Status != imager.StatusRejected || res.Reason != c.want {
				t.Fatalf("kết quả %+v, cần %s", res, c.want)
			}
			if len(rec.calls) != 0 {
				t.Fatalf("không được gọi lệnh con: %v", rec.calls)
			}
		})
	}
}

func TestImager_LoaderMustMatchDetectedType(t *testing.T) {
	// Byte đầu là JPEG nhưng libvips chọn bộ đọc khác (tệp đa định dạng): từ chối, không giải mã.
	st := newStore(t)
	rec := &recorder{next: &fakeRunner{width: 10, height: 10, loader: "svgload"}}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "poly", readImage(t, "photo-gps.jpg")), twelve(st.srv.URL)), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTypeNotAllowed {
		t.Fatalf("kết quả %+v", res)
	}
	if rec.count("vips") != 0 {
		t.Fatal("vips không được chạy khi bộ đọc khác loại đã nhận")
	}
}

func TestImager_HangKilledWithinDeadline(t *testing.T) {
	st := newStore(t)
	lim := limits(t)
	lim.StepTimeout = time.Second
	lim.StepMemory = 0
	if _, err := exec.LookPath("prlimit"); err == nil {
		lim.StepMemory = 256 * mib
	}
	pidfile := filepath.Join(t.TempDir(), "pid")
	lim.VipsBin, lim.VipsHeaderBin = fakeVips(t, "hang.sh"), fakeVips(t, "hang.sh")
	if _, err := exec.LookPath("vipsheader"); err == nil {
		lim.VipsHeaderBin = "vipsheader" // trong container: vipsheader thật, treo ở bước giải mã
	}
	r := imager.NewExecRunner([]string{"FAKEVIPS_PIDFILE=" + pidfile})
	res := imager.Process(t.Context(), r, imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "hang", readImage(t, "photo-gps.jpg")), twelve(st.srv.URL)), lim)
	done := time.Now()
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonTimeout {
		t.Fatalf("kết quả %+v", res)
	}
	// Đồng hồ 2 s chạy từ lúc lệnh treo đã khởi động (giờ sửa của pidfile mà script ghi), không từ đầu Process: tải
	// nguồn, đọc đầu tệp trước đó chậm đi khi máy tải cao nhưng không thuộc phép đo "lệnh treo bị giết trong hạn"
	// (Task 317). Hạn bước bắt đầu trước khi script chạy, nên phép đo này không dễ hơn phép đo của hạn bước.
	fi, err := os.Stat(pidfile)
	if err != nil {
		t.Fatalf("script giả chưa chạy: %v", err)
	}
	if elapsed := done.Sub(fi.ModTime()); elapsed > 2*time.Second {
		t.Fatalf("trả lời sau %v kể từ lúc lệnh treo chạy, cần ≤ 2 s", elapsed)
	}
	assertGroupGone(t, pidfile)
}

// assertGroupGone: nhóm tiến trình của lệnh treo (sh và sleep con) không còn tiến trình nào.
func assertGroupGone(t *testing.T, pidfile string) {
	t.Helper()
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("script giả chưa chạy: %v", err)
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second) // hạn chờ dọn dẹp, rộng cho máy tải cao; thường xong ngay
	for {
		err := syscall.Kill(-pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nhóm tiến trình %d vẫn còn (kill -0: %v)", pgid, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// oomLimits: vips và vipsheader là oom.sh (xin 2 GiB), giới hạn 256 MiB mỗi lệnh con. Chỉ trên Linux có prlimit.
func oomLimits(t *testing.T) imager.Limits {
	t.Helper()
	requireTools(t, "prlimit")
	if runtime.GOOS != "linux" {
		t.Skip("prlimit chỉ có trên Linux")
	}
	lim := limits(t)
	lim.StepMemory = 256 * mib
	lim.VipsBin, lim.VipsHeaderBin = fakeVips(t, "oom.sh"), fakeVips(t, "oom.sh")
	return lim
}

func TestImager_OutOfMemoryKilled(t *testing.T) {
	st := newStore(t)
	lim := oomLimits(t)
	if _, err := exec.LookPath("vipsheader"); err == nil {
		lim.VipsHeaderBin = "vipsheader" // trong container: vipsheader thật, hết bộ nhớ ở bước giải mã
	}
	res := imager.Process(t.Context(), execRunner(), imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "oom", readImage(t, "photo-gps.jpg")), twelve(st.srv.URL)), lim)
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonDecodeFailed {
		t.Fatalf("kết quả %+v", res)
	}
}

func TestImager_ExpiredPutURL(t *testing.T) {
	st := newStore(t)
	outs := twelve(st.srv.URL)
	outs[5].PutURL = st.srv.URL + "/expired/1080.webp"
	res := imager.Process(t.Context(), jpegFake(), imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "exp", readImage(t, "photo-gps.jpg")), outs), limits(t))
	if res.Status != imager.StatusRejected || res.Reason != imager.ReasonUploadFailed {
		t.Fatalf("kết quả %+v", res)
	}
}

func TestImager_SourceUnavailable(t *testing.T) {
	st := newStore(t)
	rec := &recorder{next: jpegFake()}
	res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second),
		request(st.srv.URL+"/expired/src", twelve(st.srv.URL)), limits(t))
	wantSourceFailedNoCalls(t, res, rec)
}

func TestImager_TempFilesRemoved(t *testing.T) {
	st := newStore(t)
	lim := limits(t)
	res := imager.Process(t.Context(), jpegFake(), imager.NewHTTPClient(5*time.Second),
		request(st.addSource(t, "ok", readImage(t, "photo-gps.jpg")), twelve(st.srv.URL)), lim)
	if res.Status != imager.StatusOK || len(res.Outputs) != 12 {
		t.Fatalf("kết quả %+v", res)
	}
	left, err := os.ReadDir(lim.TmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("còn tệp tạm: %v", left)
	}
}

// Quy luật (6.11): nội dung bất kỳ không phải JPEG/PNG/WebP trong danh sách cho phép thì không lệnh con nào chạy
// (không có gì được giải mã), và lý do luôn thuộc tập đã khai.
func TestImager_PropertyNoDecodeUnlessAllowedImage(t *testing.T) {
	st := newStore(t)
	magic := map[string][]byte{
		"image/jpeg": {0xFF, 0xD8, 0xFF},
		"image/png":  []byte("\x89PNG\r\n\x1a\n"),
		"image/webp": []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
	}
	known := []string{
		imager.ReasonNotImage, imager.ReasonTypeNotAllowed, imager.ReasonTooLarge, imager.ReasonTooManyPixels,
		imager.ReasonDecodeFailed, imager.ReasonTimeout, imager.ReasonUploadFailed, imager.ReasonSourceFailed,
	}
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		body := rapid.SliceOfN(rapid.Byte(), 0, 4096).Draw(rt, "body")
		prefix := rapid.SampledFrom([]string{"", "image/jpeg", "image/png", "image/webp", "html", "gif"}).Draw(rt, "prefix")
		switch prefix {
		case "html":
			body = append([]byte("<html><script>"), body...)
		case "gif":
			body = append([]byte("GIF89a"), body...)
		case "":
		default:
			body = append(slices.Clone(magic[prefix]), body...)
		}
		var allow []string
		for _, a := range allowedTypes() {
			if rapid.Bool().Draw(rt, a) {
				allow = append(allow, a)
			}
		}
		n++
		rec := &recorder{next: &fakeRunner{width: 100, height: 100, loader: "jpegload"}}
		req := request(st.addSource(t, "p"+strconv.Itoa(n), body), twelve(st.srv.URL))
		req.Allowed = allow
		res := imager.Process(t.Context(), rec, imager.NewHTTPClient(5*time.Second), req, limits(t))
		if res.Status == imager.StatusRejected && !slices.Contains(known, res.Reason) {
			rt.Fatalf("lý do lạ %q", res.Reason)
		}
		if len(rec.calls) > 0 && !slices.Contains(allow, res.DetectedType) {
			rt.Fatalf("đã chạy lệnh con cho loại %q không cho phép (%v)", res.DetectedType, allow)
		}
		if len(rec.calls) > 0 && !bytes.HasPrefix(body, magic[res.DetectedType]) {
			rt.Fatalf("đã chạy lệnh con khi nội dung không bắt đầu bằng chữ ký %s", res.DetectedType)
		}
	})
}

// timer ghi thời gian mỗi lệnh con (phép đo, docs/do-luong/imager-m1.md).
type timer struct {
	next imager.Runner
	mu   sync.Mutex
	log  []string
	sum  time.Duration
}

func (r *timer) Run(ctx context.Context, name string, args []string, memLimit int64) ([]byte, error) {
	start := time.Now()
	out, err := r.next.Run(ctx, name, args, memLimit)
	d := time.Since(start)
	r.mu.Lock()
	r.sum += d
	what := filepath.Base(name)
	if len(args) > 3 && args[0] == "thumbnail" {
		what += " " + args[3] + " " + filepath.Ext(outPath.ReplaceAllString(args[2], "$1"))
	}
	r.log = append(r.log, fmt.Sprintf("%-26s %6d ms", what, d.Milliseconds()))
	r.mu.Unlock()
	return out, err //nolint:wrapcheck // chuyển tiếp nguyên lỗi
}

// TestImager_Measure: chỉ chạy khi IMAGER_MEASURE_DIR trỏ thư mục ảnh .jpg (make test-imager, xem imager-m1.md).
// In thời gian từng lệnh con, tổng một ảnh (12 đầu ra, gồm tải và PUT qua HTTP cục bộ), và phần lệnh con chiếm.
func TestImager_Measure(t *testing.T) {
	dir := os.Getenv("IMAGER_MEASURE_DIR")
	if dir == "" {
		t.Skip("đặt IMAGER_MEASURE_DIR để đo")
	}
	requireTools(t, "vips", "vipsheader", "prlimit")
	files, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil || len(files) == 0 {
		t.Fatalf("không có ảnh trong %s", dir)
	}
	st := newStore(t)
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // G703: thư mục ảnh đo do người chạy đặt qua IMAGER_MEASURE_DIR
		if err != nil {
			t.Fatal(err)
		}
		src := st.addSource(t, filepath.Base(f), b)
		for run := range 3 {
			tm := &timer{next: imager.NewExecRunner([]string{"VIPS_CONCURRENCY=2"})}
			start := time.Now()
			res := imager.Process(t.Context(), tm, imager.NewHTTPClient(30*time.Second),
				request(src, twelve(st.srv.URL)), limits(t))
			total := time.Since(start)
			if res.Status != imager.StatusOK {
				t.Fatalf("%s: %+v", f, res)
			}
			if run == 2 {
				t.Logf("%s (%dx%d, %d byte), lần %d:\n%s", filepath.Base(f), res.Width, res.Height, len(b), run+1,
					strings.Join(tm.log, "\n"))
			}
			t.Logf("%s lần %d: tổng %d ms, lệnh con %d ms (%d lệnh)", filepath.Base(f), run+1,
				total.Milliseconds(), tm.sum.Milliseconds(), len(tm.log))
		}
	}
	// Khởi động một tiến trình vips rỗng × 13 (cùng prlimit và môi trường như imager).
	r := imager.NewExecRunner(nil)
	start := time.Now()
	for range 13 {
		if _, err := r.Run(t.Context(), "vips", []string{"--version"}, 1<<30); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("vips --version × 13 qua prlimit: %d ms", time.Since(start).Milliseconds())
}
