package reporting

// Báo cáo đơn theo chiều và danh sách chi tiết (BC02). Tệp giữ chỗ của Task 228: Task 230 (M6-07) thêm bộ lọc, chiều
// nhóm, kết quả, hàm JSON và xuất tệp trong orders*.go, không sửa registry.go.

// OrdersParams là tham số của báo cáo đơn (Task 230 thêm bộ lọc, chiều nhóm).
type OrdersParams struct {
	PeriodParams
}

// DrilldownParams là tham số của danh sách chi tiết (Task 230 thêm ô, cột số, bộ lọc).
type DrilldownParams struct {
	PeriodParams
}

func ordersDef() Def { return newDef[OrdersParams](KeyOrders, FamilySales, permSalesRead) }

func drilldownDef() Def {
	return newDef[DrilldownParams](KeyOrderDrilldown, FamilySales, permSalesRead)
}
