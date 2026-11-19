package files_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Task 409 (C-02, C-07; quyết định 19/11): lỗi hạ tầng tạm không tính lượt, thử lại theo lùi dần tới trần thời gian;
// chỉ lỗi do chính tệp mới thành rejected; tệp ở processing trong lúc chờ.

// rounds chạy worker rồi đưa việc về hạn n vòng (nhiều hơn 3 lượt cũ).
func (p *procEnv) rounds(n int) {
	p.t.Helper()
	for range n {
		p.drain()
		p.makeDue()
	}
}

// assertWaiting: lệnh kind của tệp id chờ thử lại không tính lượt (pending, 0 lượt), tệp processing chưa có lý do.
func (p *procEnv) assertWaiting(kind string, id uuid.UUID) jobState {
	p.t.Helper()
	j := p.job(kind, id)
	if j.Status != "pending" || j.Attempts != 0 {
		p.t.Fatalf("lệnh: %+v", j)
	}
	if r := p.fileRow(id); r.Status != files.StatusProcessing || r.Reason != "" {
		p.t.Fatalf("tệp: %+v", r)
	}
	return j
}

// backdate lùi mốc tính trần: updated_at của tệp (đang processing) và created_at của lệnh, cùng d.
func (p *procEnv) backdate(kind string, id uuid.UUID, d time.Duration) {
	p.t.Helper()
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(p.t.Context(), "UPDATE platform.files SET updated_at = now() - make_interval(secs => $2) WHERE id = $1",
			id, d.Seconds())
		return tag.RowsAffected() == 1, err //nolint:wrapcheck // test
	})
	if _, err := p.staff.Owner.Pool.Exec(p.t.Context(),
		"UPDATE platform.jobs SET created_at = now() - make_interval(secs => $3) WHERE kind = $1 AND dedupe_key = $2",
		kind, id.String(), d.Seconds()); err != nil {
		p.t.Fatal(err)
	}
}

// runIn trả số giây tới lần chạy kế của lệnh kind của tệp id.
func (p *procEnv) runIn(kind string, id uuid.UUID) float64 {
	p.t.Helper()
	var s float64
	if err := p.staff.Owner.Pool.QueryRow(p.t.Context(),
		"SELECT extract(epoch FROM run_at - now()) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2", kind, id.String()).
		Scan(&s); err != nil {
		p.t.Fatal(err)
	}
	return s
}

// retryJob chạy lại lệnh như POST /api/v1/platform/jobs/{id}/retry (queue.RetryJob).
func (p *procEnv) retryJob(tenant uuid.UUID, kind string, id uuid.UUID) {
	p.t.Helper()
	j := p.queueJob(kind, id)
	p.must(tenant, func(tx pgx.Tx) error {
		_, _, err := queue.RetryJob(p.t.Context(), tx, tenant, j.ID)
		return err //nolint:wrapcheck // test
	})
}

// Mỗi loại lỗi hạ tầng: sau 5 vòng (quá 3 lượt cũ) lệnh vẫn pending, 0 lượt, tệp processing (không bao giờ rejected
// trước trần), last_error mang đúng lý do.
func TestFileProcess_InfraErrorsNotCounted(t *testing.T) {
	short := func(p *procEnv) {
		files.SetProcessTimeout(p.pub, 500*time.Millisecond)
		p.w = p.newWorker()
	}
	for _, c := range []struct {
		name, body, label string
		setup             func(*procEnv)
	}{
		{"upload_failed", rejPrefix + "upload_failed\n", "upload_failed", nil},
		{"source_failed", rejPrefix + "source_failed\n", "source_failed", nil},
		{"internal", rejPrefix + "internal\n", "internal", nil},
		{"imager-timeout", rejPrefix + "timeout\n", "timeout", nil},
		{"http-500", e500Prefix, "imager_unreachable", nil},
		{"busy", busyPrefix, "busy", short},
		{"hang", hangPrefix, "timeout", short},
		{"unreachable", "anh-tot-imager-tat", "imager_unreachable", func(p *procEnv) { p.img.down.Store(true) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newProcEnv(t)
			if c.setup != nil {
				c.setup(p)
			}
			id := p.upload(p.a, "product_image", []byte(c.body))
			p.rounds(5)
			if j := p.assertWaiting(kindPublic, id); !strings.Contains(j.LastError, c.label) {
				t.Fatalf("lệnh: %+v", j)
			}
			if m := metricRows(p.reg); m["image_rejected|processing_failed"] != 0 {
				t.Fatalf("chỉ số: %v", m)
			}
		})
	}
	t.Run("private", func(t *testing.T) {
		p := newProcEnv(t)
		id := p.registerPrivate(p.a, []byte(rejPrefix+"internal\n"))
		p.rounds(5)
		p.assertWaiting(kindPrivate, id)
	})
}

