package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Task 499 (Chung-CI5, chủ shop 26/11: "gộp m4 vào main phải test full luôn e2e"; máy Mac chỉ làm task, test chạy ở
// bể): test-remote.yml có input e2e bật hai job — e2e (go test -race ./tests/e2e/... trên compose dev đủ profile như
// make dev: PostgreSQL, Jaeger, SeaweedFS hai kho, Valkey, imager, fakecarrier, filesync) và console-e2e (make
// console-e2e: Playwright Chromium mọi spec, WebKit .phone.spec.ts, API giả; trình duyệt cache theo package-lock).
// Không chạy trên vps: dịch vụ ở đó dùng chung, test filesync, dboutage dựng/dừng container.

func e2eJobs(t *testing.T) (workflow, job, job) {
	t.Helper()
	w := readWorkflowFile(t, remoteWorkflow)
	return w, needJob(t, w, "e2e"), needJob(t, w, "console-e2e")
}

func TestRemoteWorkflow_E2EInput(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	d, _ := w.On["workflow_dispatch"].(map[string]any)
	inputs, _ := d["inputs"].(map[string]any)
	in, _ := inputs["e2e"].(map[string]any)
	if in == nil || in["default"] != "false" {
		t.Errorf("input e2e phải có, mặc định 'false' (lượt của task không chạy e2e): %v", in)
	}
	for name, j := range w.Jobs {
		gated := strings.Contains(j.If, "github.event_name == 'workflow_dispatch'") && strings.Contains(j.If, "inputs.e2e == 'true'") &&
			strings.Contains(j.If, "inputs.runner != 'vps'")
		if (name == "e2e" || name == "console-e2e") != gated {
			t.Errorf("job %s: chỉ e2e và console-e2e chạy theo inputs.e2e == 'true' (dispatch, không vps); if đang là %q", name, j.If)
		}
	}
	// ALL của job plan vẫn trừ tests/e2e: e2e chạy ở job riêng có đủ dịch vụ, không chạy hai lần.
	if indexOf(needJob(t, w, "plan"), regexp.MustCompile(`skip='/tests/e2e\$'`)) < 0 {
		t.Error("job plan: ALL vẫn trừ tests/e2e")
	}
}

func TestRemoteWorkflow_E2EJob(t *testing.T) {
	w, j, _ := e2eJobs(t)
	if ro := fmt.Sprint(j.RunsOn); !strings.Contains(ro, "blacksmith-4vcpu-ubuntu-2404") || !strings.Contains(ro, "ubicloud-standard-2") ||
		!strings.Contains(ro, "ubuntu-24.04") || strings.Contains(ro, "elimaz-vps") {
		t.Errorf("job e2e: runs-on theo inputs.runner (blacksmith, ubicloud, github; không vps): %s", ro)
	}
	if !slices.ContainsFunc(j.Steps, func(s step) bool { return strings.HasPrefix(s.Uses, "actions/setup-go@") }) {
		t.Error("job e2e: setup-go")
	}
	dev := indexOf(j, regexp.MustCompile(`^make dev\b`))
	pull := indexOf(j, regexp.MustCompile(`--profile valkey pull\b.*valkey-cache`))
	run := indexOf(j, regexp.MustCompile(`^make .*test-part RACE=\./tests/e2e/\.\.\.`))
	if dev < 0 || pull < 0 || run < 0 || dev > run || pull > run {
		t.Errorf("job e2e: make dev (%d), kéo sẵn image Valkey (%d) rồi make test-part RACE=./tests/e2e/... (%d)", dev, pull, run)
	}
	// make dev bật đủ profile mà tests/e2e cần (imager cho files_pipeline, filesync cho filesync, valkey).
	out := makeDry(t, "dev")
	for _, want := range []string{"--profile imager", "--profile fakecarrier", "--profile filesync", "--profile valkey", "up -d --wait"} {
		if !strings.Contains(out, want) {
			t.Errorf("make dev thiếu %q:\n%s", want, out)
		}
	}
	// TestFileSync gọi `docker compose -f docker/compose.dev.yml run` không có -p, filestest hỏi cổng theo
	// COMPOSE_PROJECT_NAME: phải trùng dự án compose mà make dev dựng.
	// env của job không đọc được ngữ cảnh env (GitHub từ chối dispatch "Unrecognized named-value: 'env'", lượt 27/11).
	if v := j.Env["COMPOSE_PROJECT_NAME"]; v == "" || v != w.Env["COMPOSE_PROJECT"] {
		t.Errorf("job e2e: COMPOSE_PROJECT_NAME phải là dự án compose của make dev (%q), đang là %q", w.Env["COMPOSE_PROJECT"], v)
	}
	for name, jj := range w.Jobs {
		for k, v := range jj.Env {
			if strings.Contains(v, "env.") {
				t.Errorf("job %s: env %s = %q dùng ngữ cảnh env (GitHub không cho ở env của job)", name, k, v)
			}
		}
	}
	if run >= 0 && !strings.Contains(j.Steps[run].Run, "GITHUB_STEP_SUMMARY") {
		t.Error("job e2e: test đỏ và thời gian phải vào tóm tắt của run")
	}
	if j.Timeout < 20 {
		t.Errorf("job e2e: timeout-minutes %d quá ngắn cho dựng image imager và cả gói e2e", j.Timeout)
	}
}

func TestRemoteWorkflow_ConsoleE2EJob(t *testing.T) {
	_, _, j := e2eJobs(t)
	if ro := fmt.Sprint(j.RunsOn); !strings.Contains(ro, "blacksmith-4vcpu-ubuntu-2404") || !strings.Contains(ro, "ubuntu-24.04") ||
		strings.Contains(ro, "elimaz-vps") {
		t.Errorf("job console-e2e: runs-on theo inputs.runner (không vps): %s", ro)
	}
	node := slices.IndexFunc(j.Steps, func(s step) bool { return strings.HasPrefix(s.Uses, "actions/setup-node@") })
	cache := slices.IndexFunc(j.Steps, func(s step) bool {
		return strings.HasPrefix(s.Uses, "actions/cache@") && strings.Contains(fmt.Sprint(s.With["path"]), "ms-playwright")
	})
	deps := indexOf(j, regexp.MustCompile(`npx playwright install-deps chromium webkit`))
	run := indexOf(j, regexp.MustCompile(`^make .*console-e2e\b`))
	if node < 0 || cache < 0 || deps < 0 || run < 0 || node > run || cache > run || deps > run {
		t.Errorf("job console-e2e: setup-node (%d), cache trình duyệt (%d), thư viện hệ thống (%d) rồi make console-e2e (%d)", node, cache, deps, run)
	}
	if cache >= 0 {
		if k := fmt.Sprint(j.Steps[cache].With["key"]); !strings.Contains(k, "hashFiles('console/package-lock.json')") {
			t.Errorf("khóa cache trình duyệt theo phiên bản Playwright ghim trong console/package-lock.json: %q", k)
		}
	}
	// Lệnh Playwright của máy dev và CI chung một nguồn: make console-e2e cài đủ ba trình duyệt rồi chạy mọi project.
	out := makeDry(t, "console-e2e")
	for _, want := range []string{"npx playwright install chromium chromium-headless-shell webkit", "npx playwright test"} {
		if !strings.Contains(out, want) {
			t.Errorf("make console-e2e thiếu %q:\n%s", want, out)
		}
	}
	if run >= 0 && !strings.Contains(j.Steps[run].Run, "GITHUB_STEP_SUMMARY") {
		t.Error("job console-e2e: spec đỏ phải vào tóm tắt của run")
	}
}

// Playwright và tests/e2e chỉ ở hai job e2e; static, plan, test, loadtest không chạy chúng (giữ phút của lượt task).
func TestRemoteWorkflow_E2EOnlyInE2EJobs(t *testing.T) {
	w := readWorkflowFile(t, remoteWorkflow)
	for name, j := range w.Jobs {
		if name == "e2e" || name == "console-e2e" {
			continue
		}
		for _, s := range j.Steps {
			for _, banned := range []string{"playwright", "console-e2e", "RACE=./tests/e2e", "make dev"} {
				if strings.Contains(s.Run, banned) {
					t.Errorf("job %s, bước %q: có %q (chỉ ở job e2e, console-e2e)", name, s.Name, banned)
				}
			}
		}
	}
}

// remote-test.sh: CRM_CI_E2E=1 gửi e2e=true; mặc định e2e=false; giá trị lạ hay vps (job e2e không chạy ở đó → xanh
// giả) là mã 2, không chạy workflow.
func TestRemoteTest_E2EInput(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	if out, code := r.run(t, []string{"CRM_CI_E2E=1", "CRM_CI_RUNNER=blacksmith"}, "HEAD", base, "ALL", "ALL"); code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	if log, _ := os.ReadFile(r.log); !strings.Contains(string(log), "-f e2e=true") {
		t.Errorf("CRM_CI_E2E=1 phải gửi e2e=true:\n%s", log)
	}
	if out, code := r.run(t, nil, "HEAD", base, "ALL"); code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	if last := string(log)[strings.LastIndex(string(log), "workflow run"):]; !strings.Contains(last, "-f e2e=false") {
		t.Errorf("mặc định e2e=false:\n%s", last)
	}
	before := strings.Count(string(log), "workflow run")
	for _, extra := range [][]string{{"CRM_CI_E2E=yes"}, {"CRM_CI_E2E=1", "CRM_CI_RUNNER=vps"}} {
		if out, code := r.run(t, extra, "HEAD", base, "ALL"); code != 2 {
			t.Errorf("%v: mã 2, có %d:\n%s", extra, code, out)
		}
	}
	if log, _ := os.ReadFile(r.log); strings.Count(string(log), "workflow run") != before {
		t.Errorf("sai tham số thì không chạy workflow:\n%s", log)
	}
}

// Spec Playwright đỏ: in dòng tên spec của job console-e2e (không chỉ dòng --- FAIL của go test).
func TestRemoteTest_E2EFailuresShown(t *testing.T) {
	r := newRemoteEnv(t)
	base := git(t, r.dir, "rev-parse", "HEAD")
	out, code := r.run(t, []string{"FAKE_RESULT=failure", "FAKE_PW_FAIL=1", "CRM_CI_E2E=1"}, "HEAD", base, "ALL")
	if code != 1 {
		t.Fatalf("đỏ: mã 1, có %d:\n%s", code, out)
	}
	for _, want := range []string{"[chromium] › e2e/don.spec.ts:12:3 › lên đơn", "1 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("phải in %q của job console-e2e:\n%s", want, out)
		}
	}
	if strings.Contains(out, "passed (") {
		t.Errorf("không in dòng spec qua:\n%s", out)
	}
}

