package reporting

import (
	"encoding/json"
	"fmt"
	"time"
)

// VN là giờ Việt Nam (UTC+7, không đổi giờ mùa hè). Báo cáo không đọc múi giờ của máy (kế hoạch M6, Global
// Constraints).
//
//nolint:gochecknoglobals // giá trị chỉ đọc, như hằng
var VN = time.FixedZone("ICT", 7*3600)

const dayLayout = "2006-01-02"

// Period là một kỳ báo cáo trọn ngày giờ Việt Nam: FromDay, ToDay dạng YYYY-MM-DD (tính cả hai đầu); Start là 00:00
// giờ VN của FromDay, End là 00:00 giờ VN của ngày sau ToDay (loại trừ), cả hai ở UTC để lọc cột timestamptz bằng
// khoảng (dùng được chỉ mục).
type Period struct {
	FromDay, ToDay string
	Start, End     time.Time
}

// Days là số ngày của kỳ (tính cả hai đầu).
func (p Period) Days() int {
	return int(p.End.Sub(p.Start) / (24 * time.Hour))
}

// MarshalJSON ghi kỳ theo ReportPeriod của hợp đồng: {"from": FromDay, "to": ToDay}.
func (p Period) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(struct {
		From string `json:"from"`
		To   string `json:"to"`
	}{p.FromDay, p.ToDay})
	if err != nil {
		return nil, fmt.Errorf("ghi kỳ: %w", err)
	}
	return b, nil
}

// IsZero cho biết kỳ chưa được đặt.
func (p Period) IsZero() bool { return p.Start.IsZero() && p.End.IsZero() }

// ParsePeriod đọc kỳ từ hai ngày giờ VN. Sai dạng hay ngày không có → ErrBadDate; from sau to → ErrFromAfterTo; quá
// maxDays ngày → ErrRangeTooLong.
func ParsePeriod(from, to string, maxDays int) (Period, error) {
	start, err := dayStart(from)
	if err != nil {
		return Period{}, err
	}
	last, err := dayStart(to)
	if err != nil {
		return Period{}, err
	}
	if last.Before(start) {
		return Period{}, fmt.Errorf("kỳ %s – %s: %w", from, to, ErrFromAfterTo)
	}
	p := Period{FromDay: from, ToDay: to, Start: start.UTC(), End: last.AddDate(0, 0, 1).UTC()}
	if p.Days() > maxDays {
		return Period{}, fmt.Errorf("kỳ %d ngày, tối đa %d: %w", p.Days(), maxDays, ErrRangeTooLong)
	}
	return p, nil
}

// Today là ngày giờ VN của now (YYYY-MM-DD).
func Today(now time.Time) string { return now.In(VN).Format(dayLayout) }

// DayCutoff là mốc 24:00 giờ VN của day (00:00 giờ VN của ngày sau day), ở UTC. Sai dạng → ErrBadDate.
func DayCutoff(day string) (time.Time, error) {
	start, err := dayStart(day)
	if err != nil {
		return time.Time{}, err
	}
	return start.AddDate(0, 0, 1).UTC(), nil
}

// dayStart là 00:00 giờ VN của day; chỉ nhận đúng dạng YYYY-MM-DD của một ngày có trên lịch.
func dayStart(day string) (time.Time, error) {
	t, err := time.ParseInLocation(dayLayout, day, VN)
	if err != nil || t.Format(dayLayout) != day {
		return time.Time{}, fmt.Errorf("ngày %q: %w", day, ErrBadDate)
	}
	return t, nil
}
