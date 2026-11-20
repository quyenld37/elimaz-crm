package deploy_test

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// Kịch bản "một đơn đi trọn" trên compose dev (kế hoạch M1, Task 395): hãng giả nối với ingest, worker bằng khóa sinh
// lúc chạy (CRM_FAKECARRIER_SECRET), máy chủ thẻ Access giả devaccess cho api. Cả hai chỉ có ở dev, chỉ mở 127.0.0.1.

const (
	demoSecret       = "0123456789abcdef0123456789abcdef-demo"
	fakeCarrierURL   = "http://fakecarrier:4194"
	fakeCarrierHook  = "http://crm-ingest:4191/hooks/fake"
	devAccessDomain  = "devaccess:8443"
	devAccessCADir   = "/etc/ssl/devaccess"
	devAccessCAVol   = "devaccess-ca"
	devAccessTokenTo = 4195
)

func demoProject(t *testing.T, secret string) project {
	t.Helper()
	return configEnv(t, []string{"CRM_FAKECARRIER_SECRET=" + secret},
		"-p", "crm-deploy-test", "-f", "docker/compose.dev.yml",
		"--profile", "app", "--profile", "fakecarrier", "--profile", "devaccess")
}

func envOf(s service, k string) string {
	if v := s.Environment[k]; v != nil {
		return *v
	}
	return ""
}

// onlyLocalPorts: mọi cổng mở ra máy chỉ nghe 127.0.0.1.
func onlyLocalPorts(t *testing.T, name string, s service) {
	t.Helper()
	for _, p := range s.Ports {
		if p.HostIP != "127.0.0.1" {
			t.Errorf("%s mở cổng %d ra %q, chỉ được 127.0.0.1", name, p.Target, p.HostIP)
		}
	}
}

func TestDevCompose_FakeCarrierWiredWithRuntimeSecret(t *testing.T) {
	p := demoProject(t, demoSecret)
	for _, name := range []string{"crm-ingest", "crm-worker"} {
		s := p.Services[name]
		if got := envOf(s, "FAKECARRIER_URL"); got != fakeCarrierURL {
			t.Errorf("%s: FAKECARRIER_URL %q, muốn %q", name, got, fakeCarrierURL)
		}
		if envOf(s, "FAKECARRIER_WEBHOOK_SECRET") != demoSecret {
			t.Errorf("%s: FAKECARRIER_WEBHOOK_SECRET không lấy từ CRM_FAKECARRIER_SECRET", name)
		}
	}
	fc, ok := p.Services["fakecarrier"]
	if !ok {
		t.Fatal("compose dev thiếu fakecarrier")
	}
	if got := envOf(fc, "FAKECARRIER_WEBHOOK_URL"); got != fakeCarrierHook {
		t.Errorf("fakecarrier: FAKECARRIER_WEBHOOK_URL %q, muốn %q", got, fakeCarrierHook)
	}
	if envOf(fc, "FAKECARRIER_WEBHOOK_SECRET") != demoSecret {
		t.Error("fakecarrier: FAKECARRIER_WEBHOOK_SECRET không lấy từ CRM_FAKECARRIER_SECRET")
	}
	onlyLocalPorts(t, "fakecarrier", fc)
	if v := envOf(p.Services["crm-api"], "FAKECARRIER_URL"); v != "" {
		t.Errorf("crm-api không cần hãng giả, có FAKECARRIER_URL %q", v)
	}
}

// Không đặt khóa (make app-up, image-check, phiên khác): ingest, worker chạy như cũ, không nối hãng giả; compose không
// chứa khóa cố định nào cho hãng giả.
func TestDevCompose_NoSecretNoFakeCarrier(t *testing.T) {
	p := demoProject(t, "")
	for _, name := range []string{"crm-ingest", "crm-worker", "fakecarrier"} {
		s := p.Services[name]
		for _, k := range []string{"FAKECARRIER_URL", "FAKECARRIER_WEBHOOK_SECRET", "FAKECARRIER_WEBHOOK_URL"} {
			if v := envOf(s, k); v != "" {
				t.Errorf("%s: %s = %q khi không có CRM_FAKECARRIER_SECRET", name, k, v)
			}
		}
	}
	raw, err := os.ReadFile("../../docker/compose.dev.yml")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(raw)) {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && strings.HasPrefix(k, "FAKECARRIER_WEBHOOK_SECRET") &&
			!strings.Contains(v, "${CRM_FAKECARRIER_SECRET") {
			t.Errorf("compose dev ghi khóa hãng giả cố định: %q", strings.TrimSpace(line))
		}
	}
}

