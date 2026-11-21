package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
)

func testKey(t *testing.T) string {
	t.Helper()
	return "test/" + uuid.NewString()
}

// adminClient: client S3 bằng danh tính crm-admin của kho dev (chỉ test đọc bản cũ theo phiên bản).
func adminClient(t *testing.T) *s3.Client {
	t.Helper()
	c := filestest.S3Config(t, filestest.S3Admin)
	return s3.New(s3.Options{
		Region: c.Region, BaseEndpoint: aws.String(c.Endpoint), UsePathStyle: true,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey}, nil
		}),
	})
}

// Hai bản hiện thực theo cùng hợp đồng FileStore: ghi, đọc, giới hạn, không ghi đè, chép nếu chưa có.
func TestFileStore_Contract(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			body := randomBytes(t, 2048)
			key := contractPutHead(t, store, body)
			contractOpen(t, store, key, body)
			contractCopy(t, store, key, body)
			contractPresignGet(t, store, key, body)
		})
	}
}

func contractPutHead(t *testing.T, store files.FileStore, body []byte) string {
	t.Helper()
	ctx := t.Context()
	key := testKey(t)
	if _, err := store.Head(ctx, files.BucketOriginals, key); !errors.Is(err, files.ErrObjectNotFound) {
		t.Fatalf("Head khóa chưa có: muốn ErrObjectNotFound, nhận %v", err)
	}
	// Mutation testing (Task 403): kích thước 0 bị từ chối, không tạo đối tượng rỗng.
	if err := store.Put(ctx, files.BucketOriginals, key, bytes.NewReader(nil), 0, "image/png"); err == nil {
		t.Fatal("Put 0 byte phải lỗi")
	}
	if err := store.Put(ctx, files.BucketOriginals, key, bytes.NewReader(body), int64(len(body)), "image/png"); err != nil {
		t.Fatal(err)
	}
	info, err := store.Head(ctx, files.BucketOriginals, key)
	if err != nil || info.Size != int64(len(body)) || info.ContentType != "image/png" || info.ETag == "" {
		t.Fatalf("Head: %+v %v", info, err)
	}
	if err := store.Put(ctx, files.BucketOriginals, key, bytes.NewReader(body[:10]), 10, "image/png"); !errors.Is(err, files.ErrObjectExists) {
		t.Fatalf("Put lần hai: muốn ErrObjectExists, nhận %v", err)
	}
	// Mutation testing (Task 403): nội dung đã đọc dở (vị trí khác 0) → ghi đúng phần còn lại.
	mid := testKey(t)
	r := bytes.NewReader(body)
	if _, err := r.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, files.BucketOriginals, mid, r, int64(len(body)-5), "image/png"); err != nil {
		t.Fatalf("Put từ vị trí 5: %v", err)
	}
	if info, err := store.Head(ctx, files.BucketOriginals, mid); err != nil || info.Size != int64(len(body)-5) {
		t.Fatalf("Head sau Put từ vị trí 5: %+v %v", info, err)
	}
	bad := testKey(t)
	if err := store.Put(ctx, files.BucketOriginals, bad, bytes.NewReader(body[:10]), 11, "image/png"); err == nil {
		t.Fatal("Put khai 11 byte mà gửi 10 phải lỗi")
	}
	if _, err := store.Head(ctx, files.BucketOriginals, bad); !errors.Is(err, files.ErrObjectNotFound) {
		t.Fatalf("Put khai sai kích thước vẫn ghi: %v", err)
	}
	return key
}

// Open có giới hạn: vượt maxBytes → ErrTooLarge (lúc mở hoặc lúc đọc); khóa chưa có → ErrObjectNotFound.
func contractOpen(t *testing.T, store files.FileStore, key string, body []byte) {
	t.Helper()
	if _, err := store.Open(t.Context(), files.BucketOriginals, testKey(t), 10); !errors.Is(err, files.ErrObjectNotFound) {
		t.Fatalf("Open khóa chưa có: muốn ErrObjectNotFound, nhận %v", err)
	}
	rc, err := store.Open(t.Context(), files.BucketOriginals, key, 1000)
	if err == nil {
		_, err = io.ReadAll(rc)
		_ = rc.Close()
	}
	if !errors.Is(err, files.ErrTooLarge) {
		t.Fatalf("Open %d byte với giới hạn 1000: muốn ErrTooLarge, nhận %v", len(body), err)
	}
	if got := readAll(t, store, files.BucketOriginals, key); !bytes.Equal(got, body) {
		t.Fatal("đọc lại sai nội dung")
	}
}

