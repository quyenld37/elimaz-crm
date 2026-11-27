package reporting

import "errors"

// Lỗi nghiệp vụ của báo cáo (kế hoạch M6, Task 227). Lớp HTTP đổi sang mã của hợp đồng ở errors_reports.go (Task 228):
// ErrBadDate, ErrFromAfterTo → bad_date; ErrRangeTooLong → range_too_long; ErrBadParams → bad_params; ErrUnknownReport
// → unknown_report; ErrNotImplemented → 501 not_implemented; ErrRunInBackground (BackgroundError) → 200
// run_in_background.
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
	// ErrUnknownReport: khóa báo cáo lạ, hay thuộc họ khác (Task 228).
	ErrUnknownReport = errors.New("reporting: báo cáo lạ")
	// ErrNotImplemented: phần của báo cáo mà task của nó chưa làm (tệp giữ chỗ của Task 228), hay báo cáo không có bản
	// xem tại chỗ (products chỉ xuất tệp).
	ErrNotImplemented = errors.New("reporting: chưa làm")
	// ErrRunInBackground: báo cáo không chạy tại chỗ được, phải chạy nền; lỗi thật là *BackgroundError (Is nhận ra).
	ErrRunInBackground = errors.New("reporting: chạy nền")
	// ErrPageForbidden: bộ lọc Page có Page ngoài phạm vi report.sales.read của người xem (hợp đồng: 403 forbidden).
	ErrPageForbidden = errors.New("reporting: Page ngoài phạm vi")
	// ErrWarehouseForbidden: lọc một kho ngoài phạm vi report.inventory.read của người xem (hợp đồng: 403 forbidden).
	ErrWarehouseForbidden = errors.New("reporting: kho ngoài phạm vi")
	// ErrNoSnapshot: báo cáo tồn một ngày đã qua chưa có bản chụp (trước bản chụp đầu, hay ngày chưa chụp; 404
	// no_snapshot).
	ErrNoSnapshot = errors.New("reporting: ngày chưa có bản chụp tồn")
	// ErrUnknownDocType: sổ tồn có doc_type ngoài bảy loại đã biết trong ngăn on_hand của kỳ (xuất nhập tồn không lặng lẽ
	// bỏ dòng; lỗi hệ thống, 500).
	ErrUnknownDocType = errors.New("reporting: loại chứng từ lạ trong sổ tồn")
)
