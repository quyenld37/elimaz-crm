package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// createTableRe bắt tên đầy đủ của câu CREATE TABLE trong migration.
var createTableRe = regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+\.[a-z_]+)`)

// Mọi bảng M3 tạo (CREATE TABLE trong migrations/03*.sql) có tên `schema.bảng` trong ADR-46: trong một nhóm của bảng thứ
// tự khóa hoặc ở mục bảng ngoài thứ tự nhóm (kế hoạch M3, Task M3-04; luật "bảng mới được xếp vào bảng này").
func TestLockOrder_M3TablesListed(t *testing.T) {
	root := repoRoot(t)
	adr, err := os.ReadFile(filepath.Join(root, "docs", "adr", "0046-thu-tu-khoa.md"))
	if err != nil {
		t.Fatalf("đọc ADR-46: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "migrations", "03*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("không thấy migration 03*: %v", err)
	}
	tables := createdTables(t, files)
	if len(tables) < 14 {
		t.Fatalf("mong ít nhất 14 bảng M3 (0300, 0303, 0304), có %v", tables)
	}
	for _, tbl := range tables {
		if !strings.Contains(string(adr), "`"+tbl+"`") {
			t.Errorf("bảng %s của M3 chưa có trong docs/adr/0046-thu-tu-khoa.md", tbl)
		}
	}
}
