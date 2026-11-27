package reporting

// Sản phẩm bán chạy. Tệp giữ chỗ của Task 228: Task 232 (M6-09) thêm mức, cơ sở, giới hạn, kết quả, hàm JSON, xuất tệp
// trong bestsellers*.go, không sửa registry.go.

// BestSellersParams là tham số của báo cáo bán chạy (Task 232 thêm level, basis, limit).
type BestSellersParams struct {
	PeriodParams
}

func bestSellersDef() Def {
	return newDef[BestSellersParams](KeyBestSellers, FamilySales, permSalesRead)
}
