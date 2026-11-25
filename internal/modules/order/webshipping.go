package order

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
)

// Hãng/dịch vụ khách chọn trên website (Task 490; website Task 214): website gửi lựa chọn kèm báo giá và đơn; lớp
// HTTP (httpapi/web, nhập được fulfillment) đối chiếu với hãng đang hiện trên web và báo phí rồi ghi kết quả vào
// WebShipChoice.Problem; order chỉ áp luật tiền (khách không trả hơn phí hãng) và luật gửi thẳng/hàng đợi (QĐ15), rồi
// lưu lựa chọn lên đơn (ord.orders.web_shipping) để nhân viên gửi đúng hãng. Không tự gửi hãng.

// Vấn đề của lựa chọn giao hàng (WebShipChoice.Problem; rỗng: khớp).
const (
	// ShipProblemCarrierUnavailable: tài khoản hãng không còn active, hiện trên web; hay phí dự phòng đã bỏ.
	ShipProblemCarrierUnavailable = "carrier_unavailable"
	// ShipProblemFeeMismatch: phí, dịch vụ, địa chỉ lệch báo phí, hay khách trả hơn phí hãng.
	ShipProblemFeeMismatch = "fee_mismatch"
	// ShipProblemUnverified: chưa kiểm được (hãng lỗi, bộ đệm bận).
	ShipProblemUnverified = "unverified"
)

// Lỗi của lựa chọn giao hàng ở báo giá web và đơn gửi thẳng (đổi sang HTTP ở internal/httpapi/errors_web.go).
var (
	ErrShipCarrierUnavailable = errors.New("order: hãng khách chọn không còn giao trên website")
	ErrShipFeeMismatch        = errors.New("order: phí giao khách chọn không khớp báo phí")
	ErrShipUnverified         = errors.New("order: chưa kiểm được phí giao khách chọn")
)

// WebShipChoice là lựa chọn giao hàng của khách: tài khoản hãng (nil: phí dự phòng của shop), khóa, tên hãng, dịch
// vụ, phí của lựa chọn (đồng, trước miễn phí ship của website) và vấn đề (rỗng: khớp). Không dữ liệu cá nhân.
type WebShipChoice struct {
	AccountID   *uuid.UUID `json:"accountId,omitempty"`
	CarrierKey  string     `json:"carrierKey,omitempty"`
	CarrierName string     `json:"carrierName,omitempty"`
	Service     string     `json:"service,omitempty"`
	Fee         int64      `json:"fee"`
	Problem     string     `json:"problem,omitempty"`
}

// Estimated báo lựa chọn là phí dự phòng của shop (không hãng).
func (c WebShipChoice) Estimated() bool { return c.AccountID == nil }

// shipProblem là vấn đề của lựa chọn c khi khách trả fee: vấn đề lớp HTTP đã ghi, hay fee_mismatch khi khách trả hơn
// phí hãng. c nil → rỗng.
func shipProblem(c *WebShipChoice, fee int64) string {
	switch {
	case c == nil:
		return ""
	case c.Problem != "":
		return c.Problem
	case fee > c.Fee:
		return ShipProblemFeeMismatch
	}
	return ""
}

// shipError đổi vấn đề p sang sentinel (rỗng → nil).
func shipError(p string) error {
	switch p {
	case "":
		return nil
	case ShipProblemCarrierUnavailable:
		return ErrShipCarrierUnavailable
	case ShipProblemUnverified:
		return ErrShipUnverified
	}
	return ErrShipFeeMismatch
}

// validShipChoice: c nil hoặc phí trong 0–MaxMoney, vấn đề là một trong ba giá trị đã biết.
func validShipChoice(c *WebShipChoice) bool {
	if c == nil {
		return true
	}
	switch c.Problem {
	case "", ShipProblemCarrierUnavailable, ShipProblemFeeMismatch, ShipProblemUnverified:
	default:
		return false
	}
	return c.Fee >= 0 && c.Fee <= pricing.MaxMoney
}

// shipChoiceJSON là JSON của lựa chọn c với vấn đề p để lưu ở ord.orders.web_shipping (c nil → nil).
func shipChoiceJSON(c *WebShipChoice, p string) ([]byte, error) {
	if c == nil {
		return nil, nil
	}
	rec := *c
	rec.Problem = p
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("ghi lựa chọn giao hàng của khách: %w", err)
	}
	return b, nil
}

// shipChoiceOf đọc cột web_shipping (NULL → nil).
func shipChoiceOf(raw []byte) (*WebShipChoice, error) {
	if raw == nil {
		return nil, nil
	}
	var c WebShipChoice
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("đọc lựa chọn giao hàng của khách: %w", err)
	}
	return &c, nil
}