// Lỗi do chính tệp: rejected ngay với đúng lý do, lệnh dead sau 1 lượt, imager gọi 1 lần. Lý do lạ → processing_failed.
func TestFileProcess_FileErrorsRejectAtOnce(t *testing.T) {
	for _, c := range []struct{ reason, want string }{
		{"not_image", "not_image"},
		{"type_not_allowed", "type_not_allowed"},
		{"too_large", "too_large"},
		{"too_many_pixels", "too_many_pixels"},
		{"decode_failed", "decode_failed"},
		{"ly_do_la", "processing_failed"},
	} {
		t.Run(c.reason, func(t *testing.T) {
			p := newProcEnv(t)
			id := p.upload(p.a, "product_image", []byte(rejPrefix+c.reason+"\n"))
			p.rounds(2)
			if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != c.want {
				t.Fatalf("tệp: %+v", r)
			}
			if j := p.job(kindPublic, id); j.Status != "dead" || j.Attempts != 1 {
				t.Fatalf("lệnh: %+v", j)
			}
			if p.img.count() != 1 {
				t.Fatalf("imager gọi %d lần", p.img.count())
			}
		})
	}
	t.Run("source_missing", func(t *testing.T) {
		p := newProcEnv(t)
		id := p.upload(p.a, "product_image", []byte("anh-bi-xoa-khoi-kho"))
		info := p.fileRow(id)
		if err := p.mem.Delete(t.Context(), files.Bucket(info.Bucket), info.Key); err != nil {
			t.Fatal(err)
		}
		p.rounds(2)
		if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "source_missing" {
			t.Fatalf("tệp: %+v", r)
		}
	})
}

// Imager ngừng lâu (nhiều hơn 3 vòng thử, như deploy lại hay OOM rồi khởi động lại) rồi chạy lại → mọi ảnh ready ở lần
// đầu imager trả lời, không ảnh nào rejected (phát hiện C-02).
func TestFileProcess_ImagerDownThenUp(t *testing.T) {
	p := newProcEnv(t)
	p.img.down.Store(true)
	var ids []uuid.UUID
	for i := range 3 {
		ids = append(ids, p.upload(p.a, "product_image", []byte{'t', 'a', 't', byte('0' + i)}))
	}
	p.rounds(6)
	for _, id := range ids {
		if j, r := p.job(kindPublic, id), p.fileRow(id); j.Status != "pending" || j.Attempts != 0 || r.Status != files.StatusProcessing {
			t.Fatalf("lúc imager tắt: lệnh %+v, tệp %+v", j, r)
		}
	}
	p.img.down.Store(false)
	p.drain()
	for _, id := range ids {
		if j, r := p.job(kindPublic, id), p.fileRow(id); j.Status != "done" || j.Attempts != 1 || r.Status != files.StatusReady {
			t.Fatalf("sau khi imager chạy lại: lệnh %+v, tệp %+v", j, r)
		}
	}
	if p.img.count() != 3 {
		t.Fatalf("imager xử lý %d lần, muốn 3", p.img.count())
	}
}

// Trần thời gian (RetryMax): tính từ lúc tệp vào processing, không từ lúc ghi lệnh (worker ngừng 2 ngày không làm
// ảnh bị từ chối ngay); lùi dần theo tuổi, lần thử cuối đúng lúc hết trần; hết trần → rejected processing_failed, lệnh
// dead (luật cảnh báo dead:file.process), đếm image_rejected.
func TestFileProcess_RetryCapRejects(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = time.Hour })
	p.img.down.Store(true)
	id := p.upload(p.a, "product_image", []byte("anh-cho-tran"))
	if _, err := p.staff.Owner.Pool.Exec(t.Context(),
		"UPDATE platform.jobs SET created_at = now() - interval '48 hours' WHERE dedupe_key = $1", id.String()); err != nil {
		t.Fatal(err)
	}
	p.drain()
	if j, r := p.job(kindPublic, id), p.fileRow(id); j.Status != "pending" || r.Status != files.StatusProcessing {
		t.Fatalf("lệnh cũ 48 giờ, lần chạy đầu: lệnh %+v, tệp %+v", j, r)
	}
	if s := p.runIn(kindPublic, id); s < 3.9 || s > 6.1 {
		t.Fatalf("lần lỗi đầu: muốn chờ 5 s ±20%%, còn %.1f s", s)
	}
	p.backdate(kindPublic, id, 20*time.Minute)
	p.makeDue()
	p.drain()
	if s := p.runIn(kindPublic, id); s < 16*60-1 || s > 24*60+1 {
		t.Fatalf("đã chờ 20 phút: muốn chờ 20 phút ±20%%, còn %.0f s", s)
	}
	p.backdate(kindPublic, id, 59*time.Minute)
	p.makeDue()
	p.drain()
	if s := p.runIn(kindPublic, id); s < 50 || s > 61 {
		t.Fatalf("còn 1 phút tới trần: muốn chờ ≤ 60 s, còn %.0f s", s)
	}
	if r := p.fileRow(id); r.Status != files.StatusProcessing {
		t.Fatalf("trước trần: %+v", r)
	}
	p.backdate(kindPublic, id, 61*time.Minute)
	p.makeDue()
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
		t.Fatalf("hết trần: %+v", r)
	}
	if j := p.job(kindPublic, id); j.Status != "dead" || j.Attempts != 1 || !strings.Contains(j.LastError, "processing_failed") {
		t.Fatalf("lệnh hết trần: %+v", j)
	}
	if m := metricRows(p.reg); m["image_rejected|processing_failed"] != 1 {
		t.Fatalf("chỉ số: %v", m)
	}
}

