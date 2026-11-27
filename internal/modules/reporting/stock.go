package reporting

// Báo cáo tồn và xuất nhập tồn (BC03). Tệp giữ chỗ của Task 228: Task 234 (M6-11) thêm trường tham số, kiểm, kết quả,
// hàm JSON, xuất tệp trong stock*.go, movements_dynsql.go, không sửa registry.go.

// StockParams là tham số của báo cáo tồn (Task 234 thêm kho, mã SKU, con trỏ trang, giới hạn và kiểm ngày).
type StockParams struct {
	// Date là ngày giờ Việt Nam; rỗng là hôm nay.
	Date string `json:"date,omitempty"`
}

// Validate của bản giữ chỗ không kiểm gì (Task 234).
func (*StockParams) Validate() error { return nil }

// MovementsParams là tham số của xuất nhập tồn: kỳ from, to (Task 234 thêm kho, mã SKU, trang).
type MovementsParams struct {
	PeriodParams
}

func stockDef() Def { return newDef[StockParams](KeyStock, FamilyInventory, permInventoryRead) }

func movementsDef() Def {
	return newDef[MovementsParams](KeyStockMovements, FamilyInventory, permInventoryRead)
}
