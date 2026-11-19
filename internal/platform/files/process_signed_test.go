package files_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// L3 của threat model M1 (Task 414): đường dẫn ký sẵn (SourceURL, PutURL gửi imager) không lọt vào log, dấu vết hay
// cột kết quả, lỗi của lệnh — ở ảnh tốt, ảnh imager từ chối, imager lỗi 500 (thử lại). Kiểm bằng chính chữ ký của
// từng đường dẫn imager nhận (MemStore: tham số sig; S3: X-Amz-Signature — cùng vai trò).
func TestFileProcess_SignedURLsNotLogged(t *testing.T) {
	spans := testkit.CaptureSpans(t) // trước khi dựng worker: worker lấy tracer lúc dựng
	p := newProcEnv(t)
	logger, logs := testkit.CaptureLog(t)
	p.w = queue.NewWorker(p.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond, Logger: logger,
		Commands: commands.NewRunner(p.staff.App, []commands.Handler{p.pub, p.priv, p.regen, p.ver}),
	})
	p.upload(p.a, "product_image", []byte("anh-tot-khong-lo-chu-ky"))
	p.upload(p.a, "product_image", []byte(rejPrefix+"not_image\n<html>"))
	p.upload(p.a, "product_image", []byte(e500Prefix+" loi tam"))
	p.drain()

	sigs := signaturesOf(t, p.img.requests())
	if len(sigs) < 3 {
		t.Fatalf("imager phải nhận đường dẫn ký sẵn của cả ba ảnh, có %d chữ ký", len(sigs))
	}
	var jobs strings.Builder
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		var s string
		err := tx.QueryRow(t.Context(), `SELECT coalesce(string_agg(coalesce(result::text, '') || ' ' ||
			coalesce(last_error, ''), ' '), '') FROM platform.jobs`).Scan(&s)
		jobs.WriteString(s)
		return true, err //nolint:wrapcheck // test
	})
	text := logs.String() + testkit.SpanText(spans.GetSpans().Snapshots()) + jobs.String()
	if !strings.Contains(text, "not_image") {
		t.Fatalf("log, span, lệnh phải có kết quả của ảnh bị từ chối (kiểm chỗ quét có dữ liệu): %s", text)
	}
	for _, s := range sigs {
		if strings.Contains(text, s) {
			t.Errorf("log, span hoặc lệnh chứa chữ ký đường dẫn ký sẵn %q", s)
		}
	}
	if strings.Contains(text, "X-Amz-Signature") {
		t.Error("log, span hoặc lệnh chứa X-Amz-Signature")
	}
}

// signaturesOf trả chữ ký (MemStore: sig; S3: X-Amz-Signature) của mọi đường dẫn ký sẵn imager đã nhận.
func signaturesOf(t *testing.T, reqs []imager.Request) []string {
	t.Helper()
	var sigs []string
	for _, r := range reqs {
		urls := []string{r.SourceURL}
		for _, o := range r.Outputs {
			urls = append(urls, o.PutURL)
		}
		for _, raw := range urls {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range []string{"sig", "X-Amz-Signature"} {
				if v := u.Query().Get(k); v != "" {
					sigs = append(sigs, v)
				}
			}
		}
	}
	return sigs
}
