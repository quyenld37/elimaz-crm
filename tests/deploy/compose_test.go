// Package deploy_test kiểm luật của các tệp compose (kế hoạch M0 bản 5, Task 21; ADR-43, ADR-45, spec 04 mục 11)
// bằng chính bộ đọc của Docker: `docker compose config --format json` gộp biến, profile, mặc định như lúc chạy thật.
// Cần lệnh docker (không cần daemon chạy container); thiếu thì test đỏ, không tự bỏ qua.
package deploy_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// appServices là ba vai trò chạy lâu của image chính.
var appServices = []string{"crm-ingest", "crm-api", "crm-worker"} //nolint:gochecknoglobals // danh sách hằng của test

// minStopGrace: worker cần 25 s tắt êm (Task 11, spec 04 mục 5.1); Docker gửi SIGKILL sau stop_grace_period.
const minStopGrace = 30 * time.Second

type project struct {
	Services map[string]service `json:"services"`
	Networks map[string]network `json:"networks"`
}

type network struct {
	Internal bool `json:"internal"`
}

type service struct {
	Image           string             `json:"image"`
	Build           json.RawMessage    `json:"build"`
	Ports           []port             `json:"ports"`
	Networks        map[string]any     `json:"networks"`
	Environment     map[string]*string `json:"environment"`
	Healthcheck     *healthcheck       `json:"healthcheck"`
	MemLimit        json.RawMessage    `json:"mem_limit"`
	StopGracePeriod string             `json:"stop_grace_period"`
	ReadOnly        bool               `json:"read_only"`
	CapDrop         []string           `json:"cap_drop"`
	SecurityOpt     []string           `json:"security_opt"`
	User            string             `json:"user"`
	ShmSize         json.RawMessage    `json:"shm_size"`
	Volumes         []volume           `json:"volumes"`
	Profiles        []string           `json:"profiles"`
	Init            bool               `json:"init"`
}

type port struct {
	HostIP    string `json:"host_ip"`
	Published string `json:"published"`
	Target    int    `json:"target"`
}

type healthcheck struct {
	Test    []string `json:"test"`
	Disable bool     `json:"disable"`
}

type volume struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

// config đọc tệp compose (đường dẫn tính từ gốc repo) qua docker compose config.
func config(t *testing.T, args ...string) project {
	t.Helper()
	return configEnv(t, nil, args...)
}

// configEnv như config, thêm biến môi trường env ("K=V") cho lệnh docker compose (các biến khác của phiên giữ nguyên).
func configEnv(t *testing.T, env []string, args ...string) project {
	t.Helper()
	full := append([]string{"compose", "--project-directory", "../../docker"}, args...)
	full = append(full, "config", "--format", "json")
	cmd := exec.CommandContext(t.Context(), "docker", full...)
	cmd.Dir = "../.."
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // ExitError không bọc
			stderr = string(ee.Stderr)
		}
		t.Fatalf("docker compose config %v: %v\n%s", args, err, stderr)
	}
	var p project
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("đọc json của compose config: %v", err)
	}
	return p
}

func devProject(t *testing.T) project {
	t.Helper()
	return config(t, "-p", "crm-deploy-test", "-f", "docker/compose.dev.yml", "--profile", "app")
}

func prodProject(t *testing.T) project {
	t.Helper()
	return config(t, "-p", "crm-deploy-test", "-f", "docker/compose.prod.yml", "--env-file", "docker/compose.prod.env.example")
}

// bytesOf đọc mem_limit của compose config (số byte, dạng số hoặc chuỗi).
func bytesOf(t *testing.T, name string, raw json.RawMessage) int64 {
	t.Helper()
	s := strings.Trim(string(raw), `"`)
	if s == "" || s == "null" {
		t.Fatalf("%s: thiếu mem_limit", name)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("%s: mem_limit %q không phải số byte: %v", name, s, err)
	}
	return n
}

