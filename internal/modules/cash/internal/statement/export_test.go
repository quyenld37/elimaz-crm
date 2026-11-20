package statement

// SetAfterBatch đặt hàm chạy sau mỗi lô dòng đã commit của p (lỗi → Execute trả lỗi, như tiến trình chết giữa chừng).
func SetAfterBatch(p *Parser, fn func(written int) error) { p.afterBatch = fn }

// SetLimits thay giới hạn đọc tệp của p.
func SetLimits(p *Parser, lim Limits) { p.lim = lim }
