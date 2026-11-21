package arch

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeCustomers bắt câu ghi bảng hồ sơ khách (UPDATE, INSERT INTO, DELETE FROM customer.customers), kể cả xuống dòng.
var writeCustomers = regexp.MustCompile(`(?is)\b(update|insert\s+into|delete\s+from)\s+customer\.customers\b`)

// findCustomerWriters trả các tệp (.go không phải test, .sql trong queries/) có câu ghi customer.customers mà nằm ngoài
// module customer (internal/modules/customer/**, queries/customer/**). Migration không quét (chủ schema tạo bảng).
func findCustomerWriters(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(rel, d) || rel == "migrations" || rel == "console" || isTestdata(rel+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		if bad, err := customerWriteOutside(path, rel); err != nil || bad {
			if bad {
				out = append(out, rel)
			}
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("quét %s: %v", root, err)
	}
	return out
}

// TestCustomerWriter_OnlyCustomerModule (kế hoạch M3-21, B14, C3): số nhận/hoàn chỉ consumer customer_stats của M2
// ghi; không module nào khác (kể cả M3) có câu ghi customer.customers, và delivery_stats.go của M3 không ghi gì.
func TestCustomerWriter_OnlyCustomerModule(t *testing.T) {
	if got := findCustomerWriters(t, repoRoot(t)); len(got) != 0 {
		t.Errorf("câu ghi customer.customers ngoài module customer (hay trong delivery_stats.go): %v", got)
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), "internal/modules/customer/delivery_stats.go")); err != nil {
		t.Errorf("delivery_stats.go phải có (test chặn ghi dựa vào tên tệp): %v", err)
	}
	// Tự kiểm bộ bắt: câu nhiều dòng, chữ thường.
	if !writeCustomers.MatchString("update\n    customer.customers SET x = 1") ||
		writeCustomers.MatchString("SELECT * FROM customer.customers_v") {
		t.Error("regexp bắt câu ghi sai")
	}
}

// customerWriteOutside: tệp rel (.go không phải test, .sql trong queries/) có câu ghi customer.customers mà nằm ngoài
// module customer, hoặc là delivery_stats.go (M3-21: chỉ đọc số của M2).
func customerWriteOutside(path, rel string) (bool, error) {
	isGo := strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	isSQL := strings.HasPrefix(rel, "queries/") && strings.HasSuffix(rel, ".sql")
	if !isGo && !isSQL {
		return false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err //nolint:wrapcheck // test
	}
	if !writeCustomers.Match(b) {
		return false, nil
	}
	if rel == "internal/modules/customer/delivery_stats.go" {
		return true, nil
	}
	return !strings.HasPrefix(rel, "internal/modules/customer/") && !strings.HasPrefix(rel, "queries/customer/"), nil
}
