package imager

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Trạng thái của Result.
const (
	StatusOK       = "ok"
	StatusRejected = "rejected"
)

// Lý do từ chối. Bốn lý do cuối là lỗi tạm (không do nội dung ảnh): bên gọi nên chạy lại (Task M1-43: `not_sent`).
const (
	ReasonNotImage       = "not_image"
	ReasonTypeNotAllowed = "type_not_allowed"
	ReasonTooLarge       = "too_large"
	ReasonTooManyPixels  = "too_many_pixels"
	// ReasonTooSmall: ảnh hẹp hơn Request.MinWidth (Task 453: banner rộng ≥ 600 px).
	ReasonTooSmall     = "too_small"
	ReasonDecodeFailed = "decode_failed"
	ReasonTimeout      = "timeout"
	ReasonUploadFailed = "upload_failed"
	ReasonSourceFailed = "source_failed"
	ReasonInternal     = "internal"
)

// Giới hạn của một yêu cầu (kiểm ở Validate).
const (
	maxOutputs  = 32
	maxWidth    = 8192
	maxHeaders  = 8
	thumbHeight = "10000000" // VIPS_MAX_COORD: chỉ giới hạn theo chiều rộng (mặc định của vips là cao = rộng)
)

// Output là một biến thể cần tạo. PutURL là đường dẫn PUT ký sẵn; Headers (tùy chọn) là header mà chữ ký đòi.
type Output struct {
	Width   int               `json:"width"`
	Format  string            `json:"format"` // avif|webp|jpeg
	Quality int               `json:"quality"`
	PutURL  string            `json:"putUrl"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Request là việc worker giao (ADR-45). SourceURL là đường dẫn GET ký sẵn của ảnh thô.
type Request struct {
	SourceURL string `json:"sourceUrl"`
	MaxBytes  int64  `json:"maxBytes"`
	MaxPixels int64  `json:"maxPixels"`
	// MinWidth > 0: ảnh hẹp hơn bị từ chối too_small trước khi giải mã (Task 453); 0 = không kiểm (yêu cầu cũ).
	MinWidth int      `json:"minWidth,omitempty"`
	Allowed  []string `json:"allowed"`
	Outputs  []Output `json:"outputs"`
}

// OutputResult là một biến thể đã ghi. Width, Format là kích thước và định dạng đã xin (khóa của biến thể);
// PixelWidth, PixelHeight là kích thước thật (nhỏ hơn Width khi ảnh gốc nhỏ hơn, vì không phóng to).
type OutputResult struct {
	Width       int    `json:"width"`
	Format      string `json:"format"`
	PixelWidth  int    `json:"pixelWidth"`
	PixelHeight int    `json:"pixelHeight"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
}

// Result là kết quả xử lý. Width, Height, DetectedType, SourceSHA256 là của ảnh gốc, có khi đã đọc được.
type Result struct {
	Status       string         `json:"status"`
	Reason       string         `json:"reason,omitempty"`
	DetectedType string         `json:"detectedType,omitempty"`
	Width        int            `json:"width,omitempty"`
	Height       int            `json:"height,omitempty"`
	SourceSHA256 string         `json:"sourceSha256,omitempty"`
	Outputs      []OutputResult `json:"outputs,omitempty"`
}

// Limits là giới hạn của tiến trình imager (biến môi trường IMAGER_*), áp cho từng bước.
type Limits struct {
	StepTimeout   time.Duration // mỗi bước: tải nguồn, mỗi lệnh con, mỗi PUT
	StepMemory    int64         // byte, không gian địa chỉ của mỗi lệnh con (prlimit --as); 0 = không giới hạn
	TmpDir        string        // thư mục tạm (tmpfs); "" = os.TempDir()
	VipsBin       string
	VipsHeaderBin string
}

// ErrBadRequest là yêu cầu sai hình dạng (server trả 400).
var ErrBadRequest = errors.New("yêu cầu không hợp lệ")

var (
	errHTTPStatus = errors.New("kho tệp trả mã lỗi")
	errHeaderOut  = errors.New("vipsheader trả kết quả lạ")
	summaryLine   = regexp.MustCompile(`^(\d+)x(\d+) [a-z0-9]+, \d+ bands?, [a-z0-9-]+, ([a-z0-9_]+)$`)
)

