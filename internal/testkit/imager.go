package testkit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

// StubImager là imager giả luôn coi nguồn là ảnh JPEG 2000×1500 tốt: đọc nguồn bằng SourceURL, ghi mỗi đầu ra bằng
// PutURL kèm Headers (đúng hợp đồng của internal/imager), không giải mã gì. Dùng cho test cần ảnh ready thật trên kho
// (đối chiếu kho, diễn tập kho tệp) mà không cần container imager. Khi nào không dùng: test của chính đường xử lý ảnh
// (cần ca bận, treo, từ chối: imager giả của files_test).
type StubImager struct{}

// stubHTTP: hạn mỗi lần đọc, ghi của StubImager.
const stubHTTP = 30 * time.Second

// Process implements files.Imager.
func (StubImager) Process(ctx context.Context, req imager.Request) (imager.Result, error) {
	hc := &http.Client{Timeout: stubHTTP}
	src, err := stubCall(ctx, hc, http.MethodGet, req.SourceURL, nil, nil)
	if err != nil {
		return imager.Result{Status: imager.StatusRejected, Reason: imager.ReasonSourceFailed}, nil
	}
	sum := sha256.Sum256(src)
	res := imager.Result{
		Status: imager.StatusOK, DetectedType: "image/jpeg", Width: 2000, Height: 1500, SourceSHA256: hex.EncodeToString(sum[:]),
	}
	for _, o := range req.Outputs {
		body := fmt.Appendf(nil, "%x-%d.%s", sum, o.Width, o.Format)
		h := map[string]string{"Content-Type": "image/" + o.Format}
		maps.Copy(h, o.Headers)
		if _, err := stubCall(ctx, hc, http.MethodPut, o.PutURL, body, h); err != nil {
			return imager.Result{Status: imager.StatusRejected, Reason: imager.ReasonUploadFailed}, nil
		}
		res.Outputs = append(res.Outputs, imager.OutputResult{
			Width: o.Width, Format: o.Format, PixelWidth: o.Width, PixelHeight: o.Width * 3 / 4, Bytes: int64(len(body)),
		})
	}
	return res, nil
}

// stubCall gửi một yêu cầu tới đường dẫn ký sẵn; mã khác 200 là lỗi.
func stubCall(ctx context.Context, hc *http.Client, method, url string, body []byte, h map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("imager giả: %w", err)
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("imager giả: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	switch {
	case err != nil:
		return nil, fmt.Errorf("imager giả: %s: %w", method, err)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("imager giả: %s trả %d", method, resp.StatusCode) //nolint:err113 // lỗi của bản giả trong test
	}
	return out, nil
}
