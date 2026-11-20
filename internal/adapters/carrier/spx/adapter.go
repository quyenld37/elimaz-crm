// Package spx là adapter hãng SPX Express (khóa "spx"). Hiện là khung: mọi khả năng khai false, mọi lời gọi hãng trả
// fulfillment.ErrNotSupported; các task hãng của M3 thay bằng hiện thực thật và bổ sung testdata contract.
//
// Khi nào không dùng: chưa gọi được hãng; không đăng ký tài khoản mode = api cho hãng này tới khi có hiện thực.
package spx

import (
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
)

// Key là carrier_key của SPX Express.
const Key = "spx"

// Adapter là adapter SPX Express.
type Adapter struct {
	fulfillment.Unsupported
}

// New dựng adapter từ cấu hình (khung: chưa đọc gì).
func New(config.Config) *Adapter { return &Adapter{} }

// Key trả "spx".
func (*Adapter) Key() string { return Key }
