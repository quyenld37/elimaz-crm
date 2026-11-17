// Package sample là gói mẫu cho test của tools/catalog. Câu thứ hai không vào danh mục.
package sample

import "context"

// Greeter chào hỏi.
type Greeter struct {
	// Name là tên.
	Name   string
	secret string
}

// NewGreeter tạo Greeter.
func NewGreeter(name string) *Greeter { return &Greeter{Name: name, secret: "x"} }

// Greet trả lời chào.
func (g *Greeter) Greet(ctx context.Context, to string) (string, error) {
	_ = ctx
	return g.Name + " chào " + to + g.secret, nil
}

// Add cộng hai số.
func Add(a, b int) int { return a + b }

func hidden() {}
