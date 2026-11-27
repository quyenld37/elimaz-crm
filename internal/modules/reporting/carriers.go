package reporting

// Báo cáo hãng vận chuyển. Tệp giữ chỗ của Task 228: Task 231 (M6-08) thêm kết quả, hàm JSON, xuất tệp trong
// carriers*.go, không sửa registry.go.

// CarriersParams là tham số của báo cáo hãng: kỳ from, to.
type CarriersParams struct {
	PeriodParams
}

func carriersDef() Def { return newDef[CarriersParams](KeyCarriers, FamilySales, permSalesRead) }