// CopyIfAbsent: đích chưa có → chép; đích đã có nội dung khác → không đổi, trả nil; nguồn không có → ErrObjectNotFound.
func contractCopy(t *testing.T, store files.FileStore, key string, body []byte) {
	t.Helper()
	ctx := t.Context()
	dst := testKey(t)
	if err := store.CopyIfAbsent(ctx, files.BucketOriginals, key, files.BucketPublic, dst); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, store, files.BucketPublic, dst); !bytes.Equal(got, body) {
		t.Fatal("CopyIfAbsent chép sai")
	}
	other := testKey(t)
	if err := store.Put(ctx, files.BucketOriginals, other, strings.NewReader("khac"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := store.CopyIfAbsent(ctx, files.BucketOriginals, other, files.BucketPublic, dst); err != nil {
		t.Fatalf("CopyIfAbsent lên đích đã có phải trả nil: %v", err)
	}
	if got := readAll(t, store, files.BucketPublic, dst); !bytes.Equal(got, body) {
		t.Fatal("CopyIfAbsent đã ghi đè đích")
	}
	if err := store.CopyIfAbsent(ctx, files.BucketOriginals, testKey(t), files.BucketPublic, testKey(t)); !errors.Is(err, files.ErrObjectNotFound) {
		t.Fatalf("CopyIfAbsent nguồn không có: muốn ErrObjectNotFound, nhận %v", err)
	}
}

// Đường dẫn GET ký sẵn có tên tải về (Content-Disposition attachment).
func contractPresignGet(t *testing.T, store files.FileStore, key string, body []byte) {
	t.Helper()
	url, err := store.PresignGet(t.Context(), files.BucketOriginals, key, time.Minute, "ảnh gốc.png")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	code, h, got := testkit.Do(t, req)
	if code != http.StatusOK || !bytes.Equal(got, body) || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Fatalf("GET ký sẵn: %d %q, %d byte", code, h.Get("Content-Disposition"), len(got))
	}
}

// Đường dẫn GET ký sẵn hết hạn thì bị từ chối.
func TestFileStore_PresignGetExpires(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			key := testKey(t)
			if err := store.Put(t.Context(), files.BucketPrivate, key, strings.NewReader("x"), 1, "text/plain"); err != nil {
				t.Fatal(err)
			}
			url, err := store.PresignGet(t.Context(), files.BucketPrivate, key, time.Second, "")
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(2100 * time.Millisecond)
			if code, _ := get(t, url); code == http.StatusOK {
				t.Fatal("đường dẫn đã hết hạn vẫn đọc được")
			}
		})
	}
}

// Delete bằng crm-app chỉ tạo dấu xóa: crm-admin vẫn đọc được bản cũ theo phiên bản (bucket giữ phiên bản, ADR-36).
func TestS3Store_DeleteKeepsVersion(t *testing.T) {
	store := filestest.S3FileStore(t)
	admin := adminClient(t)
	ctx := t.Context()
	for _, b := range []files.Bucket{files.BucketPublic, files.BucketOriginals, files.BucketPrivate} {
		key := testKey(t)
		if err := store.Put(ctx, b, key, strings.NewReader("con-day"), 7, "text/plain"); err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(ctx, b, key); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Head(ctx, b, key); !errors.Is(err, files.ErrObjectNotFound) {
			t.Fatalf("%s: sau Delete, Head phải ErrObjectNotFound, nhận %v", b, err)
		}
		bucket := filestest.S3Config(t, filestest.S3Admin).BucketPrefix + string(b)
		vs, err := admin.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(key)})
		if err != nil {
			t.Fatal(err)
		}
		if len(vs.Versions) != 1 || len(vs.DeleteMarkers) != 1 {
			t.Fatalf("%s: muốn 1 bản + 1 dấu xóa, có %d bản, %d dấu xóa", b, len(vs.Versions), len(vs.DeleteMarkers))
		}
		out, err := admin.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), VersionId: vs.Versions[0].VersionId})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(out.Body)
		_ = out.Body.Close()
		if string(got) != "con-day" {
			t.Fatalf("%s: bản cũ đọc được %q", b, got)
		}
	}
}

// MemStore mô phỏng cùng luật: dấu xóa, bản cũ còn.
func TestMemStore_DeleteKeepsVersion(t *testing.T) {
	store := filestest.MemFileStore(t)
	key := testKey(t)
	if err := store.Put(t.Context(), files.BucketPrivate, key, strings.NewReader("con-day"), 7, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), files.BucketPrivate, key); err != nil {
		t.Fatal(err)
	}
	vs := store.Versions(files.BucketPrivate, key)
	if len(vs) != 2 || string(vs[0]) != "con-day" || vs[1] != nil {
		t.Fatalf("muốn [bản cũ, dấu xóa], có %q", vs)
	}
}

func TestNewS3Store_Validates(t *testing.T) {
	if _, err := files.NewS3Store(files.S3Config{}); err == nil {
		t.Fatal("cấu hình rỗng phải lỗi")
	}
	cfg := files.S3Config{Endpoint: "http://127.0.0.1:1", Region: "us-east-1", AccessKeyID: "ak", SecretAccessKey: "bi-mat-khong-lo", BucketPrefix: "crm-"}
	if _, err := files.NewS3Store(cfg); err != nil {
		t.Fatalf("dựng S3Store không gọi mạng: %v", err)
	}
	if strings.Contains(cfg.LogValue().String(), "bi-mat-khong-lo") {
		t.Fatal("S3Config.LogValue lộ khóa bí mật")
	}
}

