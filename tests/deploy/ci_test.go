package deploy_test

import (
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-faster/yaml"
)

// CI trên máy chủ git (kế hoạch M0 bản 5, Task 23): chạy đúng `make ci` như máy dev, với PostgreSQL 18 và SeaweedFS
// của compose dev; dựng image chính một lần, gắn nhãn theo mã commit; chạy test trình duyệt của màn hình (Task 48).
// Tệp ci.yml chưa chạy trên GitHub (cần chủ shop OK tạo repo), nên test này là chỗ duy nhất giữ các luật của nó.

const ciWorkflow = "../../.github/workflows/ci.yml"

type workflow struct {
	On          map[string]any `yaml:"on"`
	Concurrency struct {
		Group string `yaml:"group"`
	} `yaml:"concurrency"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Jobs        map[string]job    `yaml:"jobs"`
}

type job struct {
	If          string            `yaml:"if"`
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Steps       []step            `yaml:"steps"`
}

type step struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

func readWorkflow(t *testing.T) workflow {
	t.Helper()
	b, err := os.ReadFile(ciWorkflow)
	if err != nil {
		t.Fatalf("đọc %s: %v", ciWorkflow, err)
	}
	var w workflow
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatalf("ci.yml không phải YAML hợp lệ: %v", err)
	}
	return w
}

func needJob(t *testing.T, w workflow, name string) job {
	t.Helper()
	j, ok := w.Jobs[name]
	if !ok {
		t.Fatalf("ci.yml thiếu job %q", name)
	}
	return j
}

// runs trả các bước có dòng lệnh khớp re (theo từng dòng của run).
func runs(j job, re *regexp.Regexp) []step {
	var out []step
	for _, s := range j.Steps {
		for line := range strings.SplitSeq(s.Run, "\n") {
			if re.MatchString(strings.TrimSpace(line)) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// indexOf trả vị trí bước đầu tiên khớp re, -1 nếu không có.
func indexOf(j job, re *regexp.Regexp) int {
	for i, s := range j.Steps {
		for line := range strings.SplitSeq(s.Run, "\n") {
			if re.MatchString(strings.TrimSpace(line)) {
				return i
			}
		}
	}
	return -1
}

func checkedOutFullHistory(t *testing.T, name string, j job) {
	t.Helper()
	for _, s := range j.Steps {
		if strings.HasPrefix(s.Uses, "actions/checkout@") {
			if d, _ := s.With["fetch-depth"].(int); d != 0 || s.With["fetch-depth"] == nil {
				t.Errorf("job %s: checkout phải có fetch-depth: 0 (gitleaks quét cả lịch sử, oasdiff và test-guard so với main)", name)
			}
			return
		}
	}
	t.Errorf("job %s không checkout mã", name)
}

func TestCI_TriggersAndLeastPermission(t *testing.T) {
	w := readWorkflow(t)
	for _, ev := range []string{"push", "pull_request"} {
		if _, ok := w.On[ev]; !ok {
			t.Errorf("ci.yml không chạy khi %s", ev)
		}
	}
	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Errorf("quyền mặc định của workflow phải chỉ là contents: read, đang là %v", w.Permissions)
	}
	for name, j := range w.Jobs {
		for k, v := range j.Permissions {
			if v == "write" {
				t.Errorf("job %s xin quyền ghi %s: CI chưa được push image hay ghi gì lên GitHub (cần chủ shop OK)", name, k)
			}
		}
	}
}

// Lượt theo lịch (mutation, tới 6 giờ) chạy trên main: nếu chung nhóm concurrency với lượt push lên main thì push phải
// chờ hay bị hủy (review Task 23). Nhóm phải tách theo loại sự kiện.
func TestCI_ConcurrencySeparatesScheduleFromPush(t *testing.T) {
	w := readWorkflow(t)
	if !strings.Contains(w.Concurrency.Group, "github.event_name") || !strings.Contains(w.Concurrency.Group, "github.ref") {
		t.Errorf("concurrency.group phải chứa github.event_name và github.ref, đang là %q", w.Concurrency.Group)
	}
}

// CI chưa được cấp secret nào (chưa có repo, registry): không bước nào đọc secrets.
func TestCI_NoSecrets(t *testing.T) {
	b, err := os.ReadFile(ciWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secrets.") {
		t.Error("ci.yml dùng secrets: cần chủ shop OK (repo, registry) trước")
	}
}

// Mọi action bên thứ ba ghim theo mã commit 40 ký tự: thẻ phiên bản (v7) có thể bị dời sang mã khác.
func TestCI_ActionsPinnedBySHA(t *testing.T) {
	w := readWorkflow(t)
	pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+@[0-9a-f]{40}$`)
	n := 0
	for name, j := range w.Jobs {
		for _, s := range j.Steps {
			if s.Uses == "" {
				continue
			}
			n++
			if !pinned.MatchString(s.Uses) {
				t.Errorf("job %s: %q chưa ghim mã commit", name, s.Uses)
			}
		}
	}
	if n == 0 {
		t.Error("ci.yml không dùng action nào (thiếu checkout?)")
	}
}