func TestDevCompose_DevAccessForAPI(t *testing.T) {
	p := demoProject(t, demoSecret)
	da, ok := p.Services["devaccess"]
	if !ok {
		t.Fatal("compose dev thiếu devaccess")
	}
	onlyLocalPorts(t, "devaccess", da)
	if len(da.Ports) != 1 || da.Ports[0].Target != devAccessTokenTo {
		t.Errorf("devaccess chỉ mở cổng phát thẻ %d (cổng khóa HTTPS chỉ trong mạng compose): %+v", devAccessTokenTo, da.Ports)
	}
	if !da.ReadOnly || !slices.Contains(da.CapDrop, "ALL") {
		t.Error("devaccess cần read_only và cap_drop ALL")
	}
	api := p.Services["crm-api"]
	if got := envOf(api, "ACCESS_TEAM_DOMAIN"); got != devAccessDomain {
		t.Errorf("crm-api: ACCESS_TEAM_DOMAIN %q, muốn %q", got, devAccessDomain)
	}
	if got := envOf(api, "SSL_CERT_DIR"); got != devAccessCADir {
		t.Errorf("crm-api: SSL_CERT_DIR %q, muốn %q", got, devAccessCADir)
	}
	if !slices.ContainsFunc(api.Volumes, func(v volume) bool {
		return v.Type == "volume" && v.Source == devAccessCAVol && v.Target == devAccessCADir && v.ReadOnly
	}) {
		t.Errorf("crm-api phải gắn chỉ đọc volume %s ở %s: %+v", devAccessCAVol, devAccessCADir, api.Volumes)
	}
	// make db-up (không profile) không bật devaccess.
	base := config(t, "-p", "crm-deploy-test", "-f", "docker/compose.dev.yml")
	if _, ok := base.Services["devaccess"]; ok {
		t.Error("devaccess chạy cả khi không bật profile devaccess")
	}
}

// Hai công cụ giả chỉ có ở dev: compose production không có.
func TestProdCompose_NoDevTools(t *testing.T) {
	p := prodProject(t)
	for _, name := range []string{"fakecarrier", "devaccess"} {
		if _, ok := p.Services[name]; ok {
			t.Errorf("compose production có %s", name)
		}
	}
}

// PostgreSQL dev có tini làm PID 1 như production: tiến trình exec mồ côi (healthcheck, psql của make demo-mot-don)
// thoát mã khác 0 không làm postmaster khởi động lại cả cụm.
func TestDevCompose_PostgresInit(t *testing.T) {
	if pg := devProject(t).Services["postgres"]; !pg.Init {
		t.Error("postgres dev cần init: true")
	}
}

// Hãng giả giữ kiện trong volume: dựng lại container (mỗi lần make demo-mot-don có khóa mới) không làm bộ đếm mã hãng
// quay về FC00000001 và trùng mã vận đơn đã có trong database.
func TestDevCompose_FakeCarrierKeepsParcels(t *testing.T) {
	fc := demoProject(t, demoSecret).Services["fakecarrier"]
	data := envOf(fc, "FAKECARRIER_DATA")
	if !slices.ContainsFunc(fc.Volumes, func(v volume) bool {
		return v.Type == "volume" && v.Source == "fakecarrier-data" && strings.HasPrefix(data, v.Target+"/") && !v.ReadOnly
	}) {
		t.Errorf("fakecarrier: FAKECARRIER_DATA %q phải nằm trong volume fakecarrier-data: %+v", data, fc.Volumes)
	}
}
