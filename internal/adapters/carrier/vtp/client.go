package vtp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// maxBody giới hạn thân trả lời đọc vào (danh mục phường của một tỉnh vài trăm dòng).
const maxBody = 1 << 20

// reply là trả lời thô của hãng.
type reply struct {
	status int
	header http.Header
	body   []byte
}

// envelope là phong bì trả lời chung của VTP (trang "Môi trường và tham số chung"); message là chuỗi hay mảng.
type envelope struct {
	Status  int             `json:"status"`
	Error   bool            `json:"error"`
	Message json.RawMessage `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// baseURL chọn đường dẫn gốc: Config.BaseURL, hoặc theo môi trường của tài khoản; môi trường lạ → lỗi permanent,
// không gọi mạng.
func (a *Adapter) baseURL(acc fulfillment.Account) (string, error) {
	if a.base != "" {
		return a.base, nil
	}
	switch acc.Environment {
	case "sandbox":
		return sandboxURL, nil
	case "production":
		return productionURL, nil
	}
	return "", &fulfillment.CarrierError{Class: fulfillment.ClassPermanent, Code: codeEnvironment, Err: errStatus}
}

// call gọi một API cần mã truy cập: lấy mã (trong bộ nhớ hay đăng nhập), gửi, đọc phong bì, giải data vào out. Hãng báo
// lỗi token (mã hết hạn, bị thu hồi) → bỏ mã, đăng nhập lại đúng một lần rồi gửi lại (lỗi token nghĩa là hãng chưa xử
// lý yêu cầu); lần hai vẫn lỗi token → lỗi auth. Lỗi của bước đăng nhập là "chưa gửi" với yêu cầu chính.
func (a *Adapter) call(ctx context.Context, acc fulfillment.Account, k kind, method, path string, in, out any) error {
	base, err := a.baseURL(acc)
	if err != nil {
		return err
	}
	tok, err := a.token(ctx, acc, base)
	if err != nil {
		return asNotSent(err)
	}
	for retried := false; ; retried = true {
		rep, err := a.send(ctx, k, base, method, path, tok, in)
		if err != nil {
			return err
		}
		data, ce := decode(k, rep)
		if ce != nil && ce.Code == codeToken && !retried {
			a.tokens.drop(sessionKey(acc, base), tok)
			if tok, err = a.login(ctx, acc, base); err != nil {
				return asNotSent(err)
			}
			continue
		}
		if ce != nil {
			return ce
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				return afterSend(k, codeBadReply)
			}
		}
		return nil
	}
}

// decode đọc trả lời: HTTP khác 200 → classifyHTTP; thân là mảng trần (getPriceAll) → chính nó; phong bì lỗi →
// classifyBody; thân không đọc được → lỗi sau khi gửi (tạo vận đơn: không rõ).
func decode(k kind, rep reply) (json.RawMessage, *fulfillment.CarrierError) {
	if rep.status != http.StatusOK {
		return nil, classifyHTTP(k, rep.status, rep.header)
	}
	body := bytes.TrimSpace(rep.body)
	if len(body) > 0 && body[0] == '[' {
		return body, nil
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil || env.Status == 0 {
		return nil, afterSend(k, codeBadReply) // không phải phong bì của VTP: không biết hãng đã làm gì
	}
	if env.Status != vtpOK || env.Error {
		var msg string
		_ = json.Unmarshal(env.Message, &msg) // message dạng mảng: chỉ phân lớp theo mã
		return nil, classifyBody(k, env.Status, msg)
	}
	return env.Data, nil
}

// send gửi một yêu cầu với hạn giờ của loại việc k. Không dựng được hay không nối được → chưa gửi; lỗi khác sau khi
// gửi (quá giờ, đứt) → afterSend. URL không chép vào lỗi; thân yêu cầu (tên, SĐT, địa chỉ) không bao giờ vào lỗi hay log.
func (a *Adapter) send(ctx context.Context, k kind, base, method, path, token string, in any) (reply, error) {
	ctx, cancel := context.WithTimeout(ctx, k.timeout())
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return reply{}, &fulfillment.CarrierError{
				Class: fulfillment.ClassNotSentRetry, Code: codeBuildRequest,
				Err: fmt.Errorf("mã hóa yêu cầu VTP: %w: %w", err, fulfillment.ErrNotSent),
			}
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return reply{}, &fulfillment.CarrierError{
			Class: fulfillment.ClassNotSentRetry, Code: codeBuildRequest,
			Err: fmt.Errorf("dựng yêu cầu VTP: %w", fulfillment.ErrNotSent),
		}
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	if token != "" {
		req.Header.Set("Token", token)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return reply{}, transportErr(k, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return reply{}, transportErr(k, err)
	}
	return reply{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

// transportErr phân lớp lỗi mạng: không nối được (dial) → chưa gửi; còn lại → afterSend. Bỏ *url.Error (có URL) khỏi
// chuỗi lỗi; lỗi quá giờ vẫn khớp errors.Is(err, context.DeadlineExceeded).
func transportErr(k kind, err error) *fulfillment.CarrierError {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
		return &fulfillment.CarrierError{
			Class: fulfillment.ClassNotSentRetry, Code: codeDial, Err: fmt.Errorf("nối VTP: %w", fulfillment.ErrNotSent),
		}
	}
	ce := afterSend(k, codeTransport)
	ce.Err = fmt.Errorf("gọi VTP: %w: %w", ce.Err, err)
	return ce
}
