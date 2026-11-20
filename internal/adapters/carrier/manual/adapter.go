// Package manual là adapter của tài khoản giao tay (shop tự giao hay nhập mã hãng bằng tay): không gọi hãng nào, mọi
// khả năng false, mọi lời gọi trả fulfillment.ErrNotSupported. Tài khoản mode = manual của mọi hãng không gọi adapter
// của hãng đó (Task 15 của M3).
//
// Khi nào không dùng: không dùng cho hãng có API.
package manual

import "elimaz.com.vn/crm/internal/modules/fulfillment"

// Adapter là adapter giao tay với khóa hãng cho trước.
type Adapter struct {
	fulfillment.Unsupported

	key string
}

// New dựng adapter giao tay có khóa key (cmd/crm dùng "self").
func New(key string) *Adapter { return &Adapter{key: key} }

// Key trả khóa đã cho.
func (a *Adapter) Key() string { return a.key }