// Validate kiểm hình dạng của yêu cầu. Không kiểm nội dung ảnh (việc của Process).
func (r Request) Validate() error {
	switch {
	case !httpURL(r.SourceURL):
		return fmt.Errorf("%w: sourceUrl", ErrBadRequest)
	case r.MaxBytes <= 0 || r.MaxPixels <= 0:
		return fmt.Errorf("%w: maxBytes, maxPixels", ErrBadRequest)
	case r.MinWidth < 0 || r.MinWidth > maxWidth:
		return fmt.Errorf("%w: minWidth", ErrBadRequest)
	case len(r.Allowed) == 0:
		return fmt.Errorf("%w: allowed", ErrBadRequest)
	case len(r.Outputs) == 0 || len(r.Outputs) > maxOutputs:
		return fmt.Errorf("%w: outputs", ErrBadRequest)
	}
	seen := map[string]bool{}
	for i, o := range r.Outputs {
		key := strconv.Itoa(o.Width) + "." + o.Format
		switch {
		case o.Width <= 0 || o.Width > maxWidth || o.Quality < 0 || o.Quality > 100:
			return fmt.Errorf("%w: outputs[%d] width, quality", ErrBadRequest, i)
		case extension(o.Format) == "":
			return fmt.Errorf("%w: outputs[%d] format", ErrBadRequest, i)
		case !httpURL(o.PutURL) || len(o.Headers) > maxHeaders:
			return fmt.Errorf("%w: outputs[%d] putUrl, headers", ErrBadRequest, i)
		case seen[key]:
			return fmt.Errorf("%w: outputs[%d] trùng", ErrBadRequest, i)
		}
		seen[key] = true
	}
	return nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func extension(format string) string {
	switch format {
	case "avif":
		return "avif"
	case "webp":
		return "webp"
	case "jpeg":
		return "jpg"
	}
	return ""
}

// NewHTTPClient trả client cho kho tệp. Không theo chuyển hướng (đường dẫn ký sẵn không chuyển hướng; theo thì có thể
// bị dẫn tới địa chỉ khác), không proxy, không tự giải nén (đếm byte trên đúng nội dung kho trả).
func NewHTTPClient(timeout time.Duration) *http.Client {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t = &http.Transport{}
	}
	tr := t.Clone()
	tr.Proxy = nil
	tr.DisableCompression = true
	return &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Process xử lý một ảnh theo thứ tự của ADR-45: (1) tải tối đa MaxBytes, băm; (2) nhận loại bằng byte đầu;
// (3) đọc kích thước bằng vipsheader, từ chối bom điểm ảnh trước khi giải mã; (4) mỗi đầu ra: vips thumbnail bỏ
// siêu dữ liệu, rồi PUT. Luôn trả Result (Request phải qua Validate trước); tệp tạm bị xóa khi xong.
func Process(ctx context.Context, r Runner, hc *http.Client, req Request, lim Limits) Result {
	dir, err := os.MkdirTemp(lim.TmpDir, "img-")
	if err != nil {
		return Result{Status: StatusRejected, Reason: ReasonInternal}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	p := &pipeline{r: r, hc: hc, req: req, lim: lim, src: filepath.Join(dir, "src"), dir: dir}
	return p.run(ctx)
}

type pipeline struct {
	r        Runner
	hc       *http.Client
	req      Request
	lim      Limits
	dir, src string
	res      Result
}

func (p *pipeline) run(ctx context.Context) Result {
	steps := []func(context.Context) string{p.fetch, p.sniff, p.header, p.outputs}
	for _, step := range steps {
		if reason := step(ctx); reason != "" {
			p.res.Status, p.res.Reason, p.res.Outputs = StatusRejected, reason, nil
			return p.res
		}
	}
	p.res.Status = StatusOK
	return p.res
}

// step chạy f với hạn của một bước.
func (p *pipeline) step(ctx context.Context, f func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, p.lim.StepTimeout)
	defer cancel()
	return f(ctx)
}

// (1) tải nguồn vào tệp tạm, tối đa MaxBytes, tính sha256.
func (p *pipeline) fetch(ctx context.Context) string {
	var size int64
	tooLarge := false
	err := p.step(ctx, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.req.SourceURL, nil)
		if err != nil {
			return fmt.Errorf("tạo yêu cầu tải nguồn: %w", err)
		}
		resp, err := p.hc.Do(req)
		if err != nil {
			return fmt.Errorf("tải nguồn: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("tải nguồn: %w %d", errHTTPStatus, resp.StatusCode)
		}
		if resp.ContentLength > p.req.MaxBytes {
			tooLarge = true
			return nil
		}
		f, err := os.OpenFile(p.src, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("tạo tệp nguồn: %w", err)
		}
		h := sha256.New()
		size, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, p.req.MaxBytes+1))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("đọc nguồn: %w", err)
		}
		if size > p.req.MaxBytes {
			tooLarge = true
			return nil
		}
		p.res.SourceSHA256 = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	switch {
	case err != nil && isTimeout(err):
		return ReasonTimeout
	case err != nil:
		return ReasonSourceFailed
	case tooLarge:
		return ReasonTooLarge
	}
	return ""
}

