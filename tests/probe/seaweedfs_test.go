//go:build probe

// Package probe đo các tính chất của SeaweedFS bản ghim mà ADR-36 và ADR-44 dựa vào (kế hoạch M1, Task M1-40),
// trên compose dev (`make dev`). Không phải mã ứng dụng: ứng dụng chỉ nói chuyện với kho qua `files.FileStore`
// (Task M1-41). Kết quả và số đo ghi ở docs/do-luong/seaweedfs-m1.md.
//
//	CRM_S3_PORT=8440 CRM_S3_OFFSITE_PORT=8441 go test -tags probe -count=1 -v ./tests/probe/...
package probe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Bốn bucket của kho chính (kế hoạch M1-40, Interfaces); ba bucket sau giữ phiên bản và được chép ra kho ngoài.
const (
	bucketTmp       = "crm-tmp"
	bucketPublic    = "crm-public"
	bucketOriginals = "crm-originals"
	bucketPrivate   = "crm-private"
	consoleOrigin   = "http://localhost:4192"
)

func versionedBuckets() []string { return []string{bucketPublic, bucketOriginals, bucketPrivate} }

// identity là một danh tính trong docker/seaweedfs/s3.json (khóa dev cố định, chỉ cho compose dev).
type identity struct {
	Name        string `json:"name"`
	Credentials []struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
	} `json:"credentials"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("không biết vị trí tệp test")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// keys đọc khóa của danh tính name trong tệp cấu hình file (s3.json hoặc s3-offsite.json).
func keys(t *testing.T, file, name string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docker", "seaweedfs", file))
	if err != nil {
		t.Fatalf("đọc %s: %v", file, err)
	}
	var cfg struct {
		Identities []identity `json:"identities"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("giải %s: %v", file, err)
	}
	for _, id := range cfg.Identities {
		if id.Name == name && len(id.Credentials) > 0 {
			return id.Credentials[0].AccessKey, id.Credentials[0].SecretKey
		}
	}
	t.Fatalf("%s không có danh tính %s", file, name)
	return "", ""
}

func port(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

func mainEndpoint() string    { return "http://127.0.0.1:" + port("CRM_S3_PORT", "8333") }
func offsiteEndpoint() string { return "http://127.0.0.1:" + port("CRM_S3_OFFSITE_PORT", "8334") }

// newClient dựng client S3 kiểu đường dẫn (ADR-44) không đọc ~/.aws; opts chỉnh thêm (ví dụ cách tính tổng kiểm).
func newClient(t *testing.T, endpoint, file, name string, opts ...func(*s3.Options)) *s3.Client {
	t.Helper()
	ak, sk := keys(t, file, name)
	creds := aws.CredentialsProviderFunc(func(_ context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: ak, SecretAccessKey: sk, Source: "probe"}, nil
	})
	base := []func(*s3.Options){func(o *s3.Options) {
		o.Region = "us-east-1"
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		o.Credentials = creds
		o.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}}
	return s3.New(s3.Options{}, append(base, opts...)...)
}

func appClient(t *testing.T) *s3.Client { return newClient(t, mainEndpoint(), "s3.json", "crm-app") }

func adminClient(t *testing.T) *s3.Client {
	return newClient(t, mainEndpoint(), "s3.json", "crm-admin")
}

func uniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("probe/%s/%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func errCode(err error) string {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return ae.ErrorCode()
	}
	return ""
}

func requireDenied(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: được phép, cần bị từ chối", what)
	}
	if c := errCode(err); c != "AccessDenied" {
		t.Fatalf("%s: lỗi %q (%v), cần AccessDenied", what, c, err)
	}
}

func httpClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

