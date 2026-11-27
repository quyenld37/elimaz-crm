//go:build probe

package probe

import (
	"bytes"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Bucket tệp xuất crm-exports (Task 515; kế hoạch M6 G5 chốt (a)): không giữ phiên bản, không chép kho ngoài, tự xóa
// theo tiền tố của khóa: reports/ sau 7 ngày, customers/ sau 1 ngày. Như crm-tmp, xóa thật là nhờ lifecycle TTL fast
// path (đo 27/11/2026: PutObject gắn ttlSec theo luật Expiration khớp tiền tố của khóa), docs/do-luong/seaweedfs-m1.md.

const bucketExports = "crm-exports"

// crm-exports không giữ phiên bản: PutObject không trả VersionId, trạng thái giữ phiên bản rỗng (chưa từng bật).
func TestExports_NotVersioned(t *testing.T) {
	ctx := t.Context()
	v, err := adminClient(t).GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucketExports)})
	if err != nil {
		t.Fatalf("đọc giữ phiên bản của crm-exports: %v", err)
	}
	if v.Status != "" {
		t.Fatalf("crm-exports giữ phiên bản: %q, muốn chưa từng bật (fast path không áp cho bucket giữ phiên bản)", v.Status)
	}
	put, err := appClient(t).PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketExports), Key: aws.String("reports/" + uniqueKey(t)), Body: bytes.NewReader([]byte("x")),
	})
	if err != nil {
		t.Fatalf("crm-app ghi crm-exports: %v", err)
	}
	if id := aws.ToString(put.VersionId); id != "" && id != "null" {
		t.Fatalf("PutObject vào crm-exports trả VersionId %q", id)
	}
	t.Log("KẾT QUẢ: crm-exports không giữ phiên bản")
}

// Hai luật Expiration theo tiền tố đọc lại đúng, đang bật; fast path của crm-exports bật.
func TestExports_LifecycleRules(t *testing.T) {
	out, err := adminClient(t).GetBucketLifecycleConfiguration(t.Context(),
		&s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucketExports)})
	if err != nil {
		t.Fatalf("đọc quy tắc hết hạn của crm-exports: %v", err)
	}
	got := map[string]int32{}
	for _, r := range out.Rules {
		if r.Status != types.ExpirationStatusEnabled || r.Expiration == nil || r.Filter == nil {
			t.Fatalf("luật không bật hay thiếu Expiration/Filter: %+v", r)
		}
		got[aws.ToString(r.Filter.Prefix)] = aws.ToInt32(r.Expiration.Days)
	}
	// Hai luật hết hạn theo tiền tố (QĐ-515-1), đọc lại bằng crm-admin.
	if len(got) != 2 || got["reports/"] != 7 || got["customers/"] != 1 {
		t.Fatalf("luật hết hạn của crm-exports: %v, muốn reports/ 7, customers/ 1", got)
	}
	if out := weedShell(t, "s3.bucket.lifecycle.fastpath -name "+bucketExports); !bytes.Contains([]byte(out), []byte("fast path: enabled")) {
		t.Fatalf("crm-exports chưa bật lifecycle TTL fast path (bootstrap.sh):\n%s", out)
	}
	t.Log("KẾT QUẢ: reports/ 7 ngày, customers/ 1 ngày, fast path bật")
}

// Đường thật của tệp xuất: crm-app PutObject (files.S3Store.Put). Khóa reports/… mang ttlSec 7 ngày, customers/… 1
// ngày, dữ liệu ở volume có hạn của collection crm-exports; khóa ngoài hai tiền tố không có hạn (ứng dụng không bao giờ
// ghi khóa như vậy: files.Register đòi tiền tố của mục đích).
func TestExports_UploadStampedByPrefix(t *testing.T) {
	cases := []struct {
		prefix string
		ttlSec int
		volTTL string
	}{{"reports/", 7 * 86400, "1w"}, {"customers/", 86400, "1d"}}
	body := randomBytes(t, 2048)
	for _, c := range cases {
		key := c.prefix + uniqueKey(t)
		if _, err := appClient(t).PutObject(t.Context(), &s3.PutObjectInput{
			Bucket: aws.String(bucketExports), Key: aws.String(key), Body: bytes.NewReader(body),
		}); err != nil {
			t.Fatalf("ghi %s: %v", key, err)
		}
		m := metaOf(t, "/buckets/"+bucketExports+"/"+key)
		if m.Attributes.TTLSec != c.ttlSec {
			t.Fatalf("%s: ttlSec %d, muốn %d", c.prefix, m.Attributes.TTLSec, c.ttlSec)
		}
		requireChunksIn(t, "tệp xuất "+c.prefix, m, bucketExports, c.volTTL)
		t.Logf("KẾT QUẢ: %s → ttlSec %d, volume %d Ttl %s", c.prefix, m.Attributes.TTLSec, m.Chunks[0].Fid.VolumeID, c.volTTL)
	}
}

// crm-app không xóa được tệp xuất (bucket không giữ phiên bản: xóa là mất hẳn, ADR-36); dọn là việc của luật hết hạn.
func TestExports_AppCannotDelete(t *testing.T) {
	ctx := t.Context()
	app := appClient(t)
	key := "reports/" + uniqueKey(t)
	if _, err := app.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketExports), Key: aws.String(key), Body: bytes.NewReader([]byte("x")),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := app.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucketExports), Key: aws.String(key)})
	requireDenied(t, "crm-app xóa tệp xuất", err)
	if _, err := app.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketExports), Key: aws.String(key)}); err != nil {
		t.Fatalf("sau lần thử xóa bị từ chối, tệp mất: %v", err)
	}
	t.Log("KẾT QUẢ: crm-app DeleteObject trên crm-exports → AccessDenied")
}

// Không chép ra kho ngoài: crm-filesync không đọc được crm-exports ở kho chính; kho ngoài không có bucket này.
func TestExports_NotSyncedOffsite(t *testing.T) {
	ctx := t.Context()
	onMain := newClient(t, mainEndpoint(), "s3.json", "crm-filesync")
	if _, err := onMain.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucketExports)}); err == nil {
		t.Fatal("crm-filesync liệt kê được crm-exports: tệp xuất (có SĐT) có thể bị chép ra kho ngoài")
	}
	offAdmin := newClient(t, offsiteEndpoint(), "s3-offsite.json", "crm-admin")
	if _, err := offAdmin.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketExports)}); err == nil {
		t.Fatal("kho ngoài giả có bucket crm-exports")
	}
	t.Log("KẾT QUẢ: crm-filesync không đọc crm-exports; kho ngoài không có crm-exports")
}