// goMemLimit đọc GOMEMLIMIT dạng <số>MiB.
func goMemLimit(t *testing.T, name string, env map[string]*string) int64 {
	t.Helper()
	v := env["GOMEMLIMIT"]
	if v == nil {
		t.Fatalf("%s: thiếu GOMEMLIMIT", name)
	}
	num, ok := strings.CutSuffix(*v, "MiB")
	n, err := strconv.ParseInt(num, 10, 64)
	if !ok || err != nil {
		t.Fatalf("%s: GOMEMLIMIT %q phải có dạng <số>MiB", name, *v)
	}
	return n << 20
}

// checkAppService: kiểm sức khỏe bằng tệp chạy, giới hạn bộ nhớ, GOMEMLIMIT 90% giới hạn (làm tròn xuống MiB),
// tắt êm đủ 30 s, chỉ đọc, bỏ mọi capability, không leo quyền.
func checkAppService(t *testing.T, name string, s service) {
	t.Helper()
	if s.Healthcheck == nil || s.Healthcheck.Disable || len(s.Healthcheck.Test) == 0 {
		t.Errorf("%s: thiếu healthcheck", name)
	} else if got := strings.Join(s.Healthcheck.Test, " "); got != "CMD /crm healthcheck" {
		t.Errorf("%s: healthcheck %q, muốn CMD /crm healthcheck", name, got)
	}
	limit := bytesOf(t, name, s.MemLimit)
	want := (limit * 9 / 10) >> 20 << 20
	if got := goMemLimit(t, name, s.Environment); got != want {
		t.Errorf("%s: GOMEMLIMIT %d MiB, muốn %d MiB (90%% của %d MiB)", name, got>>20, want>>20, limit>>20)
	}
	grace, err := time.ParseDuration(s.StopGracePeriod)
	if err != nil || grace < minStopGrace {
		t.Errorf("%s: stop_grace_period %q, cần ≥ %v", name, s.StopGracePeriod, minStopGrace)
	}
	if !s.ReadOnly {
		t.Errorf("%s: cần read_only", name)
	}
	if !slices.Contains(s.CapDrop, "ALL") {
		t.Errorf("%s: cần cap_drop ALL", name)
	}
	if !slices.Contains(s.SecurityOpt, "no-new-privileges:true") {
		t.Errorf("%s: cần no-new-privileges", name)
	}
	if u, _, _ := strings.Cut(s.User, ":"); u == "0" || u == "root" {
		t.Errorf("%s: không được chạy bằng root", name)
	}
	if role := s.Environment["CRM_ROLE"]; role == nil || "crm-"+*role != name {
		t.Errorf("%s: CRM_ROLE không khớp tên dịch vụ", name)
	}
}

func TestDevCompose_AppServices(t *testing.T) {
	p := devProject(t)
	for _, name := range append([]string{"crm-migrate"}, appServices...) {
		s, ok := p.Services[name]
		if !ok {
			t.Fatalf("compose dev thiếu dịch vụ %s", name)
		}
		if !slices.Contains(s.Profiles, "app") {
			t.Errorf("%s: phải nằm trong profile app để make db-up không bật", name)
		}
	}
	for _, name := range appServices {
		checkAppService(t, name, p.Services[name])
	}
	// Không bật profile app thì không dịch vụ ứng dụng nào chạy (phiên khác dùng make db-up không đổi).
	base := config(t, "-p", "crm-deploy-test", "-f", "docker/compose.dev.yml")
	for _, name := range appServices {
		if _, ok := base.Services[name]; ok {
			t.Errorf("%s chạy cả khi không bật profile app", name)
		}
	}
}

func TestProdCompose_AppServices(t *testing.T) {
	p := prodProject(t)
	for _, name := range appServices {
		s, ok := p.Services[name]
		if !ok {
			t.Fatalf("compose production thiếu dịch vụ %s", name)
		}
		checkAppService(t, name, s)
	}
}

