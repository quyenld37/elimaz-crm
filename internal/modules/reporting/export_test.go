package reporting

// Cửa cho test gói reporting_test (EXPLAIN của câu dựng lúc chạy, Task 234): dựng câu một trang của báo cáo tồn và xuất
// nhập tồn như lúc chạy thật.

// StockQuery dựng câu của báo cáo tồn (day rỗng: hôm nay) với phạm vi s, phần lọc pg.
func StockQuery(s DataScope, pg StockPage, day string) (string, []any, error) {
	a, err := pg.resolve()
	if err != nil {
		return "", nil, err
	}
	b := &sqlBuilder{}
	q, err := stockSQL(s, a, day, b)
	return q, b.args, err
}

// MovementsQuery dựng câu xuất nhập tồn kỳ p với phạm vi s.
func MovementsQuery(s DataScope, p MovementsParams) (string, []any, error) {
	period, a, err := p.resolve()
	if err != nil {
		return "", nil, err
	}
	b := &sqlBuilder{}
	q, err := movementsSQL(s, a, period, b)
	return q, b.args, err
}
