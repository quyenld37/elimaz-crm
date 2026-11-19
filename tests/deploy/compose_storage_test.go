package deploy_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// Kho tệp trong compose (Task 410; review độc lập M1 C-01, C-03, C-06).

// imagerS3 là địa chỉ kho mà imager dùng: bộ chuyển cổng s3-imaging, chỉ chuyển cổng S3 có kiểm danh tính.
const imagerS3 = "http://s3-imaging:8333"

// checkImagingNetwork (C-01; ADR-45, ADR-36): imager chỉ ở mạng internal; trên các mạng đó chỉ có dịch vụ trong allowed;
// seaweedfs (master, volume, filer không xác thực) không chung mạng nào với imager; s3-imaging chỉ chuyển đúng
// 8333 → seaweedfs:8333 và chạy cứng hóa; worker ký đường dẫn cho imager theo địa chỉ s3-imaging.
func checkImagingNetwork(t *testing.T, p project, allowed []string) {
	t.Helper()
	checkImagerPeers(t, p, allowed)
	checkS3Forwarder(t, p)
	w, ok := p.Services["crm-worker"]
	if !ok {
		t.Fatal("thiếu crm-worker")
	}
	if v := w.Environment["S3_IMAGER_ENDPOINT"]; v == nil || *v != imagerS3 {
		t.Errorf("crm-worker: S3_IMAGER_ENDPOINT = %v, muốn %s (SigV4 ký cả host)", v, imagerS3)
	}
}

// hardened: chỉ đọc, bỏ mọi capability, không leo quyền, không root.
func hardened(s service) bool {
	return s.ReadOnly && slices.Contains(s.CapDrop, "ALL") && slices.Contains(s.SecurityOpt, "no-new-privileges:true") &&
		s.User != "" && !strings.HasPrefix(s.User, "0") && s.User != "root"
}

// checkImagerPeers: imager chỉ ở mạng internal, trên đó chỉ có dịch vụ trong allowed, không có seaweedfs.
func checkImagerPeers(t *testing.T, p project, allowed []string) {
	t.Helper()
	im, ok := p.Services["imager"]
	if !ok || len(im.Networks) == 0 {
		t.Fatal("thiếu imager hoặc imager không khai mạng")
	}
	for n := range im.Networks {
		if !p.Networks[n].Internal {
			t.Errorf("imager ở mạng %s không internal", n)
		}
		for name, s := range p.Services {
			if _, on := s.Networks[n]; !on {
				continue
			}
			if name == "seaweedfs" {
				t.Errorf("seaweedfs chung mạng %s với imager: imager tới được filer, master, volume không xác thực", n)
			} else if !slices.Contains(allowed, name) {
				t.Errorf("%s chung mạng %s với imager (chỉ cho %v)", name, n, allowed)
			}
		}
	}
}

// checkS3Forwarder: s3-imaging chỉ chuyển đúng 8333 → seaweedfs:8333, ghim digest, cứng hóa, không mở cổng ra máy.
func checkS3Forwarder(t *testing.T, p project) {
	t.Helper()
	fw, ok := p.Services["s3-imaging"]
	if !ok {
		t.Fatal("thiếu dịch vụ s3-imaging")
	}
	if got, want := fw.Command, []string{"TCP-LISTEN:8333,fork,reuseaddr", "TCP:seaweedfs:8333"}; !slices.Equal(got, want) {
		t.Errorf("s3-imaging: command %q, muốn %q (chỉ một cổng, một đích)", got, want)
	}
	if !strings.Contains(fw.Image, "@sha256:") {
		t.Errorf("s3-imaging: image %q chưa ghim digest", fw.Image)
	}
	if !hardened(fw) {
		t.Errorf("s3-imaging chưa cứng hóa: read_only %v, cap_drop %v, security_opt %v, user %q",
			fw.ReadOnly, fw.CapDrop, fw.SecurityOpt, fw.User)
	}
	if len(fw.Ports) > 0 {
		t.Errorf("s3-imaging mở cổng ra máy: %+v", fw.Ports)
	}
	if _, on := fw.Networks["default"]; !on {
		t.Error("s3-imaging phải ở mạng default để tới seaweedfs")
	}
}

func TestProdCompose_ImagerReachesOnlyS3(t *testing.T) {
	checkImagingNetwork(t, prodProject(t), []string{"imager", "crm-worker", "s3-imaging"})
}

func TestDevCompose_ImagerReachesOnlyS3(t *testing.T) {
	p := config(t, "-p", "crm-deploy-test", "-f", "docker/compose.dev.yml", "--profile", "app", "--profile", "imager")
	checkImagingNetwork(t, p, []string{"imager", "crm-worker", "s3-imaging", "imager-port"})
	if !slices.Contains(p.Services["s3-imaging"].Profiles, "imager") {
		t.Error("s3-imaging (dev) phải nằm trong profile imager: chỉ cần khi có imager")
	}
}

