package arch

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// external-probe chạy ngoài VPS và chỉ dùng thư viện chuẩn (README của nó); trong module chỉ được nhập các gói nền tảng
// cũng chỉ dùng thư viện chuẩn trong danh sách dưới.
func TestExternalProbeStdlibOnly(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{
		modPrefix + "scripts/external-probe":   true,
		modPrefix + "internal/platform/urlerr": true,
	}
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}",
		"./scripts/external-probe")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for p := range strings.FieldsSeq(string(out)) {
		if !allowed[p] {
			t.Errorf("external-probe nhập %s (ngoài thư viện chuẩn)", p)
		}
	}
}