// PresignPutAnySize (việc (3) của Task 42): kích thước biến thể chỉ biết sau khi mã hóa, nên chữ ký không gồm
// content-length; vẫn gồm loại và If-None-Match (không ghi đè).
func TestFileStore_PresignPutAnySize(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			key := testKey(t)
			put, err := store.PresignPutAnySize(t.Context(), files.BucketPublic, key, "image/avif", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			body := randomBytes(t, 777)
			if code := putVia(t, put, body); code != http.StatusOK {
				t.Fatalf("PUT cỡ bất kỳ: %d", code)
			}
			info, err := store.Head(t.Context(), files.BucketPublic, key)
			if err != nil || info.Size != 777 || info.ContentType != "image/avif" {
				t.Fatalf("Head: %+v %v", info, err)
			}
			if code := putVia(t, put, randomBytes(t, 5)); code != http.StatusPreconditionFailed {
				t.Fatalf("ghi đè: muốn 412, nhận %d", code)
			}
			other, err := store.PresignPutAnySize(t.Context(), files.BucketPublic, testKey(t), "image/avif", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			other.Headers["Content-Type"] = "image/webp"
			if code := putVia(t, other, body); code != http.StatusForbidden {
				t.Fatalf("khác loại đã ký: muốn 403, nhận %d", code)
			}
		})
	}
}

// Mutation testing (Task 403): kho trả nhiều hơn số byte đã khai → lần đọc sau khi đủ left byte báo ErrTooLarge ngay
// (không trả 0 byte mà không lỗi, làm io.ReadAll quay mãi); đúng left byte thì hết bằng io.EOF.
func TestLimitedBody_Boundary(t *testing.T) {
	buf := make([]byte, 8)
	over := files.LimitedBody(io.NopCloser(strings.NewReader("abcdef")), 3)
	if n, err := over.Read(buf); n != 3 || err != nil {
		t.Fatalf("lần đọc đầu: %d %v", n, err)
	}
	if _, err := over.Read(buf); !errors.Is(err, files.ErrTooLarge) {
		t.Fatalf("đọc quá left: muốn ErrTooLarge, nhận %v", err)
	}
	exact, err := io.ReadAll(files.LimitedBody(io.NopCloser(strings.NewReader("abc")), 3))
	if err != nil || string(exact) != "abc" {
		t.Fatalf("đúng left byte: %q %v", exact, err)
	}
}

// Mutation testing (Task 403): ký PUT với tham số sai bị từ chối ở cả hai kho; header trả về không gồm Host,
// Content-Length (trình duyệt tự đặt).
func TestFileStore_PresignRejectsBadParams(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			for what, err := range map[string]error{
				"cỡ 0":               second(store.PresignPut(ctx, files.BucketTmp, testKey(t), 0, "image/jpeg", time.Minute)),
				"hạn 0":              second(store.PresignPut(ctx, files.BucketTmp, testKey(t), 10, "image/jpeg", 0)),
				"bất kỳ, hạn 0":      second(store.PresignPutAnySize(ctx, files.BucketTmp, testKey(t), "image/jpeg", 0)),
				"bất kỳ, không loại": second(store.PresignPutAnySize(ctx, files.BucketTmp, testKey(t), "", time.Minute)),
			} {
				if err == nil {
					t.Errorf("%s: phải lỗi", what)
				}
			}
			put, err := store.PresignPut(ctx, files.BucketTmp, testKey(t), 10, "image/jpeg", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := put.Headers["Host"]; ok || put.Headers["Content-Length"] != "" || put.Headers["Content-Type"] != "image/jpeg" {
				t.Errorf("header của PUT ký sẵn: %v", put.Headers)
			}
		})
	}
}

// Mutation testing (Task 403): Open đúng maxBytes byte thì đọc đủ ở cả hai kho.
func TestFileStore_OpenExactlyMaxBytes(t *testing.T) {
	for name, store := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			body := randomBytes(t, 64)
			key := testKey(t)
			if err := store.Put(ctx, files.BucketOriginals, key, bytes.NewReader(body), int64(len(body)), "image/png"); err != nil {
				t.Fatal(err)
			}
			rc, err := store.Open(ctx, files.BucketOriginals, key, int64(len(body)))
			if err != nil {
				t.Fatalf("Open đúng maxBytes: %v", err)
			}
			got, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil || !bytes.Equal(got, body) {
				t.Fatalf("đọc đúng maxBytes: %d byte, %v", len(got), err)
			}
		})
	}
}

func second[T any](_ T, err error) error { return err }
