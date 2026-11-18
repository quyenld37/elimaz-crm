// Package catalog: luật (a) — nhập phần riêng của module order. Nhập identity là hợp lệ (cột "Được gọi").
package catalog

import (
	_ "elimaz.com.vn/crm/internal/modules/identity"
	_ "elimaz.com.vn/crm/internal/modules/order/internal/store"
)
