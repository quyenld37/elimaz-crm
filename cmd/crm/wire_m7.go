package main

import (
	"time"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// m7Parts là phần chạy nền của phần khởi động M7 cần kho tệp mà module chưa có chỗ nối: nhập số đếm (Task 263),
// nhập đơn dự phòng (Task 268). Lịch, lệnh của conversation (Task 266) và reporting (Task 270, 271) đi qua chỗ nối có
// sẵn của module (wireM4, wireM6), không qua đây (QĐ-260-4).
type m7Parts struct {
	Jobs []queue.Handler
}

// wireM7 gọi một lần hàm nối dây của inventory, order (khung Task 260); task sau chỉ sửa tệp của mình trong module.
// fs nil (vai trò không có kho tệp): việc cần tệp chờ trong hàng đợi như lệnh của M3.
func wireM7(d *db.DB, fs files.FileStore) m7Parts {
	return m7Parts{Jobs: background.Collect(
		inventory.CountImportJobs(inventory.CountImportDeps{Now: time.Now}),
		order.FallbackImportJobs(order.FallbackImportDeps{DB: d, Files: fs, Queue: queue.NewQueue(), Now: time.Now}),
	)}
}
