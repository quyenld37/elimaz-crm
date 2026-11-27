package reporting

// Tổng quan trong ngày (BC01). Tệp giữ chỗ của Task 228: Task 229 (M6-06) thêm kết quả, hàm JSON và trường tham số
// trong tệp này (dashboard*.go), không sửa registry.go.

// DashboardParams là tham số của tổng quan: kỳ from, to.
type DashboardParams struct {
	PeriodParams
}

func dashboardDef() Def { return newDef[DashboardParams](KeyDashboard, FamilySales, permSalesRead) }