// Hết trần mà không ghi được rejected (database lỗi mãi): lần thử lại tính lượt (không KeepAttempt) để 3 lượt chặn vòng
// lặp, lệnh không chờ mãi mà không bao giờ dead (phát hiện của reviewer).
func TestFileProcess_RetryCapCountsWhenRejectFails(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = time.Hour })
	p.img.down.Store(true)
	id := p.upload(p.a, "product_image", []byte("anh-khong-ghi-duoc"))
	p.drain()
	p.assertWaiting(kindPublic, id)
	p.backdate(kindPublic, id, 2*time.Hour)
	p.bus.fail.Store(true) // markRejected phát báo hiệu trong transaction: lỗi → lùi lại
	for want := 1; want <= 2; want++ {
		p.makeDue()
		p.drain()
		if j := p.job(kindPublic, id); j.Status != "pending" || j.Attempts != want {
			t.Fatalf("lần %d quá trần, ghi rejected lỗi: %+v", want, j)
		}
		if r := p.fileRow(id); r.Status != files.StatusProcessing {
			t.Fatalf("tệp: %+v", r)
		}
	}
	p.bus.fail.Store(false)
	p.makeDue()
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
		t.Fatalf("database hồi lại: %+v", r)
	}
}

// Transaction ghi processing lùi lại (báo hiệu hay commit lỗi): mốc trần không lấy updated_at chưa commit — lệnh đã quá
// trần thì lần thử tính lượt, không được đặt lại trần mỗi lần (phát hiện của reviewer lượt 2).
func TestFileProcess_RetryCapNotResetByRolledBackProcessing(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = time.Hour })
	id := p.upload(p.a, "product_image", []byte("anh-ghi-processing-loi"))
	p.backdate(kindPublic, id, 2*time.Hour) // tệp uploaded, lệnh ghi 2 giờ trước
	p.bus.fail.Store(true)
	for want := 1; want <= 2; want++ {
		p.makeDue()
		p.drain()
		if j := p.job(kindPublic, id); j.Status != "pending" || j.Attempts != want {
			t.Fatalf("lần %d, ghi processing lỗi: %+v", want, j)
		}
		if r := p.fileRow(id); r.Status != files.StatusUploaded {
			t.Fatalf("tệp: %+v", r)
		}
	}
}

// Chạy lại lệnh dead do hết trần (người có quyền, spec 04 mục 5.3) cứu được ảnh khi imager đã chạy lại; lệnh dead do
// nội dung ảnh chạy lại vẫn rejected, không gọi imager lần hai.
func TestFileProcess_RetryDeadRescues(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = time.Hour })
	p.img.down.Store(true)
	id := p.upload(p.a, "product_image", []byte("anh-duoc-cuu"))
	p.drain()
	p.backdate(kindPublic, id, 2*time.Hour)
	p.makeDue()
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
		t.Fatalf("hết trần: %+v", r)
	}
	p.img.down.Store(false)
	p.retryJob(p.a, kindPublic, id)
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusReady || r.Reason != "" || len(p.variants(id)) != 12 {
		t.Fatalf("sau chạy lại: %+v, %d biến thể", r, len(p.variants(id)))
	}
	if j := p.job(kindPublic, id); j.Status != "done" {
		t.Fatalf("lệnh: %+v", j)
	}

	bad := p.upload(p.a, "product_image", []byte(rejPrefix+"decode_failed\n"))
	p.drain()
	calls := p.img.count()
	p.retryJob(p.a, kindPublic, bad)
	p.drain()
	if r := p.fileRow(bad); r.Status != files.StatusRejected || r.Reason != "decode_failed" {
		t.Fatalf("ảnh hỏng sau chạy lại: %+v", r)
	}
	if p.img.count() != calls {
		t.Fatalf("ảnh hỏng chạy lại vẫn gọi imager: %d → %d", calls, p.img.count())
	}
}

