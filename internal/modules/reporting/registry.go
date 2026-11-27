package reporting

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/xlsx"
)

// Family là họ báo cáo: quyết định quyền đọc và nhóm lần chạy nền (cột family của report_runs).
type Family string

// Ba họ báo cáo.
const (
	FamilySales     Family = "sales"
	FamilyInventory Family = "inventory"
	FamilyCustomers Family = "customers"
)

// Key là khóa báo cáo, đúng chín giá trị của CHECK report_runs.report (enum ReportKey của hợp đồng).
type Key string

// Chín khóa báo cáo.
const (
	KeyDashboard      Key = "dashboard"
	KeyOrders         Key = "orders"
	KeyOrderDrilldown Key = "order_drilldown"
	KeyCarriers       Key = "carriers"
	KeyBestSellers    Key = "best_sellers"
	KeyStock          Key = "stock"
	KeyStockMovements Key = "stock_movements"
	KeyProducts       Key = "products"
	KeyCustomers      Key = "customers"
)

// Quyền đọc của họ khách (hai họ còn lại ở scope.go).
const permCustomerExport identity.Permission = "customer.export"

// Def là định nghĩa một báo cáo: khóa, họ, quyền đọc, kiểu tham số và các cách ra kết quả. JSON là bản xem tại chỗ (và
// lần chạy nền dạng json), nil khi báo cáo chỉ xuất tệp (products) hoặc task của nó chưa làm; Rows, Columns, FileName
// là bản xuất XLSX (Task 237), nil khi chưa có. Mọi hàm chạy trong transaction chỉ đọc của TxReport (có thể chạy lại:
// trả kết quả bằng giá trị trả về, không cộng dồn vào biến ngoài).
type Def struct {
	Key    Key
	Family Family
	Read   identity.Permission
	// NewParams trả con trỏ tới giá trị rỗng của kiểu tham số của báo cáo (DecodeParams đọc JSON vào đó).
	NewParams func() Params
	JSON      func(ctx context.Context, tx pgx.Tx, s DataScope, p Params) (any, error)
	Rows      func(ctx context.Context, tx pgx.Tx, s DataScope, p Params, emit func([]xlsx.Cell) error) error
	Columns   func(p Params) []xlsx.Column
	FileName  func(p Params) string
}

// Defs là sổ đăng ký đủ chín báo cáo. Mỗi báo cáo khai Def của mình trong tệp của nó (dashboard.go, orders.go, …), nên
// task của một báo cáo không sửa tệp này.
func Defs() map[Key]Def {
	defs := map[Key]Def{}
	for _, d := range []Def{
		dashboardDef(), ordersDef(), drilldownDef(), carriersDef(), bestSellersDef(),
		stockDef(), movementsDef(), productsDef(), customersDef(),
	} {
		defs[d.Key] = d
	}
	return defs
}

// newDef dựng Def của báo cáo k (họ f, quyền đọc read) có kiểu tham số P (*P hiện thực Params); tệp của báo cáo gán
// thêm JSON, Rows, Columns, FileName vào kết quả.
func newDef[P any, PP interface {
	*P
	Params
}](k Key, f Family, read identity.Permission) Def {
	return Def{Key: k, Family: f, Read: read, NewParams: func() Params { return PP(new(P)) }}
}

// Lookup tìm báo cáo k trong họ f; khóa lạ hay thuộc họ khác → ErrUnknownReport.
func Lookup(f Family, k Key) (Def, error) {
	d, ok := Defs()[k]
	if !ok || d.Family != f {
		return Def{}, fmt.Errorf("báo cáo %s/%s: %w", f, k, ErrUnknownReport)
	}
	return d, nil
}