// Mục 1: PutObject với cấu hình kiểm tổng mặc định của aws-sdk-go-v2 bản ghim (ADR-44 "thử ở M1").
func TestPutObject_DefaultChecksum(t *testing.T) {
	ctx := t.Context()
	key := uniqueKey(t)
	body := randomBytes(t, 4096)
	_, err := appClient(t).PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketTmp), Key: aws.String(key), Body: bytes.NewReader(body),
	})
	if err != nil {
		t.Logf("mặc định (WhenSupported) lỗi: %v; thử WhenRequired", err)
		c := newClient(t, mainEndpoint(), "s3.json", "crm-app", func(o *s3.Options) {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		})
		if _, err2 := c.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketTmp), Key: aws.String(key), Body: bytes.NewReader(body),
		}); err2 != nil {
			t.Fatalf("cả WhenRequired cũng lỗi: %v", err2)
		}
		t.Fatalf("KẾT QUẢ: mặc định không được, phải đặt RequestChecksumCalculation = WhenRequired")
	}
	out, err := appClient(t).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucketTmp), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	got, err := io.ReadAll(out.Body)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("đọc lại khác byte đã ghi (err=%v)", err)
	}
	t.Log("KẾT QUẢ: PutObject với kiểm tổng mặc định (WhenSupported, CRC32) được, đọc lại đúng byte")
}

// Mục 2: đường dẫn ký sẵn PUT có ký content-length = 1000.
func TestPresignedPut_ContentLengthSigned(t *testing.T) {
	ctx := t.Context()
	pc := s3.NewPresignClient(appClient(t), s3.WithPresignExpires(5*time.Minute))
	put := func(key string, n int) (int, string) {
		req, err := pc.PresignPutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketTmp), Key: aws.String(key), ContentLength: aws.Int64(1000),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(req.URL), "content-length") {
			t.Fatalf("chữ ký không gồm content-length: %s", req.URL)
		}
		hr, err := http.NewRequestWithContext(ctx, http.MethodPut, req.URL, bytes.NewReader(randomBytes(t, n)))
		if err != nil {
			t.Fatal(err)
		}
		for k, vs := range req.SignedHeader {
			if strings.EqualFold(k, "content-length") || strings.EqualFold(k, "host") {
				continue
			}
			for _, v := range vs {
				hr.Header.Add(k, v)
			}
		}
		res, err := httpClient().Do(hr)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := put(uniqueKey(t)+"-1000", 1000); code != http.StatusOK {
		t.Fatalf("gửi 1.000 byte: %d %s", code, body)
	}
	key := uniqueKey(t) + "-1001"
	code, body := put(key, 1001)
	if code == http.StatusOK {
		t.Fatalf("gửi 1.001 byte vào chữ ký 1.000 byte: được nhận, cần bị từ chối")
	}
	if _, err := adminClient(t).HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketTmp), Key: aws.String(key)}); err == nil {
		t.Fatal("bị từ chối nhưng đối tượng vẫn được ghi")
	}
	t.Logf("KẾT QUẢ: 1.000 byte → 200; 1.001 byte → %d (%s)", code, firstLine(body))
}

func firstLine(s string) string {
	if i := strings.Index(s, "<Code>"); i >= 0 {
		if j := strings.Index(s[i:], "</Code>"); j > 0 {
			return s[i+6 : i+j]
		}
	}
	return strings.SplitN(s, "\n", 2)[0]
}

// Mục 3: đường dẫn ký sẵn GET hạn 2 s.
func TestPresignedGet_Expires(t *testing.T) {
	ctx := t.Context()
	key := uniqueKey(t)
	if _, err := appClient(t).PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketPrivate), Key: aws.String(key), Body: bytes.NewReader([]byte("riêng tư")),
	}); err != nil {
		t.Fatal(err)
	}
	req, err := s3.NewPresignClient(appClient(t), s3.WithPresignExpires(2*time.Second)).
		PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucketPrivate), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	get := func() int {
		hr, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := httpClient().Do(hr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		return res.StatusCode
	}
	if c := get(); c != http.StatusOK {
		t.Fatalf("đọc ngay: %d", c)
	}
	time.Sleep(3 * time.Second)
	if c := get(); c != http.StatusForbidden {
		t.Fatalf("đọc sau 3 s: %d, cần 403", c)
	}
	t.Log("KẾT QUẢ: đọc ngay → 200; sau 3 s → 403")
}

