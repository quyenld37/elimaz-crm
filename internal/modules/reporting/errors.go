package reporting

import "errors"

// Lỗi nghiệp vụ của báo cáo (kế hoạch M6, Task 227). Lớp HTTP đổi sang mã của hợp đồng ở errors_reports.go (Task 228):
// ErrBadDate, ErrFromAfterTo → bad_date; ErrRangeTooLong → range_too_long; ErrBadParams → bad_params.
var (
	// ErrBadDate: ngày không đúng dạng YYYY-MM-DD hoặc không có trên lịch.
	ErrBadDate = errors.New("reporting: ngày sai")
	// ErrFromAfterTo: ngày đầu sau ngày cuối.
	ErrFromAfterTo = errors.New("reporting: ngày đầu sau ngày cuối")
	// ErrRangeTooLong: kỳ dài hơn số ngày tối đa.
	ErrRangeTooLong = errors.New("reporting: kỳ quá dài")
	// ErrBadParams: tham số báo cáo lạ (chiều nhóm, cột số, cơ sở ngày, khóa ô, bộ lọc).
	ErrBadParams = errors.New("reporting: tham số sai")
	// ErrNoOwnSeller: tenant chưa có người bán của chính shop (kind own, active).
	ErrNoOwnSeller = errors.New("reporting: không có người bán own")
)
