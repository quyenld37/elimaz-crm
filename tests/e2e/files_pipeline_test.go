//go:build imager

// Đường xử lý ảnh trọn vẹn với imager thật (TP01, R10; Task M1-43): PostgreSQL, SeaweedFS và imager của compose dev
// (make db-up, rồi `docker compose --profile imager up -d --wait imager imager-port`). Worker chạy trên máy nên đường
// dẫn ký cho imager dùng địa chỉ trong mạng imaging (S3_IMAGER_ENDPOINT, mặc định http://seaweedfs:8333).
//
//	go test -tags imager -count=1 -run TestFilesPipeline ./tests/e2e/
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

const e2ePrivatePurpose = "test_private_image"

type pipeline struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	fs     *files.S3Store
	w      *queue.Worker
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newPipeline(t *testing.T) *pipeline {
	t.Helper()
	s := testkit.OpenStaff(t)
	p := &pipeline{t: t, staff: s, tenant: s.Tenant(t, "A"), fs: testkit.S3FileStore(t)}
	files.RegisterPurpose(files.Purpose{
		Key: e2ePrivatePurpose, Bucket: files.BucketPrivate, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"},
		MaxBytes: 20 << 20, Image: true, Upload: true, Private: true,
	})
	if _, err := s.Owner.Pool.Exec(t.Context(),
		"INSERT INTO platform.file_purposes (key, bucket, mode) VALUES ($1, 'private', 'private') ON CONFLICT DO NOTHING",
		e2ePrivatePurpose); err != nil {
		t.Fatal(err)
	}
	signCfg := testkit.S3Config(t, testkit.S3App)
	signCfg.Endpoint = envOr("S3_IMAGER_ENDPOINT", "http://seaweedfs:8333")
	signer, err := files.NewS3Store(signCfg)
	if err != nil {
		t.Fatal(err)
	}
	o := files.ProcessorOptions{
		DB: s.App, Store: p.fs, Signer: signer, PresetVersion: 1,
		Imager: imager.NewClient("http://127.0.0.1:"+envOr("CRM_IMAGER_PORT", "4193"), &http.Client{}, 2),
	}
	p.w = queue.NewWorker(s.App, nil, queue.WorkerOptions{
		WorkerID: "e2e-imager", Commands: commands.NewRunner(s.App, []commands.Handler{
			files.NewProcessHandler(o), files.NewProcessPrivateHandler(o),
		}),
	})
	return p
}

// image đọc ảnh thử của Task M1-42 (testdata/images ở gốc module).
func image(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot, "testdata", "images", name))
	if err != nil {
		t.Fatalf("đọc ảnh thử %s: %v", name, err)
	}
	return b
}

const moduleRoot = "../.."

func (p *pipeline) inTenant(fn func(pgx.Tx) error) {
	p.t.Helper()
	if err := tenancy.WithTenant(p.t.Context(), p.staff.App, p.tenant, fn, db.TxAPI); err != nil {
		p.t.Fatal(err)
	}
}

