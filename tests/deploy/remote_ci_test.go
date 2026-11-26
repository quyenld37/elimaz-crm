package deploy_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-faster/yaml"
)

// Task 493 (Chung-CI1, chủ shop OK 26/11): cổng kiểm và test của task chạy trên GitHub Actions (workflow test-remote.yml,
// chỉ bấm tay qua scripts/ci/remote-test.sh) để nhiều phiên song song không xếp hàng một lượt test của máy dev. Gói Free
// 2.000 phút/tháng, runner 2 lõi: chia gói thành tối đa 6 phần cân bằng, ít gói thì ít phần (mỗi job tốn ~2 phút dựng).

const remoteWorkflow = "../../.github/workflows/test-remote.yml"

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// ---- test-remote.yml

func TestRemoteWorkflow_DispatchOnlyWithInputs(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	// Push chỉ được phép ở đúng nhánh ci/dang-ky (làm GitHub biết workflow khi tệp chưa có trên main) và lượt đó không
	// chạy job nào: mọi job gốc (không needs) đòi github.event_name == 'workflow_dispatch'.
	if evs := slices.Sorted(maps.Keys(w.On)); !slices.Equal(evs, []string{"push", "workflow_dispatch"}) &&
		!slices.Equal(evs, []string{"workflow_dispatch"}) {
		t.Errorf("test-remote.yml chỉ chạy bằng workflow_dispatch (không lịch, không PR), đang có %v", evs)
	}
	if p, ok := w.On["push"]; ok {
		checkRegisterPush(t, w, p)
	}
	d, ok := w.On["workflow_dispatch"].(map[string]any)
	if !ok {
		t.Fatal("test-remote.yml thiếu workflow_dispatch có inputs")
	}
	inputs, _ := d["inputs"].(map[string]any)
	for _, in := range []string{"base", "packages", "race_packages", "static"} {
		if _, ok := inputs[in]; !ok {
			t.Errorf("workflow_dispatch thiếu input %q", in)
		}
	}
	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Errorf("quyền của workflow phải chỉ là contents: read, đang là %v", w.Permissions)
	}
	b, err := os.ReadFile(remoteWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secrets.") {
		t.Error("test-remote.yml không được dùng secret")
	}
	for _, banned := range []string{"playwright", "-tags e2e", "docker build", "make image", "console-e2e"} {
		if strings.Contains(string(b), banned) {
			t.Errorf("test-remote.yml không dựng image, không e2e, không Playwright: có %q", banned)
		}
	}
}

// checkRegisterPush: push chỉ ở nhánh ci/dang-ky và lượt push đó không chạy job nào (0 phút).
func checkRegisterPush(t *testing.T, w workflow, p any) {
	t.Helper()
	pm, _ := p.(map[string]any)
	br, _ := pm["branches"].([]any)
	if len(pm) != 1 || len(br) != 1 || br[0] != "ci/dang-ky" {
		t.Errorf("push chỉ ở nhánh ci/dang-ky, đang là %v", p)
	}
	for name, j := range w.Jobs {
		if j.Needs == nil && !strings.Contains(j.If, "github.event_name == 'workflow_dispatch'") {
			t.Errorf("job %s chạy cả ở lượt push đăng ký (tốn phút): if đang là %q", name, j.If)
		}
	}
}

func TestRemoteWorkflow_ConcurrencyPerRefCancels(t *testing.T) {
	b, err := os.ReadFile(remoteWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel any    `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
	}
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Concurrency.Group, "github.ref") {
		t.Errorf("concurrency.group phải theo github.ref, đang là %q", w.Concurrency.Group)
	}
	if w.Concurrency.Cancel != true {
		t.Errorf("cancel-in-progress phải là true (lượt mới của cùng worktree hủy lượt cũ), đang là %v", w.Concurrency.Cancel)
	}
}

func TestRemoteWorkflow_PinnedAndTimeouts(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+@[0-9a-f]{40}$`)
	for name, j := range w.Jobs {
		if j.Timeout <= 0 || j.Timeout > 60 {
			t.Errorf("job %s: timeout-minutes phải có và ≤ 60 (giữ phút miễn phí), đang là %d", name, j.Timeout)
		}
		for _, s := range j.Steps {
			if s.Uses != "" && !pinned.MatchString(s.Uses) {
				t.Errorf("job %s: %q chưa ghim mã commit", name, s.Uses)
			}
		}
	}
}

// Job static chạy đủ cổng tĩnh qua scripts/ci/static-gates.sh (cùng danh sách với ci-task) với base của input, sau khi
// cài công cụ đã ghim và dựng PostgreSQL (sqlc-vet, tests/arch dùng database thử).
func TestRemoteWorkflow_StaticJob(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	j := needJob(t, w, "static")
	checkedOutFullHistory(t, "static", j)
	if !strings.Contains(j.If, "inputs.static") {
		t.Errorf("job static chỉ chạy khi inputs.static là 'true', if đang là %q", j.If)
	}
	tools := indexOf(j, regexp.MustCompile(`^scripts/ci/install-tools\.sh\b`))
	dbUp := indexOf(j, regexp.MustCompile(`^make db-up\b`))
	gates := indexOf(j, regexp.MustCompile(`^scripts/ci/static-gates\.sh "?\$\{?\{? ?(inputs\.base|BASE)`))
	if tools < 0 || dbUp < 0 || gates < 0 || tools > gates || dbUp > gates {
		t.Errorf("job static: cài công cụ (%d), make db-up (%d) rồi scripts/ci/static-gates.sh với base (%d)", tools, dbUp, gates)
	}
	for _, name := range []string{"static", "test"} {
		checkToolCache(t, name, needJob(t, w, name))
	}
	b, err := os.ReadFile("../../scripts/ci/install-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "make ci-tools") {
		t.Error("install-tools.sh phải cài bằng make ci-tools (đúng bản ghim ở tools/versions.mk)")
	}
}