// merge-test.sh với CRM_CI_E2E=1: toàn bộ + e2e trên một bản bằng một lệnh, ở bể từ xa; không bao giờ ở Mac (chủ shop:
// máy Mac chỉ làm task) hay vps (job e2e bỏ qua ở đó).
func TestMergeTest_E2E(t *testing.T) {
	r := newRemoteEnv(t)
	out, code := r.mergeTest(t, []string{"CRM_CI_E2E=1"}, "HEAD", "HEAD")
	if code != 0 || !strings.Contains(out, "MERGE-TEST XANH (blacksmith)") {
		t.Fatalf("e2e ở chỗ trống nhanh nhất (blacksmith): %d\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	for _, want := range []string{"runner=blacksmith", "packages=ALL", "race_packages=ALL", "static=true", "-f e2e=true"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("gh thiếu %q:\n%s", want, log)
		}
	}
	// HEAD sạch nhưng không chạy được từ xa: e2e không lùi về Mac.
	if out, code := r.mergeTest(t, []string{"CRM_CI_E2E=1", "CRM_CI_REMOTE=0"}, "HEAD", "HEAD"); code != 3 || strings.Contains(out, "bể máy: mac") {
		t.Errorf("CRM_CI_E2E=1, không chạy được từ xa: mã 3, không chạy mac; có %d\n%s", code, out)
	}
	if out, code := r.mergeTest(t, []string{"CRM_CI_E2E=1", "CRM_CI_RUNNER=mac"}, "HEAD", "HEAD"); code != 2 {
		t.Errorf("CRM_CI_E2E=1 ép mac: mã 2, có %d\n%s", code, out)
	}
	// Mọi bể từ xa thường đều hỏng, vps bật lại, tệp gói nhỏ: lượt e2e không gửi vps, không dự phòng Mac → mã 3.
	pe := poolEnv{dir: r.pool}
	for _, p := range []string{"blacksmith", "ubicloud", "github"} {
		pe.markDown(t, p, time.Now().Add(time.Hour))
	}
	one := r.pkgFile(t, "./a\n")
	out, code = r.mergeTest(t, []string{"CRM_CI_E2E=1", "CRM_CI_POOL_SLOTS=vps=1"}, "HEAD", "HEAD", one, one)
	if code != 3 || strings.Contains(out, "bể máy: vps") || strings.Contains(out, "bể máy: mac") {
		t.Errorf("e2e: không vps, không mac → mã 3; có %d\n%s", code, out)
	}
}
