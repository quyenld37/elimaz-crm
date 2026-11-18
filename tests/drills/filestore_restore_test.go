//go:build drill

package drills

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
)

const (
	restoreBefore = 20 // ảnh tải lên trước lần chép cuối
	restoreAfter  = 2  // ảnh tải lên sau lần chép cuối (mất khi kho hỏng, TP09)
)

// Khóa dev công khai của crm-admin (docker/seaweedfs/s3.json): danh tính có quyền ghi kho chính khi chép ngược.
const (
	devAdminKey    = "crm-admin-dev"
	devAdminSecret = "crm-admin-dev-only-not-a-secret" //nolint:gosec // khóa dev công khai
)

// filesync chạy container filesync của dự án diễn tập (một lần, --rm), ghi vào database thử của bài diễn tập.
func (d *drill) filesync(timeout time.Duration, args ...string) string {
	d.t.Helper()
	var dbName string
	if err := d.staff.Owner.Pool.QueryRow(d.t.Context(), "SELECT current_database()").Scan(&dbName); err != nil {
		d.t.Fatal(err)
	}
	// Script lấy từ mã đang có (gắn chỉ đọc), không dựng lại image crm-filesync:dev (thẻ image dùng chung giữa các dự
	// án compose trên máy).
	full := append([]string{
		"--profile", "filesync", "run", "--rm", "-T",
		"-v", filepath.Join(repoDir(), "scripts", "filesync") + ":/opt/filesync:ro",
		"-e", "FILESYNC_DATABASE_URL=postgres://crm_filesync:crm_filesync_dev@postgres:5432/" + dbName + "?sslmode=disable",
	}, args...)
	return d.compose(timeout, full...)
}

// uploadReady tải n ảnh lên qua đường thật rồi chạy worker; đòi mọi ảnh ready.
func (d *drill) uploadReady(n int) []uuid.UUID {
	d.t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		body := make([]byte, 64<<10)
		_, _ = rand.Read(body)
		id, err := d.upload(d.t.Context(), body)
		if err != nil {
			d.t.Fatalf("tải ảnh %d: %v", i, err)
		}
		ids[i] = id
	}
	d.drain()
	for _, id := range ids {
		if st := d.status(id); st != files.StatusReady {
			d.t.Fatalf("ảnh %s: %s, muốn ready", id, st)
		}
	}
	return ids
}

func (d *drill) status(id uuid.UUID) string {
	d.t.Helper()
	var st string
	d.staff.Exec(d.t, d.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(d.t.Context(), "SELECT status FROM platform.files WHERE id = $1", id).Scan(&st)
	})
	return st
}

// storeKiB là dung lượng thư mục dữ liệu của SeaweedFS chính (du -sk /data trong container): dung lượng kho lúc
// diễn tập (mọi bucket, mọi phiên bản, kể cả siêu dữ liệu).
func (d *drill) storeKiB() int64 {
	d.t.Helper()
	out := strings.Fields(d.compose(time.Minute, "exec", "-T", "seaweedfs", "du", "-sk", "/data"))
	if len(out) == 0 {
		d.t.Fatal("du không trả gì")
	}
	n, err := strconv.ParseInt(out[0], 10, 64)
	if err != nil {
		d.t.Fatalf("đọc du: %v", err)
	}
	return n
}

// verifyAll gọi POST /api/v1/platform/files/verify rồi chạy worker tới khi mọi lệnh của lượt đó done.
func (d *drill) verifyAll() {
	d.t.Helper()
	code, raw, err := d.call(d.t.Context(), http.MethodPost, "/api/v1/platform/files/verify", "")
	var acc struct {
		JobID uuid.UUID `json:"jobId"`
	}
	if err != nil || code != http.StatusAccepted || json.Unmarshal(raw, &acc) != nil {
		d.t.Fatalf("verifyFiles: %d %v %s", code, err, raw)
	}
	d.drain()
	if n := d.staff.Count(d.t, `SELECT count(*) FROM platform.jobs WHERE kind = 'file.verify' AND status <> 'done'
		AND payload->>'runId' = (SELECT payload->>'runId' FROM platform.jobs WHERE id = $1)`, acc.JobID); n != 0 {
		d.t.Fatalf("lượt đối chiếu còn %d lệnh chưa done", n)
	}
}