// checkToolCache: cache công cụ khóa theo đúng các lệnh của make ci-tools (đổi phiên bản hay thêm công cụ thì trượt
// cache và cài lại; sửa phần khác của Makefile thì vẫn trúng — lượt 26/11 cài lại 5 phút vì khóa băm cả Makefile).
func checkToolCache(t *testing.T, name string, j job) {
	t.Helper()
	if indexOf(j, regexp.MustCompile(`make -n .*ci-tools \| sha256sum`)) < 0 {
		t.Errorf("job %s: khóa cache công cụ phải băm make -n ci-tools", name)
	}
	for _, s := range j.Steps {
		if strings.HasPrefix(s.Uses, "actions/cache@") && strings.Contains(fmt.Sprint(s.With["path"]), "go/bin") {
			if k := fmt.Sprint(s.With["key"]); !strings.Contains(k, "steps.toolkey.outputs.key") || strings.Contains(k, "Makefile") {
				t.Errorf("job %s: khóa cache công cụ %q", name, k)
			}
			return
		}
	}
	t.Errorf("job %s không cache công cụ (~/go/bin)", name)
}

// make ci-tools cài đủ công cụ mà cổng tĩnh và test gọi bằng tên lệnh: oasdiff (tools/apicheck, make oasdiff) thiếu thì
// đỏ "executable file not found" (lượt chạy thật đầu tiên của test-remote.yml, 26/11).
func TestMakeCITools_InstallsEveryPinnedTool(t *testing.T) {
	out := makeDry(t, "ci-tools")
	for _, want := range []string{"golangci-lint@", "govulncheck@", "sqlc@", "ogen@", "oasdiff/oasdiff@", "gitleaks/v8@", "gremlins@", "jscpd@"} {
		if !strings.Contains(out, want) {
			t.Errorf("make ci-tools thiếu %q:\n%s", want, out)
		}
	}
}

// Job test chạy theo ma trận do job plan chia; dựng compose dev như ci.yml rồi go test -count=1 (race cho gói của
// race_packages), có tóm tắt FAIL.
func TestRemoteWorkflow_PlanAndTestMatrix(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	plan := needJob(t, w, "plan")
	if indexOf(plan, regexp.MustCompile(`skip='/tests/e2e\$'`)) < 0 {
		t.Error("job plan: packages=ALL là mọi gói trừ tests/e2e (e2e chỉ chạy khi xong toàn bộ task)")
	}
	if indexOf(plan, regexp.MustCompile(`scripts/ci/split_packages\.py`)) < 0 {
		t.Error("job plan phải chia gói bằng scripts/ci/split_packages.py")
	}
	if plan.Outputs["matrix"] == "" {
		t.Error("job plan phải xuất outputs.matrix")
	}
	j := needJob(t, w, "test")
	if !needs(j, "plan") {
		t.Error("job test phải chờ job plan (needs: plan)")
	}
	m, _ := j.Strategy["matrix"].(map[string]any)
	if inc, _ := m["include"].(string); !strings.Contains(inc, "fromJSON(needs.plan.outputs.matrix)") {
		t.Errorf("ma trận của job test phải lấy từ needs.plan.outputs.matrix, đang là %v", j.Strategy["matrix"])
	}
	if ff, ok := j.Strategy["fail-fast"]; !ok || ff != false {
		t.Error("job test: fail-fast: false (một phần đỏ vẫn cho các phần khác chạy xong để báo đủ test đỏ)")
	}
	tools := indexOf(j, regexp.MustCompile(`^scripts/ci/install-tools\.sh\b`))
	dbUp := indexOf(j, regexp.MustCompile(`^make db-up\b`))
	part := indexOf(j, regexp.MustCompile(`^make .*test-part RACE="\$RACE" PLAIN="\$PLAIN"`))
	if tools < 0 || dbUp < 0 || part < 0 || tools > part || dbUp > part {
		t.Errorf("job test: cài công cụ (%d), make db-up (%d) rồi make test-part RACE PLAIN (%d)", tools, dbUp, part)
	}
	if pull := indexOf(j, regexp.MustCompile(`--profile valkey pull\b.*valkey-cache`)); pull < 0 || pull > part {
		t.Error("job test phải kéo sẵn image Valkey trước khi test (testkit.StartValkey đỏ khi docker run phải kéo image)")
	}
	// make test-part: qua make để có biến test (CRM_TEST_DATABASE_URL…) như make test; -race cho RACE, thường cho PLAIN.
	out := makeDry(t, "test-part", "RACE=./a/...", "PLAIN=./b/...")
	for _, want := range []string{"go test -race -count=1 -timeout 30m ./a/...", "go test -count=1 -timeout 30m ./b/..."} {
		if !strings.Contains(out, want) {
			t.Errorf("make test-part thiếu %q:\n%s", want, out)
		}
	}
	if indexOf(j, regexp.MustCompile(`GITHUB_STEP_SUMMARY`)) < 0 {
		t.Error("job test phải ghi tóm tắt FAIL vào GITHUB_STEP_SUMMARY")
	}
}

// ---- split_packages.py

type part struct {
	Part    int    `json:"part"`
	Race    string `json:"race"`
	Plain   string `json:"plain"`
	Seconds int    `json:"seconds"`
}