// Job ci chạy đúng `make ci` (cổng kiểm của máy dev) sau khi cài công cụ đã ghim và dựng PostgreSQL 18 + SeaweedFS
// của compose dev (test `files` cần kho S3, Task 41). test-guard đọc đúng tệp task của nhánh.
func TestCI_GateJobRunsMakeCI(t *testing.T) {
	w := readWorkflow(t)
	j := needJob(t, w, "ci")
	checkedOutFullHistory(t, "ci", j)
	tools := indexOf(j, regexp.MustCompile(`^make ci-tools\b`))
	dbUp := indexOf(j, regexp.MustCompile(`^make db-up\b`))
	gate := indexOf(j, regexp.MustCompile(`^make ci$`))
	if tools < 0 || dbUp < 0 || gate < 0 {
		t.Fatalf("job ci phải có các bước make ci-tools (%d), make db-up (%d), make ci (%d)", tools, dbUp, gate)
	}
	if tools > gate || dbUp > gate {
		t.Error("make ci-tools và make db-up phải chạy trước make ci")
	}
	if indexOf(j, regexp.MustCompile(`git (fetch|branch).*\bmain\b`)) < 0 {
		t.Error("job ci phải có nhánh main cục bộ (oasdiff -base-ref=main, test-guard.sh main)")
	}
	if indexOf(j, regexp.MustCompile(`TASK_FILE=`)) < 0 {
		t.Error("job ci phải đặt TASK_FILE cho test-guard (checkout mới thì mọi tệp cùng giờ sửa, ls -t chọn bừa)")
	}
	if indexOf(j, regexp.MustCompile(`make db-reset\b`)) < 0 {
		t.Error("job ci phải dọn compose sau khi chạy (make db-reset)")
	}
}

// Image chính dựng đúng một lần trong cả workflow, gắn nhãn theo mã commit, và qua image-check (Task 21 Step 1).
func TestCI_ImageBuiltOnceTaggedByCommit(t *testing.T) {
	w := readWorkflow(t)
	build := regexp.MustCompile(`(^|\s)(docker build|make image\b|make image-check\b)`)
	var builds []string
	for name, j := range w.Jobs {
		for _, s := range runs(j, build) {
			builds = append(builds, name+"/"+s.Name)
		}
	}
	if len(builds) != 1 {
		t.Fatalf("image chính phải dựng đúng một lần trong workflow, đang có %d bước: %v", len(builds), builds)
	}
	j := needJob(t, w, "image")
	steps := runs(j, regexp.MustCompile(`^make image-check\b`))
	if len(steps) != 1 {
		t.Fatal("job image phải chạy make image-check (dựng và kiểm image)")
	}
	tag := steps[0].Env["CRM_IMAGE"]
	if tag == "" {
		tag = j.Env["CRM_IMAGE"]
	}
	if !strings.Contains(tag, "${{ github.sha }}") {
		t.Errorf("image phải gắn nhãn theo mã commit (CRM_IMAGE chứa ${{ github.sha }}), đang là %q", tag)
	}
	if !needs(j, "ci") {
		t.Error("job image phải chờ job ci xanh (needs: ci)")
	}
}

// Test trình duyệt của màn hình (make console-e2e, Task 48) chạy trong CI.
func TestCI_ConsoleE2E(t *testing.T) {
	w := readWorkflow(t)
	j := needJob(t, w, "console-e2e")
	if len(runs(j, regexp.MustCompile(`^make console-e2e\b`))) != 1 {
		t.Error("job console-e2e phải chạy make console-e2e")
	}
}

// Mutation testing vùng cao chạy theo lịch (quá lâu cho mỗi lần push), không chạy ở push hay pull request.
func TestCI_MutationScheduledOnly(t *testing.T) {
	w := readWorkflow(t)
	if _, ok := w.On["schedule"]; !ok {
		t.Fatal("ci.yml thiếu lịch (schedule) cho mutation testing")
	}
	j := needJob(t, w, "mutation")
	if !strings.Contains(j.If, "github.event_name == 'schedule'") {
		t.Errorf("job mutation chỉ chạy theo lịch, if đang là %q", j.If)
	}
	if len(runs(j, regexp.MustCompile(`^make mutation\b`))) != 1 {
		t.Error("job mutation phải chạy make mutation")
	}
	for name, other := range w.Jobs {
		if name != "mutation" && !strings.Contains(other.If, "github.event_name != 'schedule'") {
			t.Errorf("job %s không được chạy lại theo lịch của mutation (if: github.event_name != 'schedule')", name)
		}
	}
}