// (2) nhận loại bằng sniffLen byte đầu.
func (p *pipeline) sniff(context.Context) string {
	f, err := os.Open(p.src)
	if err != nil {
		return ReasonInternal
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return ReasonInternal
	}
	p.res.DetectedType = detect(head[:n])
	return classify(p.res.DetectedType, p.req.Allowed)
}

// (3) kích thước và bộ đọc libvips bằng một lần vipsheader (chỉ đọc phần đầu tệp); bộ đọc phải khớp loại đã nhận;
// vượt MaxPixels hay hẹp hơn MinWidth thì dừng trước khi giải mã.
func (p *pipeline) header(ctx context.Context) string {
	line, err := p.run1(ctx, p.lim.VipsHeaderBin, []string{p.src})
	if err != nil {
		return runReason(err)
	}
	w, h, loader, ok := parseSummary(line, p.src)
	if !ok {
		return ReasonDecodeFailed
	}
	if want, _ := loaderFor(p.res.DetectedType); loader != want {
		return ReasonTypeNotAllowed
	}
	p.res.Width, p.res.Height = w, h
	if int64(w)*int64(h) > p.req.MaxPixels {
		return ReasonTooManyPixels
	}
	if w < p.req.MinWidth {
		return ReasonTooSmall
	}
	return ""
}

// parseSummary đọc dòng tóm tắt của vipsheader: "<tệp>: <rộng>x<cao> <kiểu>, <n> band(s), <màu>, <bộ đọc>".
// Tên tệp do imager đặt; phần còn lại là số và tên liệt kê của libvips, không có chữ lấy từ siêu dữ liệu ảnh.
func parseSummary(line []byte, path string) (w, h int, loader string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSuffix(string(line), "\n"), path+": ")
	m := summaryLine.FindStringSubmatch(rest)
	if !found || m == nil {
		return 0, 0, "", false
	}
	w, werr := strconv.Atoi(m[1])
	h, herr := strconv.Atoi(m[2])
	if werr != nil || herr != nil || w <= 0 || h <= 0 {
		return 0, 0, "", false
	}
	return w, h, m[3], true
}

// (4) mỗi đầu ra: tạo bằng vips thumbnail (không phóng to, bỏ siêu dữ liệu, lỗi giải mã là lỗi), đọc kích thước
// thật, rồi PUT; xóa tệp đầu ra ngay sau khi ghi để thư mục tạm không phình. Làm JPEG trước trong mỗi chiều rộng:
// ba định dạng cùng chiều rộng có cùng kích thước, đọc từ phần đầu JPEG bằng mã Go thì không tốn tiến trình con.
// Kết quả giữ đúng thứ tự của yêu cầu.
func (p *pipeline) outputs(ctx context.Context) string {
	order := make([]int, len(p.req.Outputs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		oa, ob := p.req.Outputs[a], p.req.Outputs[b]
		return cmp.Or(cmp.Compare(oa.Width, ob.Width), cmp.Compare(jpegLast(oa), jpegLast(ob)))
	})
	results := make([]OutputResult, len(p.req.Outputs))
	dims := map[int][2]int{}
	for _, i := range order {
		o := p.req.Outputs[i]
		out := filepath.Join(p.dir, "out-"+strconv.Itoa(i)+"."+extension(o.Format))
		opts := "keep=none"
		if o.Quality > 0 {
			opts = "Q=" + strconv.Itoa(o.Quality) + "," + opts
		}
		args := []string{
			"thumbnail", p.src, out + "[" + opts + "]", strconv.Itoa(o.Width),
			"--height", thumbHeight, "--size", "down", "--fail-on", "error",
		}
		if _, err := p.run1(ctx, p.lim.VipsBin, args); err != nil {
			return runReason(err)
		}
		d, ok := dims[o.Width]
		if !ok {
			var reason string
			if d, reason = p.measure(ctx, out, o.Format); reason != "" {
				return reason
			}
			dims[o.Width] = d
		}
		res := OutputResult{Width: o.Width, Format: o.Format, PixelWidth: d[0], PixelHeight: d[1]}
		if reason := p.put(ctx, out, o, &res); reason != "" {
			return reason
		}
		_ = os.Remove(out)
		results[i] = res
	}
	p.res.Outputs = results
	return ""
}

