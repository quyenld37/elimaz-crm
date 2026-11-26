package deploy_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Task 494 (Chung-CI2, chủ shop 26/11: "các task nào làm song song mà cần test thì cứ chia cho các máy này chạy song
// song"): scripts/ci/pool.py chọn chỗ trống theo thứ tự nhanh nhất blacksmith (4 chỗ) → mac (1, chung khóa với
// run-in-slot.py) → github (3) → vps (1, chỉ ≤ 15 gói và không ALL); giữ chỗ bằng flock; sổ phút theo tháng; bể lỗi hạ
// tầng (mã 3) tạm hỏng 30 phút; CRM_CI_RUNNER ép bể.
// Task 495 (Chung-CI3, chủ shop 26/11: "máy t sử dụng để xử lý các task, còn chạy test thì dùng trong bể"): thứ tự mới
// blacksmith (4) → ubicloud (3, hết ở 1.100 phút tính ×1) → github (3) → vps (1); mac 0 chỗ trong lượt chọn bình thường,
// chỉ là dự phòng cuối (--local-only, hay mọi bể từ xa đều bị loại) và khi đó in "== DỰ PHÒNG CUỐI: chạy trên máy Mac".
// Task 498 (Chung-CI4, chủ shop 26/11: "thằng vps chậm thế chắc ignore đi"): vps mặc định 0 chỗ, không bao giờ được
// chọn; CRM_CI_POOL_SLOTS=vps=1 bật lại (test luật vps dưới đây bật nó).

const lastResort = "== DỰ PHÒNG CUỐI: chạy trên máy Mac (lý do: "

// vpsOn bật lại bể vps (mặc định 0 chỗ từ Task 498) cho test luật của vps.
const vpsOn = "CRM_CI_POOL_SLOTS=vps=1"

// remotePools: các bể từ xa và số chỗ khi bật (vps mặc định 0, Task 498).
func remotePools() map[string]int {
	return map[string]int{"blacksmith": 4, "ubicloud": 3, "github": 3, "vps": 1}
}

// downAllRemote đánh dấu mọi bể từ xa tạm hỏng một giờ.
func (p poolEnv) downAllRemote(t *testing.T) {
	t.Helper()
	for pool := range remotePools() {
		p.markDown(t, pool, time.Now().Add(time.Hour))
	}
}

type poolEnv struct {
	dir, slotLock string
	env           []string
}

func newPoolEnv(t *testing.T) poolEnv {
	t.Helper()
	dir := t.TempDir()
	slot := filepath.Join(dir, "crm-test-slot.lock")
	return poolEnv{dir: dir, slotLock: slot, env: []string{
		"CRM_CI_POOL_DIR=" + dir, "CRM_TEST_SLOT_LOCK=" + slot, "CRM_CI_POOL_POLL=0.1",
	}}
}

// poolCmd dựng lệnh pool.py; bỏ biến của phiên (CRM_CI_RUNNER, các dấu đang giữ lượt) để test không phụ thuộc phiên.
func (p poolEnv) poolCmd(t *testing.T, extra []string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "python3", append([]string{filepath.Join(repoRoot(t), "scripts/ci/pool.py")}, args...)...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		for _, k := range []string{"CRM_CI_RUNNER=", "CRM_CI_POOL_", "CRM_TEST_SLOT_", "CRM_CI_DOWN_"} {
			if strings.HasPrefix(kv, k) {
				return true
			}
		}
		return false
	}), p.env...), extra...)
	return cmd
}

func (p poolEnv) run(t *testing.T, extra []string, args ...string) (string, int) {
	t.Helper()
	return outputAndCode(t, p.poolCmd(t, extra, args...))
}

// outputAndCode chạy lệnh, trả đầu ra gộp và mã thoát (lỗi khác mã thoát làm test dừng).
func outputAndCode(t *testing.T, cmd *exec.Cmd) (string, int) {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return string(out), ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// syncBuf: đầu ra của lệnh chạy nền, đọc được trong lúc lệnh còn ghi (-race: strings.Builder không an toàn).
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p) //nolint:wrapcheck // strings.Builder.Write không bao giờ trả lỗi
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startBg chạy lệnh nền, đầu ra vào một syncBuf.
func startBg(t *testing.T, cmd *exec.Cmd) *syncBuf {
	t.Helper()
	buf := &syncBuf{}
	cmd.Stdout, cmd.Stderr = buf, buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return buf
}

// echoRunner: lệnh con in bể nó được giao và dấu giữ lượt máy.
func echoRunner() []string {
	return []string{"--", "sh", "-c", `echo "RUNNER=$CRM_CI_RUNNER HELD=${CRM_TEST_SLOT_HELD:-} POOL=${CRM_CI_POOL_HELD:-}"`}
}

func gotRunner(t *testing.T, out string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if r, ok := strings.CutPrefix(line, "RUNNER="); ok {
			name, _, _ := strings.Cut(r, " ")
			return name
		}
	}
	t.Fatalf("lệnh con không chạy:\n%s", out)
	return ""
}

