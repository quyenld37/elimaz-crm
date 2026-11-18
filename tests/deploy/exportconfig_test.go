package deploy_test

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// scripts/ops/export-config.sh (kế hoạch M0 bản 5, Task 23; ADR-30 mục 5): gom cấu hình máy chủ thành một tệp nén —
// tệp compose, cấu hình cloudflared, digest image đang chạy, TÊN biến môi trường (không giá trị) — giữ 8 bản, đẩy lên
// kho ngoài dưới tiền tố config/. Thấy giá trị bí mật (gitleaks) thì dừng, không tạo tệp nén. Lệnh docker và aws trong
// test là bản giả đặt trước PATH; gitleaks là bản thật.

const exportScript = "../../scripts/ops/export-config.sh"

// fakeBin tạo thư mục chứa docker và aws giả: docker in danh sách container, image; aws ghi lại lệnh vào calls.log.
func fakeBin(t *testing.T, s3List string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls.log")
	docker := `#!/usr/bin/env bash
case "$*" in
  *" ps -q"*) echo c1; echo c2 ;;
  "inspect"*) echo "/crm-api crm@sha256:aaaa sha256:1111"; echo "/crm-worker crm@sha256:aaaa sha256:1111" ;;
  *) echo "docker giả: lệnh lạ $*" >&2; exit 1 ;;
esac
`
	aws := `#!/usr/bin/env bash
echo "aws $*" >> "` + calls + `"
case "$*" in
  "s3 ls"*) printf '%s' "$FAKE_S3_LIST" ;;
esac
`
	for name, body := range map[string]string{"docker": docker, "aws": aws} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil { //nolint:gosec // tệp chạy giả của test
			t.Fatal(err)
		}
	}
	t.Setenv("FAKE_S3_LIST", s3List)
	return dir, calls
}

type exportEnv struct {
	compose, envFile, tunnel, out string
}

func writeFixture(t *testing.T, composeExtra string) exportEnv {
	t.Helper()
	dir := t.TempDir()
	e := exportEnv{
		compose: filepath.Join(dir, "compose.prod.yml"),
		envFile: filepath.Join(dir, "crm.env"),
		tunnel:  filepath.Join(dir, "cloudflared.yml"),
		out:     filepath.Join(dir, "out"),
	}
	files := map[string]string{
		e.compose: "services:\n  crm-api:\n    image: ${CRM_IMAGE}\n" + composeExtra,
		// Giá trị thật chỉ nằm trong tệp biến; bản xuất chỉ được mang tên.
		e.envFile: "# tệp biến của máy chủ\nCRM_IMAGE=crm@sha256:aaaa\nDATABASE_URL=postgres://crm_app:" + randHex(t) +
			"@postgres:5432/crm\n\nWALG_LIBSODIUM_KEY=" + randHex(t) + "\nexport HEALTH_TOKEN=" + randHex(t) + "\n",
		e.tunnel: "tunnel: crm\ningress:\n  - hostname: crm.example\n    service: http://crm-api:4190\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func randHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", b)
}

func runExport(t *testing.T, e exportEnv, bin string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", exportScript, e.out)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"CRM_COMPOSE_FILE="+e.compose,
		"CRM_ENV_FILE="+e.envFile,
		"CRM_TUNNEL_CONFIG="+e.tunnel,
		"COMPOSE_PROJECT=crm",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// readTarGz trả tên tệp → nội dung của tệp nén.
func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[filepath.Base(h.Name)] = string(b)
	}
}

func archives(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "crm-config-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(m)
	return m
}