// waitStatus chạy worker tới khi tệp rời trạng thái chờ (tối đa 30 s, TP01) và trả dòng tệp.
func (p *pipeline) waitStatus(id uuid.UUID) files.Info {
	p.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := p.w.RunOnce(p.t.Context(), 5); err != nil {
			p.t.Fatal(err)
		}
		var info files.Info
		p.inTenant(func(tx pgx.Tx) error {
			var err error
			info, err = files.Describe(p.t.Context(), tx, id, "https://img.test")
			return err //nolint:wrapcheck // test
		})
		if info.Status == files.StatusReady || info.Status == files.StatusRejected {
			return info
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("tệp %s chưa xử lý xong sau 30 s: %s", id, info.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (p *pipeline) read(b files.Bucket, key string) []byte {
	p.t.Helper()
	rc, err := p.fs.Open(p.t.Context(), b, key, 64<<20)
	if err != nil {
		p.t.Fatalf("mở %s/%s: %v", b, key, err)
	}
	defer func() { _ = rc.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		p.t.Fatal(err)
	}
	return buf.Bytes()
}

func (p *pipeline) absent(b files.Bucket, key string) {
	p.t.Helper()
	if _, err := p.fs.Head(p.t.Context(), b, key); !errors.Is(err, files.ErrObjectNotFound) {
		p.t.Fatalf("%s/%s không được có: %v", b, key, err)
	}
}

// hasEXIF: JPEG còn đoạn APP1 Exif (nơi chứa GPS).
func hasEXIF(b []byte) bool { return bytes.Contains(b, []byte("Exif\x00\x00")) }

// TP01: ảnh điện thoại có GPS tải lên như nhân viên → ready trong 30 s, 12 biến thể công khai không còn EXIF/GPS.
func TestFilesPipeline_PublicPhotoGPS(t *testing.T) {
	p := newPipeline(t)
	// Byte ngẫu nhiên sau EOI: mã băm mới mỗi lần, nên imager thật luôn được gọi (kho dev giữ bản kê của lần trước).
	body := append(image(t, "photo-gps.jpg"), []byte(uuid.NewString())...)
	if !hasEXIF(body) {
		t.Fatal("ảnh thử phải có EXIF")
	}
	var f files.File
	var put files.PresignedPut
	p.inTenant(func(tx pgx.Tx) error {
		var err error
		f, put, err = files.CreateUpload(t.Context(), tx, p.fs, files.UploadInput{
			TenantID: p.tenant, Purpose: "product_image", FileName: "photo-gps.jpg", ContentType: "image/jpeg",
			SizeBytes: int64(len(body)),
		}, uuid.Nil)
		return err //nolint:wrapcheck // test
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, put.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range put.Headers {
		req.Header.Set(k, v)
	}
	if code, _, _ := testkit.Do(t, req); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}
	if _, err := files.CompleteUpload(t.Context(), p.staff.App, p.fs, queue.NewQueue(), p.tenant, f.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	info := p.waitStatus(f.ID)
	if info.Status != files.StatusReady || len(info.Variants) != 12 {
		t.Fatalf("tệp: %s, %d biến thể (%s)", info.Status, len(info.Variants), info.RejectReason)
	}
	t.Logf("ready sau %v", time.Since(start))
	for _, v := range info.Variants {
		key := strings.TrimPrefix(v.URL, "https://img.test/")
		data := p.read(files.BucketPublic, key)
		if v.Format == "jpeg" && hasEXIF(data) {
			t.Fatalf("%s còn EXIF", key)
		}
		if bytes.Contains(data, []byte("GPS")) {
			t.Fatalf("%s còn chữ GPS", key)
		}
	}
}

// R10: ảnh khách qua RegisterPrivateImage → biến thể chỉ ở crm-private dưới priv/<tenant>/<file>/, không có ảnh gốc
// hay biến thể công khai, không còn EXIF; HTML đổi đuôi và bom điểm ảnh → rejected đúng lý do, không biến thể.
func TestFilesPipeline_PrivateImages(t *testing.T) {
	p := newPipeline(t)
	// Kho dev dùng chung giữa các lần chạy: thêm byte ngẫu nhiên sau ảnh (sau EOI của JPEG, sau IEND của PNG, bộ đọc
	// bỏ qua) để mã băm mới, nên "không có ảnh gốc, biến thể công khai của mã băm này" kiểm được.
	unique := func(name string) []byte { return append(image(t, name), []byte(uuid.NewString())...) }
	register := func(body []byte, name, contentType string) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if err := p.fs.Put(t.Context(), files.BucketTmp, files.TmpKey(p.tenant, id), bytes.NewReader(body), int64(len(body)), contentType); err != nil {
			t.Fatal(err)
		}
		p.inTenant(func(tx pgx.Tx) error {
			_, err := files.RegisterPrivateImage(t.Context(), tx, queue.NewQueue(), files.NewPrivateImage{
				ID: id, Purpose: e2ePrivatePurpose, Size: int64(len(body)), ContentType: contentType, FileName: name,
			})
			return err //nolint:wrapcheck // test
		})
		return id
	}
	gpsBody := unique("photo-gps.jpg")
	gps := register(gpsBody, "photo-gps.jpg", "image/jpeg")
	info := p.waitStatus(gps)
	if info.Status != files.StatusReady || len(info.Variants) != 0 {
		t.Fatalf("ảnh khách: %s, %d biến thể công khai", info.Status, len(info.Variants))
	}
	sum := sha256Hex(gpsBody)
	p.absent(files.BucketOriginals, files.OriginalKey(sum))
	specs, _ := files.Preset(1)
	for _, s := range specs {
		p.absent(files.BucketPublic, files.VariantKey(sum, 1, s.Width, s.Format))
		data := p.read(files.BucketPrivate, files.PrivateVariantKey(p.tenant, gps, 1, s.Width, s.Format))
		if (s.Format == "jpeg" && hasEXIF(data)) || bytes.Contains(data, []byte("GPS")) {
			t.Fatalf("biến thể riêng tư %d.%s còn siêu dữ liệu", s.Width, s.Format)
		}
	}
	var u string
	p.inTenant(func(tx pgx.Tx) error {
		var err error
		u, err = files.DownloadURL(t.Context(), tx, p.fs, gps, time.Minute, "")
		return err //nolint:wrapcheck // test
	})
	if !strings.Contains(u, "/crm-private/"+files.PrivateVariantKey(p.tenant, gps, 1, 1600, "jpeg")) {
		t.Fatalf("DownloadURL: %s", u)
	}
	for name, want := range map[string]string{"html-as.jpg": "not_image", "pixel-bomb.png": "too_many_pixels"} {
		ct := "image/jpeg"
		if strings.HasSuffix(name, ".png") {
			ct = "image/png"
		}
		id := register(unique(name), name, ct)
		if info := p.waitStatus(id); info.Status != files.StatusRejected || info.RejectReason != want {
			t.Fatalf("%s: %s %q, muốn rejected %s", name, info.Status, info.RejectReason, want)
		}
		for _, s := range specs {
			p.absent(files.BucketPrivate, files.PrivateVariantKey(p.tenant, id, 1, s.Width, s.Format))
		}
	}
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
