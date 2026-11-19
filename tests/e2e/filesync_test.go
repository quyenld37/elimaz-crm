//go:build filesync

// Chép sang kho ngoài (TP10, Task M1-45) trên compose dev: kho chính SeaweedFS, kho ngoài giả seaweedfs-offsite,
// container filesync dựng từ docker/filesync/Dockerfile. Database của test là database thử riêng (testkit) trên cùng
// PostgreSQL của compose; container ghi vào đó bằng role crm_filesync.
//
//	make db-up && docker compose -f docker/compose.dev.yml --profile filesync build filesync
//	go test -tags filesync -count=1 -run TestFileSync ./tests/e2e/
package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

// syncedBuckets là ba bucket logic filesync chép; file_sync_runs.bucket và ls-offsite.sh dùng tên này (Task 410, C-06).
var syncedBuckets = []string{"public", "originals", "private"} //nolint:gochecknoglobals // hằng của test

// realBucket: tên bucket thật ở kho chính và kho ngoài giả của compose dev (tiền tố mặc định crm-).
func realBucket(b string) string { return "crm-" + b }

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// fsync là một lần chạy test: database thử, tiền tố đối tượng riêng, hai client S3 quản trị (kho chính, kho ngoài).
type fsync struct {
	t              *testing.T
	dbName, prefix string
	owner          *pgx.Conn
	main, offsite  *s3.Client
}

func s3Admin(endpoint string) *s3.Client {
	creds := aws.CredentialsProviderFunc(func(_ context.Context) (aws.Credentials, error) {
		// Khóa dev công khai trong docker/seaweedfs/s3.json, s3-offsite.json.
		return aws.Credentials{AccessKeyID: "crm-admin-dev", SecretAccessKey: "crm-admin-dev-only-not-a-secret", Source: "e2e"}, nil
	})
	return s3.New(s3.Options{
		Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: creds,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	})
}

func newFsync(t *testing.T) *fsync {
	t.Helper()
	_, ownerURL, _ := testkit.StartPostgres(t)
	u, err := url.Parse(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(t.Context(), ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	f := &fsync{
		t: t, dbName: strings.TrimPrefix(u.Path, "/"), prefix: "e2e-filesync-" + hex.EncodeToString(b), owner: owner,
		main:    s3Admin("http://127.0.0.1:" + envDefault("CRM_S3_PORT", "8333")),
		offsite: s3Admin("http://127.0.0.1:" + envDefault("CRM_S3_OFFSITE_PORT", "8334")),
	}
	// Dọn tệp của test ở kho chính (kho ngoài chỉ thêm, giữ lại: kho ngoài nhiều hơn kho chính không làm đối chiếu
	// của lần chạy sau lệch; kho chính còn tệp mà kho ngoài đã bị test xóa thì có).
	t.Cleanup(func() {
		ctx := context.Background()
		for _, bucket := range []string{"crm-tmp", realBucket("public"), realBucket("originals"), realBucket("private")} {
			list, err := f.main.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(f.prefix + "/")})
			if err != nil {
				t.Logf("dọn %s: %v", bucket, err)
				continue
			}
			for _, o := range list.Contents {
				_, _ = f.main.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: o.Key})
			}
		}
	})
	return f
}

func repoDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// compose chạy `docker compose --profile filesync run --rm` cho dịch vụ filesync, nối vào database thử. Trả stdout
// của container (tiến độ của compose và log của script đi stderr; khi lỗi thì trả kèm stderr).
func (f *fsync) compose(args ...string) (string, error) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.t.Context(), 5*time.Minute)
	defer cancel()
	full := append([]string{
		"compose", "-f", "docker/compose.dev.yml", "--profile", "filesync", "run", "--rm", "-T",
		"-e", "FILESYNC_DATABASE_URL=postgres://crm_filesync:crm_filesync_dev@postgres:5432/" + f.dbName + "?sslmode=disable",
	}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = repoDir(f.t)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out) + stderr.String(), fmt.Errorf("docker %v: %w", args, err)
	}
	return string(out), nil
}

// runOnce chạy một lượt run.sh; trả kết quả (ok của từng bucket, số tệp đã chép) đọc từ dòng copy mới nhất.
func (f *fsync) runOnce() map[string]row {
	f.t.Helper()
	if out, err := f.compose("filesync", "--once"); err != nil {
		f.t.Logf("run.sh --once: %v\n%s", err, out)
	}
	return f.latest("copy")
}

func (f *fsync) reconcile() map[string]row {
	f.t.Helper()
	if out, err := f.compose("--entrypoint", "/opt/filesync/reconcile.sh", "filesync"); err != nil {
		f.t.Logf("reconcile.sh: %v\n%s", err, out)
	}
	return f.latest("reconcile")
}

type row struct {
	ok             bool
	objects, bytes *int64
	detail         *string
	n              int
}