func split(t *testing.T, pkgs []string, args ...string) []part {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "python3", append([]string{"scripts/ci/split_packages.py"}, args...)...)
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader(strings.Join(pkgs, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("split_packages.py: %v\n%s", err, ee.Stderr)
		}
		t.Fatal(err)
	}
	var parts []part
	if err := json.Unmarshal(out, &parts); err != nil {
		t.Fatalf("đầu ra không phải JSON: %v\n%s", err, out)
	}
	return parts
}

func timesFile(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "times.txt")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func allPkgs(parts []part) []string {
	var out []string
	for _, p := range parts {
		out = append(out, strings.Fields(p.Race)...)
		out = append(out, strings.Fields(p.Plain)...)
	}
	slices.Sort(out)
	return out
}

func TestSplit_BalancedByTimes(t *testing.T) {
	tf := timesFile(t, "# gói giây", mod+"a 600", mod+"b 300", mod+"c 300", mod+"d 200", mod+"e 100", mod+"f 100")
	pkgs := []string{mod + "a", mod + "b", mod + "c", mod + "d", mod + "e", mod + "f"}
	parts := split(t, pkgs, "--times", tf, "--max-parts", "3", "--min-seconds", "60")
	if len(parts) != 3 {
		t.Fatalf("muốn 3 phần, có %d: %+v", len(parts), parts)
	}
	if got := allPkgs(parts); !slices.Equal(got, pkgs) {
		t.Errorf("mỗi gói đúng một phần: có %v", got)
	}
	// Tổng 1600 s, 3 phần: phần nặng nhất là gói a (600); không phần nào vượt 600.
	for _, p := range parts {
		if p.Seconds > 600 {
			t.Errorf("phần %d nặng %d s, chia lệch: %+v", p.Part, p.Seconds, parts)
		}
	}
}

func TestSplit_FewPackagesFewParts(t *testing.T) {
	tf := timesFile(t, mod+"a 20", mod+"b 30")
	parts := split(t, []string{mod + "a", mod + "b"}, "--times", tf, "--max-parts", "6", "--min-seconds", "120")
	if len(parts) != 1 {
		t.Errorf("tổng 50 s dưới ngưỡng 120 s một phần: muốn 1 phần, có %d", len(parts))
	}
	parts = split(t, []string{mod + "a"}, "--times", tf, "--max-parts", "6", "--min-seconds", "1")
	if len(parts) != 1 {
		t.Errorf("một gói thì một phần, có %d", len(parts))
	}
	if parts := split(t, nil, "--max-parts", "6"); len(parts) != 0 {
		t.Errorf("không gói nào thì không phần nào, có %+v", parts)
	}
}

func TestSplit_MaxSixParts(t *testing.T) {
	var lines, pkgs []string
	for _, c := range "abcdefghij" {
		pkgs = append(pkgs, mod+string(c))
		lines = append(lines, mod+string(c)+" 500")
	}
	parts := split(t, pkgs, "--times", timesFile(t, lines...), "--max-parts", "6", "--min-seconds", "60")
	if len(parts) != 6 {
		t.Errorf("tối đa 6 phần, có %d", len(parts))
	}
}

// Gói chưa có trong tệp thời gian: ước theo số tệp test (gói thật trong repo), không bỏ gói.
func TestSplit_UnknownPackagesByTestFiles(t *testing.T) {
	pkgs := []string{mod + "internal/modules/order", mod + "internal/platform/fieldcrypt", mod + "tools/catalog"}
	parts := split(t, pkgs, "--times", timesFile(t, "# rỗng"), "--max-parts", "2", "--min-seconds", "1")
	if got := allPkgs(parts); !slices.Equal(got, pkgs) {
		t.Errorf("mọi gói phải được chia: %v", got)
	}
	if len(parts) != 2 {
		t.Fatalf("muốn 2 phần, có %+v", parts)
	}
	// order có nhiều tệp test nhất: đứng một mình một phần.
	for _, p := range parts {
		if strings.Contains(p.Race+p.Plain, "modules/order") && len(strings.Fields(p.Race+" "+p.Plain)) != 1 {
			t.Errorf("gói order (nhiều tệp test nhất) phải ở riêng một phần: %+v", parts)
		}
	}
}

func TestSplit_RaceSelection(t *testing.T) {
	tf := timesFile(t, mod+"a 10", mod+"b 10")
	parts := split(t, []string{mod + "a", mod + "b"}, "--times", tf, "--race", mod+"a")
	if len(parts) != 1 || parts[0].Race != mod+"a" || parts[0].Plain != mod+"b" {
		t.Errorf("a chạy -race, b chạy thường: %+v", parts)
	}
	parts = split(t, []string{mod + "a", mod + "b"}, "--times", tf, "--race", "ALL")
	if len(parts) != 1 || parts[0].Plain != "" || len(strings.Fields(parts[0].Race)) != 2 {
		t.Errorf("--race ALL: mọi gói -race: %+v", parts)
	}
}

// ---- scripts/ci/lib.sh: dò nhánh gốc, chọn chế độ

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", file)
	git(t, dir, "commit", "-q", "-m", file)
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "a.txt")
	return dir
}