// TestFileStoreRestore (TP09, TP12): chép một lượt sang kho ngoài giả; tải thêm 2 ảnh sau lần chép cuối; bấm giờ:
// dừng SeaweedFS chính, xóa volume, khởi động rỗng, dựng bucket (bootstrap.sh), chép ngược từ kho ngoài (giải mã
// crm-originals, crm-private); rồi verifyFiles và chờ xong. Đúng 2 ảnh tải sau lần chép cuối thành lost, mọi ảnh còn
// lại đọc được ảnh gốc và mọi biến thể. In `FILES RESTORE OK restore_seconds=<x> lost=<n>`, ghi dòng
// file_sync_runs(kind = 'restore_drill').
func TestFileStoreRestore(t *testing.T) {
	d := newDrill(t)
	before := d.uploadReady(restoreBefore)
	d.filesync(10*time.Minute, "filesync", "--once")
	after := d.uploadReady(restoreAfter)
	storeKiB := d.storeKiB()

	start := time.Now()
	d.compose(2*time.Minute, "stop", "seaweedfs")
	d.compose(2*time.Minute, "rm", "-f", "seaweedfs")
	d.docker(2*time.Minute, "volume", "rm", d.project+"_seaweed")
	d.compose(3*time.Minute, "up", "-d", "--wait", "--no-deps", "seaweedfs")
	d.compose(3*time.Minute, "up", "-d", "--wait", "--no-deps", "--force-recreate", "s3-bootstrap")
	d.filesync(20*time.Minute,
		"-e", "FILESYNC_SRC_ACCESS_KEY_ID="+devAdminKey, "-e", "FILESYNC_SRC_SECRET_ACCESS_KEY="+devAdminSecret,
		"--entrypoint", "/opt/filesync/restore.sh", "filesync")
	restoreSeconds := time.Since(start).Seconds()
	d.waitStore()

	verifyStart := time.Now()
	d.verifyAll()
	verifySeconds := time.Since(verifyStart).Seconds()

	lost := 0
	for _, id := range after {
		if st := d.status(id); st != files.StatusLost {
			t.Errorf("ảnh tải sau lần chép cuối %s: %s, muốn lost", id, st)
		}
	}
	d.staff.Exec(t, d.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM platform.files WHERE status = 'lost'").Scan(&lost)
	})
	for _, id := range before {
		if st := d.status(id); st != files.StatusReady {
			t.Errorf("ảnh đã chép %s: %s, muốn ready", id, st)
		}
		d.checkReadable(id)
	}
	if lost != restoreAfter {
		t.Errorf("lost = %d, muốn %d", lost, restoreAfter)
	}
	detail := fmt.Sprintf("restore_seconds=%.1f verify_seconds=%.1f lost=%d", restoreSeconds, verifySeconds, lost)
	if _, err := d.staff.Owner.Pool.Exec(t.Context(), `INSERT INTO platform.file_sync_runs
		(kind, bucket, ok, started_at, finished_at, objects, bytes, detail) VALUES ('restore_drill', 'all', $1, $2, $3, $4, $5, $6)`,
		!t.Failed(), start, start.Add(time.Duration(restoreSeconds*float64(time.Second))), restoreBefore+restoreAfter, storeKiB*1024, detail); err != nil {
		t.Fatal(err)
	}
	if n := d.staff.Count(t, "SELECT count(*) FROM platform.file_sync_runs WHERE kind = 'restore_drill'"); n != 1 {
		t.Fatalf("có %d dòng restore_drill", n)
	}
	if t.Failed() {
		return
	}
	fmt.Printf("FILES RESTORE OK restore_seconds=%.1f lost=%d verify_seconds=%.1f files=%d store_kib=%d\n",
		restoreSeconds, lost, verifySeconds, restoreBefore+restoreAfter, storeKiB)
}

// checkReadable: ảnh gốc và mọi biến thể của tệp đọc được trên kho đã dựng lại (ảnh hiển thị được).
func (d *drill) checkReadable(id uuid.UUID) {
	d.t.Helper()
	var sha []byte
	var keys []string
	d.staff.Exec(d.t, d.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(d.t.Context(), "SELECT content_sha256 FROM platform.files WHERE id = $1", id).Scan(&sha); err != nil {
			return err //nolint:wrapcheck // test
		}
		rows, err := tx.Query(d.t.Context(), "SELECT key FROM platform.file_variants WHERE file_id = $1", id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		keys, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	want := fmt.Sprintf("%x", sha)
	if got, err := d.sha(files.BucketOriginals, files.OriginalKey(want)); err != nil || got != want {
		d.t.Errorf("ảnh gốc của %s sau khi dựng lại: %s (%v)", id, got, err)
	}
	for _, k := range keys {
		if _, err := d.sha(files.BucketPublic, k); err != nil {
			d.t.Errorf("biến thể %s của %s không đọc được: %v", k, id, err)
		}
	}
	if len(keys) != 12 {
		d.t.Errorf("tệp %s có %d biến thể, muốn 12", id, len(keys))
	}
}
