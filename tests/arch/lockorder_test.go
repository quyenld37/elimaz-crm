package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// createdTables trả tên đầy đủ (schema.bảng) của mọi câu CREATE TABLE trong files, theo thứ tự gặp.
func createdTables(t *testing.T, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range createTableRe.FindAllStringSubmatch(string(b), -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// missingLockTables trả các bảng tạo trong files mà adr (nội dung docs/adr/0046-thu-tu-khoa.md) không nêu tên trong
// dấu `…`, theo thứ tự chữ.
func missingLockTables(t *testing.T, adr string, files []string) []string {
	t.Helper()
	var out []string
	for _, tbl := range createdTables(t, files) {
		if !strings.Contains(adr, "`"+tbl+"`") {
			out = append(out, tbl)
		}
	}
	slices.Sort(out)
	return out
}

func readADR46(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "adr", "0046-thu-tu-khoa.md"))
	if err != nil {
		t.Fatalf("đọc ADR-46: %v", err)
	}
	return string(b)
}

// Mọi bảng M4 tạo (CREATE TABLE trong migrations/04*.sql, dải của M4) có tên trong ADR-46 (kế hoạch M4, Task 143;
// luật "bảng mới được xếp vào bảng thứ tự khóa"). Chưa có migration 04* thì không có gì để kiểm.
func TestLockOrder_M4TablesListed(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "migrations", "04*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if got := missingLockTables(t, readADR46(t), files); len(got) > 0 {
		t.Fatalf("bảng của M4 chưa có trong docs/adr/0046-thu-tu-khoa.md: %v", got)
	}
}

// Mẫu: migration 0499 tạo conversation.ghost (không có trong ADR-46) và conversation.messages (có) → báo đúng ghost.
func TestLockOrder_M4Sample(t *testing.T) {
	t.Parallel()
	got := missingLockTables(t, readADR46(t), []string{filepath.Join("testdata", "lockorder_m4", "0499_ghost.sql")})
	if !slices.Equal(got, []string{"conversation.ghost"}) {
		t.Fatalf("mong [conversation.ghost], có %v", got)
	}
}
