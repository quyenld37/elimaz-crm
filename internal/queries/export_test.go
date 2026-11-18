package queries

import "github.com/google/uuid"

// ProductsSQL cho explain_test đọc câu mà ListProducts chạy.
func ProductsSQL(f ProductFilter, inCategory []uuid.UUID) (string, []any, error) {
	return productsSQL(f, inCategory)
}