// ciLib chạy một hàm của scripts/ci/lib.sh trong dir; trả stdout đã bỏ khoảng trắng.
func ciLib(t *testing.T, dir string, env []string, fn string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", "-c", `. "$1"; `+fn, "bash", filepath.Join(repoRoot(t), "scripts/ci/lib.sh"))
	cmd.Dir = dir
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "CRM_BASE_BRANCH=") || strings.HasPrefix(kv, "CRM_CI_REMOTE=") || strings.HasPrefix(kv, "CRM_GH=")
	}), env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v", fn, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCILib_BaseBranch(t *testing.T) {
	// Dò theo điểm tách mới nhất với HEAD. Mỗi bước: "b <nhánh>" tạo nhánh mới và chuyển sang, "c <nhánh>" chuyển sang nhánh có sẵn, "m <nhánh>" gộp
	// --no-ff, "f <tệp>" commit một tệp. Repo bắt đầu ở main có một commit.
	cases := []struct {
		name  string
		steps []string
		env   []string
		want  string
	}{
		{"nhánh tách từ m4 (m4 đi trước main): m4", []string{"b m4", "f m4.txt", "b task-1", "f t.txt"}, nil, "m4"},
		{"CRM_BASE_BRANCH thắng dò tự động", []string{"b m4", "f m4.txt", "b task-1", "f t.txt"}, []string{"CRM_BASE_BRANCH=main"}, "main"},
		{"không có m4: main", []string{"b task-1", "f t.txt"}, nil, "main"},
		{
			"m4 đi tiếp sau khi task tách (HEAD không chứa đầu m4): vẫn m4",
			[]string{"b m4", "f m4.txt", "b task-1", "f t.txt", "c m4", "f m4b.txt", "c task-1"},
			nil, "m4",
		},
		{"có m4 nhưng nhánh tách từ main, không chứa m4: main", []string{"b m4", "f m4.txt", "c main", "b task-1", "f t.txt"}, nil, "main"},
		{
			"m4 đã gộp vào main, task tách từ main mới hơn: main",
			[]string{"b m4", "f m4.txt", "c main", "m m4", "f main2.txt", "b task-1", "f t.txt"},
			nil, "main",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := newRepo(t)
			for _, st := range c.steps {
				op, arg, _ := strings.Cut(st, " ")
				switch op {
				case "b":
					git(t, dir, "checkout", "-q", "-b", arg)
				case "c":
					git(t, dir, "checkout", "-q", arg)
				case "m":
					git(t, dir, "merge", "-q", "--no-ff", "-m", "gộp "+arg, arg)
				case "f":
					commit(t, dir, arg)
				}
			}
			if got := ciLib(t, dir, c.env, "ci_base_branch"); got != c.want {
				t.Errorf("ci_base_branch = %q, muốn %q", got, c.want)
			}
		})
	}
}

func TestCILib_Mode(t *testing.T) {
	withOrigin := newRepo(t)
	git(t, withOrigin, "remote", "add", "origin", "https://example.invalid/r.git")
	noOrigin := newRepo(t)
	cases := []struct {
		name, dir string
		env       []string
		want      string
	}{
		{"có origin, gh dùng được: từ xa", withOrigin, []string{"CRM_GH=true"}, "remote"},
		{"CRM_CI_REMOTE=0: ép chạy máy", withOrigin, []string{"CRM_GH=true", "CRM_CI_REMOTE=0"}, "local"},
		{"không có origin: chạy máy", noOrigin, []string{"CRM_GH=true"}, "local"},
		{"gh lỗi (chưa đăng nhập, không có): chạy máy", withOrigin, []string{"CRM_GH=false"}, "local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ciLib(t, c.dir, c.env, "ci_mode")
			if first, _, _ := strings.Cut(got, " "); first != c.want {
				t.Errorf("ci_mode = %q, muốn bắt đầu bằng %q", got, c.want)
			}
			if c.want == "local" && !strings.Contains(got, " ") {
				t.Errorf("chạy máy phải kèm lý do: %q", got)
			}
		})
	}
}

// ---- static-gates.sh: cùng danh sách cổng với ci-task, bước có điều kiện theo phần đổi so với base

func staticDry(t *testing.T, dir, base string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(repoRoot(t), "scripts/ci/static-gates.sh"), base)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI_STATIC_DRY=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("static-gates.sh: %v\n%s", err, out)
	}
	return string(out)
}

func TestStaticGates_Steps(t *testing.T) {
	dir := newRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	commit(t, dir, "internal/x/x.go")
	out := staticDry(t, dir, base)
	for _, want := range []string{
		"make build", "make vet", "make lint", "make catalog-check", "make jscpd", "make gitleaks",
		"make oasdiff OASDIFF_BASE=-base-ref=" + base, "scripts/agent/test-guard.sh " + base, "make --no-print-directory test-part RACE=./tests/arch/...",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cổng tĩnh thiếu %q:\n%s", want, out)
		}
	}
	for _, skip := range []string{"make gen-check", "make sqlc-vet", "make vuln", "make console-check"} {
		if strings.Contains(out, "RUN "+skip) {
			t.Errorf("không đổi nguồn liên quan thì bỏ %q:\n%s", skip, out)
		}
	}
	commit(t, dir, "queries/a.sql")
	commit(t, dir, "go.mod")
	commit(t, dir, "console/src/a.ts")
	out = staticDry(t, dir, base)
	for _, want := range []string{"make gen-check", "make sqlc-vet", "make vuln", "make console-check"} {
		if !strings.Contains(out, "RUN "+want) {
			t.Errorf("đổi queries, go.mod, console thì phải chạy %q:\n%s", want, out)
		}
	}
}

// ---- remote-test.sh với origin là repo trần cục bộ và gh giả

