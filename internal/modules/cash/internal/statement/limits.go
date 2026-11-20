// Package statement là phần nhập bảng kê COD của module cash (kế hoạch M3, Task M3-31; TI04, phần đọc tệp của TI02):
// ghi bảng kê từ tệp đã tải lên (chống nhập trùng theo băm nội dung), lệnh đọc tệp ngoài transaction trong giới hạn
// Limits, ghi dòng theo lô chạy lại an toàn, đọc bảng kê và dòng. Tệp là dữ liệu không tin được (04 mục 13): chỉ lưu
// cột của cod_statement_lines; lỗi chỉ có mã, cột, số dòng, không bao giờ có giá trị ô.
package statement

import "elimaz.com.vn/crm/internal/platform/xlsx"

// maxUnzipped là tổng dung lượng giải nén tối đa của tệp .xlsx (50 MiB, kế hoạch M3 Task M3-31).
const maxUnzipped = 50 << 20

// Limits là giới hạn đọc một tệp bảng kê.
type Limits struct {
	MaxBytes   int64 // kích thước tệp, 10 MiB
	MaxRows    int   // số dòng dữ liệu (không kể dòng trước tiêu đề, tiêu đề, dòng trống), 50.000
	MaxCols    int   // số cột của một dòng (tới ô cuối có giá trị), 60
	MaxCellLen int   // số ký tự của một ô được đọc (tiêu đề, cột của định dạng), 1.000
}

// DefaultLimits là giới hạn của kế hoạch (Task M3-31).
func DefaultLimits() Limits {
	return Limits{MaxBytes: 10 << 20, MaxRows: 50_000, MaxCols: 60, MaxCellLen: 1_000}
}

// xlsxLimits là giới hạn cho xlsx.ReadRows: số dòng không trống tối đa gồm cả các dòng tới dòng tiêu đề.
func (l Limits) xlsxLimits(headerRow int) xlsx.Limits {
	return xlsx.Limits{
		MaxBytes: l.MaxBytes, MaxRows: l.MaxRows + headerRow, MaxUnzipped: maxUnzipped, MaxCellChars: l.MaxCellLen,
	}
}