func TestExportConfig_NamesOnlyAndDigests(t *testing.T) {
	bin, _ := fakeBin(t, "")
	e := writeFixture(t, "")
	if out, err := runExport(t, e, bin); err != nil {
		t.Fatalf("export-config.sh: %v\n%s", err, out)
	}
	a := archives(t, e.out)
	if len(a) != 1 {
		t.Fatalf("muốn 1 tệp nén, có %d", len(a))
	}
	got := readTarGz(t, a[0])
	for _, f := range []string{"compose.prod.yml", "cloudflared.yml", "env-names.txt", "images.txt"} {
		if _, ok := got[f]; !ok {
			t.Errorf("bản xuất thiếu %s (có %v)", f, keys(got))
		}
	}
	if names := strings.Fields(got["env-names.txt"]); !slices.Equal(names,
		[]string{"CRM_IMAGE", "DATABASE_URL", "HEALTH_TOKEN", "WALG_LIBSODIUM_KEY"}) {
		t.Errorf("env-names.txt = %q, muốn đúng tên biến đã sắp xếp", names)
	}
	envBody, _ := os.ReadFile(e.envFile)
	for line := range strings.SplitSeq(string(envBody), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || len(v) < 8 || k == "CRM_IMAGE" { // digest image được phép có trong images.txt
			continue
		}
		for f, body := range got {
			if strings.Contains(body, v) {
				t.Errorf("%s chứa giá trị của biến (%s…): bản xuất chỉ được có tên", f, v[:4])
			}
		}
	}
	if !strings.Contains(got["images.txt"], "crm@sha256:aaaa") {
		t.Errorf("images.txt thiếu digest image đang chạy: %q", got["images.txt"])
	}
}

func TestExportConfig_StopsOnSecret(t *testing.T) {
	bin, calls := fakeBin(t, "")
	// Chuỗi dạng khóa GitHub dựng lúc chạy, không nằm trong mã (gitleaks của repo không thấy).
	token := "ghp_" + randAlnum(t, 36)
	e := writeFixture(t, "    environment:\n      GH_TOKEN: "+token+"\n")
	out, err := runExport(t, e, bin, "CRM_CONFIG_S3_URI=s3://backup/crm")
	if err == nil {
		t.Fatalf("có giá trị bí mật trong compose mà export-config.sh vẫn thoát 0:\n%s", out)
	}
	if strings.Contains(out, token) {
		t.Error("thông báo lỗi in ra chính giá trị bí mật")
	}
	if a := archives(t, e.out); len(a) != 0 {
		t.Errorf("thấy bí mật mà vẫn để lại tệp nén: %v", a)
	}
	if b, _ := os.ReadFile(calls); len(b) != 0 {
		t.Errorf("thấy bí mật mà vẫn gọi kho ngoài: %s", b)
	}
}

// Giữ 8 bản ở thư mục ra và ở kho ngoài (tiền tố config/); bản cũ nhất bị xóa trước.
func TestExportConfig_KeepsEightAndUploads(t *testing.T) {
	var listing strings.Builder
	for i := 1; i <= 9; i++ {
		fmt.Fprintf(&listing, "2026-11-%02d 02:00:00 100 crm-config-202611%02dT020000Z.tar.gz\n", i, i)
	}
	bin, calls := fakeBin(t, listing.String())
	e := writeFixture(t, "")
	if err := os.MkdirAll(e.out, 0o750); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 9; i++ {
		p := filepath.Join(e.out, fmt.Sprintf("crm-config-202611%02dT020000Z.tar.gz", i))
		if err := os.WriteFile(p, []byte("cũ"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := runExport(t, e, bin, "CRM_CONFIG_S3_URI=s3://backup/crm", "CRM_S3_ENDPOINT=https://s3.example"); err != nil {
		t.Fatalf("export-config.sh: %v\n%s", err, out)
	}
	a := archives(t, e.out)
	if len(a) != 8 {
		t.Fatalf("thư mục ra còn %d bản, muốn 8: %v", len(a), a)
	}
	if strings.Contains(strings.Join(a, " "), "20261102T") {
		t.Errorf("bản cũ nhất chưa bị xóa: %v", a)
	}
	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("không gọi kho ngoài: %v", err)
	}
	log := string(b)
	newest := filepath.Base(a[len(a)-1])
	for _, want := range []string{
		"s3 cp " + a[len(a)-1] + " s3://backup/crm/config/" + newest,
		"--endpoint-url https://s3.example",
		"s3 rm s3://backup/crm/config/crm-config-20261101T020000Z.tar.gz",
		"s3 rm s3://backup/crm/config/crm-config-20261102T020000Z.tar.gz",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("thiếu lệnh %q trong:\n%s", want, log)
		}
	}
	if strings.Contains(log, "rm s3://backup/crm/config/crm-config-20261103") {
		t.Errorf("xóa nhầm bản thứ ba:\n%s", log)
	}
}

// randAlnum sinh chuỗi chữ và số ngẫu nhiên (độ hỗn loạn cao như khóa thật, để gitleaks nhận ra).
func randAlnum(t *testing.T, n int) string {
	t.Helper()
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
