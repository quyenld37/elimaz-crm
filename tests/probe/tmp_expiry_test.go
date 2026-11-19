//go:build probe

package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Hạn 24 giờ của crm-tmp (R10, Q18; Task 410, review độc lập M1 C-08). Đo 19/11/2026: `weed server` không chạy
// lifecycle worker, nên quy tắc Expiration Days=1 một mình không xóa gì. Cách xóa thật là lifecycle TTL fast path của
// bucket (bootstrap.sh bật): PutObject gắn ttlSec = 86400 vào mục của filer và ghi dữ liệu vào volume Ttl:1d; filer và
// volume server tự xóa khi hết hạn, không cần tiến trình nào khác. Kết quả ghi ở docs/do-luong/seaweedfs-m1.md.

// seaweedContainer: id container SeaweedFS chính (container mở cổng S3 chính ra máy).
func seaweedContainer(t *testing.T) string {
	t.Helper()
	id, err := exec.CommandContext(t.Context(), "docker", "ps", "-q", "--filter", "publish="+port("CRM_S3_PORT", "8333")).Output()
	if err != nil || len(bytes.TrimSpace(id)) == 0 {
		t.Fatalf("tìm container SeaweedFS chính: %v (id=%q)", err, id)
	}
	return strings.TrimSpace(string(id))
}

// inSeaweed chạy một lệnh sh trong container SeaweedFS chính (cổng nội bộ chỉ tới được từ trong container).
func inSeaweed(t *testing.T, script string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "docker", "exec", seaweedContainer(t), "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec %q: %v\n%s", script, err, out)
	}
	return string(out)
}

// weedShell chạy một lệnh weed shell trong container SeaweedFS chính.
func weedShell(t *testing.T, cmd string) string {
	t.Helper()
	return inSeaweed(t, "echo '"+cmd+"' | weed shell 2>&1")
}

// entryMeta: phần cần dùng của `fs.meta.cat` (mục của filer).
type entryMeta struct {
	Chunks []struct {
		Fid struct {
			VolumeID int `json:"volumeId"`
		} `json:"fid"`
	} `json:"chunks"`
	Attributes struct {
		TTLSec int `json:"ttlSec"`
	} `json:"attributes"`
}

func metaOf(t *testing.T, path string) entryMeta {
	t.Helper()
	out := weedShell(t, "fs.meta.cat "+path)
	var m entryMeta
	// Sau JSON, lệnh in thêm một dòng "chunks …"; chỉ đọc giá trị JSON đầu.
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("fs.meta.cat %s không ra JSON:\n%s", path, out)
	}
	if err := json.NewDecoder(strings.NewReader(out[start:])).Decode(&m); err != nil {
		t.Fatalf("đọc fs.meta.cat %s: %v\n%s", path, err, out)
	}
	return m
}

// volumeTTLs: TTL của mỗi volume theo `volume.list` (rỗng: không hạn), kèm collection (= tên bucket).
func volumeTTLs(t *testing.T) map[int][2]string {
	t.Helper()
	re := regexp.MustCompile(`volume Id:(\d+), .*Collection:([^,]*), .*Ttl:([^,]*),`)
	vols := map[int][2]string{}
	for _, m := range re.FindAllStringSubmatch(weedShell(t, "volume.list"), -1) {
		var id int
		_, _ = fmt.Sscan(m[1], &id)
		vols[id] = [2]string{m[2], m[3]}
	}
	if len(vols) == 0 {
		t.Fatal("volume.list không có volume nào")
	}
	return vols
}

// Fast path phải bật cho crm-tmp; không bật thì đối tượng có ttlSec 0 và nằm lại mãi (đo 19/11).
func TestTmpExpiry_FastPathEnabled(t *testing.T) {
	out := weedShell(t, "s3.bucket.lifecycle.fastpath -name "+bucketTmp)
	if !strings.Contains(out, "fast path: enabled") {
		t.Fatalf("crm-tmp chưa bật lifecycle TTL fast path (bootstrap.sh):\n%s", out)
	}
	t.Log("KẾT QUẢ: crm-tmp bật lifecycle TTL fast path")
}

// presignedPutTmp ghi body vào crm-tmp/key bằng đường dẫn ký sẵn của crm-app, như trình duyệt.
func presignedPutTmp(t *testing.T, key string, body []byte) {
	t.Helper()
	ctx := t.Context()
	req, err := s3.NewPresignClient(appClient(t)).PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketTmp), Key: aws.String(key), ContentLength: aws.Int64(int64(len(body))),
	}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPut, req.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res, err := httpClient().Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT ký sẵn vào crm-tmp: %d", res.StatusCode)
	}
}

// requireChunksIn: mọi chunk của m nằm ở volume của collection bucket có Ttl ttl ("" là không hạn).
func requireChunksIn(t *testing.T, what string, m entryMeta, bucket, ttl string) {
	t.Helper()
	if len(m.Chunks) == 0 {
		t.Fatalf("%s không có chunk (muốn dữ liệu ở volume, không nhúng trong mục)", what)
	}
	vols := volumeTTLs(t)
	for _, c := range m.Chunks {
		if v := vols[c.Fid.VolumeID]; v[0] != bucket || v[1] != ttl {
			t.Fatalf("%s: chunk ở volume %d (collection %q, Ttl %q), muốn collection %s, Ttl %q", what, c.Fid.VolumeID, v[0], v[1], bucket, ttl)
		}
	}
}