// Mục 4 và 5: xóa không VersionId tạo dấu xóa, bản cũ còn; crm-app không xóa vĩnh viễn được (ADR-36).
func TestDelete_AppCannotPurge(t *testing.T) {
	app, admin := appClient(t), adminClient(t)
	for _, b := range versionedBuckets() {
		t.Run(b, func(t *testing.T) { checkNoPurge(t, app, admin, b) })
	}
	t.Log("KẾT QUẢ: dấu xóa được tạo, crm-admin đọc bản cũ; crm-app DeleteObject/DeleteObjects có VersionId → AccessDenied")
}

// checkNoPurge: trên bucket b, xóa thường tạo dấu xóa; crm-app không xóa vĩnh viễn được phiên bản nào.
func checkNoPurge(t *testing.T, app, admin *s3.Client, b string) {
	t.Helper()
	ctx := t.Context()
	key := uniqueKey(t)
	put, err := app.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b), Key: aws.String(key), Body: bytes.NewReader([]byte("bản 1")),
	})
	if err != nil {
		t.Fatal(err)
	}
	v1 := aws.ToString(put.VersionId)
	if v1 == "" || v1 == "null" {
		t.Fatalf("bucket %s không trả VersionId: giữ phiên bản chưa bật", b)
	}
	del, err := app.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b), Key: aws.String(key)})
	if err != nil {
		t.Fatalf("crm-app xóa không VersionId: %v", err)
	}
	if !aws.ToBool(del.DeleteMarker) {
		t.Fatal("xóa không VersionId không tạo dấu xóa")
	}
	if _, err := app.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b), Key: aws.String(key)}); err == nil {
		t.Fatal("sau dấu xóa vẫn đọc được bản mới nhất")
	}
	out, err := admin.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b), Key: aws.String(key), VersionId: aws.String(v1)})
	if err != nil {
		t.Fatalf("crm-admin đọc bản cũ theo VersionId: %v", err)
	}
	got, _ := io.ReadAll(out.Body)
	_ = out.Body.Close()
	if string(got) != "bản 1" {
		t.Fatalf("bản cũ đọc ra %q", got)
	}

	// Mục 5: xóa vĩnh viễn một phiên bản, theo cả hai đường của S3.
	_, err = app.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b), Key: aws.String(key), VersionId: aws.String(v1)})
	requireDenied(t, "crm-app DeleteObject có VersionId", err)
	_, err = app.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b), Key: aws.String(key), VersionId: del.VersionId})
	requireDenied(t, "crm-app xóa dấu xóa (DeleteObject có VersionId của dấu xóa)", err)
	multi, err := app.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(b), Delete: &types.Delete{
		Objects: []types.ObjectIdentifier{{Key: aws.String(key), VersionId: aws.String(v1)}},
	}})
	if err == nil && (len(multi.Errors) != 1 || aws.ToString(multi.Errors[0].Code) != "AccessDenied") {
		t.Fatalf("crm-app DeleteObjects có VersionId: deleted=%d errors=%v, cần AccessDenied", len(multi.Deleted), multi.Errors)
	}
	if err != nil {
		requireDenied(t, "crm-app DeleteObjects có VersionId", err)
	}
	if _, err := admin.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b), Key: aws.String(key), VersionId: aws.String(v1)}); err != nil {
		t.Fatalf("sau các lần crm-app thử xóa, bản cũ mất: %v", err)
	}
}