func jpegLast(o Output) int {
	if o.Format == "jpeg" {
		return 0
	}
	return 1
}

// measure trả kích thước thật của một đầu ra do chính vips vừa ghi: JPEG đọc phần đầu bằng image/jpeg, định dạng
// khác bằng dòng tóm tắt của vipsheader.
func (p *pipeline) measure(ctx context.Context, out, format string) ([2]int, string) {
	if format == "jpeg" {
		f, err := os.Open(out) //nolint:gosec // G304: tệp đầu ra trong thư mục tạm của imager
		if err != nil {
			return [2]int{}, ReasonInternal
		}
		defer func() { _ = f.Close() }()
		c, err := jpeg.DecodeConfig(f)
		if err != nil || c.Width <= 0 || c.Height <= 0 {
			return [2]int{}, ReasonDecodeFailed
		}
		return [2]int{c.Width, c.Height}, ""
	}
	line, err := p.run1(ctx, p.lim.VipsHeaderBin, []string{out})
	if err != nil {
		return [2]int{}, runReason(err)
	}
	w, h, _, ok := parseSummary(line, out)
	if !ok {
		return [2]int{}, ReasonDecodeFailed
	}
	return [2]int{w, h}, ""
}

func (p *pipeline) put(ctx context.Context, out string, o Output, res *OutputResult) string {
	sum, size, err := hashFile(out)
	if err != nil {
		return ReasonInternal
	}
	res.SHA256, res.Bytes = sum, size
	err = p.step(ctx, func(ctx context.Context) error {
		f, err := os.Open(out) //nolint:gosec // G304: tệp đầu ra trong thư mục tạm của imager
		if err != nil {
			return fmt.Errorf("mở đầu ra: %w", err)
		}
		defer func() { _ = f.Close() }()
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, o.PutURL, f)
		if err != nil {
			return fmt.Errorf("tạo yêu cầu ghi: %w", err)
		}
		req.ContentLength = size
		for k, v := range o.Headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "image/"+o.Format)
		}
		resp, err := p.hc.Do(req)
		if err != nil {
			return fmt.Errorf("ghi đầu ra: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("ghi đầu ra: %w %d", errHTTPStatus, resp.StatusCode)
		}
		return nil
	})
	switch {
	case err != nil && isTimeout(err):
		return ReasonTimeout
	case err != nil:
		return ReasonUploadFailed
	}
	return ""
}

// run1 chạy một lệnh con với hạn và giới hạn bộ nhớ của một bước.
func (p *pipeline) run1(ctx context.Context, name string, args []string) ([]byte, error) {
	var out []byte
	err := p.step(ctx, func(ctx context.Context) error {
		var err error
		out, err = p.r.Run(ctx, name, args, p.lim.StepMemory)
		if err != nil {
			return fmt.Errorf("lệnh con: %w", err)
		}
		if len(out) == 0 && name == p.lim.VipsHeaderBin {
			return errHeaderOut
		}
		return nil
	})
	return out, err
}

func runReason(err error) string {
	if isTimeout(err) {
		return ReasonTimeout
	}
	return ReasonDecodeFailed
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		(errors.As(err, &ne) && ne.Timeout())
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path) //nolint:gosec // G304: tệp đầu ra trong thư mục tạm của imager
	if err != nil {
		return "", 0, fmt.Errorf("mở đầu ra: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("băm đầu ra: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
