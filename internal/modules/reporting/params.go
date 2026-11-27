package reporting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxDays là số ngày tối đa của một kỳ báo cáo (tính cả hai đầu; hợp đồng: quá → range_too_long).
const MaxDays = 366

// Params là tham số của một báo cáo; mỗi báo cáo một kiểu cụ thể (khai trong tệp của báo cáo). Validate kiểm giá trị
// (ngày, khóa lọc) và trả sentinel của module (ErrBadDate, ErrFromAfterTo, ErrRangeTooLong, ErrBadParams).
type Params interface {
	Validate() error
}

// PeriodParams là kỳ from, to (ngày giờ Việt Nam, YYYY-MM-DD) dùng chung của các báo cáo theo kỳ; tên trường JSON
// cùng tên tham số query của GET.
type PeriodParams struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Validate kiểm kỳ bằng ParsePeriod (tối đa MaxDays ngày).
func (p PeriodParams) Validate() error {
	_, err := p.Period()
	return err
}

// Period đọc kỳ đã kiểm.
func (p PeriodParams) Period() (Period, error) {
	return ParsePeriod(p.From, p.To, MaxDays)
}

// ReportDays là số ngày của kỳ cho thuộc tính report.days của span (0 khi kỳ sai).
func (p PeriodParams) ReportDays() int {
	period, err := p.Period()
	if err != nil {
		return 0
	}
	return period.Days()
}

// DecodeParams đọc tham số JSON raw (cột params của report_runs, thân yêu cầu tạo lần chạy) của báo cáo k: đúng một
// đối tượng JSON, từ chối trường lạ và phần thừa (ErrBadParams), rồi Validate. Khóa lạ → ErrUnknownReport.
//
//nolint:ireturn // Interfaces của kế hoạch: mỗi báo cáo một kiểu tham số cụ thể sau giao diện Params
func DecodeParams(k Key, raw json.RawMessage) (Params, error) {
	d, ok := Defs()[k]
	if !ok {
		return nil, fmt.Errorf("báo cáo %s: %w", k, ErrUnknownReport)
	}
	p := d.NewParams()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return nil, fmt.Errorf("đọc tham số %s: %w (%w)", k, ErrBadParams, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("tham số %s có phần thừa: %w", k, ErrBadParams)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("tham số %s: %w", k, err)
	}
	return p, nil
}