// Các đường khác để làm mất bản cũ: tắt giữ phiên bản, đặt hạn cho bản cũ, sửa hay xóa chính sách bucket.
func TestAppCannotLoosenBuckets(t *testing.T) {
	ctx := t.Context()
	app := appClient(t)
	for _, b := range append([]string{bucketTmp, bucketExports}, versionedBuckets()...) {
		t.Run(b, func(t *testing.T) {
			_, err := app.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
				Bucket:                  aws.String(b),
				VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusSuspended},
			})
			requireDenied(t, "crm-app tắt giữ phiên bản", err)
			_, err = app.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
				Bucket: aws.String(b),
				LifecycleConfiguration: &types.BucketLifecycleConfiguration{Rules: []types.LifecycleRule{{
					ID: aws.String("xoa-ban-cu"), Status: types.ExpirationStatusEnabled,
					Filter:                      &types.LifecycleRuleFilter{Prefix: aws.String("")},
					NoncurrentVersionExpiration: &types.NoncurrentVersionExpiration{NoncurrentDays: aws.Int32(1)},
				}}},
			})
			requireDenied(t, "crm-app đặt quy tắc hết hạn", err)
			_, err = app.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{Bucket: aws.String(b)})
			requireDenied(t, "crm-app xóa quy tắc hết hạn", err)
			_, err = app.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
				Bucket: aws.String(b), Policy: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
			})
			requireDenied(t, "crm-app ghi chính sách bucket", err)
			_, err = app.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(b)})
			requireDenied(t, "crm-app xóa chính sách bucket", err)
			_, err = app.PutBucketCors(ctx, &s3.PutBucketCorsInput{
				Bucket: aws.String(b),
				CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{{
					AllowedOrigins: []string{"*"}, AllowedMethods: []string{http.MethodPut},
				}}},
			})
			requireDenied(t, "crm-app ghi CORS", err)
			_, err = app.DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{Bucket: aws.String(b)})
			requireDenied(t, "crm-app xóa CORS", err)
			_, err = app.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(b)})
			requireDenied(t, "crm-app xóa bucket", err)
		})
	}
	if _, err := app.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("crm-probe-moi")}); err == nil {
		t.Fatal("crm-app tạo được bucket mới")
	}
}

// Mục 6: quy tắc hết hạn 1 ngày của crm-tmp đọc lại được.
func TestTmpExpiration(t *testing.T) {
	out, err := adminClient(t).GetBucketLifecycleConfiguration(t.Context(),
		&s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucketTmp)})
	if err != nil {
		t.Fatalf("đọc quy tắc hết hạn của crm-tmp: %v", err)
	}
	ok := slices.ContainsFunc(out.Rules, func(r types.LifecycleRule) bool {
		return r.Status == types.ExpirationStatusEnabled && r.Expiration != nil && aws.ToInt32(r.Expiration.Days) == 1
	})
	if !ok {
		t.Fatalf("crm-tmp không có quy tắc Expiration Days=1 đang bật: %+v", out.Rules)
	}
	// Các bucket giữ lâu không được có hạn.
	for _, b := range versionedBuckets() {
		lc, err := adminClient(t).GetBucketLifecycleConfiguration(t.Context(),
			&s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(b)})
		if err == nil && len(lc.Rules) > 0 {
			t.Fatalf("bucket %s có quy tắc hết hạn: %+v", b, lc.Rules)
		}
	}
	t.Log("KẾT QUẢ: crm-tmp có quy tắc S3 Expiration Days=1 (PutBucketLifecycleConfiguration), ba bucket kia không có")
}

// Mục 7: tải nhiều phần 3 × 5 MiB.
func TestMultipartUpload(t *testing.T) {
	ctx := t.Context()
	app := appClient(t)
	key := uniqueKey(t)
	mu, err := app.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucketTmp), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	var parts []types.CompletedPart
	var all []byte
	for i := int32(1); i <= 3; i++ {
		chunk := randomBytes(t, 5<<20)
		all = append(all, chunk...)
		up, err := app.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(bucketTmp), Key: aws.String(key), UploadId: mu.UploadId,
			PartNumber: aws.Int32(i), Body: bytes.NewReader(chunk),
		})
		if err != nil {
			t.Fatalf("phần %d: %v", i, err)
		}
		parts = append(parts, types.CompletedPart{
			ETag: up.ETag, PartNumber: aws.Int32(i), ChecksumCRC32: up.ChecksumCRC32,
		})
	}
	if _, err := app.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucketTmp), Key: aws.String(key), UploadId: mu.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		t.Fatalf("ghép phần: %v", err)
	}
	out, err := app.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucketTmp), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(out.Body)
	_ = out.Body.Close()
	if !bytes.Equal(got, all) {
		t.Fatalf("đọc lại %d byte, khác %d byte đã ghi", len(got), len(all))
	}
	t.Log("KẾT QUẢ: 3 phần × 5 MiB ghép được, đọc lại đúng 15 MiB")
}