func needs(j job, name string) bool {
	switch v := j.Needs.(type) {
	case string:
		return v == name
	case []any:
		return slices.Contains(v, any(name))
	}
	return false
}

// makeDry in các lệnh `make -n <đích>` sẽ chạy (không chạy thật).
func makeDry(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "make", append([]string{"-n", "--no-print-directory"}, args...)...)
	cmd.Dir = "../.."
	// CRM_IMAGE của phiên (hay của job CI) không được đè mặc định mà test kiểm.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "CRM_IMAGE=") })
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// make ci gồm đủ các cổng; test kiến trúc (tests/arch: boundaries, sqlusage, dynsql…) nằm trong `go test ./...`.
func TestMakeCI_RunsEveryGate(t *testing.T) {
	out := makeDry(t, "ci")
	for _, want := range []string{
		"go build", "go vet ./...", "golangci-lint run ./...", "sqlc generate", "git diff --exit-code",
		"TestSqlcVet", "go test -race -count=1 ./...", "govulncheck ./...", "tools/apicheck", "tools/catalog",
		"jscpd", "test-guard.sh main", "gitleaks git", "gitleaks dir", "npx tsc --noEmit", "npx vitest run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("make ci thiếu bước %q", want)
		}
	}
}

// Danh sách thư mục của make mutation trùng đúng vùng rủi ro cao của hook (scripts/agent/lib.sh, HIGH_RISK_DIRS).
func TestMakeMutation_CoversHighRiskDirs(t *testing.T) {
	lib, err := os.ReadFile("../../scripts/agent/lib.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^HIGH_RISK_DIRS="([^"]+)"`).FindSubmatch(lib)
	if m == nil {
		t.Fatal("không đọc được HIGH_RISK_DIRS trong scripts/agent/lib.sh")
	}
	out := makeDry(t, "mutation")
	for d := range strings.FieldsSeq(string(m[1])) {
		dir := d
		if strings.HasSuffix(d, ".go") { // tệp lẻ: đột biến cả gói nhưng loại mọi tệp khác
			dir = d[:strings.LastIndex(d, "/")]
		}
		if !strings.Contains(out, "gremlins unleash") || !strings.Contains(out, dir) {
			t.Errorf("make mutation không đột biến %s", d)
		}
	}
	if !strings.Contains(out, "--config .gremlins.yaml") {
		t.Error("make mutation phải dùng .gremlins.yaml (ngưỡng 80%)")
	}
}

// Image của compose dev theo dự án compose (việc treo Task 395): phiên song song không ghi đè image của nhau.
func TestMakeImage_TagPerComposeProject(t *testing.T) {
	out := makeDry(t, "image", "COMPOSE_PROJECT=crm-t99")
	if !strings.Contains(out, "docker build -t crm:crm-t99 .") {
		t.Errorf("make image phải dựng crm:<dự án compose>, đang là:\n%s", out)
	}
	out = makeDry(t, "image", "CRM_IMAGE=crm:abc123")
	if !strings.Contains(out, "docker build -t crm:abc123 .") {
		t.Errorf("CRM_IMAGE phải đè tên image, đang là:\n%s", out)
	}
}

func TestDevCompose_AppImageFromEnv(t *testing.T) {
	names := append([]string{"crm-migrate"}, appServices...)
	p := configEnv(t, []string{"CRM_IMAGE=crm:crm-t99"},
		"-p", "crm-deploy-test", "-f", "docker/compose.dev.yml", "--profile", "app")
	for _, n := range names {
		if got := p.Services[n].Image; got != "crm:crm-t99" {
			t.Errorf("%s: image %q, muốn crm:crm-t99 (từ CRM_IMAGE)", n, got)
		}
	}
	p = configEnv(t, []string{"CRM_IMAGE="},
		"-p", "crm-deploy-test", "-f", "docker/compose.dev.yml", "--profile", "app")
	for _, n := range names {
		if got := p.Services[n].Image; got != "crm:dev" {
			t.Errorf("%s: không đặt CRM_IMAGE thì image %q, muốn crm:dev", n, got)
		}
	}
}