// latest đọc dòng mới nhất của kind cho mỗi bucket (và số dòng của kind ở bucket đó).
func (f *fsync) latest(kind string) map[string]row {
	f.t.Helper()
	out := map[string]row{}
	for _, b := range syncedBuckets {
		var r row
		err := f.owner.QueryRow(f.t.Context(), `SELECT ok, objects, bytes, detail, count(*) OVER ()
			FROM platform.file_sync_runs WHERE kind = $1 AND bucket = $2 ORDER BY finished_at DESC, id DESC LIMIT 1`,
			kind, b).Scan(&r.ok, &r.objects, &r.bytes, &r.detail, &r.n)
		if err != nil {
			f.t.Fatalf("đọc dòng %s của %s: %v", kind, b, err)
		}
		out[b] = r
	}
	return out
}

func (f *fsync) put(c *s3.Client, bucket, key string, body []byte) {
	f.t.Helper()
	if _, err := c.PutObject(f.t.Context(), &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(body),
	}); err != nil {
		f.t.Fatalf("ghi %s/%s: %v", bucket, key, err)
	}
}

// offsiteKeys liệt kê khóa hiện có ở kho ngoài (đọc thẳng, không giải mã).
func (f *fsync) offsiteKeys(bucket string) []string {
	f.t.Helper()
	var keys []string
	p := s3.NewListObjectsV2Paginator(f.offsite, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	for p.HasMorePages() {
		page, err := p.NextPage(f.t.Context())
		if err != nil {
			f.t.Fatalf("liệt kê kho ngoài %s: %v", bucket, err)
		}
		for _, o := range page.Contents {
			keys = append(keys, aws.ToString(o.Key))
		}
	}
	return keys
}

// decrypted liệt kê tệp dưới tiền tố của test ở kho ngoài qua đúng cấu hình rclone của container (giải mã bucket
// riêng tư bằng mật khẩu crypt của compose dev).
func (f *fsync) decrypted(bucket string) []string {
	f.t.Helper()
	out, err := f.compose("--entrypoint", "/opt/filesync/ls-offsite.sh", "filesync", bucket, f.prefix)
	if err != nil {
		f.t.Fatalf("ls-offsite %s: %v\n%s", bucket, err, out)
	}
	return strings.Fields(out)
}

func (f *fsync) get(c *s3.Client, bucket, key string) []byte {
	f.t.Helper()
	obj, err := c.GetObject(f.t.Context(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		f.t.Fatalf("đọc %s/%s: %v", bucket, key, err)
	}
	defer func() { _ = obj.Body.Close() }()
	raw, err := io.ReadAll(obj.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return raw
}

func (f *fsync) del(c *s3.Client, bucket, key string) {
	f.t.Helper()
	if _, err := c.DeleteObject(f.t.Context(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}); err != nil {
		f.t.Fatalf("xóa %s/%s: %v", bucket, key, err)
	}
}

// seed ghi 3 đối tượng ngẫu nhiên dưới tiền tố của test ở mỗi bucket chép (và một ở crm-tmp); trả nội dung theo
// "<bucket>/<khóa>".
func (f *fsync) seed() map[string][]byte {
	f.t.Helper()
	body := map[string][]byte{}
	for _, b := range syncedBuckets {
		for i := range 3 {
			k := fmt.Sprintf("%s/tep-%d", f.prefix, i)
			v := make([]byte, 64+i)
			_, _ = rand.Read(v)
			body[b+"/"+k] = v
			f.put(f.main, realBucket(b), k, v)
		}
	}
	f.put(f.main, "crm-tmp", f.prefix+"/tam", []byte("tạm"))
	return body
}

// checkEncrypted: ở crm-originals, crm-private, đọc thẳng kho ngoài không ra tên, không ra byte gốc.
func (f *fsync) checkEncrypted(body map[string][]byte) {
	f.t.Helper()
	for _, b := range []string{"originals", "private"} {
		for _, k := range f.offsiteKeys(realBucket(b)) {
			if strings.Contains(k, f.prefix) || strings.Contains(k, "tep-") {
				f.t.Fatalf("kho ngoài %s có tên chưa mã hóa: %s", b, k)
			}
			raw := f.get(f.offsite, realBucket(b), k)
			for name, v := range body {
				if strings.HasPrefix(name, b+"/") && bytes.Contains(raw, v[:32]) {
					f.t.Fatalf("kho ngoài %s/%s chứa byte gốc của %s", b, k, name)
				}
			}
		}
	}
}

// wantThreeOffsite: mỗi bucket ở kho ngoài (giải mã qua crypt) có đúng 3 tệp dưới tiền tố của test.
func (f *fsync) wantThreeOffsite(what string) {
	f.t.Helper()
	for _, b := range syncedBuckets {
		if got := f.decrypted(b); len(got) != 3 {
			f.t.Fatalf("%s: kho ngoài %s dưới %s có %v, muốn 3 tệp", what, b, f.prefix, got)
		}
	}
}

// wantAll: mọi bucket của lượt có ok = want.
func wantAll(t *testing.T, what string, rows map[string]row, check func(b string, r row) bool) {
	t.Helper()
	for b, r := range rows {
		if !check(b, r) {
			t.Fatalf("%s %s: %+v %s", what, b, r, deref(r.detail))
		}
	}
}

func TestFileSync(t *testing.T) {
	f := newFsync(t)
	body := f.seed()

	// Lượt 1: mỗi bucket ok, chép ít nhất 3 tệp; qua crypt đọc lại đủ 3 tệp của test.
	first := f.runOnce()
	wantAll(t, "lượt 1", first, func(_ string, r row) bool { return r.ok && r.objects != nil && *r.objects >= 3 })
	f.wantThreeOffsite("lượt 1")
	f.checkEncrypted(body)
	// Bucket công khai: không mã hóa, đọc thẳng ra đúng byte.
	pub := f.prefix + "/tep-0"
	if !bytes.Equal(f.get(f.offsite, "crm-public", pub), body["public/"+pub]) {
		t.Fatal("kho ngoài crm-public: nội dung khác kho chính")
	}
	// crm-tmp không chép: kho ngoài không có bucket đó (danh tính chép không tạo được bucket), hay có mà rỗng.
	if list, err := f.offsite.ListObjectsV2(t.Context(), &s3.ListObjectsV2Input{Bucket: aws.String("crm-tmp")}); err == nil &&
		len(list.Contents) > 0 {
		t.Fatalf("crm-tmp không được chép: %d đối tượng ở kho ngoài", len(list.Contents))
	}

	// Đối chiếu ngay sau lượt chép: đủ tệp hai phía → ok.
	wantAll(t, "đối chiếu sau lượt 1", f.reconcile(), func(_ string, r row) bool { return r.ok })

	// Lượt 2: không có gì mới → 0 tệp chép thêm, một dòng mới.
	wantAll(t, "lượt 2", f.runOnce(), func(b string, r row) bool {
		return r.ok && r.objects != nil && *r.objects == 0 && r.n == first[b].n+1
	})

	// Xóa ở kho chính → kho ngoài vẫn còn sau lượt chép kế.
	for _, b := range syncedBuckets {
		f.del(f.main, realBucket(b), f.prefix+"/tep-1")
	}
	wantAll(t, "lượt 3", f.runOnce(), func(_ string, r row) bool { return r.ok })
	f.wantThreeOffsite("xóa ở kho chính rồi chép: kho ngoài phải còn đủ")

	// Ghi đè ở kho ngoài bằng nội dung khác → lượt sau ok = false (--immutable), các bucket khác vẫn ok; bản ghi đè
	// không bị chép đè lại từ kho chính (chỉ thêm).
	f.put(f.offsite, "crm-public", pub, []byte("nội dung khác hẳn, dài khác bản gốc"))
	wantAll(t, "lượt 4", f.runOnce(), func(b string, r row) bool {
		if b == "public" {
			return !r.ok && r.detail != nil && strings.Contains(*r.detail, "immutable")
		}
		return r.ok
	})
	if bytes.Equal(f.get(f.offsite, "crm-public", pub), body["public/"+pub]) {
		t.Fatal("rclone đã ghi đè đối tượng ở kho ngoài")
	}

	// Mất tệp ở kho ngoài (người quản trị xóa) → đối chiếu ok = false. Đối chiếu đếm cả bucket, nên xóa mọi tệp của
	// các lần chạy e2e ở kho ngoài crm-public (cả của lần chạy trước còn lại, không thì kho ngoài vẫn nhiều hơn): kho
	// chính còn tep-0, tep-2 của lần này mà kho ngoài không còn.
	for _, k := range f.offsiteKeys("crm-public") {
		if strings.HasPrefix(k, "e2e-filesync-") {
			f.del(f.offsite, "crm-public", k)
		}
	}
	if r := f.reconcile()["public"]; r.ok {
		t.Fatalf("kho ngoài thiếu tệp: đối chiếu phải ok = false, có %s", deref(r.detail))
	}

	// Tệp tải lên sau lần chép tốt cuối chưa có ở kho ngoài là bình thường: đối chiếu trừ chúng ra. Xóa nốt hai tệp của
	// lần này ở kho chính (hai phía bằng nhau), rồi thêm một tệp mới: kho chính hơn kho ngoài đúng 1 tệp mới → ok.
	f.del(f.main, "crm-public", f.prefix+"/tep-0")
	f.del(f.main, "crm-public", f.prefix+"/tep-2")
	f.put(f.main, "crm-public", f.prefix+"/moi-sau-lan-chep", []byte("tệp mới"))
	if r := f.reconcile()["public"]; !r.ok || r.detail == nil || !strings.Contains(*r.detail, "mới sau lần chép tốt cuối 1") {
		t.Fatalf("tệp mới hơn lần chép tốt cuối: muốn ok, trừ 1 tệp mới; có ok=%v %s", r.ok, deref(r.detail))
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
