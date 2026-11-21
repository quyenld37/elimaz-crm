package arch

import (
	"strings"
	"testing"
)

// testkitPkg: gói nền của test (PostgreSQL thử, database mẫu, cấu hình, tệp, hàng đợi). Gói con (shiptest, codtest,
// shoptest) là seed theo luồng nghiệp vụ, được nhập module.
const testkitPkg = modPrefix + "internal/testkit"

// testkitModuleDeps trả các gói internal/modules/** mà internal/testkit cần (cả bắc cầu) theo kết quả go list.
func testkitModuleDeps(pkgs []listedPackage) (deps []string, found bool) {
	for _, p := range pkgs {
		if p.ImportPath != testkitPkg {
			continue
		}
		for _, d := range p.Deps {
			if rel, ok := strings.CutPrefix(d, modPrefix); ok && hasPathPrefix(rel, "internal/modules") {
				deps = append(deps, d)
			}
		}
		return deps, true
	}
	return nil, false
}

// TestTestkitNoModules: internal/testkit không nhập internal/modules/**, kể cả bắc cầu (Task 424, M1-x10). Nếu nhập,
// mọi gói test dùng testkit biên dịch kèm module đó và đổi một tệp của module buộc cổng kiểm chạy lại gần cả bộ.
// Phần cần module đặt ở gói con (testkit/shoptest, realtime/realtimetest, <module>test).
func TestTestkitNoModules(t *testing.T) {
	deps, found := testkitModuleDeps(goListPackages(t, repoRoot(t)))
	if !found {
		t.Fatalf("go list không có gói %s", testkitPkg)
	}
	if len(deps) > 0 {
		t.Errorf("internal/testkit cần %d gói internal/modules/** (dời phần đó ra gói con):\n%s", len(deps), strings.Join(deps, "\n"))
	}
}

// TestTestkitModuleDeps_DetectsTransitive: luật bắt cả gói module nhập bắc cầu, bỏ qua gói con của testkit và gói khác.
func TestTestkitModuleDeps_DetectsTransitive(t *testing.T) {
	pkgs := []listedPackage{
		{ImportPath: testkitPkg + "/shoptest", Deps: []string{modPrefix + "internal/modules/catalog"}},
		{ImportPath: testkitPkg, Deps: []string{
			modPrefix + "internal/platform/realtime", modPrefix + "internal/modules/identity", "github.com/jackc/pgx/v5",
			modPrefix + "internal/modulesx",
		}},
	}
	deps, found := testkitModuleDeps(pkgs)
	if !found || len(deps) != 1 || deps[0] != modPrefix+"internal/modules/identity" {
		t.Errorf("mong đúng một gói module (identity), có %v, %v", deps, found)
	}
	if _, found := testkitModuleDeps(pkgs[:1]); found {
		t.Error("không có gói testkit mà vẫn báo thấy")
	}
}
