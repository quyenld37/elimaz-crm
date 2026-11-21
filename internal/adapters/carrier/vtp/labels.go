package vtp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Nhãn (trang "Lấy link in vận đơn"): POST /v2/order/printing-code {EXPIRY_TIME, ORDER_ARRAY ≤ 100 mã} → mã in ở
// message; link in = <máy in>/DigitalizePrint/report.do?type=<khổ>&bill=<mã in>&showPostage=<0|1>.
const (
	maxLabelCodes = 100
	// maxLabelPDF là cỡ tối đa của tệp nhãn (Task 107: ≤ 20 MB).
	maxLabelPDF = 20 << 20
	// printTTL là hạn của mã in: chỉ cần đủ để tải ngay.
	printTTL = 10 * time.Minute
	// labelType là khổ nhãn: 2 = mẫu link thứ hai của tài liệu, dưới mục "Nhãn A6" (1 là A5, 100/1001 là A7).
	labelType = "2"
	// showPostage = 0: không in tiền cước lên nhãn (người nhận không thấy cước shop trả).
	showPostage = "0"

	codeLabelCount    = "vtp_label_count"
	codeLabelRedirect = "vtp_label_redirect"
	codeInvalidPDF    = "invalid_pdf"
	codeTooLarge      = "too_large"
	pdfMagic          = "%PDF-"
)

type printBody struct {
	ExpiryTime int64    `json:"EXPIRY_TIME"`
	OrderArray []string `json:"ORDER_ARRAY"`
}

// printURL chọn máy in nhãn: Config.PrintURL (hay BaseURL khi có), hoặc theo môi trường của tài khoản; môi trường lạ →
// lỗi permanent, không gọi mạng. Máy in luôn do adapter khai, không bao giờ lấy từ trả lời của hãng (chống SSRF).
func (a *Adapter) printURL(acc fulfillment.Account) (string, error) {
	return byEnvironment(a.print, acc, sandboxPrintURL, productionPrintURL)
}

// Labels lấy nhãn PDF của 1–100 vận đơn VTP một lần (LabelsBatch): xin mã in rồi tải PDF từ máy in của môi trường.
// Pages = số vận đơn (một nhãn mỗi vận đơn). Số mã sai hay mã rỗng → từ chối, không gọi hãng. Tải nhãn là việc đọc:
// lỗi tạm là chưa gửi, đọc lại được.
func (a *Adapter) Labels(ctx context.Context, acc fulfillment.Account, trackingCodes []string) (fulfillment.Label, error) {
	if len(trackingCodes) == 0 || len(trackingCodes) > maxLabelCodes {
		return fulfillment.Label{}, fmt.Errorf("lấy nhãn Viettel Post: %w", rejected(codeLabelCount))
	}
	for _, c := range trackingCodes {
		if c == "" {
			return fulfillment.Label{}, fmt.Errorf("lấy nhãn Viettel Post: %w", rejected(codeNoTrack))
		}
	}
	printer, err := a.printURL(acc)
	if err != nil {
		return fulfillment.Label{}, fmt.Errorf("lấy nhãn Viettel Post: %w", err)
	}
	body := printBody{ExpiryTime: a.now().Add(printTTL).UnixMilli(), OrderArray: trackingCodes}
	_, msg, err := a.exchange(ctx, acc, kindLabel, http.MethodPost, "/v2/order/printing-code", body)
	if err != nil {
		return fulfillment.Label{}, fmt.Errorf("lấy mã in Viettel Post: %w", err)
	}
	var code string
	if json.Unmarshal(msg, &code) != nil || code == "" {
		return fulfillment.Label{}, fmt.Errorf("lấy mã in Viettel Post: %w", notSent(codeBadReply))
	}
	pdf, err := a.download(ctx, printer+"/DigitalizePrint/report.do?type="+labelType+"&bill="+url.QueryEscape(code)+
		"&showPostage="+showPostage)
	if err != nil {
		return fulfillment.Label{}, fmt.Errorf("tải nhãn Viettel Post: %w", err)
	}
	return fulfillment.Label{PDF: pdf, Pages: len(trackingCodes)}, nil
}

// download tải tệp nhãn ở link (máy in do adapter khai): không gửi header Token (mã truy cập chỉ cho máy API), không
// theo chuyển hướng (3xx → permanent, chống SSRF); 429, 5xx theo classifyHTTP; HTTP khác 200 còn lại, quá 20 MB hay
// không bắt đầu %PDF- → permanent. URL không vào lỗi (có mã in).
func (a *Adapter) download(ctx context.Context, link string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, kindLabel.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, notSent(codeBuildRequest)
	}
	hc := *a.hc
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return nil, transportErr(kindLabel, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch st := resp.StatusCode; {
	case st >= 300 && st < 400:
		return nil, permanent(codeLabelRedirect)
	case st == http.StatusTooManyRequests || st >= 500:
		return nil, classifyHTTP(kindLabel, st, resp.Header) // chờ Retry-After, hay chưa gửi (đọc lại được)
	case st != http.StatusOK:
		return nil, permanent("http_" + strconv.Itoa(st)) // máy in không dùng mã truy cập: 401/403 không phải lỗi khóa
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxLabelPDF+1))
	switch {
	case err != nil:
		return nil, transportErr(kindLabel, err)
	case len(b) > maxLabelPDF:
		return nil, permanent(codeTooLarge)
	case !bytes.HasPrefix(b, []byte(pdfMagic)):
		return nil, permanent(codeInvalidPDF)
	}
	return b, nil
}

func permanent(code string) *fulfillment.CarrierError {
	return &fulfillment.CarrierError{Class: fulfillment.ClassPermanent, Code: code, Err: errStatus}
}
