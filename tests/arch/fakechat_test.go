package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Kế hoạch M4 Task 145 (04 mục 4: thêm kênh không sửa lõi): kênh nhắn tin giả chỉ có tệp trong internal/adapters/fake,
// và ngoài test chỉ cmd/crm nhập nó (nối vào Registry ở wire_m4.go).
func TestFakeChat_OnlyCmdImports(t *testing.T) {
	t.Parallel()
	const fakePkg = modPrefix + "internal/adapters/fake"
	var importers []string
	for _, p := range goListPackages(t, repoRoot(t)) {
		if slices.Contains(p.Imports, fakePkg) {
			importers = append(importers, strings.TrimPrefix(p.ImportPath, modPrefix))
		}
	}
	if !slices.Equal(importers, []string{"cmd/crm"}) {
		t.Errorf("gói nhập kênh giả = %v, chỉ được cmd/crm", importers)
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "internal", "adapters", "fake"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("kênh giả có gói con %s: giữ mọi tệp trong internal/adapters/fake", e.Name())
		}
	}
}
