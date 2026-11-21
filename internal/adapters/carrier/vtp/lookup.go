package vtp

import (
	"context"
	"fmt"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// LookupByCode trả ErrNotSupported: tài liệu VTP không có API tra đơn theo mã của đối tác (ORDER_NUMBER) hay theo mã
// vận đơn (Capabilities.LookupByRef = false, docs/carriers/vtp.md). Vận đơn "không rõ" (quá giờ, 206 đơn đã tồn tại)
// luôn chờ người nhập mã hãng (Task 12); không đoán "không có".
func (*Adapter) LookupByCode(context.Context, fulfillment.Account, string) (fulfillment.LookupResult, error) {
	return fulfillment.LookupResult{}, fmt.Errorf("tra vận đơn Viettel Post theo mã: %w", fulfillment.ErrNotSupported)
}
