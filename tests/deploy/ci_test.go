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
	RunsOn      any               `yaml:"runs-on"`
	Timeout     int               `yaml:"timeout-minutes"`
	Strategy    map[string]any    `yaml:"strategy"`
	Outputs     map[string]string `yaml:"outputs"`
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Steps       []step            `yaml:"steps"`
}

type step struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

func readWorkflow(t *testing.T) workflow {
	t.Helper()
	return readWorkflowFile(t, ciWorkflow)
}

func readWorkflowFile(t *testing.T, path string) workflow {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc %s: %v", path, err)
	}
	var w workflow
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s không phải YAML hợp lệ: %v", path, err)
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

// Lượt mutation (tới 6 giờ, chạy bằng tay) trên main: nếu chung nhóm concurrency với lượt push lên main thì push phải
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

// Mutation testing vùng cao chỉ chạy bằng tay (workflow_dispatch), không theo lịch: một lượt tới 6 giờ tiêu hết 2.000
// phút miễn phí mỗi tháng của gói Free (Task 493, chủ shop 26/11). Không chạy ở push hay pull request.
func TestCI_MutationManualOnly(t *testing.T) {
	w := readWorkflow(t)
	if _, ok := w.On["schedule"]; ok {
		t.Error("ci.yml không được chạy theo lịch (schedule): mutation chỉ chạy bằng tay")
	}
	if _, ok := w.On["workflow_dispatch"]; !ok {
		t.Fatal("ci.yml thiếu workflow_dispatch cho mutation testing")
	}
	j := needJob(t, w, "mutation")
	if !strings.Contains(j.If, "github.event_name == 'workflow_dispatch'") {
		t.Errorf("job mutation chỉ chạy bằng tay, if đang là %q", j.If)
	}
	if len(runs(j, regexp.MustCompile(`^make mutation\b`))) != 1 {
		t.Error("job mutation phải chạy make mutation")
	}
	for name, other := range w.Jobs {
		if name != "mutation" && !strings.Contains(other.If, "github.event_name != 'workflow_dispatch'") {
			t.Errorf("job %s không được chạy lại khi bấm chạy mutation (if: github.event_name != 'workflow_dispatch')", name)
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
		"go build", "go vet ./...", "go vet -tags drill,probe", "golangci-lint run ./...", "sqlc generate", "git diff --exit-code",
		"TestSqlcVet", "go test -race -count=1 ./...", "govulncheck ./...", "tools/apicheck", "tools/catalog",
		"jscpd", "test-guard.sh main", "gitleaks git", "gitleaks dir", "npx tsc --noEmit", "npx vitest run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("make ci thiếu bước %q", want)
		}
	}
}

// make lint chờ lượt khi phiên khác giữ khóa $TMPDIR/golangci-lint.lock thay vì đỏ "parallel golangci-lint is running"
// (Task 481; Task 480 gặp 24/11), như tests/arch/lint_test.go.
func TestMakeLint_WaitsForOtherRunner(t *testing.T) {
	if out := makeDry(t, "lint"); !strings.Contains(out, "golangci-lint run ./... --allow-serial-runners") {
		t.Errorf("make lint phải chạy golangci-lint với --allow-serial-runners, có:\n%s", out)
	}
}

