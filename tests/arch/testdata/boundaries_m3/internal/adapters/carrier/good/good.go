// Package good: adapter hợp lệ — chỉ nhập phần công khai của fulfillment và thư viện chuẩn.
package good

import (
	_ "net/http"

	_ "elimaz.com.vn/crm/internal/modules/fulfillment"
)