// Đường thật của ảnh: trình duyệt PUT bằng đường dẫn ký sẵn của crm-app vào crm-tmp. Đối tượng phải có ttlSec 86400 và
// dữ liệu ở volume Ttl:1d. Ba bucket giữ lâu không có volume nào có hạn.
func TestTmpExpiry_UploadStampedOneDay(t *testing.T) {
	ctx := t.Context()
	key := uniqueKey(t)
	body := randomBytes(t, 2048)
	presignedPutTmp(t, key, body)
	m := metaOf(t, "/buckets/"+bucketTmp+"/"+key)
	if m.Attributes.TTLSec != 86400 {
		t.Fatalf("đối tượng crm-tmp có ttlSec %d, muốn 86400 (không hạn thì ảnh thô nằm lại mãi)", m.Attributes.TTLSec)
	}
	requireChunksIn(t, "đối tượng crm-tmp", m, bucketTmp, "1d")
	// Ảnh gốc được chép từ crm-tmp sang crm-originals bằng CopyObject (files.S3Store.CopyIfAbsent): bản chép phải có
	// chunk mới ở volume không hạn và ttlSec 0, không mang hạn của bản ở crm-tmp.
	if _, err := appClient(t).CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(bucketOriginals), Key: aws.String(key), CopySource: aws.String(bucketTmp + "/" + key),
	}); err != nil {
		t.Fatalf("chép crm-tmp → crm-originals: %v", err)
	}
	verDir := "/buckets/" + bucketOriginals + "/" + key + ".versions"
	fields := strings.Fields(weedShell(t, "fs.ls "+verDir))
	if len(fields) != 1 || !strings.HasPrefix(fields[0], "v_") {
		t.Fatalf("fs.ls %s: muốn đúng một phiên bản, có %q", verDir, fields)
	}
	cp := metaOf(t, verDir+"/"+fields[0])
	if cp.Attributes.TTLSec != 0 {
		t.Fatalf("bản chép ở crm-originals: ttlSec %d, muốn 0", cp.Attributes.TTLSec)
	}
	requireChunksIn(t, "bản chép ở crm-originals", cp, bucketOriginals, "")
	// Đối chứng: ghi vào cả ba bucket giữ lâu rồi soát mọi volume của chúng: không volume nào có hạn.
	for _, b := range versionedBuckets() {
		if _, err := appClient(t).PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(b), Key: aws.String(key), Body: bytes.NewReader(body),
		}); err != nil {
			t.Fatalf("ghi %s: %v", b, err)
		}
	}
	for id, v := range volumeTTLs(t) {
		for _, b := range versionedBuckets() {
			if v[0] == b && v[1] != "" {
				t.Errorf("volume %d của %s có Ttl %q: bucket giữ lâu không được có hạn", id, b, v[1])
			}
		}
	}
	t.Logf("KẾT QUẢ: PUT ký sẵn vào crm-tmp → ttlSec 86400, volume %d Ttl 1d; chép sang crm-originals → ttlSec 0, volume %d "+
		"không hạn; ba bucket giữ lâu không volume nào có hạn", m.Chunks[0].Fid.VolumeID, cp.Chunks[0].Fid.VolumeID)
}

// Cơ chế xóa theo ttlSec của filer bản ghim chạy thật (không đợi được 24 giờ trong test): một mục ttl 1 phút ghi qua
// filer phải biến mất trong 4 phút, không ai gọi xóa.
func TestTmpExpiry_TTLEntryDisappears(t *testing.T) {
	dir := fmt.Sprintf("/probe-ttl-%d", time.Now().UnixNano())
	up := inSeaweed(t, "echo probe > /tmp/ttl-probe.txt && curl -s -F file=@/tmp/ttl-probe.txt 'http://127.0.0.1:8888"+dir+"/?ttl=1m'")
	if !strings.Contains(up, `"name":"ttl-probe.txt"`) {
		t.Fatalf("ghi mục ttl 1m qua filer: %s", up)
	}
	get := "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8888" + dir + "/ttl-probe.txt"
	if code := inSeaweed(t, get); code != "200" {
		t.Fatalf("đọc ngay sau khi ghi: %s, muốn 200", code)
	}
	if m := metaOf(t, dir+"/ttl-probe.txt"); m.Attributes.TTLSec != 60 {
		t.Fatalf("mục ttl 1m có ttlSec %d, muốn 60", m.Attributes.TTLSec)
	}
	start := time.Now()
	for time.Since(start) < 4*time.Minute {
		time.Sleep(10 * time.Second)
		if code := inSeaweed(t, get); code == "404" {
			t.Logf("KẾT QUẢ: mục ttlSec 60 biến mất sau %s, không ai gọi xóa", time.Since(start).Round(time.Second))
			return
		}
	}
	t.Fatal("mục ttlSec 60 vẫn còn sau 4 phút: cơ chế hạn của filer không chạy")
}
