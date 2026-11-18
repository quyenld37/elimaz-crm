// Package inventory: luật (a) — inventory chỉ được gọi catalog, không được gọi order. Nhập catalog là hợp lệ.
package inventory

import (
	_ "elimaz.com.vn/crm/internal/modules/catalog"
	_ "elimaz.com.vn/crm/internal/modules/order"
)