// C-03 (Task M1-45, ADR-36, TP10): worker production bật cảnh báo chép kho thì compose phải có dịch vụ filesync chạy
// cùng; filesync không mang khóa của crm-app, nối database bằng role crm_filesync, đọc kho chính trong máy, chạy cứng
// hóa, giữ mốc lượt chép ở volume, không ở mạng edge hay imaging.
func TestProdCompose_FilesyncService(t *testing.T) {
	p := prodProject(t)
	if v := p.Services["crm-worker"].Environment["FILESYNC_EXPECTED"]; v == nil || *v != "true" {
		t.Fatalf("crm-worker: FILESYNC_EXPECTED = %v, muốn true", v)
	}
	s, ok := p.Services["filesync"]
	if !ok {
		t.Fatal("FILESYNC_EXPECTED=true nhưng compose production không có dịch vụ filesync: cảnh báo gấp kêu mãi, không có bản sao ngoài")
	}
	if len(s.Profiles) > 0 {
		t.Errorf("filesync production không được nằm trong profile (phải chạy cùng up -d): %v", s.Profiles)
	}
	appSecret := p.Services["crm-api"].Environment["S3_SECRET_ACCESS_KEY"]
	if appSecret == nil || *appSecret == "" {
		t.Fatal("crm-api không có S3_SECRET_ACCESS_KEY để so")
	}
	checkNoAppKeys(t, s.Environment, *appSecret)
	checkFilesyncProdEnv(t, s.Environment)
	if !hardened(s) || !s.Init {
		t.Errorf("filesync chưa cứng hóa: read_only %v, cap_drop %v, security_opt %v, user %q, init %v",
			s.ReadOnly, s.CapDrop, s.SecurityOpt, s.User, s.Init)
	}
	if !slices.ContainsFunc(s.Volumes, func(v volume) bool { return v.Type == "volume" && v.Target == "/state" }) {
		t.Errorf("filesync phải gắn volume ở /state (mốc lần chép tốt cuối): %+v", s.Volumes)
	}
	_ = bytesOf(t, "filesync", s.MemLimit)
	for _, n := range []string{"edge", "imaging"} {
		if _, on := s.Networks[n]; on {
			t.Errorf("filesync không được ở mạng %s", n)
		}
	}
}

// checkFilesyncProdEnv: đủ biến của filesync production; database bằng role crm_filesync; nguồn là kho chính trong máy.
func checkFilesyncProdEnv(t *testing.T, env map[string]*string) {
	t.Helper()
	for _, k := range []string{
		"FILESYNC_SRC_ACCESS_KEY_ID", "FILESYNC_SRC_SECRET_ACCESS_KEY", "FILESYNC_DST_ENDPOINT",
		"FILESYNC_DST_ACCESS_KEY_ID", "FILESYNC_DST_SECRET_ACCESS_KEY", "FILESYNC_DST_REGION", "FILESYNC_DST_BUCKET_PREFIX",
		"RCLONE_CRYPT_PASSWORD", "RCLONE_CRYPT_PASSWORD2", "FILESYNC_HEARTBEAT_URL", "FILESYNC_SRC_BUCKET_PREFIX",
	} {
		if v := env[k]; v == nil || *v == "" {
			t.Errorf("filesync thiếu %s", k)
		}
	}
	if v := env["FILESYNC_DATABASE_URL"]; v == nil || !strings.HasPrefix(*v, "postgres://crm_filesync:") {
		t.Errorf("filesync nối database không bằng role crm_filesync: %v", v)
	}
	if v := env["FILESYNC_SRC_ENDPOINT"]; v == nil || *v != "http://seaweedfs:8333" {
		t.Errorf("filesync FILESYNC_SRC_ENDPOINT = %v, muốn http://seaweedfs:8333", v)
	}
}

// C-06: tên bucket thật = tiền tố + bucket logic. Ứng dụng (S3_BUCKET_PREFIX) và filesync (FILESYNC_SRC_BUCKET_PREFIX)
// đọc cùng một biến của tệp biến máy chủ, mặc định crm-; không lệch nhau được.
func TestProdCompose_BucketPrefixShared(t *testing.T) {
	for _, c := range []struct{ env, want string }{{"S3_BUCKET_PREFIX=", "crm-"}, {"S3_BUCKET_PREFIX=stg-", "stg-"}} {
		p := configEnv(t, []string{c.env}, "-p", "crm-deploy-test", "-f", "docker/compose.prod.yml",
			"--env-file", "docker/compose.prod.env.example")
		for _, name := range []string{"crm-api", "crm-worker"} {
			if v := p.Services[name].Environment["S3_BUCKET_PREFIX"]; v == nil || *v != c.want {
				t.Errorf("%s (%s): S3_BUCKET_PREFIX = %v, muốn %s", name, c.env, v, c.want)
			}
		}
		if v := p.Services["filesync"].Environment["FILESYNC_SRC_BUCKET_PREFIX"]; v == nil || *v != c.want {
			t.Errorf("filesync (%s): FILESYNC_SRC_BUCKET_PREFIX = %v, muốn %s", c.env, v, c.want)
		}
	}
}