const fakeGH = `#!/usr/bin/env bash
# gh giả: ghi lệnh vào $FAKE_LOG; run list trả sha đang ở nhánh ci/* của repo trần $FAKE_BARE.
echo "gh $*" >> "$FAKE_LOG"
case "$1 $2" in
  "auth status") exit 0 ;;
  "workflow run") [ "${FAKE_DISPATCH_FAIL:-0}" = 1 ] && { echo "HTTP 404" >&2; exit 1; }
    for a in "$@"; do case "$a" in --ref=*) echo "${a#--ref=}" > "$FAKE_LOG.ref";; esac; done
    prev=""; for a in "$@"; do [ "$prev" = --ref ] && echo "$a" > "$FAKE_LOG.ref"; prev=$a; done; exit 0 ;;
  "run list") ref=$(cat "$FAKE_LOG.ref"); sha=$(git --git-dir="$FAKE_BARE" rev-parse "refs/heads/$ref")
    echo "[{\"databaseId\":99,\"headSha\":\"0000000000000000000000000000000000000000\"},{\"databaseId\":42,\"headSha\":\"$sha\"}]" ;;
  "run watch") [ "${FAKE_RESULT:-success}" = success ] ;;
  "run cancel") exit 0 ;;
  "run view") if [[ " $* " == *" status,jobs "* ]]; then
      if [ "${FAKE_QUEUED:-0}" = 1 ]; then echo '{"status":"queued","jobs":[{"status":"queued","conclusion":""}]}'; else echo '{"status":"in_progress","jobs":[{"status":"in_progress","conclusion":""}]}'; fi; exit 0; fi
    n=$(cat "$FAKE_LOG.views" 2>/dev/null || echo 0); echo $((n+1)) > "$FAKE_LOG.views"
    if [[ " $* " != *" --log-failed "* ]] && [ "$n" -lt "${FAKE_PENDING_VIEWS:-0}" ]; then echo '{"conclusion":"","url":"u","jobs":[]}'; exit 0; fi
    if [[ " $* " == *" --log-failed "* ]]; then printf 'test (2)\tgo test\t--- FAIL: TestHong (0.01s)\ntest (2)\tgo test\tok   elimaz.com.vn/crm/x 1.0s\n'; else
    echo "{\"conclusion\":\"${FAKE_RESULT:-success}\",\"url\":\"https://example.invalid/run/42\",\"jobs\":[{\"name\":\"static\",\"conclusion\":\"${FAKE_RESULT:-success}\",\"startedAt\":\"2026-11-26T01:00:00Z\",\"completedAt\":\"2026-11-26T01:04:30Z\"}]}"; fi ;;
  *) echo "gh giả không biết: $*" >&2; exit 9 ;;
esac
`

type remoteEnv struct {
	dir, bare, log, pool string
	env                  []string
}

func newRemoteEnv(t *testing.T) remoteEnv {
	t.Helper()
	dir := newRepo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, dir, "init", "-q", "--bare", bare)
	git(t, dir, "remote", "add", "origin", bare)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o700); err != nil { //nolint:gosec // script thử
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "gh.log")
	return remoteEnv{dir: dir, bare: bare, log: log, pool: t.TempDir(), env: []string{
		"CRM_GH=" + filepath.Join(bin, "gh"), "FAKE_LOG=" + log, "FAKE_BARE=" + bare, "CRM_CI_LABEL=t1",
		"CRM_CI_POLL=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	}}
}

func (r remoteEnv) run(t *testing.T, extra []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{filepath.Join(repoRoot(t), "scripts/ci/remote-test.sh")}, args...)...)
	cmd.Dir = r.dir
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "CRM_CI_RUNNER=") || strings.HasPrefix(kv, "CRM_CI_POOL_")
	})
	cmd.Env = append(append(append(env, "CRM_CI_POOL_DIR="+r.pool), r.env...), extra...)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return string(out), ee.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return string(out), 0
}

func (r remoteEnv) pkgFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pkgs.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRemoteTest_GreenPushesSnapshotAndCleansUp(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	commit(t, r.dir, "b.txt")
	// Tệp chưa commit nằm trong tree được gửi (như snap_tree của ci-task).
	if err := os.WriteFile(filepath.Join(r.dir, "chua-commit.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx := filepath.Join(t.TempDir(), "index")
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "cp .git/index \"$IDX\" && GIT_INDEX_FILE=\"$IDX\" git add -A . && GIT_INDEX_FILE=\"$IDX\" git write-tree")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "IDX="+idx)
	treeOut, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSpace(string(treeOut))
	head := git(t, r.dir, "rev-parse", "HEAD")
	branchBefore := git(t, r.dir, "rev-parse", "--abbrev-ref", "HEAD")

	out, code := r.run(t, nil, tree, base, r.pkgFile(t, "./internal/a/...\n./tools/...\n"), r.pkgFile(t, "./internal/a/...\n"))
	if code != 0 {
		t.Fatalf("lượt xanh phải thoát 0, có %d:\n%s", code, out)
	}
	if !strings.Contains(out, "REMOTE-TEST XANH") || !strings.Contains(out, "https://example.invalid/run/42") {
		t.Errorf("phải in kết quả và link run:\n%s", out)
	}
	if !strings.Contains(out, "static") || !strings.Contains(out, "5 phút") {
		t.Errorf("phải in thời gian từng job và số phút tính tiền (4m30s → 5 phút):\n%s", out)
	}
	log, _ := os.ReadFile(r.log)
	for _, want := range []string{
		"workflow run test-remote.yml", "--ref ci/t1", "base=" + base,
		"packages=./internal/a/... ./tools/...", "race_packages=./internal/a/...", "static=true", "run watch 42",
	} {
		if !strings.Contains(string(log), want) {
			t.Errorf("gh thiếu %q:\n%s", want, log)
		}
	}
	// Nhánh ci/t1 đã bị xóa trên origin; không nhánh cục bộ nào bị đổi.
	if refs := git(t, r.dir, "--git-dir="+r.bare, "for-each-ref", "refs/heads/ci/"); refs != "" {
		t.Errorf("nhánh ci/* phải bị xóa khi xong, còn: %s", refs)
	}
	if git(t, r.dir, "rev-parse", "HEAD") != head || git(t, r.dir, "rev-parse", "--abbrev-ref", "HEAD") != branchBefore {
		t.Error("remote-test.sh không được đổi HEAD hay nhánh hiện tại")
	}
	// Commit đã đẩy có đúng tree được gửi, cha là HEAD.
	pushed := regexp.MustCompile(`commit tạm ([0-9a-f]{40})`).FindStringSubmatch(out)
	if pushed == nil {
		t.Fatalf("phải in mã commit tạm:\n%s", out)
	}
	if got := git(t, r.dir, "--git-dir="+r.bare, "rev-parse", pushed[1]+"^{tree}"); got != tree {
		t.Errorf("tree đã đẩy %s, muốn %s", got, tree)
	}
	if got := git(t, r.dir, "--git-dir="+r.bare, "rev-parse", pushed[1]+"^"); got != head {
		t.Errorf("cha của commit tạm %s, muốn HEAD %s", got, head)
	}
}