// HIGH_RISK_DIRS của hook (scripts/agent/lib.sh): nguồn duy nhất của danh sách vùng rủi ro cao.
func highRiskDirs(t *testing.T) []string {
	t.Helper()
	lib, err := os.ReadFile("../../scripts/agent/lib.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^HIGH_RISK_DIRS="([^"]+)"`).FindSubmatch(lib)
	if m == nil {
		t.Fatal("không đọc được HIGH_RISK_DIRS trong scripts/agent/lib.sh")
	}
	return strings.Fields(string(m[1]))
}

// Danh sách thư mục của make mutation trùng đúng vùng rủi ro cao của hook (scripts/agent/lib.sh, HIGH_RISK_DIRS).
func TestMakeMutation_CoversHighRiskDirs(t *testing.T) {
	out := makeDry(t, "mutation")
	for _, d := range highRiskDirs(t) {
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

// Một nguồn danh sách (Task 392): Makefile đọc HIGH_RISK_DIRS từ scripts/agent/lib.sh, không chép lại; mục là tệp lẻ
// (internal/httpapi/guard.go) thì loại mọi tệp .go khác cùng gói và mọi thư mục con, kể cả tệp thêm sau này.
func TestMakeMutation_SingleSourceAndFileEntries(t *testing.T) {
	dirs := highRiskDirs(t)
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	src := mutationSource(string(mk))
	if !strings.Contains(src, "scripts/agent/lib.sh") {
		t.Error("đích mutation phải đọc HIGH_RISK_DIRS từ scripts/agent/lib.sh")
	}
	if d := copiedHighRisk(src, dirs); d != "" {
		t.Errorf("Makefile chép lại mục %s của HIGH_RISK_DIRS cho đích mutation; phải đọc từ scripts/agent/lib.sh", d)
	}
	out := makeDry(t, "mutation")
	for _, f := range dirs {
		if strings.HasSuffix(f, ".go") {
			checkSingleFileMutation(t, out, f)
		}
	}
}

var (
	makeRefRe  = regexp.MustCompile(`\$[({](?:call\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	makeDefRe  = regexp.MustCompile(`^(?:override\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?::{1,3}=|\?=|\+=|=)`)
	makeRuleRe = regexp.MustCompile(`^mutation\s*:`)
)

// mutationSource trả phần Makefile quyết định đích mutation chạy gì: dòng khai đích, công thức của nó và định nghĩa mọi
// biến/hàm make được dùng trong đó (bắc cầu). Dòng tiếp nối (\ cuối dòng) được nối lại; chú thích và đích khác bị bỏ.
func mutationSource(mk string) string {
	var lines []string
	cur := ""
	for l := range strings.SplitSeq(mk, "\n") {
		if s, ok := strings.CutSuffix(l, "\\"); ok {
			cur += s + " "
			continue
		}
		lines = append(lines, cur+l)
		cur = ""
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	defs := map[string][]string{}
	var src []string
	inRecipe := false
	for _, l := range lines {
		switch {
		case makeRuleRe.MatchString(l):
			inRecipe = true
			src = append(src, l)
		case inRecipe && strings.HasPrefix(l, "\t"):
			src = append(src, l)
		default:
			inRecipe = false
			if m := makeDefRe.FindStringSubmatch(l); m != nil && !strings.HasPrefix(l, "\t") {
				defs[m[1]] = append(defs[m[1]], l)
			}
		}
	}
	seen := map[string]bool{}
	for i := 0; i < len(src); i++ {
		for _, m := range makeRefRe.FindAllStringSubmatch(src[i], -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				src = append(src, defs[m[1]]...)
			}
		}
	}
	return strings.Join(src, "\n")
}

// copiedHighRisk trả mục đầu tiên của dirs xuất hiện nguyên văn trong src, hoặc "" nếu không có.
func copiedHighRisk(src string, dirs []string) string {
	for _, d := range dirs {
		if strings.Contains(src, d) {
			return d
		}
	}
	return ""
}

// checkSingleFileMutation: lượt gremlins của gói chứa tệp f loại mọi thứ trong gói (thư mục con, tệp .go khác) trừ f.
func checkSingleFileMutation(t *testing.T, out, f string) {
	t.Helper()
	dir, name := f[:strings.LastIndex(f, "/")], f[strings.LastIndex(f, "/")+1:]
	pkg := regexp.MustCompile(`\./` + regexp.QuoteMeta(dir) + `(\s|;|$)`)
	var line string
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "gremlins unleash") && pkg.MatchString(l) {
			line = l
		}
	}
	if line == "" {
		t.Errorf("make mutation không có lượt gremlins cho gói %s (tệp %s)", dir, f)
		return
	}
	var excludes []*regexp.Regexp
	for _, e := range regexp.MustCompile(`-E '([^']+)'`).FindAllStringSubmatch(line, -1) {
		excludes = append(excludes, regexp.MustCompile(e[1]))
	}
	excluded := func(p string) bool {
		return slices.ContainsFunc(excludes, func(re *regexp.Regexp) bool { return re.MatchString(p) })
	}
	if excluded(name) {
		t.Errorf("make mutation loại chính tệp %s", f)
	}
	entries, err := os.ReadDir("../../" + dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		p := e.Name()
		switch {
		case e.IsDir():
			p += "/x.go"
		case p == name || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
			continue
		}
		if !excluded(p) {
			t.Errorf("make mutation không loại %s/%s (chỉ được đột biến %s)", dir, p, name)
		}
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

// Task 401: chỉ công thức đích mutation và các biến/hàm nó dùng (bắc cầu) không được chép danh sách vùng cao; đích khác
// (perf-test chạy ./internal/platform/queue/) nhắc một gói cụ thể là hợp lệ.
func TestMutationSource_OnlyMutationRecipeAndItsVars(t *testing.T) {
	dirs := []string{"internal/platform/queue", "internal/httpapi/guard.go"}
	const lib = "MUTATION_HIGH_RISK := $(shell sed -n 's/^HIGH_RISK_DIRS=\"\\(.*\\)\"$$/\\1/p' scripts/agent/lib.sh)\n"
	const perf = "perf-test: ## đo\n\tgo test -tags perf ./internal/platform/queue/\n\n"
	cases := []struct {
		name, mk, copied string
	}{
		{"đích khác nhắc gói: hợp lệ", lib + perf +
			"mutation: ## m\n\tfor d in $(MUTATION_HIGH_RISK); do gremlins unleash ./$$d; done\n", ""},
		{"chép danh sách vào công thức mutation", lib + perf +
			"mutation:\n\tfor d in internal/platform/queue; do gremlins unleash ./$$d; done\n", "internal/platform/queue"},
		{"chép danh sách vào biến mutation dùng (bắc cầu)", perf +
			"LIST = internal/httpapi/guard.go\nMUTATION_X := $(filter %.go,$(LIST))\n" +
			"mutation:\n\t$(foreach f,$(MUTATION_X),gremlins unleash $(f);)\n", "internal/httpapi/guard.go"},
		{"chép danh sách vào hàm make mutation gọi", lib +
			"mx = -E '$(1)' internal/platform/queue\nmutation:\n\tgremlins unleash $(call mx,a)\n", "internal/platform/queue"},
		{"chép vào dòng tiếp nối của biến", lib +
			"MUTATION_X := a \\\n\tinternal/platform/queue\nmutation:\n\tgremlins unleash $(MUTATION_X)\n", "internal/platform/queue"},
		{"biến không ai dùng trong mutation: không tính", lib +
			"PERF_PKG := ./internal/platform/queue/\nperf-test:\n\tgo test $(PERF_PKG)\n" +
			"mutation:\n\tgremlins unleash $(MUTATION_HIGH_RISK)\n", ""},
		{"dòng sau công thức mutation không thuộc đích", lib +
			"mutation:\n\tgremlins unleash $(MUTATION_HIGH_RISK)\n\n# chú thích\nother:\n\tgo test ./internal/platform/queue/\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := copiedHighRisk(mutationSource(c.mk), dirs); got != c.copied {
				t.Errorf("copiedHighRisk = %q, muốn %q", got, c.copied)
			}
		})
	}
}