// Mục 8: CORS của crm-tmp cho phép PUT từ gốc của màn hình, không cho gốc lạ.
func TestTmpCORS(t *testing.T) {
	preflight := func(origin string) http.Header { return preflightTo(t, bucketTmp, origin) }
	h := preflight(consoleOrigin)
	if !strings.Contains(h.Get("Access-Control-Allow-Methods"), http.MethodPut) ||
		(h.Get("Access-Control-Allow-Origin") != consoleOrigin && h.Get("Access-Control-Allow-Origin") != "*") {
		t.Fatalf("OPTIONS từ %s: Allow-Origin=%q Allow-Methods=%q", consoleOrigin,
			h.Get("Access-Control-Allow-Origin"), h.Get("Access-Control-Allow-Methods"))
	}
	t.Logf("KẾT QUẢ: OPTIONS từ %s → Allow-Origin=%q Allow-Methods=%q", consoleOrigin,
		h.Get("Access-Control-Allow-Origin"), h.Get("Access-Control-Allow-Methods"))
	evil := preflight("http://evil.example")
	t.Logf("gốc lạ http://evil.example → Allow-Origin=%q Allow-Methods=%q",
		evil.Get("Access-Control-Allow-Origin"), evil.Get("Access-Control-Allow-Methods"))
	if evil.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("gốc lạ cũng được CORS cho phép")
	}
	// CORS chỉ cho crm-tmp: bucket khác không nhận tải thẳng từ trình duyệt, kể cả từ gốc của màn hình.
	for _, b := range versionedBuckets() {
		h := preflightTo(t, b, consoleOrigin)
		if h.Get("Access-Control-Allow-Origin") != "" || strings.Contains(h.Get("Access-Control-Allow-Methods"), http.MethodPut) {
			t.Fatalf("OPTIONS %s từ %s: Allow-Origin=%q Allow-Methods=%q, cần không cho phép", b, consoleOrigin,
				h.Get("Access-Control-Allow-Origin"), h.Get("Access-Control-Allow-Methods"))
		}
	}
}

// preflightTo gửi OPTIONS (CORS) xin PUT vào bucket b từ origin, trả header trả lời.
func preflightTo(t *testing.T, b, origin string) http.Header {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, mainEndpoint()+"/"+b+"/probe/cors", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res, err := httpClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	return res.Header
}

// Mục 9: PutObject với If-None-Match: * lên khóa đã có.
func TestPutIfNoneMatch(t *testing.T) {
	ctx := t.Context()
	app := appClient(t)
	key := uniqueKey(t)
	in := func(body string) *s3.PutObjectInput {
		return &s3.PutObjectInput{
			Bucket: aws.String(bucketOriginals), Key: aws.String(key), Body: strings.NewReader(body), IfNoneMatch: aws.String("*"),
		}
	}
	if _, err := app.PutObject(ctx, in("lần 1")); err != nil {
		t.Fatalf("ghi lần đầu với If-None-Match: %v", err)
	}
	_, err := app.PutObject(ctx, in("lần 2"))
	if err == nil {
		t.Log("KẾT QUẢ: KHÔNG hỗ trợ — If-None-Match: * vẫn ghi đè; FileStore phải HeadObject trước")
		t.Fail()
		return
	}
	t.Logf("KẾT QUẢ: lần ghi thứ hai bị từ chối: %s", errCode(err))
	if c := errCode(err); c != "PreconditionFailed" {
		t.Fatalf("mã lỗi %q, cần PreconditionFailed", c)
	}
	out, err := app.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucketOriginals), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(out.Body)
	_ = out.Body.Close()
	if string(got) != "lần 1" {
		t.Fatalf("nội dung bị ghi đè: %q", got)
	}
}