// test-guard ở runner không có trạng thái task của worktree: remote-test.sh gửi tệp task đang chạy (task_file).
func TestRemoteTest_TaskFileFromWorktreeState(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	commit(t, r.dir, "docs/tasks/task-7.md")
	state := filepath.Join(git(t, r.dir, "rev-parse", "--absolute-git-dir"), "crm-task-"+"state")
	if err := os.WriteFile(state, []byte("Task: 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := r.run(t, nil, "HEAD", base, r.pkgFile(t, "")); code != 0 {
		t.Fatalf("muốn xanh, có %d:\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	if !strings.Contains(string(log), "task_file=docs/tasks/task-7.md") {
		t.Errorf("phải gửi task_file của task đang chạy:\n%s", log)
	}
	if out, _ := r.run(t, []string{"TASK_FILE=docs/tasks/task-9.md"}, "HEAD", base, r.pkgFile(t, "")); !strings.Contains(out, "XANH") {
		t.Fatal(out)
	}
	log, _ = os.ReadFile(r.log)
	if !strings.Contains(string(log), "task_file=docs/tasks/task-9.md") {
		t.Errorf("TASK_FILE phải thắng trạng thái worktree:\n%s", log)
	}
}

// Tệp gói không có là sai tham số (mã 2), không phải "không test" (xanh giả: chỉ cổng tĩnh chạy). ALL viết thẳng thì
// nhận như tệp chứa ALL.
func TestRemoteTest_PackageArgs(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	for _, args := range [][]string{{"/khong/co/tep"}, {r.pkgFile(t, "ALL\n"), "/khong/co/tep-race"}} {
		if out, code := r.run(t, nil, append([]string{"HEAD", base}, args...)...); code != 2 {
			t.Errorf("tệp gói không có %v: muốn mã 2, có %d:\n%s", args, code, out)
		}
	}
	if log, _ := os.ReadFile(r.log); strings.Contains(string(log), "workflow run") {
		t.Errorf("sai tham số thì không được chạy workflow:\n%s", log)
	}
	if out, code := r.run(t, nil, "HEAD", base, "ALL", "ALL"); code != 0 {
		t.Fatalf("ALL viết thẳng: muốn xanh, có %d:\n%s", code, out)
	}
	if log, _ := os.ReadFile(r.log); !strings.Contains(string(log), "packages=ALL") || !strings.Contains(string(log), "race_packages=ALL") {
		t.Errorf("ALL viết thẳng phải gửi packages=ALL, race_packages=ALL:\n%s", log)
	}
}

func TestRemoteTest_RedShowsFailures(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	out, code := r.run(t, []string{"FAKE_RESULT=failure"}, "HEAD", base, r.pkgFile(t, "ALL\n"))
	if code != 1 {
		t.Fatalf("test đỏ phải thoát 1, có %d:\n%s", code, out)
	}
	if !strings.Contains(out, "REMOTE-TEST ĐỎ") || !strings.Contains(out, "--- FAIL: TestHong") {
		t.Errorf("phải in ĐỎ và dòng FAIL của job đỏ:\n%s", out)
	}
	if strings.Contains(out, "ok   elimaz.com.vn/crm/x") {
		t.Errorf("chỉ in dòng FAIL, không in cả log:\n%s", out)
	}
	if refs := git(t, r.dir, "--git-dir="+r.bare, "for-each-ref", "refs/heads/ci/"); refs != "" {
		t.Errorf("nhánh ci/* phải bị xóa cả khi đỏ, còn: %s", refs)
	}
}

// gh run watch thoát khi run chưa xong (mạng chập chờn): hỏi lại và chờ tiếp tới khi có kết quả.
func TestRemoteTest_WatchRetriesUntilDone(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	out, code := r.run(t, []string{"FAKE_PENDING_VIEWS=2"}, "HEAD", base, r.pkgFile(t, "ALL\n"))
	if code != 0 || !strings.Contains(out, "REMOTE-TEST XANH") {
		t.Fatalf("run chưa xong ở 2 lần hỏi đầu: phải chờ tiếp rồi báo xanh, có %d:\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	if n := strings.Count(string(log), "gh run watch 42"); n != 3 {
		t.Errorf("muốn gọi run watch 3 lần, có %d", n)
	}
	// Không bao giờ xong: bỏ cuộc sau CRM_CI_WATCH_TRIES lần với mã 3.
	out, code = r.run(t, []string{"FAKE_PENDING_VIEWS=99", "CRM_CI_WATCH_TRIES=2"}, "HEAD", base, r.pkgFile(t, "ALL\n"))
	if code != 3 {
		t.Errorf("run không xong: muốn mã 3, có %d:\n%s", code, out)
	}
	// Bỏ cuộc thì hủy run (không tốn phút trong lúc bên gọi lùi về chạy máy).
	if log, _ := os.ReadFile(r.log); !strings.Contains(string(log), "gh run cancel 42") {
		t.Errorf("bỏ cuộc phải hủy run 42:\n%s", log)
	}
}

// Không chạy được từ xa (gh lỗi, mất mạng): mã 3 để ci-task lùi về chạy máy; vẫn dọn nhánh ci/*.
func TestRemoteTest_InfraErrorExit3(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	out, code := r.run(t, []string{"FAKE_DISPATCH_FAIL=1"}, "HEAD", base, r.pkgFile(t, "ALL\n"))
	if code != 3 {
		t.Fatalf("gh lỗi phải thoát 3, có %d:\n%s", code, out)
	}
	if refs := git(t, r.dir, "--git-dir="+r.bare, "for-each-ref", "refs/heads/ci/"); refs != "" {
		t.Errorf("nhánh ci/* phải bị xóa, còn: %s", refs)
	}
	r.env = append(r.env, "CRM_GH=/khong/co/gh")
	if _, code := r.run(t, nil, "HEAD", base, r.pkgFile(t, "ALL\n")); code != 3 {
		t.Errorf("không có gh phải thoát 3, có %d", code)
	}
}

// Bản vá ci-task (scripts/ci/ci-task-tu-xa.patch, chủ shop áp vì hook chặn agent sửa scripts/agent/) phải luôn áp được
// lên ci-task.sh hiện tại; đã áp rồi thì bản vá ngược áp được. Gãy nghĩa là ci-task đổi sau Task 493: làm lại bản vá.
func TestCITaskPatch_Applies(t *testing.T) {
	const patch = "scripts/ci/ci-task-tu-xa.patch"
	check := func(args ...string) error {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"apply", "--check"}, args...)...)
		cmd.Dir = "../.."
		return cmd.Run()
	}
	if check(patch) != nil && check("-R", patch) != nil {
		t.Errorf("%s không áp được lên scripts/agent/ci-task.sh (cũng chưa áp sẵn)", patch)
	}
	// Task 494: ci-task giữ chỗ qua bộ điều phối và luôn in một dòng CI-TASK (bên chờ dò dòng này) kể cả khi không máy
	// nào chạy được.
	b, err := os.ReadFile("../../" + patch)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+  python3 scripts/ci/pool.py run ", "CI-TASK ĐỎ: không máy nào chạy được", `CRM_CI_RUNNER:-mac}" != mac`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("bản vá thiếu %q", want)
		}
	}
}

// ---- Task 494: chọn máy chạy (input runner), sổ phút, hạn bắt đầu run

// Input runner chọn máy cho cả static, plan, test: blacksmith (4 vCPU), github (ubuntu-24.04), vps (runner tự cài
// elimaz-vps). vps có sẵn dịch vụ test trên 127.0.0.1 (PostgreSQL 5600, S3 20600/21600, OTLP 4600): không dựng compose,
// không khôi phục cache (Go cache, công cụ giữ lại trên máy). Số phần: blacksmith ≤ 2 (một máy đã 6,5 phút toàn bộ),
// vps 1 (một job một lúc), github ≤ 6. Task 495: ubicloud (ubicloud-standard-2, 2 vCPU, dựng dịch vụ như github), ≤ 4
// phần (1.100 phút/tháng: ít job hơn github để bớt phút dựng mỗi job).
func TestRemoteWorkflow_RunnerInput(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	d, _ := w.On["workflow_dispatch"].(map[string]any)
	inputs, _ := d["inputs"].(map[string]any)
	in, _ := inputs["runner"].(map[string]any)
	if in["default"] != "github" || in["type"] != "choice" || fmt.Sprint(in["options"]) != "[github blacksmith ubicloud vps]" {
		t.Errorf("input runner: choice github|blacksmith|ubicloud|vps, mặc định github; đang là %v", in)
	}
	for _, name := range []string{"static", "plan", "test"} {
		ro := fmt.Sprint(needJob(t, w, name).RunsOn)
		for _, want := range []string{"inputs.runner", "blacksmith-4vcpu-ubuntu-2404", "inputs.runner == 'ubicloud' && '\"ubicloud-standard-2\"'", `"self-hosted","elimaz-vps"`, "ubuntu-24.04"} {
			if !strings.Contains(ro, want) {
				t.Errorf("job %s: runs-on thiếu %q: %s", name, want, ro)
			}
		}
	}
	for k, port := range map[string]string{"CRM_PG_PORT": "5600", "CRM_S3_PORT": "20600", "CRM_S3_OFFSITE_PORT": "21600", "CRM_OTLP_PORT": "4600"} {
		if v := w.Env[k]; !strings.Contains(v, "inputs.runner == 'vps'") || !strings.Contains(v, "'"+port+"'") {
			t.Errorf("env %s trên vps phải là %s (dịch vụ chạy sẵn), đang là %q", k, port, v)
		}
	}
	for _, name := range []string{"static", "test"} {
		checkVPSSteps(t, name, needJob(t, w, name))
	}
	checkMaxParts(t, needJob(t, w, "plan"))
}

// checkVPSSteps: trên vps không dựng compose, không khôi phục cache của GitHub (giữ trên máy).
func checkVPSSteps(t *testing.T, name string, j job) {
	t.Helper()
	dbUp := indexOf(j, regexp.MustCompile(`^make db-up\b`))
	if dbUp < 0 || !strings.Contains(j.Steps[dbUp].If, "inputs.runner != 'vps'") {
		t.Errorf("job %s: make db-up bỏ trên vps (không chạy docker compose up ở đó)", name)
	}
	for _, s := range j.Steps {
		if strings.HasPrefix(s.Uses, "actions/cache@") && !strings.Contains(s.If, "inputs.runner != 'vps'") {
			t.Errorf("job %s: %s không khôi phục cache trên vps (công cụ, cache lint giữ trên máy)", name, s.Name)
		}
		if strings.HasPrefix(s.Uses, "actions/setup-go@") && !strings.Contains(fmt.Sprint(s.With["cache"]), "inputs.runner != 'vps'") {
			t.Errorf("job %s: setup-go không khôi phục Go cache trên vps (đã có trên máy), with.cache = %v", name, s.With["cache"])
		}
	}
}

func checkMaxParts(t *testing.T, plan job) {
	t.Helper()
	if indexOf(plan, regexp.MustCompile(`--max-parts "\$MAX_PARTS"`)) < 0 {
		t.Error("job plan: số phần tối đa lấy từ MAX_PARTS")
	}
	var mp string
	for _, s := range plan.Steps {
		if v, ok := s.Env["MAX_PARTS"]; ok {
			mp = v
		}
	}
	for _, want := range []string{"inputs.runner == 'blacksmith' && '2'", "inputs.runner == 'ubicloud' && '4'", "inputs.runner == 'vps' && '1'", "'6'"} {
		if !strings.Contains(mp, want) {
			t.Errorf("MAX_PARTS thiếu %q: %q", want, mp)
		}
	}
}

// Máy tự cài (vps) không có cache của GitHub: install-tools.sh bỏ cài khi đã có dấu đúng khóa công cụ.
func TestInstallTools_StampSkipsReinstall(t *testing.T) {
	b, err := os.ReadFile("../../scripts/ci/install-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".ci-tools-", "make ci-tools"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("install-tools.sh thiếu %q", want)
		}
	}
	w := readWorkflowFile(t, remoteWorkflow)
	for _, name := range []string{"static", "test"} {
		j := needJob(t, w, name)
		inst := indexOf(j, regexp.MustCompile(`install-tools\.sh .*steps\.toolkey\.outputs\.key`))
		if inst < 0 {
			t.Errorf("job %s: install-tools.sh nhận khóa công cụ (dấu đã cài trên vps)", name)
		}
		// make ci-tools cài jscpd bằng npm; vps không có Node sẵn (lượt 26/11 đỏ "npm: command not found").
		node := slices.IndexFunc(j.Steps, func(s step) bool { return strings.HasPrefix(s.Uses, "actions/setup-node@") })
		if node < 0 || node > inst {
			t.Errorf("job %s: setup-node trước install-tools.sh (npm cho jscpd)", name)
		}
	}
}

// remote-test.sh gửi đúng máy (CRM_CI_RUNNER; mặc định github; mac không phải máy từ xa → mã 2) và ghi sổ phút của
// bộ điều phối (blacksmith ×2, ubicloud ×1).
func TestRemoteTest_RunnerAndLedger(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	if out, code := r.run(t, []string{"CRM_CI_RUNNER=blacksmith"}, "HEAD", base, "ALL"); code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	if !strings.Contains(string(log), "runner=blacksmith") {
		t.Errorf("phải gửi runner=blacksmith:\n%s", log)
	}
	led, _ := os.ReadFile(filepath.Join(r.pool, "crm-runner-minutes.tsv"))
	if !strings.Contains(string(led), "\tblacksmith\t42\t5\t10\t") {
		t.Errorf("sổ phút phải có run 42 blacksmith 5 phút máy, 10 phút tính (4m30s làm tròn lên, ×2):\n%s", led)
	}
	if out, code := r.run(t, []string{"CRM_CI_RUNNER=ubicloud"}, "HEAD", base, "ALL"); code != 0 {
		t.Fatalf("ubicloud: %d\n%s", code, out)
	}
	if log, _ := os.ReadFile(r.log); !strings.Contains(string(log), "runner=ubicloud") {
		t.Errorf("phải gửi runner=ubicloud:\n%s", log)
	}
	if led, _ := os.ReadFile(filepath.Join(r.pool, "crm-runner-minutes.tsv")); !strings.Contains(string(led), "\tubicloud\t42\t5\t5\t") {
		t.Errorf("sổ phút phải có run 42 ubicloud 5 phút máy, 5 phút tính (×1):\n%s", led)
	}
	if out, code := r.run(t, nil, "HEAD", base, "ALL"); code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	if log, _ := os.ReadFile(r.log); !strings.Contains(string(log), "runner=github") {
		t.Errorf("mặc định runner=github:\n%s", log)
	}
	if out, code := r.run(t, []string{"CRM_CI_RUNNER=mac"}, "HEAD", base, "ALL"); code != 2 {
		t.Errorf("CRM_CI_RUNNER=mac: không chạy từ xa, mã 2; có %d:\n%s", code, out)
	}
}

// Run nằm hàng chờ quá hạn (không máy nào nhận: runner tắt, hết chỗ): hủy run, mã 3 để bộ điều phối chuyển bể khác.
func TestRemoteTest_NotStartedInTimeExit3(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	out, code := r.run(t, []string{"FAKE_QUEUED=1", "CRM_CI_START_TIMEOUT=1"}, "HEAD", base, "ALL")
	if code != 3 || !strings.Contains(out, "không bắt đầu") {
		t.Fatalf("run không bắt đầu: muốn mã 3, có %d:\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	if !strings.Contains(string(log), "gh run cancel 42") || strings.Contains(string(log), "run watch") {
		t.Errorf("phải hủy run 42 và không chờ nó:\n%s", log)
	}
}
