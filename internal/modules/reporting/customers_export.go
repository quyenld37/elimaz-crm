package reporting

// Xuất danh sách khách (PQ06, vùng cao). Tệp giữ chỗ của Task 228: Task 238 (M6-15) thêm trường tham số, kiểm, Rows,
// Columns, FileName trong customers_export*.go.

// CustomerExportParams là tham số của danh sách khách (Task 238 thêm ngày tạo, ngày mua cuối, vòng đời, nhãn, số đơn).
type CustomerExportParams struct{}

// Validate của bản giữ chỗ không kiểm gì (Task 238).
func (*CustomerExportParams) Validate() error { return nil }

func customersDef() Def {
	return newDef[CustomerExportParams](KeyCustomers, FamilyCustomers, permCustomerExport)
}