// Danh tính crm-filesync: đọc kho chính, không ghi; ghi kho ngoài giả, không xóa ở đó (chỉ chép thêm, ADR-36).
func TestFilesyncIdentity(t *testing.T) {
	ctx := t.Context()
	onMain := newClient(t, mainEndpoint(), "s3.json", "crm-filesync")
	onOffsite := newClient(t, offsiteEndpoint(), "s3-offsite.json", "crm-filesync")
	key := uniqueKey(t)
	if _, err := appClient(t).PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketPublic), Key: aws.String(key), Body: strings.NewReader("ảnh"),
	}); err != nil {
		t.Fatal(err)
	}
	for _, b := range versionedBuckets() {
		if _, err := onMain.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(b), MaxKeys: aws.Int32(1)}); err != nil {
			t.Fatalf("crm-filesync liệt kê %s ở kho chính: %v", b, err)
		}
	}
	if _, err := onMain.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucketPublic), Key: aws.String(key)}); err != nil {
		t.Fatalf("crm-filesync đọc kho chính: %v", err)
	}
	_, err := onMain.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucketPublic), Key: aws.String(key + "-w"), Body: strings.NewReader("x")})
	requireDenied(t, "crm-filesync ghi kho chính", err)
	_, err = onMain.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucketPublic), Key: aws.String(key)})
	requireDenied(t, "crm-filesync xóa ở kho chính", err)
	if _, err := onMain.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucketTmp)}); err == nil {
		t.Fatal("crm-filesync đọc được crm-tmp (không chép, không cần quyền)")
	}

	for _, b := range versionedBuckets() {
		put, err := onOffsite.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(b), Key: aws.String(key), Body: strings.NewReader("bản sao")})
		if err != nil {
			t.Fatalf("crm-filesync ghi kho ngoài %s: %v", b, err)
		}
		_, err = onOffsite.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b), Key: aws.String(key)})
		requireDenied(t, "crm-filesync xóa ở kho ngoài "+b, err)
		_, err = onOffsite.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b), Key: aws.String(key), VersionId: put.VersionId})
		requireDenied(t, "crm-filesync xóa vĩnh viễn ở kho ngoài "+b, err)
		_, err = onOffsite.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket:                  aws.String(b),
			VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusSuspended},
		})
		requireDenied(t, "crm-filesync tắt giữ phiên bản ở kho ngoài "+b, err)
		_, err = onOffsite.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(b)})
		requireDenied(t, "crm-filesync xóa chính sách ở kho ngoài "+b, err)
		_, err = onOffsite.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
			Bucket: aws.String(b),
			LifecycleConfiguration: &types.BucketLifecycleConfiguration{Rules: []types.LifecycleRule{{
				ID: aws.String("xoa"), Status: types.ExpirationStatusEnabled,
				Filter:     &types.LifecycleRuleFilter{Prefix: aws.String("")},
				Expiration: &types.LifecycleExpiration{Days: aws.Int32(1)},
			}}},
		})
		requireDenied(t, "crm-filesync đặt hạn ở kho ngoài "+b, err)
	}
	t.Log("KẾT QUẢ: crm-filesync đọc kho chính, không ghi/xóa ở đó; ghi kho ngoài, không xóa ở đó")
}