// Trạng thái processing được ghi thật (C-07): trong lúc imager xử lý tệp ở processing; mỗi lần đổi trạng thái có một
// báo hiệu (processing, rồi ready).
func TestFileProcess_WritesProcessing(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "product_image", []byte("anh-dang-xu-ly"))
	if r := p.fileRow(id); r.Status != files.StatusUploaded {
		t.Fatalf("trước khi xử lý: %+v", r)
	}
	seen := make(chan string, 4)
	p.img.mu.Lock()
	p.img.onCall = func() {
		var st string
		err := pgx.BeginFunc(t.Context(), p.staff.Owner.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(t.Context(), "SELECT set_config('app.tenant_id', $1, true)", p.a.String()); err != nil {
				return err //nolint:wrapcheck // test
			}
			return tx.QueryRow(t.Context(), "SELECT status FROM platform.files WHERE id = $1", id).Scan(&st)
		})
		if err != nil {
			st = "lỗi: " + err.Error()
		}
		seen <- st
	}
	p.img.mu.Unlock()
	p.drain()
	if got := <-seen; got != files.StatusProcessing {
		t.Fatalf("trong lúc imager xử lý tệp ở %q, muốn processing", got)
	}
	if r := p.fileRow(id); r.Status != files.StatusReady {
		t.Fatalf("sau khi xử lý: %+v", r)
	}
	if n := len(p.bus.signals()); n != 2 {
		t.Fatalf("muốn 2 báo hiệu (processing, ready), có %d", n)
	}
}

// retryDelay: lùi theo tuổi (5 s → tối đa 1 giờ, ±20%), không dưới gợi ý của bên ngoài, không quá phần còn lại tới
// trần, tối thiểu 1 s.
func TestRetryDelay(t *testing.T) {
	const capD = 24 * time.Hour
	for _, c := range []struct {
		name          string
		elapsed, hint time.Duration
		rnd           float64
		lo, hi        time.Duration
	}{
		{"dau-thap", 0, 0, 0, 4 * time.Second, 4 * time.Second},
		{"dau-cao", 0, 0, 1, 6 * time.Second, 6 * time.Second},
		{"20-phut", 20 * time.Minute, 0, 0.5, 20 * time.Minute, 20 * time.Minute},
		{"tran-1-gio", 5 * time.Hour, 0, 1, 72 * time.Minute, 72 * time.Minute},
		{"goi-y", 0, 7 * time.Second, 0, 7 * time.Second, 7 * time.Second},
		{"gan-tran", capD - 30*time.Second, 0, 1, 30 * time.Second, 30 * time.Second},
		{"qua-tran", capD + time.Minute, 0, 0, time.Second, time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := files.RetryDelay(c.elapsed, c.hint, capD, func() float64 { return c.rnd })
			if got < c.lo || got > c.hi {
				t.Fatalf("muốn [%v, %v], có %v", c.lo, c.hi, got)
			}
		})
	}
}

// Task 426: với trần mặc định (RetryMax 0 → DefaultRetryMax), ảnh lỗi hạ tầng mãi hết trần — rejected
// processing_failed — trước khi bản gốc ở crm-tmp tự xóa (config.TmpObjectTTL). Ở 30 phút trước hạn sống, nguồn còn
// trên kho: lượt đó phải là lượt cuối theo trần, không phải chờ tiếp tới lúc nguồn mất rồi ra source_missing.
func TestFileProcess_DefaultCapBeforeTmpExpiry(t *testing.T) {
	p := newProcEnv(t)
	p.img.down.Store(true)
	id := p.upload(p.a, "product_image", []byte("anh-gio-cuoi"))
	p.drain()
	p.assertWaiting(kindPublic, id)
	p.backdate(kindPublic, id, config.TmpObjectTTL-30*time.Minute)
	p.makeDue()
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
		t.Fatalf("30 phút trước khi crm-tmp xóa nguồn: muốn rejected processing_failed, có %+v", r)
	}
	if j := p.job(kindPublic, id); j.Status != "dead" {
		t.Fatalf("lệnh: %+v", j)
	}
	if files.DefaultRetryMax >= config.TmpObjectTTL {
		t.Fatalf("trần mặc định %v không nhỏ hơn hạn crm-tmp %v", files.DefaultRetryMax, config.TmpObjectTTL)
	}
}