// Máy chủ chỉ nhận kết nối qua Cloudflare (ADR-43, 04 mục 11; Task 14: cf-connecting-ip chỉ đáng tin khi vậy):
// không dịch vụ nào mở cổng ra máy; lối vào duy nhất là cloudflared trên mạng edge.
func TestProdCompose_OnlyCloudflareIngress(t *testing.T) {
	p := prodProject(t)
	for name, s := range p.Services {
		if len(s.Ports) > 0 {
			t.Errorf("%s mở cổng ra máy: %+v", name, s.Ports)
		}
	}
	var onEdge []string
	for name, s := range p.Services {
		if _, ok := s.Networks["edge"]; ok {
			onEdge = append(onEdge, name)
		}
	}
	slices.Sort(onEdge)
	if want := []string{"cloudflared", "crm-api", "crm-ingest"}; !slices.Equal(onEdge, want) {
		t.Errorf("dịch vụ trên mạng edge: %v, muốn %v", onEdge, want)
	}
	if _, ok := p.Services["cloudflared"]; !ok {
		t.Fatal("thiếu cloudflared")
	}
}

// imager không ra internet, không tới database (ADR-45; Task 40 Q3): chỉ ở mạng internal.
func TestProdCompose_ImagerIsolated(t *testing.T) {
	p := prodProject(t)
	im, ok := p.Services["imager"]
	if !ok {
		t.Fatal("compose production thiếu imager")
	}
	if len(im.Networks) == 0 {
		t.Fatal("imager không khai mạng (sẽ vào mạng default có internet)")
	}
	for n := range im.Networks {
		if !p.Networks[n].Internal {
			t.Errorf("imager ở mạng %s không internal", n)
		}
		for name, s := range p.Services {
			if _, ok := s.Networks[n]; ok && name == "postgres" {
				t.Errorf("postgres chung mạng %s với imager", n)
			}
		}
	}
	for k := range im.Environment {
		if lk := strings.ToLower(k); strings.Contains(lk, "secret") || strings.Contains(lk, "password") ||
			strings.Contains(lk, "database") || strings.Contains(lk, "token") {
			t.Errorf("imager có biến %s", k)
		}
	}
}

// Image dựng ở CI, không dựng trên máy chạy thật (ADR-43); image bên thứ ba ghim digest.
func TestProdCompose_ImagesPinnedNotBuilt(t *testing.T) {
	p := prodProject(t)
	fromEnv := map[string]bool{"crm-migrate": true, "crm-ingest": true, "crm-api": true, "crm-worker": true, "imager": true, "postgres": true}
	for name, s := range p.Services {
		if len(s.Build) > 0 && string(s.Build) != "null" {
			t.Errorf("%s có build: production chỉ chạy image dựng ở CI", name)
		}
		if !fromEnv[name] && !strings.Contains(s.Image, "@sha256:") {
			t.Errorf("%s: image %q chưa ghim digest", name, s.Image)
		}
	}
}

// PostgreSQL 18: ổ dữ liệu gắn đúng /var/lib/postgresql, có shm_size (ADR-43).
func TestProdCompose_PostgresVolume(t *testing.T) {
	p := prodProject(t)
	pg, ok := p.Services["postgres"]
	if !ok {
		t.Fatal("thiếu postgres")
	}
	if !slices.ContainsFunc(pg.Volumes, func(v volume) bool { return v.Type == "volume" && v.Target == "/var/lib/postgresql" }) {
		t.Errorf("postgres phải gắn volume ở /var/lib/postgresql: %+v", pg.Volumes)
	}
	if s := strings.Trim(string(pg.ShmSize), `"`); s == "" || s == "null" || s == "0" {
		t.Errorf("postgres thiếu shm_size")
	}
	// Script sao lưu chạy bằng exec; tiến trình mồ côi bị giết về tay postmaster làm cả cụm khởi động lại (Task 21).
	if !pg.Init {
		t.Errorf("postgres cần init: true (tini làm PID 1)")
	}
	for _, k := range []string{"WALG_S3_PREFIX", "WALG_LIBSODIUM_KEY", "WALG_LIBSODIUM_KEY_TRANSFORM"} {
		if pg.Environment[k] == nil {
			t.Errorf("postgres thiếu %s", k)
		}
	}
}