// Mục 10: bộ nhớ của SeaweedFS lúc rảnh và khi ghi 1.000 tệp 200 KB (ADR-36 "chưa xác minh").
func TestMemory(t *testing.T) {
	ctx := t.Context()
	idle := dockerMem(t)
	t.Logf("SỐ ĐO: bộ nhớ lúc rảnh %s", idle)
	app := appClient(t)
	body := randomBytes(t, 200<<10)
	prefix := uniqueKey(t)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
		peak  string
		peakB float64
	)
	sem := make(chan struct{}, 16)
	stop := make(chan struct{})
	samplerDone := make(chan struct{})
	go func() { // lấy mẫu bộ nhớ trong lúc ghi
		defer close(samplerDone)
		for {
			select {
			case <-stop:
				return
			case <-time.After(500 * time.Millisecond):
				s, err := dockerMemErr(t.Context())
				if err != nil {
					mu.Lock()
					first = cmpOr(first, err)
					mu.Unlock()
					return
				}
				if b := parseMiB(s); b > peakB {
					peakB, peak = b, s
				}
			}
		}
	}()
	start := time.Now()
	for i := range 1000 {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, err := app.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(bucketOriginals), Key: aws.String(prefix + "/" + strconv.Itoa(i)), Body: bytes.NewReader(body),
			})
			if err != nil {
				mu.Lock()
				first = cmpOr(first, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	close(stop)
	<-samplerDone
	if first != nil {
		t.Fatalf("ghi 1.000 tệp: %v", first)
	}
	after := dockerMem(t)
	t.Logf("SỐ ĐO: ghi 1.000 × 200 KB (16 luồng) trong %s; bộ nhớ cao nhất khi ghi %s; ngay sau khi ghi %s",
		elapsed.Round(time.Millisecond), peak, after)
}

func cmpOr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// dockerMem đọc "MemUsage" của container đang mở cổng S3 chính.
func dockerMem(t *testing.T) string {
	t.Helper()
	s, err := dockerMemErr(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dockerMemErr như dockerMem nhưng trả lỗi, để gọi được từ goroutine phụ (không gọi t.Fatal ngoài goroutine của test).
func dockerMemErr(ctx context.Context) (string, error) {
	id, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "publish="+port("CRM_S3_PORT", "8333")).Output()
	if err != nil || len(bytes.TrimSpace(id)) == 0 {
		return "", fmt.Errorf("tìm container SeaweedFS chính: %w (id=%q)", err, id)
	}
	out, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{.MemUsage}}",
		strings.TrimSpace(string(id))).Output()
	if err != nil {
		return "", fmt.Errorf("docker stats: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// parseMiB đổi phần đầu của "123.4MiB / 7.6GiB" ra MiB.
func parseMiB(s string) float64 {
	v, _, _ := strings.Cut(s, " /")
	mult := 1.0
	switch {
	case strings.HasSuffix(v, "GiB"):
		mult, v = 1024, strings.TrimSuffix(v, "GiB")
	case strings.HasSuffix(v, "MiB"):
		v = strings.TrimSuffix(v, "MiB")
	case strings.HasSuffix(v, "KiB"):
		mult, v = 1.0/1024, strings.TrimSuffix(v, "KiB")
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f * mult
}

// Mục 11: compose dev hợp lệ; không profile thì không có ba dịch vụ chưa có mã.
func TestComposeConfig(t *testing.T) {
	file := filepath.Join(repoRoot(t), "docker", "compose.dev.yml")
	services := func(args ...string) []string {
		cmd := exec.CommandContext(t.Context(), "docker", append([]string{"compose", "-f", file}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker compose %v: %v\n%s", args, err, out)
		}
		return strings.Fields(string(out))
	}
	plain := services("config", "--services")
	for _, s := range []string{"postgres", "jaeger", "seaweedfs", "seaweedfs-offsite", "s3-bootstrap"} {
		if !slices.Contains(plain, s) {
			t.Fatalf("không profile: thiếu dịch vụ %s (%v)", s, plain)
		}
	}
	for _, s := range []string{"imager", "fakecarrier", "filesync"} {
		if slices.Contains(plain, s) {
			t.Fatalf("không profile mà có %s: docker compose up sẽ đòi build mã chưa có", s)
		}
		withProfile := services("--profile", s, "config", "--services")
		if !slices.Contains(withProfile, s) {
			t.Fatalf("profile %s không bật dịch vụ %s", s, s)
		}
	}
	t.Logf("KẾT QUẢ: không profile: %v; ba dịch vụ còn lại chỉ có khi bật profile", plain)
}
