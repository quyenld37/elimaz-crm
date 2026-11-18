package xlsx

// Limits là giới hạn khi đọc một tệp người dùng tải lên. Trường ≤ 0 lấy giá trị của DefaultLimits.
type Limits struct {
	MaxBytes     int64 // kích thước tệp nén, mặc định 5 MiB
	MaxRows      int   // số dòng dữ liệu (không kể tiêu đề và dòng trống), mặc định 20.000
	MaxUnzipped  int64 // tổng kích thước giải nén theo header zip, mặc định 50 MiB
	MaxCellChars int   // số ký tự tối đa của một ô dữ liệu, mặc định 2.000
}

// DefaultLimits là giới hạn mặc định (kế hoạch M1, Task M1-38).
func DefaultLimits() Limits {
	return Limits{MaxBytes: 5 << 20, MaxRows: 20_000, MaxUnzipped: 50 << 20, MaxCellChars: 2_000}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxRows <= 0 {
		l.MaxRows = d.MaxRows
	}
	if l.MaxUnzipped <= 0 {
		l.MaxUnzipped = d.MaxUnzipped
	}
	if l.MaxCellChars <= 0 {
		l.MaxCellChars = d.MaxCellChars
	}
	return l
}