// Image filesync của compose dev đè được bằng CRM_FILESYNC_IMAGE (cùng tên biến với production): phiên song song dựng
// image riêng, không ghi đè crm-filesync:dev của nhau.
func TestDevCompose_FilesyncImageFromEnv(t *testing.T) {
	args := []string{"-p", "crm-deploy-test", "-f", "docker/compose.dev.yml", "--profile", "filesync"}
	if got := configEnv(t, []string{"CRM_FILESYNC_IMAGE=crm-filesync:t99"}, args...).Services["filesync"].Image; got != "crm-filesync:t99" {
		t.Errorf("filesync: image %q, muốn crm-filesync:t99", got)
	}
	if got := configEnv(t, []string{"CRM_FILESYNC_IMAGE="}, args...).Services["filesync"].Image; got != "crm-filesync:dev" {
		t.Errorf("filesync: không đặt CRM_FILESYNC_IMAGE thì image %q, muốn crm-filesync:dev", got)
	}
}

// sourceLib chạy script sh nạp scripts/filesync/lib.sh với biến env (chỉ PATH và các biến đã cho).
func sourceLib(t *testing.T, env []string, script string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `. ./lib.sh; `+script)
	cmd.Dir = "../../scripts/filesync"
	cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// C-06: filesync chép ba bucket logic; tên nguồn theo FILESYNC_SRC_BUCKET_PREFIX (mặc định crm-), tên đích theo
// FILESYNC_DST_BUCKET_PREFIX; file_sync_runs.bucket ghi tên logic (luật cảnh báo không cần biết tiền tố).
func TestFilesyncLib_BucketNames(t *testing.T) {
	const show = `printf '%s|' "$FILESYNC_BUCKETS"; for b in $FILESYNC_BUCKETS; do printf '%s>%s ' "$(src_of "$b")" "$(dst_of "$b")"; done`
	for _, c := range []struct {
		env  []string
		want string
	}{
		{nil, "public originals private|main:crm-public>offsite:crm-public main:crm-originals>offsite-originals: main:crm-private>offsite-private: "},
		{
			[]string{"FILESYNC_SRC_BUCKET_PREFIX=stg-", "FILESYNC_DST_BUCKET_PREFIX=b2-"},
			"public originals private|main:stg-public>offsite:b2-public main:stg-originals>offsite-originals: main:stg-private>offsite-private: ",
		},
	} {
		out, err := sourceLib(t, c.env, show)
		if err != nil || out != c.want {
			t.Errorf("env %v:\n có  %q (%v)\n muốn %q", c.env, out, err, c.want)
		}
	}
	// Bucket lạ (kể cả tên thật kiểu cũ): dừng, không chép nhầm chỗ.
	for _, b := range []string{"crm-public", "tmp"} {
		if out, err := sourceLib(t, nil, `dst_of `+b); err == nil {
			t.Errorf("dst_of %s phải lỗi, có %q", b, out)
		}
	}
	// Mẫu cấu hình rclone: tên bucket kho ngoài của hai remote mã hóa theo tiền tố đích.
	tmpl, err := os.ReadFile("../../scripts/filesync/rclone.conf.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"remote = offsite:${FILESYNC_DST_BUCKET_PREFIX}originals", "remote = offsite:${FILESYNC_DST_BUCKET_PREFIX}private"} {
		if !strings.Contains(string(tmpl), want) {
			t.Errorf("rclone.conf.tmpl thiếu %q", want)
		}
	}
}

// Đường lui khi chưa có kho ngoài (RUNBOOK; Task 410): gộp compose.prod.no-filesync.yml thì filesync bị loại khỏi
// project (không kéo image, không chạy) và worker tắt cảnh báo chép kho; mọi dịch vụ khác giữ nguyên.
func TestProdCompose_NoFilesyncOverride(t *testing.T) {
	base := prodProject(t)
	p := config(t, "-p", "crm-deploy-test", "-f", "docker/compose.prod.yml", "-f", "docker/compose.prod.no-filesync.yml",
		"--env-file", "docker/compose.prod.env.example")
	if _, ok := p.Services["filesync"]; ok {
		t.Error("gộp compose.prod.no-filesync.yml mà filesync vẫn trong project")
	}
	if v := p.Services["crm-worker"].Environment["FILESYNC_EXPECTED"]; v == nil || *v != "false" {
		t.Errorf("crm-worker: FILESYNC_EXPECTED = %v, muốn false khi chưa có kho ngoài", v)
	}
	for name := range base.Services {
		if _, ok := p.Services[name]; !ok && name != "filesync" {
			t.Errorf("override làm mất dịch vụ %s", name)
		}
	}
}
