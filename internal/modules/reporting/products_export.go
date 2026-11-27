package reporting

// Danh sách sản phẩm xuất Excel (Q25, Q-X6). Tệp giữ chỗ của Task 228: Task 237 (M6-14) thêm trường tham số, Rows,
// Columns, FileName trong products_export*.go. Báo cáo này chỉ xuất tệp: JSON luôn nil.

// ProductsExportParams là tham số của danh sách sản phẩm (Task 237 thêm trạng thái, tiền tố SKU).
type ProductsExportParams struct{}

// Validate của bản giữ chỗ không kiểm gì (Task 237).
func (*ProductsExportParams) Validate() error { return nil }

func productsDef() Def {
	return newDef[ProductsExportParams](KeyProducts, FamilyInventory, permInventoryRead)
}
