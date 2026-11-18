//go:build drill

package drills

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/files"
)

const (
	crashUploads  = 200 // số lượt tải lên song song
	crashParallel = 20  // số lượt chạy cùng lúc
	crashKillAt   = 100 // giết kho khi đã có chừng này lượt complete trả 200
	crashBodySize = 256 << 10
)

// attempt là một lượt tải lên: nội dung, mã tệp, complete có trả 200 không, lỗi của bước hỏng.
type attempt struct {
	sha  string
	body []byte
	id   uuid.UUID
	ok   bool
	err  error
}

// TestFileStoreCrash (TP08): 200 lượt tải lên song song qua đường thật (createUpload → PUT thẳng lên kho → complete);
// khi đã có 100 lượt complete trả 200 thì `docker kill -s KILL` SeaweedFS rồi khởi động lại. Mọi tệp mà complete đã
// trả 200 đọc ra đúng sha256 đã tính phía client; lượt lỗi giữa chừng thì client nhận lỗi, tải lại bằng createUpload
// mới thì được.
func TestFileStoreCrash(t *testing.T) {
	d := newDrill(t)
	ctx := t.Context()
	runs := make([]attempt, crashUploads)
	for i := range runs {
		runs[i].body = make([]byte, crashBodySize)
		_, _ = rand.Read(runs[i].body)
		runs[i].sha = shaHex(runs[i].body)
	}
	var (
		mu       sync.Mutex
		done     int
		killOnce sync.Once
		killed   = make(chan struct{})
		killedAt time.Time
	)
	kill := func() {
		id := strings.TrimSpace(d.compose(30*time.Second, "ps", "-q", "seaweedfs"))
		if id == "" {
			t.Error("không tìm thấy container seaweedfs của dự án diễn tập")
			close(killed)
			return
		}
		d.docker(30*time.Second, "kill", "-s", "KILL", id)
		killedAt = time.Now()
		close(killed)
	}
	work := make(chan int)
	var wg sync.WaitGroup
	for range crashParallel {
		wg.Go(func() {
			for i := range work {
				id, err := d.upload(ctx, runs[i].body)
				mu.Lock()
				runs[i].id, runs[i].ok, runs[i].err = id, err == nil, err
				if err == nil {
					done++
				}
				reached := done >= crashKillAt
				mu.Unlock()
				if reached {
					killOnce.Do(kill)
				}
			}
		})
	}
	for i := range runs {
		work <- i
	}
	close(work)
	wg.Wait()
	select {
	case <-killed:
	default:
		t.Fatalf("chưa tới %d lượt complete 200 (%d): không giết kho được", crashKillAt, done)
	}
	d.compose(3*time.Minute, "up", "-d", "--wait", "--no-deps", "seaweedfs")
	d.waitStore()
	t.Logf("kho dậy lại sau %.1f s", time.Since(killedAt).Seconds())

	completed, failed, retried := 0, 0, 0
	warm := time.Now()
	for _, r := range runs {
		if !r.ok {
			failed++
			continue
		}
		completed++
		if got, err := d.sha(files.BucketTmp, files.TmpKey(d.tenant, r.id)); err != nil || got != r.sha {
			t.Errorf("tệp %s đã báo xong mà đọc lại sai: sha %s, muốn %s (%v)", r.id, got, r.sha, err)
		}
	}
	t.Logf("đọc lại %d tệp đã báo xong mất %.1f s (gồm lúc kho nạp lại volume)", completed, time.Since(warm).Seconds())
	if failed == 0 {
		t.Log("không lượt nào lỗi giữa chừng (kho dậy kịp trước mọi lượt còn lại)")
	}
	for _, r := range runs {
		if r.ok {
			continue
		}
		id, err := d.upload(context.WithoutCancel(ctx), r.body)
		if err != nil {
			t.Errorf("tải lại lượt lỗi (%v) bằng createUpload mới: %v", r.err, err)
			continue
		}
		if got, err := d.sha(files.BucketTmp, files.TmpKey(d.tenant, id)); err != nil || got != r.sha {
			t.Errorf("tệp tải lại %s đọc ra sai: %s (%v)", id, got, err)
			continue
		}
		retried++
	}
	if t.Failed() {
		return
	}
	fmt.Printf("FILES CRASH OK uploads=%d completed=%d failed_midway=%d retried_ok=%d\n", crashUploads, completed, failed, retried)
}
