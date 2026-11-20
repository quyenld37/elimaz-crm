// Package best là adapter hãng BEST Express (khóa "best"). Hiện là khung: mọi khả năng khai false, mọi lời gọi hãng trả
// fulfillment.ErrNotSupported; các task hãng của M3 thay bằng hiện thực thật và bổ sung testdata contract.
//
// Khi nào không dùng: chưa gọi được hãng; không đăng ký tài khoản mode = api cho hãng này tới khi có hiện thực.
package best

import (
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
)

// Key là carrier_key của BEST Express.
const Key = "best"

// Adapter là adapter BEST Express.
type Adapter struct {
	fulfillment.Unsupported
}

// New dựng adapter từ cấu hình (khung: chưa đọc gì).
func New(config.Config) *Adapter { return &Adapter{} }

// Key trả "best".
func (*Adapter) Key() string { return Key }