func (p poolEnv) lockPath(pool string, n int) string {
	if pool == "mac" {
		return p.slotLock
	}
	return filepath.Join(p.dir, fmt.Sprintf("crm-runner-%s.%d.lock", pool, n))
}

// hold giữ khóa flock của một chỗ (như một phiên khác đang chạy ở đó); trả hàm nhả.
func hold(t *testing.T, path string) func() {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("giữ %s: %v", path, err)
	}
	var once sync.Once
	release := func() { once.Do(func() { _ = f.Close() }) }
	t.Cleanup(release)
	return release
}

func (p poolEnv) holdAll(t *testing.T, pool string, n int) []func() {
	t.Helper()
	var rel []func()
	for i := 1; i <= n; i++ {
		rel = append(rel, hold(t, p.lockPath(pool, i)))
	}
	return rel
}

func (p poolEnv) markDown(t *testing.T, pool string, until time.Time) {
	t.Helper()
	b := fmt.Sprintf("%d thử\n", until.Unix())
	if err := os.WriteFile(filepath.Join(p.dir, "crm-runner-"+pool+".down"), []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (p poolEnv) ledger(t *testing.T, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.dir, "crm-runner-minutes.tsv"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPool_FastestFreeFirst(t *testing.T) {
	p := newPoolEnv(t)
	p.env = append(p.env, vpsOn)
	steps := []struct {
		name, want string
		hold       func()
	}{
		{"trống hết: blacksmith", "blacksmith", func() {}},
		{"blacksmith đủ 4 chỗ: ubicloud (không phải mac)", "ubicloud", func() { p.holdAll(t, "blacksmith", 4) }},
		{"ubicloud đủ 3 chỗ: github", "github", func() { p.holdAll(t, "ubicloud", 3) }},
		{"github đủ 3 chỗ, 3 gói, vps bật: vps", "vps", func() { p.holdAll(t, "github", 3) }},
	}
	for _, s := range steps {
		s.hold()
		out, code := p.run(t, nil, append([]string{"run", "--count", "3"}, echoRunner()...)...)
		if code != 0 {
			t.Fatalf("%s: thoát %d:\n%s", s.name, code, out)
		}
		if got := gotRunner(t, out); got != s.want {
			t.Errorf("%s: chọn %q, muốn %q:\n%s", s.name, got, s.want, out)
		}
		if strings.Contains(out, lastResort) {
			t.Errorf("%s: còn bể từ xa thì không phải dự phòng cuối:\n%s", s.name, out)
		}
	}
}

// Hết chỗ: chờ chỗ ĐẦU TIÊN trống ở bất kỳ bể nào (không chờ một bể cố định).
func TestPool_WaitsForFirstFreeSlot(t *testing.T) {
	p := newPoolEnv(t)
	p.holdAll(t, "blacksmith", 4)
	p.holdAll(t, "ubicloud", 3)
	gh := p.holdAll(t, "github", 3)
	cmd := p.poolCmd(t, nil, append([]string{"run", "--all"}, echoRunner()...)...)
	buf := startBg(t, cmd)
	time.Sleep(700 * time.Millisecond)
	gh[1]()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	out := buf.String()
	if got := gotRunner(t, out); got != "github" {
		t.Errorf("chỗ github vừa trống phải được lấy, có %q:\n%s", got, out)
	}
	if !strings.Contains(out, "== chờ lượt test của máy") {
		t.Errorf("phải báo đang chờ (dòng điều phối viên dò):\n%s", out)
	}
	if strings.Contains(out, lastResort) {
		t.Errorf("bể từ xa chỉ đầy (không hỏng): chờ, không rơi về mac:\n%s", out)
	}
}

// vps (3 lõi, toàn bộ 40 phút) khi được bật chỉ nhận tập gói ≤ 15 và không ALL; không bể nào nhận được thì mã 3.
func TestPool_VPSOnlySmallSets(t *testing.T) {
	p := newPoolEnv(t)
	p.env = append(p.env, vpsOn)
	p.markDown(t, "blacksmith", time.Now().Add(time.Hour))
	p.markDown(t, "ubicloud", time.Now().Add(time.Hour))
	p.markDown(t, "github", time.Now().Add(time.Hour))
	cases := []struct {
		args []string
		want string
		code int
	}{
		{[]string{"--count", "15"}, "vps", 0},
		{[]string{"--count", "16"}, "", 3},
		{[]string{"--all"}, "", 3},
		{nil, "", 3}, // không biết số gói: không gửi vps
	}
	for _, c := range cases {
		out, code := p.run(t, nil, append(append([]string{"run", "--no-mac"}, c.args...), echoRunner()...)...)
		if code != c.code {
			t.Errorf("%v: thoát %d, muốn %d:\n%s", c.args, code, c.code, out)
			continue
		}
		if c.want != "" && gotRunner(t, out) != c.want {
			t.Errorf("%v: muốn %s:\n%s", c.args, c.want, out)
		}
		if c.code == 3 && strings.Contains(out, "RUNNER=") {
			t.Errorf("%v: không bể nào nhận thì không chạy lệnh con:\n%s", c.args, out)
		}
	}
}

// Task 498: vps mặc định tắt — không được chọn kể cả khi mọi bể khác bị loại và tập gói nhỏ (báo lý do bỏ, cách bật);
// CRM_CI_POOL_SLOTS=vps=1 bật lại; ép CRM_CI_RUNNER=vps khi đang tắt: mã 2, không chạy; status báo 0 chỗ.
func TestPool_VPSOffByDefault(t *testing.T) {
	p := newPoolEnv(t)
	for _, pool := range []string{"blacksmith", "ubicloud", "github"} {
		p.markDown(t, pool, time.Now().Add(time.Hour))
	}
	out, code := p.run(t, nil, append([]string{"run", "--no-mac", "--count", "1"}, echoRunner()...)...)
	if code != 3 || strings.Contains(out, "RUNNER=") || !strings.Contains(out, "bỏ bể vps: tắt mặc định ("+vpsOn+" bật lại)") {
		t.Errorf("vps tắt mặc định: không chọn vps dù 1 gói và mọi bể khác hỏng, báo lý do: %d\n%s", code, out)
	}
	out, code = p.run(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "mac" || !strings.Contains(out, lastResort) {
		t.Errorf("vps tắt, mọi bể khác hỏng: mac dự phòng cuối, không vps: %d\n%s", code, out)
	}
	out, code = p.run(t, []string{vpsOn}, append([]string{"run", "--no-mac", "--count", "1"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "vps" {
		t.Errorf("%s: chọn lại vps: %d\n%s", vpsOn, code, out)
	}
	out, code = p.run(t, []string{"CRM_CI_RUNNER=vps"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if code != 2 || strings.Contains(out, "bể máy:") || !strings.Contains(out, vpsOn) {
		t.Errorf("ép vps khi đang tắt: mã 2, không chạy, chỉ cách bật: %d\n%s", code, out)
	}
	out, _ = p.run(t, nil, "status")
	if !strings.Contains(out, "vps: 0/0 chỗ bận") {
		t.Errorf("status: vps 0 chỗ:\n%s", out)
	}
}

// CRM_CI_RUNNER ép đúng một bể: bỏ qua tạm hỏng, sổ phút, luật vps; bể đó đầy thì chờ chính nó.
func TestPool_ForcedRunner(t *testing.T) {
	p := newPoolEnv(t)
	p.markDown(t, "github", time.Now().Add(time.Hour))
	out, code := p.run(t, []string{"CRM_CI_RUNNER=vps", vpsOn}, append([]string{"run", "--all"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "vps" {
		t.Errorf("ép vps (đã bật) với ALL: %d\n%s", code, out)
	}
	out, _ = p.run(t, []string{"CRM_CI_RUNNER=github"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "github" {
		t.Errorf("ép github (đang đánh dấu hỏng) vẫn chạy github:\n%s", out)
	}
	gh := p.holdAll(t, "github", 3)
	cmd := p.poolCmd(t, []string{"CRM_CI_RUNNER=github"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	buf := startBg(t, cmd)
	time.Sleep(500 * time.Millisecond)
	if strings.Contains(buf.String(), "RUNNER=") {
		t.Errorf("ép github mà github đầy: phải chờ, không chạy bể khác (blacksmith trống):\n%s", buf.String())
	}
	gh[0]()
	if err := cmd.Wait(); err != nil || gotRunner(t, buf.String()) != "github" {
		t.Errorf("%v\n%s", err, buf.String())
	}
	// mac mặc định 0 chỗ trong lượt chọn, nhưng ép mac (người gõ) vẫn chạy ở mac, không phải dòng dự phòng cuối.
	out, code = p.run(t, []string{"CRM_CI_RUNNER=mac"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "mac" || strings.Contains(out, lastResort) {
		t.Errorf("ép mac: chạy mac, không in dự phòng cuối: %d\n%s", code, out)
	}
	out, _ = p.run(t, []string{"CRM_CI_RUNNER=ubicloud"}, append([]string{"run", "--all"}, echoRunner()...)...)
	if gotRunner(t, out) != "ubicloud" {
		t.Errorf("ép ubicloud:\n%s", out)
	}
	if out, code := p.run(t, []string{"CRM_CI_RUNNER=azure"}, append([]string{"run"}, echoRunner()...)...); code != 2 {
		t.Errorf("bể không có: mã 2, có %d:\n%s", code, out)
	}
	// --local-only (CRM_CI_REMOTE=0, không gh) thắng ép bể từ xa: chạy máy.
	out, _ = p.run(t, []string{"CRM_CI_RUNNER=github"}, append([]string{"run", "--local-only"}, echoRunner()...)...)
	if gotRunner(t, out) != "mac" {
		t.Errorf("--local-only phải chạy mac:\n%s", out)
	}
}

// Sổ phút theo tháng (UTC): blacksmith tính ×2 (4 vCPU), hết khi ≥ 2.700; ubicloud ×1, hết khi ≥ 1.100 (chủ shop không
// muốn trả tiền: cutoff 1 USD ở console không chặn cứng); github ≥ 1.600; tháng trước không tính.
func TestPool_MonthlyMinutesBudget(t *testing.T) {
	month := time.Now().UTC().Format("2006-01")
	last := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")
	p := newPoolEnv(t)
	p.ledger(t, last+"\tblacksmith\t1\t5000\t10000\tx", month+"\tblacksmith\t2\t1349\t2698\tx",
		last+"\tubicloud\t3\t5000\t5000\tx", month+"\tubicloud\t4\t1099\t1099\tx")
	out, _ := p.run(t, nil, append([]string{"run", "--count", "2"}, echoRunner()...)...)
	if gotRunner(t, out) != "blacksmith" {
		t.Errorf("2.698 phút tháng này (tháng trước không tính): còn blacksmith:\n%s", out)
	}
	// record: 1 phút máy blacksmith = 2 phút tính → 2.700: hết.
	if out, code := p.run(t, nil, "record", "--runner", "blacksmith", "--run", "77", "--minutes", "1"); code != 0 {
		t.Fatalf("record: %d\n%s", code, out)
	}
	b, _ := os.ReadFile(filepath.Join(p.dir, "crm-runner-minutes.tsv"))
	if !strings.Contains(string(b), month+"\tblacksmith\t77\t1\t2\t") {
		t.Errorf("sổ phải ghi tháng, bể, run, phút máy, phút tính (×2):\n%s", b)
	}
	out, _ = p.run(t, nil, append([]string{"run", "--count", "2"}, echoRunner()...)...)
	if gotRunner(t, out) != "ubicloud" {
		t.Errorf("blacksmith đã 2.700 phút: bỏ qua; ubicloud 1.099 < 1.100 (tháng trước không tính): ubicloud:\n%s", out)
	}
	// ubicloud: 1 phút máy = 1 phút tính → 1.100: hết.
	if out, code := p.run(t, nil, "record", "--runner", "ubicloud", "--run", "80", "--minutes", "1"); code != 0 {
		t.Fatalf("record ubicloud: %d\n%s", code, out)
	}
	b, _ = os.ReadFile(filepath.Join(p.dir, "crm-runner-minutes.tsv"))
	if !strings.Contains(string(b), month+"\tubicloud\t80\t1\t1\t") {
		t.Errorf("ubicloud tính ×1:\n%s", b)
	}
	out, _ = p.run(t, nil, append([]string{"run", "--count", "2"}, echoRunner()...)...)
	if gotRunner(t, out) != "github" || !strings.Contains(out, "bỏ bể ubicloud: hết phút tháng này (1100 ≥ 1100)") {
		t.Errorf("ubicloud đã 1.100 phút: bỏ qua (báo lý do), chọn github (không phải mac):\n%s", out)
	}
	if _, code := p.run(t, nil, "record", "--runner", "github", "--run", "78", "--minutes", "1600"); code != 0 {
		t.Fatal("record github")
	}
	out, _ = p.run(t, []string{vpsOn}, append([]string{"run", "--count", "2"}, echoRunner()...)...)
	if gotRunner(t, out) != "vps" {
		t.Errorf("blacksmith, ubicloud, github hết phút, vps bật: vps:\n%s", out)
	}
	// ALL (vps không nhận) mà mọi bể khác hết phút: dự phòng cuối trên mac, nói rõ lý do.
	out, _ = p.run(t, nil, append([]string{"run", "--all"}, echoRunner()...)...)
	if gotRunner(t, out) != "mac" || !strings.Contains(out, lastResort) || !strings.Contains(out, "ubicloud: hết phút") {
		t.Errorf("mọi bể từ xa bị loại: mac dự phòng cuối, có lý do:\n%s", out)
	}
	if out, code := p.run(t, nil, "record", "--runner", "vps", "--run", "79", "--minutes", "40"); code != 0 {
		t.Fatalf("record vps: %s", out)
	}
	out, _ = p.run(t, nil, "status")
	for _, want := range []string{"blacksmith", "2700", "ubicloud", "1100/1100", "github", "1600", "vps", "mac", "dự phòng cuối"} {
		if !strings.Contains(out, want) {
			t.Errorf("status thiếu %q:\n%s", want, out)
		}
	}
}

// Lệnh con thoát 3 (không chạy được từ xa: run không bắt đầu, gh lỗi) trên bể từ xa: đánh dấu tạm hỏng 30 phút, chọn bể
// khác ngay. Mã 1 (đỏ thật) không thử lại. Hết hạn hỏng thì bể dùng lại được.
func TestPool_InfraErrorMarksDownAndMovesOn(t *testing.T) {
	p := newPoolEnv(t)
	child := []string{"--", "sh", "-c", `echo "RUNNER=$CRM_CI_RUNNER"; [ "$CRM_CI_RUNNER" = blacksmith ] && exit 3; exit 0`}
	out, code := p.run(t, nil, append([]string{"run", "--count", "1"}, child...)...)
	if code != 0 || !strings.Contains(out, "RUNNER=blacksmith") || !strings.Contains(out, "RUNNER=ubicloud") {
		t.Fatalf("blacksmith lỗi hạ tầng → chuyển ubicloud: %d\n%s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(p.dir, "crm-runner-blacksmith.down"))
	if err != nil {
		t.Fatalf("phải đánh dấu blacksmith tạm hỏng: %v", err)
	}
	until, _ := strconv.ParseInt(strings.Fields(string(b))[0], 10, 64)
	if d := time.Until(time.Unix(until, 0)); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("tạm hỏng 30 phút, có %s", d)
	}
	out, _ = p.run(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "ubicloud" {
		t.Errorf("blacksmith đang tạm hỏng: bỏ qua:\n%s", out)
	}
	p.markDown(t, "blacksmith", time.Now().Add(-time.Minute))
	out, _ = p.run(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "blacksmith" {
		t.Errorf("hết hạn hỏng: dùng lại blacksmith:\n%s", out)
	}
	red := []string{"--", "sh", "-c", `echo "RUNNER=$CRM_CI_RUNNER"; exit 1`}
	out, code = p.run(t, nil, append([]string{"run", "--count", "1"}, red...)...)
	if code != 1 || strings.Count(out, "RUNNER=") != 1 {
		t.Errorf("đỏ thật (mã 1): trả 1, không thử bể khác: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(p.dir, "crm-runner-blacksmith.down")); err == nil {
		if b, _ := os.ReadFile(filepath.Join(p.dir, "crm-runner-blacksmith.down")); strings.HasPrefix(string(b), strconv.FormatInt(until, 10)) {
			t.Error("mã 1 không được đánh dấu hỏng")
		}
	}
	// Mọi bể từ xa lỗi hạ tầng, mac bận chỗ khác không có: cuối cùng trả 3, không lặp vô hạn.
	q := newPoolEnv(t)
	all3 := []string{"--", "sh", "-c", `echo "RUNNER=$CRM_CI_RUNNER"; exit 3`}
	out, code = q.run(t, nil, append([]string{"run", "--no-mac", "--count", "1"}, all3...)...)
	if code != 3 || strings.Count(out, "RUNNER=") != 3 || strings.Contains(out, "RUNNER=vps") {
		t.Errorf("blacksmith, ubicloud, github đều mã 3 (vps tắt): thử mỗi bể một lần rồi trả 3: %d\n%s", code, out)
	}
	// Không --no-mac: mọi bể từ xa lỗi hạ tầng → dự phòng cuối trên mac đúng một lần, rồi trả mã của mac.
	z := newPoolEnv(t)
	remote3 := []string{"--", "sh", "-c", `echo "RUNNER=$CRM_CI_RUNNER"; [ "$CRM_CI_RUNNER" = mac ] && exit 0; exit 3`}
	out, code = z.run(t, nil, append([]string{"run", "--count", "1"}, remote3...)...)
	if code != 0 || strings.Count(out, "RUNNER=") != 4 || !strings.Contains(out, "RUNNER=mac") ||
		!strings.Contains(out, lastResort) || !strings.Contains(out, "vừa lỗi hạ tầng") {
		t.Errorf("ba bể từ xa đang bật mã 3 → mac dự phòng cuối (in lý do): %d\n%s", code, out)
	}
	out, code = z.run(t, nil, append([]string{"run", "--count", "1"}, all3...)...)
	if code != 3 || strings.Count(out, "RUNNER=") != 1 || !strings.Contains(out, "RUNNER=mac") {
		t.Errorf("mac dự phòng cũng mã 3: trả 3, không lặp (bể từ xa đang tạm hỏng): %d\n%s", code, out)
	}
}

// Nhiều phiên cùng lúc: không bể nào vượt số chỗ; mọi lượt đều chạy xong.
func TestPool_ConcurrentNeverExceedsSlots(t *testing.T) {
	p := newPoolEnv(t)
	log := filepath.Join(t.TempDir(), "log")
	child := []string{"--", "sh", "-c", `echo "+ $CRM_CI_RUNNER" >> "$LOG"; sleep 1; echo "- $CRM_CI_RUNNER" >> "$LOG"`}
	var wg sync.WaitGroup
	errs := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			if out, code := p.run(t, []string{"LOG=" + log}, append([]string{"run", "--count", "20"}, child...)...); code != 0 {
				errs <- out
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("một lượt lỗi:\n%s", e)
	}
	f, err := os.Open(log)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	// mac 0: bể từ xa chỉ đầy (không hỏng) thì chờ, không bao giờ chạy test ở máy dev.
	limit := map[string]int{"blacksmith": 4, "ubicloud": 3, "mac": 0, "github": 3, "vps": 0}
	cur, peak, n := map[string]int{}, map[string]int{}, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		op, r, _ := strings.Cut(sc.Text(), " ")
		if op == "+" {
			cur[r]++
			n++
			peak[r] = max(peak[r], cur[r])
		} else {
			cur[r]--
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 12 {
		t.Errorf("12 lượt phải chạy đủ, có %d", n)
	}
	for r, v := range peak {
		if v > limit[r] {
			t.Errorf("bể %s chạy %d lượt cùng lúc, tối đa %d (20 gói: không vps)", r, v, limit[r])
		}
	}
	if peak["blacksmith"] != 4 {
		t.Errorf("12 lượt cùng lúc phải lấp đủ 4 chỗ blacksmith, có %v", peak)
	}
}

// Tiến trình giữ chỗ chết (kill -9 cả nhóm): hệ điều hành nhả khóa, chỗ dùng lại được ngay.
func TestPool_SlotFreedWhenProcessDies(t *testing.T) {
	p := newPoolEnv(t)
	p.holdAll(t, "blacksmith", 3)
	cmd := p.poolCmd(t, nil, "run", "--count", "1", "--", "sh", "-c", "echo RUNNER=$CRM_CI_RUNNER; exec sleep 60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	buf := startBg(t, cmd)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), "RUNNER=blacksmith") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	// Khi lệnh con còn chạy, chỗ 4 vẫn bị giữ: lượt mới sang ubicloud.
	out, _ := p.run(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "ubicloud" {
		t.Errorf("chỗ thứ 4 đang giữ: phải sang ubicloud:\n%s\n%s", out, buf.String())
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
	out, _ = p.run(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "blacksmith" {
		t.Errorf("tiến trình chết: chỗ blacksmith phải trống lại:\n%s", out)
	}
}

// Lồng nhau: lệnh chạy bên trong một chỗ đã giữ (CRM_CI_POOL_HELD=1) chạy luôn, giữ bể đã chọn.
func TestPool_NestedRunsDirectly(t *testing.T) {
	p := newPoolEnv(t)
	p.holdAll(t, "github", 3)
	out, code := p.run(t, []string{"CRM_CI_POOL_HELD=1", "CRM_CI_RUNNER=github"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "github" {
		t.Errorf("lồng nhau phải chạy ngay ở bể đã giữ: %d\n%s", code, out)
	}
}

// ---- merge-test.sh: bản đã gộp của điều phối viên qua cùng bộ điều phối

func (r remoteEnv) mergeTest(t *testing.T, extra []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{filepath.Join(repoRoot(t), "scripts/ci/merge-test.sh")}, args...)...)
	cmd.Dir = r.dir
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		for _, k := range []string{"CRM_CI_RUNNER=", "CRM_CI_POOL_", "CRM_TEST_SLOT_", "CRM_CI_REMOTE=", "CRM_CI_LABEL="} {
			if strings.HasPrefix(kv, k) {
				return true
			}
		}
		return false
	})
	cmd.Env = append(append(append(env, "CRM_CI_POOL_DIR="+r.pool, "CRM_TEST_SLOT_LOCK="+filepath.Join(r.pool, "slot.lock"),
		"CRM_CI_POOL_POLL=0.1"), r.env...), extra...)
	return outputAndCode(t, cmd)
}

func TestMergeTest_RunsMergedCommitThroughPool(t *testing.T) {
	r := newRemoteEnv(t)
	commit(t, r.dir, "b.txt")
	before := git(t, r.dir, "rev-parse", "HEAD")
	git(t, r.dir, "checkout", "-q", "-b", "task-1", "HEAD~1")
	commit(t, r.dir, "c.txt")
	git(t, r.dir, "checkout", "-q", "main")
	git(t, r.dir, "merge", "-q", "--no-ff", "-m", "gộp task-1", "task-1")
	merged := git(t, r.dir, "rev-parse", "HEAD")
	out, code := r.mergeTest(t, nil, merged)
	if code != 0 || !strings.Contains(out, "MERGE-TEST XANH (blacksmith)") {
		t.Fatalf("bản gộp xanh trên chỗ trống nhanh nhất (blacksmith): %d\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	for _, want := range []string{"runner=blacksmith", "packages=ALL", "race_packages=ALL", "base=" + before, "static=true"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("gh thiếu %q (base mặc định là cha thứ nhất của commit gộp):\n%s", want, log)
		}
	}
	if led, _ := os.ReadFile(filepath.Join(r.pool, "crm-runner-minutes.tsv")); !strings.Contains(string(led), "\tblacksmith\t42\t") {
		t.Errorf("phút của run phải vào sổ:\n%s", led)
	}
	out, code = r.mergeTest(t, []string{"FAKE_RESULT=failure"}, merged)
	if code != 1 || !strings.Contains(out, "MERGE-TEST ĐỎ") {
		t.Errorf("đỏ: mã 1: %d\n%s", code, out)
	}
	// Bản cần test không phải HEAD của thư mục này: không chạy ở máy dev.
	if out, code := r.mergeTest(t, []string{"CRM_CI_RUNNER=mac"}, before); code != 2 {
		t.Errorf("ép mac với bản khác HEAD: mã 2, có %d\n%s", code, out)
	}
	// Không chạy được từ xa: chỉ máy dev, và chỉ khi bản cần test là HEAD sạch (repo thử không có Makefile, tệp gói rỗng:
	// đỏ ở mac).
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = r.mergeTest(t, []string{"CRM_CI_REMOTE=0"}, merged, "", empty, empty)
	if code != 1 || !strings.Contains(out, "bể máy: mac") || !strings.Contains(out, "MERGE-TEST ĐỎ (mac") {
		t.Errorf("CRM_CI_REMOTE=0, bản là HEAD: chạy ở mac: %d\n%s", code, out)
	}
	if out, code := r.mergeTest(t, []string{"CRM_CI_REMOTE=0"}, before); code != 3 {
		t.Errorf("CRM_CI_REMOTE=0, bản khác HEAD: không máy nào chạy được, mã 3; có %d\n%s", code, out)
	}
	// Mẫu gói mở ra nhiều gói: đếm gói thật, không đếm dòng (repo thử: go list lỗi → coi như ALL, không vps).
	pk := filepath.Join(t.TempDir(), "pk")
	if err := os.WriteFile(pk, []byte("./...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"blacksmith", "ubicloud", "github"} {
		down := fmt.Sprintf("%d thử\n", time.Now().Add(time.Hour).Unix())
		if err := os.WriteFile(filepath.Join(r.pool, "crm-runner-"+p+".down"), []byte(down), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, code := r.mergeTest(t, nil, before, "", pk, pk); code != 3 || strings.Contains(out, "bể máy: vps") {
		t.Errorf("không đếm được gói: không gửi vps (bản khác HEAD: không mac) → mã 3; có %d\n%s", code, out)
	}
	if out, code := r.mergeTest(t, nil, merged, "", "/khong/co"); code != 2 {
		t.Errorf("tệp gói không có: mã 2, có %d\n%s", code, out)
	}
}

// CRM_CI_POOL_SLOTS đổi số chỗ (bớt tải một bể, đo); 0 tắt hẳn bể đó.
func TestPool_SlotsOverride(t *testing.T) {
	p := newPoolEnv(t)
	hold(t, p.lockPath("blacksmith", 1))
	out, _ := p.run(t, []string{"CRM_CI_POOL_SLOTS=blacksmith=1"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "ubicloud" {
		t.Errorf("blacksmith còn 1 chỗ (đang giữ): sang ubicloud:\n%s", out)
	}
	out, _ = p.run(t, []string{"CRM_CI_POOL_SLOTS=blacksmith=0, ubicloud=0"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if gotRunner(t, out) != "github" {
		t.Errorf("tắt blacksmith và ubicloud: github:\n%s", out)
	}
	// mac=1: mac vào lượt chọn bình thường, sau mọi bể từ xa (không phải dự phòng cuối, không in dòng đó).
	q := newPoolEnv(t)
	for pool, n := range remotePools() {
		q.holdAll(t, pool, n)
	}
	out, code := q.run(t, []string{"CRM_CI_POOL_SLOTS=mac=1"}, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "mac" || strings.Contains(out, lastResort) {
		t.Errorf("mac=1, mọi bể từ xa đầy: chạy mac như bể thường: %d\n%s", code, out)
	}
	// Mặc định (mac 0 chỗ): cùng cảnh đó thì chờ bể từ xa, không chạy ở mac.
	cmd := q.poolCmd(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	buf := startBg(t, cmd)
	time.Sleep(500 * time.Millisecond)
	if strings.Contains(buf.String(), "RUNNER=") {
		t.Errorf("mac mặc định 0 chỗ: bể từ xa đầy thì chờ, không chạy mac:\n%s", buf.String())
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// Mac chỉ là dự phòng cuối: mọi bể từ xa bị loại (tạm hỏng, hết phút, không nhận lượt) hay --local-only thì chạy mac và
// in đúng dòng "== DỰ PHÒNG CUỐI: chạy trên máy Mac (lý do: …)" để điều phối viên báo chủ shop; không có mac (--no-mac,
// mac=0) thì mã 3, không chạy gì.
func TestPool_MacLastResortOnly(t *testing.T) {
	p := newPoolEnv(t)
	p.downAllRemote(t)
	out, code := p.run(t, nil, append([]string{"run", "--count", "3"}, echoRunner()...)...)
	if code != 0 || !strings.Contains(out, "RUNNER=mac HELD=1 POOL=1") || !strings.Contains(out, lastResort) ||
		!strings.Contains(out, "blacksmith: tạm hỏng") || !strings.Contains(out, "ubicloud: tạm hỏng") {
		t.Errorf("mọi bể từ xa tạm hỏng: mac dự phòng cuối (giữ lượt máy), in lý do: %d\n%s", code, out)
	}
	if out, code := p.run(t, nil, append([]string{"run", "--no-mac", "--count", "3"}, echoRunner()...)...); code != 3 ||
		strings.Contains(out, "RUNNER=") || strings.Contains(out, lastResort) {
		t.Errorf("--no-mac và mọi bể từ xa hỏng: mã 3, không chạy: %d\n%s", code, out)
	}
	if out, code := p.run(t, []string{"CRM_CI_POOL_SLOTS=mac=0"}, append([]string{"run", "--count", "3"}, echoRunner()...)...); code != 3 ||
		strings.Contains(out, "RUNNER=") {
		t.Errorf("mac=0 tắt hẳn cả dự phòng: mã 3: %d\n%s", code, out)
	}
	// --local-only (không chạy được từ xa) vẫn chạy được trên mac, in dòng dự phòng cuối.
	q := newPoolEnv(t)
	out, code = q.run(t, nil, append([]string{"run", "--local-only", "--all"}, echoRunner()...)...)
	if code != 0 || gotRunner(t, out) != "mac" || !strings.Contains(out, lastResort+"không chạy được từ xa") {
		t.Errorf("--local-only: mac, dòng dự phòng cuối: %d\n%s", code, out)
	}
	// Mac dự phòng đang bận (lượt run-in-slot của phiên khác): chờ chỗ mac, rồi chạy.
	rel := hold(t, p.slotLock)
	cmd := p.poolCmd(t, nil, append([]string{"run", "--count", "1"}, echoRunner()...)...)
	buf := startBg(t, cmd)
	time.Sleep(500 * time.Millisecond)
	if strings.Contains(buf.String(), "RUNNER=") {
		t.Errorf("mac đang bận: phải chờ:\n%s", buf.String())
	}
	rel()
	if err := cmd.Wait(); err != nil || gotRunner(t, buf.String()) != "mac" {
		t.Errorf("%v\n%s", err, buf.String())
	}
}

// Bên gọi đã giữ lượt máy (chạy bên trong run-in-slot.py, CRM_TEST_SLOT_HELD=1) mà chỉ chạy được ở máy: chạy luôn ở
// mac, không chờ chính khóa của tiến trình cha (treo mãi).
func TestPool_SlotAlreadyHeldRunsOnMac(t *testing.T) {
	p := newPoolEnv(t)
	hold(t, p.slotLock)
	out, code := p.run(t, []string{"CRM_TEST_SLOT_HELD=1"}, append([]string{"run", "--local-only"}, echoRunner()...)...)
	if code != 0 || !strings.Contains(out, "RUNNER=mac HELD=1 POOL=1") || !strings.Contains(out, lastResort) {
		t.Errorf("đã giữ lượt máy: chạy ngay ở mac (dự phòng cuối, in dòng): %d\n%s", code, out)
	}
}

// merge-test: tệp gói chứa ALL (cách ci-task ghi) là ALL; ở máy dev, go list lỗi (gói gõ sai) là mã 2, không XANH.
func TestMergeTest_PackageFiles(t *testing.T) {
	r := newRemoteEnv(t)
	allFile := filepath.Join(t.TempDir(), "all")
	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(allFile, []byte("ALL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("./khong/co/...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := r.mergeTest(t, nil, "HEAD", "HEAD", allFile, allFile); code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	log, _ := os.ReadFile(r.log)
	if !strings.Contains(string(log), "packages=ALL") || !strings.Contains(string(log), "race_packages=ALL") {
		t.Errorf("tệp chứa ALL phải gửi ALL:\n%s", log)
	}
	out, code := r.mergeTest(t, []string{"CRM_CI_REMOTE=0", "CRM_CI_STATIC=false"}, "HEAD", "HEAD", bad, bad)
	if code != 2 || strings.Contains(out, "XANH") {
		t.Errorf("máy dev, gói không có: mã 2, không XANH; có %d\n%s", code, out)
	}
}

// Nhãn nhánh mặc định của merge-test riêng mỗi lượt (merge-<thư mục>-<pid>): hai lượt cùng lúc chung nhánh ci/<nhãn> thì
// concurrency của workflow hủy run của nhau (review Task 494). Bể mac tắt mà chỉ chạy được ở máy: mã 3, không chờ mãi.
func TestMergeTest_DefaultLabelPerRun(t *testing.T) {
	r := newRemoteEnv(t)
	for range 2 {
		if out, code := r.mergeTest(t, []string{"CRM_CI_LABEL="}, "HEAD", "HEAD", "ALL", "ALL"); code != 0 {
			t.Fatalf("%d\n%s", code, out)
		}
	}
	log, _ := os.ReadFile(r.log)
	refs := regexp.MustCompile(`--ref ci/(merge-[^ ]+-[0-9]+) `).FindAllStringSubmatch(string(log), -1)
	if len(refs) != 2 || refs[0][1] == refs[1][1] {
		t.Errorf("hai lượt phải ra hai nhánh ci/merge-<thư mục>-<pid> khác nhau: %v\n%s", refs, log)
	}
	p := newPoolEnv(t)
	if out, code := p.run(t, []string{"CRM_CI_POOL_SLOTS=mac=0"}, append([]string{"run", "--local-only"}, echoRunner()...)...); code != 3 {
		t.Errorf("chỉ máy mà mac tắt: mã 3, có %d\n%s", code, out)
	}
}
