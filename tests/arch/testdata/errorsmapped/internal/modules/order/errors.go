// Package order mẫu: ErrFoo chưa ánh xạ (báo), ErrBar đã ánh xạ, errHidden không xuất ra (bỏ qua).
package order

import "errors"

var (
	ErrFoo    = errors.New("foo")
	ErrBar    = errors.New("bar")
	errHidden = errors.New("hidden")
)
