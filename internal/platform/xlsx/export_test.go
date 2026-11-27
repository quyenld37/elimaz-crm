package xlsx

// SetMaxDataRowsForTest hạ trần số dòng của w để test ErrTooManyRows mà không ghi một triệu dòng.
func SetMaxDataRowsForTest(w *Writer, n int) { w.maxRows = n }
