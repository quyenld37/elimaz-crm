// Package lint chứa các mẫu sai cài sẵn; tests/arch/lint_test.go đòi linter báo đủ từng mẫu.
package lint

import (
	"errors"
	"fmt"
	"os"
)

// ErrNotFound là lỗi mẫu.
var ErrNotFound = errors.New("not found")

// PrintIt in thẳng ra stdout thay vì dùng slog.
func PrintIt() {
	fmt.Println("xin chào")
}

// ReadEnv đọc biến môi trường ngoài internal/platform/config.
func ReadEnv() string {
	return os.Getenv("CRM_ENV")
}

// Silence tắt linter mà không nêu tên linter và lý do.
func Silence() int {
	x := 1 //nolint
	return x
}

// IsNotFound so lỗi bằng == thay vì errors.Is.
func IsNotFound(err error) bool {
	return err == ErrNotFound
}

// IsNotFoundByText so lỗi bằng chuỗi.
func IsNotFoundByText(err error) bool {
	return err.Error() == "not found"
}
