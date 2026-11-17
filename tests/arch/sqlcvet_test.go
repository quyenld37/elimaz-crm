package arch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// sqlcVet chạy `sqlc vet` (luật sqlc/db-prepare) trên một database thử đã migrate; trả lỗi và đầu ra.
func sqlcVet(t *testing.T, ownerURL string, args ...string) (string, error) {
	t.Helper()
	bin, err := exec.LookPath("sqlc")
	if err != nil {
		t.Fatalf("thiếu sqlc (cài theo tools/versions.mk): %v", err)
	}
	cmd := exec.CommandContext(t.Context(), bin, append([]string{"vet"}, args...)...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CRM_SQLC_DATABASE_URL="+ownerURL)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// `make sqlc-vet` chạy test này: câu thật trong queries/ phải qua; câu mẫu sai tên cột phải đỏ.
func TestSqlcVet(t *testing.T) {
	_, owner, _ := testkit.StartPostgres(t)
	if out, err := sqlcVet(t, owner); err != nil {
		t.Fatalf("sqlc vet trên queries/ phải xanh: %v\n%s", err, out)
	}
	out, err := sqlcVet(t, owner, "-f", filepath.Join("tests", "arch", "testdata", "sqlc", "sqlc.yaml"))
	if err == nil || !strings.Contains(out, "no_such_column") {
		t.Fatalf("sqlc vet với câu sai tên cột phải đỏ và nêu cột: %v\n%s", err, out)
	}
}
