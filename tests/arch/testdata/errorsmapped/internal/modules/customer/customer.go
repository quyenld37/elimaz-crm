// Package customer mẫu: sentinel ngoài errors.go vẫn được quét; đã ánh xạ.
package customer

import "errors"

var ErrNotFound = errors.New("nf")
